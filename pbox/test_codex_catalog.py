"""Small offline regressions; no account credentials or model endpoints are used."""
import copy
import ctypes
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import struct
import sys
import unittest
from unittest.mock import patch

loader = importlib.machinery.SourceFileLoader("catalog", str(Path(__file__).with_name("cliproxyapi-codex-catalog")))
spec = importlib.util.spec_from_loader(loader.name, loader)
catalog = importlib.util.module_from_spec(spec)
loader.exec_module(catalog)


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.auth_dir = self.root / "auth"
        self.auth_dir.mkdir()
        self.a = self.auth_dir / "account-a.json"  # filenames need not start with codex-
        self.b = self.auth_dir / "account-b.json"
        for f in (self.a, self.b):
            f.write_text(json.dumps({"type": "codex", "access_token": f.stem, "excluded_models": ["manual-*"]}))
            f.chmod(0o600)
        self.config = self.root / "codex.yaml"
        self.config.write_text(f'config-version: 8\nmodels:\n  devin-catalog: embed\ncredentials:\n  auth-dir: "{self.auth_dir}"\n')
        self.config.chmod(0o600)
        self.claude_config = self.root / "claude" / "config.yaml"
        self.claude_config.parent.mkdir()
        self.claude_config.write_text("config-version: 8\nport: 8318\n")
        self.claude_config.chmod(0o600)
        self.cache = self.root / "claudeAgent.json"
        self.cache.write_text(json.dumps({"models": [{"slug": "claude-sonnet-6-0", "name": "Claude Sonnet 6"}]}))
        self.out = self.root / "catalogs"
        self.t3 = self.root / "t3"
        self.calls = self.root / "refresh-calls"
        self.t3.write_text("#!/usr/bin/python3\nimport sys\nfrom pathlib import Path\n"
                           "if '--help' in sys.argv: print('SUBCOMMANDS\\n  refresh  Refresh providers')\n"
                           f"else: Path({str(self.calls)!r}).open('a').write('refresh\\n')\n")
        self.t3.chmod(0o755)
        attrs = dict(CONFIG=self.config, CLAUDE_CONFIG=self.claude_config, OUT=self.out,
                     STATE=self.out / "state.json", CLAUDE_CACHE=self.cache,
                     ALERTS=self.root / "alerts", T3=self.t3, DRY=False)
        p = patch.multiple(catalog, **attrs)
        p.start()
        self.addCleanup(p.stop)
        self.old = {"id": "gpt-6.1-sol", "description": "template", "context_length": 100,
                    "thinking": {"levels": ["low"]}, "max_completion_tokens": 99}
        self.official = {ch: [copy.deepcopy(self.old)] for ch in catalog.CODEX_CHANNELS}
        self.official["claude"] = [{"id": "claude-sonnet-5", "context_length": 200},
                                   {"id": "claude-sonnet-5-5", "context_length": 1000000}]
        self.client = {"models": [{"slug": "gpt-6.1-sol", "official": True}]}
        self.new = {"slug": "gpt-6.2-sol", "display_name": "Sol", "context_window": 500000,
                    "supported_reasoning_levels": [{"effort": "low"}, {"effort": "ultra"}]}
        self.other = {"slug": "review-experimental", "display_name": "Review", "context_window": 321,
                      "input_modalities": ["text"], "supported_reasoning_levels": [{"effort": "high"}]}
        self.backend = {self.a.stem: [self.new, self.other], self.b.stem: [self.other]}
        p = patch.object(catalog, "get", side_effect=self.get)
        p.start()
        self.addCleanup(p.stop)
        p = patch.object(catalog, "codex_version", return_value="test")
        p.start()
        self.addCleanup(p.stop)
        # Any accidental HTTP beyond the stub is a test failure, including localhost.
        p = patch.object(catalog.urllib.request, "urlopen", side_effect=AssertionError("network forbidden"))
        p.start()
        self.addCleanup(p.stop)

    def get(self, url, headers=None):
        if url == catalog.OFFICIAL + "models.json":
            return copy.deepcopy(self.official)
        if url == catalog.OFFICIAL + "codex_client_models.json":
            return copy.deepcopy(self.client)
        self.assertTrue(url.startswith(catalog.CODEX_MODELS))
        value = self.backend[headers["Authorization"].split()[-1]]
        if isinstance(value, Exception):
            raise value
        return {"models": copy.deepcopy(value)}

    def snapshot(self):
        return {str(p.relative_to(self.root)): p.read_bytes() for p in self.root.rglob("*") if p.is_file()}

    def loaded(self, key, config=None):
        return json.loads(Path(catalog.config_value(key, config or self.config)).read_text())

    def test_real_run_both_proxies_and_noop(self):
        self.assertEqual(catalog.main(), 0)
        general = self.loaded("catalog")
        for ch in catalog.CODEX_CHANNELS:
            entries = {e["id"]: e for e in general[ch]}
            self.assertIn("review-experimental", entries)
            self.assertEqual(entries[self.new["slug"]]["thinking"]["levels"], ["low", "ultra"])
            self.assertEqual(entries[self.new["slug"]]["max_completion_tokens"], 99)
            self.assertEqual(entries[self.other["slug"]]["context_length"], 321)
        self.assertIn(self.new, self.loaded("codex-catalog")["models"])
        sonnet = next(e for e in self.loaded("catalog", self.claude_config)["claude"] if e["id"] == "claude-sonnet-6-0")
        self.assertEqual(sonnet["context_length"], 1000000)
        self.assertIn("  devin-catalog: embed\n", self.config.read_text())
        self.assertEqual(json.loads(self.b.read_text())["excluded_models"], ["manual-*", "gpt-6.2-sol"])
        self.assertEqual(json.loads(self.b.read_text())["access_token"], self.b.stem)
        self.assertEqual(self.calls.read_text(), "refresh\n")
        before = self.snapshot()
        self.assertEqual(catalog.main(), 0)
        self.assertEqual(self.snapshot(), before)
        for path in (self.config, self.claude_config, self.a, self.b):
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_dry_run_writes_nothing_and_never_refreshes(self):
        before = self.snapshot()
        with patch.object(catalog, "DRY", True):
            self.assertEqual(catalog.main(), 0)
        self.assertEqual(self.snapshot(), before)

    def test_partial_failure_keeps_catalogs_and_exclusions(self):
        self.assertEqual(catalog.main(), 0)
        self.backend[self.a.stem] = OSError("discovery failed")
        before = self.snapshot()
        self.assertEqual(catalog.main(), 1)
        self.assertEqual(self.snapshot(), before)

    def test_empty_discovery_is_a_failure(self):
        self.backend[self.a.stem] = []
        before = self.snapshot()
        self.assertEqual(catalog.main(), 1)
        self.assertEqual(self.snapshot(), before)

    def test_official_general_and_client_entries_win_independently(self):
        entry = {"id": self.new["slug"], "description": "official"}
        self.official["codex-pro"].append(entry)
        general = copy.deepcopy(self.official)
        client = copy.deepcopy(self.client)
        catalog.add_codex(general, client, {self.a: {self.new["slug"]: self.new}})
        self.assertIn(self.new, client["models"])
        self.assertEqual(next(e for e in general["codex-free"] if e["id"] == self.new["slug"]), entry)
        client = {"models": [{"slug": self.other["slug"], "description": "official client"}]}
        catalog.add_codex(general, client, {self.a: {self.other["slug"]: self.other}})
        self.assertEqual(client["models"][0]["description"], "official client")

    def test_path_changes_reload_each_changed_catalog_and_keep_two_slots(self):
        self.assertEqual(catalog.main(), 0)
        paths = (catalog.config_value("catalog"), catalog.config_value("codex-catalog"))
        self.new["context_window"] += 1
        self.assertEqual(catalog.main(), 0)
        self.assertNotEqual(paths[0], catalog.config_value("catalog"))
        self.assertNotEqual(paths[1], catalog.config_value("codex-catalog"))
        self.new["context_window"] += 1
        self.assertEqual(catalog.main(), 0)
        self.assertEqual(paths[0], catalog.config_value("catalog"))
        self.assertEqual(len(list(self.out.glob("models*.json"))), 2)
        self.assertEqual(self.calls.read_text().count("refresh"), 3)

    @unittest.skipUnless(sys.platform == "linux", "CLIProxyAPI uses inotify on pbox")
    def test_config_write_preserves_inotify_watch(self):
        libc = ctypes.CDLL(None, use_errno=True)
        fd = libc.inotify_init1(os.O_NONBLOCK)
        self.assertGreaterEqual(fd, 0)
        self.addCleanup(os.close, fd)
        self.assertGreaterEqual(libc.inotify_add_watch(fd, os.fsencode(self.config), 0xfff), 0)
        inode = self.config.stat().st_ino
        catalog.publish(self.config, self.out, {"catalog": ("models", self.official)})
        events = os.read(fd, 4096)
        mask = 0
        while events:
            _, flags, _, size = struct.unpack_from("iIII", events)
            mask |= flags
            events = events[16 + size:]
        self.assertFalse(mask & 0x8000, "config replacement detached its watcher (IN_IGNORED)")
        self.assertTrue(mask & 2, "config update did not emit IN_MODIFY")
        self.assertEqual(self.config.stat().st_ino, inode)

    def test_cache_and_explicit_alert_ids_without_inventing_slugs(self):
        self.cache.write_text(json.dumps({"models": [{"slug": "claude-opus-6-0", "isCustom": True}]}))
        catalog.ALERTS.mkdir()
        (catalog.ALERTS / "state.json").write_text(json.dumps({"outbox": {
            "alert": {"text": "Claude Sonnet 7 released; exact id claude-sonnet-6-1"}}}))
        state = {"claude_models": {"claude-sonnet-6-0": "Sonnet 6"}}
        self.assertEqual(set(catalog.claude_models(state)), {"claude-sonnet-6-0", "claude-sonnet-6-1"})
        general = copy.deepcopy(self.official)
        catalog.add_claude(general, {"claude-sonnet-6-0": "Sonnet 6"})
        general["claude"][0]["context_length"] = 99
        self.assertEqual(self.official["claude"][-1]["context_length"], 1000000)

    def test_known_claude_alias_keeps_its_official_version_capabilities(self):
        old = {"id": "claude-haiku-4-5-20251001", "context_length": 200000,
               "thinking": {"min": 1024, "max": 128000}}
        new = {"id": "claude-haiku-5-5", "context_length": 1000000,
               "thinking": {"dynamic_allowed": True}}
        general = {"claude": [old, new]}
        catalog.add_claude(general, {"claude-haiku-4-5": "Haiku 4.5"})
        self.assertEqual(general["claude"][0]["thinking"], old["thinking"])
        self.assertEqual(general["claude"][0]["context_length"], 200000)

    def test_manual_exclusions_survive_removal_of_owned_exclusions(self):
        self.assertEqual(catalog.main(), 0)
        self.backend[self.b.stem].append(self.new)
        self.assertEqual(catalog.main(), 0)
        self.assertEqual(json.loads(self.b.read_text())["excluded_models"], ["manual-*"])

    def test_refresh_command_absent_is_quiet(self):
        self.t3.write_text("#!/usr/bin/python3\nprint('SUBCOMMANDS\\n  thread  Manage threads; refresh manually')\n")
        self.assertEqual(catalog.main(), 0)
        self.assertFalse(self.calls.exists())

    def test_newest_family_template_uses_numeric_versions(self):
        entries = [{"id": "gpt-6.9-sol", "description": "older"},
                   {"id": "gpt-6.10-sol", "description": "newer"}]
        entry = catalog.catalog_entry("gpt-6.11-sol", {}, entries)
        self.assertEqual(entry["description"], "newer")
        self.assertEqual(catalog.family("claude-3-7-sonnet-20250219"), ("sonnet", (3, 7)))


if __name__ == "__main__":
    unittest.main()
