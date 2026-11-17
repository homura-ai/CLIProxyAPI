# CLIProxyAPI fork: per-model priority

This fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) adds `model_priority` to auth files, so credential order can depend on the requested model. The patch lives on the [`model-priority`](../../tree/model-priority) branch; this `automation` branch holds only the tooling.

## Keeping up with upstream

`.github/workflows/sync-upstream.yml` runs every hour and stops early when nothing has changed. It does the following:

1. Rebases `model-priority` onto the latest upstream release.
2. Runs `go vet` and `go test ./...`.
3. Publishes a release tagged `v<upstream>-mp.<n>` with a linux-amd64 build and `checksums.txt`.

If the rebase conflicts or the tests fail, the workflow opens an issue that records the failing patch head (`head=<sha>`), and publishes nothing.

## Auto-rebase agent (pbox)

`pbox/cliproxyapi-rebase-agent` runs hourly at :45 from a user timer. For each open `sync-upstream failed for <tag>` issue, it starts a T3 agent (Sol, xhigh) in the `cliproxyapi` project. The agent:

1. Rebases or fixes the patch.
2. Validates it on boat.dev.
3. Gets a fresh Sol review.
4. Force-pushes `model-priority` and closes the issue.
5. Verifies that the issue is closed, then settles its T3 thread. No-change resolutions also settle; blocked repairs stay unsettled.

The next hourly sync then releases the fix. The script starts one agent per failing head, never while another is still working, and at most 3 per issue. It keeps its state in `~/.local/state/cliproxyapi-rebase-agent/`.

## New Codex and Claude models before upstream lists them (pbox)

`pbox/cliproxyapi-codex-catalog` runs every 10 minutes from the existing user timer. It asks each Codex account which models it serves (the same `/codex/models` call Codex CLI makes). Every served slug is added to each plan catalog that lacks it, including slugs outside the `gpt-<version>-<family>` naming pattern. New entries clone the newest same-family entry when available, otherwise use the backend's name, context, modalities and reasoning levels. The Codex client catalog gets the backend's own entry independently. Official entries always win.

It sets per-account `excluded_models` for models still rolling out, removing only exclusions it owns. If any enabled account lookup fails, catalogs and exclusions stay unchanged until the next successful run.

Claude has the same catalog routing gate (verified through `claude`: an unknown id returns `400 unknown provider for model`). Its ids come from `~/.t3/caches/claudeAgent.json`, populated by Claude Code, and explicit Claude ids in model-release-watch's local alert outbox. Prose release names are never converted into guessed ids. Observed ids are remembered in `state.json`; new versions clone the newest official entry of their family, while known aliases reuse the matching official version's capabilities. Custom cache entries and families without templates are skipped. **The updater never makes HTTP requests to Anthropic or the Claude proxy, including management and model listing. Test Claude only with the `claude` CLI.**

The updater alternates two local catalog files under each proxy's `catalogs/` directory and updates just the catalog paths in its config. Config hot reload makes changes effective without restarting either proxy or calling management endpoints. After a catalog change it runs `~/.t3/bin/t3 provider refresh` if `t3 provider --help` advertises `refresh`; older T3 versions are skipped quietly. `--dry-run` writes nothing and runs no refresh. Python 3, standard library only. Offline checks: `python3 pbox/test_codex_catalog.py`.

To install it:

```
install -m 755 pbox/cliproxyapi-codex-catalog ~/.local/bin/
cp pbox/cliproxyapi-codex-catalog.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cliproxyapi-codex-catalog.timer
```

To undo it, disable the timer, restore the pre-install configs for both proxies, and remove only the account exclusions recorded in `~/.cli-proxy-api/catalogs/state.json`. The pbox upgrade also saves a one-command rollback under `~/credentials/cliproxyapi/before-all-models-catalog-*/rollback`.

## pbox install

`pbox/cliproxyapi-update` runs hourly from a user timer. It installs the newest `-mp` release into `~/.cli-proxy-api/app/` only when both proxies (ports 8317 and 8318) have no open client connections, or after an update has waited 24 hours. It then restarts both proxies and checks that they are running and listening. If either check fails, it restores `cli-proxy-api.prev` and opens an issue. It never sends a request to either proxy.

To install it:

```
install -m 755 pbox/cliproxyapi-update pbox/cliproxyapi-rebase-agent ~/.local/bin/
cp pbox/cliproxyapi-update.{service,timer} pbox/cliproxyapi-rebase-agent.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cliproxyapi-update.timer cliproxyapi-rebase-agent.timer
```

To roll back by hand:

```
cp ~/.cli-proxy-api/app/cli-proxy-api.prev ~/.cli-proxy-api/app/cli-proxy-api && systemctl --user restart cliproxyapi cliproxyapi-claude
```
