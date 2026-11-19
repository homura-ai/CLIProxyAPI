# CLIProxyAPI fork: per-model priority

This fork of [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) adds `model_priority` to auth files, so credential order can depend on the requested model. The patch lives on the [`model-priority`](../../tree/model-priority) branch; this `automation` branch holds only the tooling.

## Keeping up with upstream

`.github/workflows/sync-upstream.yml` runs every hour and stops early when nothing has changed. It does the following:

1. Rebases `model-priority` onto the latest upstream release.
2. Refreshes the model catalogs, runs `go vet ./sdk/cliproxy/auth/...`, and runs `go test -count=1 -json ./...`. Test failures block release only if they repeat on the patch branch and are absent from the unmodified upstream release.
3. Publishes a release tagged `v<upstream>-mp.<n>` with a linux-amd64 build and `checksums.txt`.

If the rebase conflicts or the validation gate fails, the workflow opens or updates an issue that records the failing patch head (`head=<sha>`), and publishes nothing. Build or release failures are also recorded in the issue.

## Auto-rebase agent

`local-tools/cliproxyapi-rebase-agent` runs hourly at :45 from a user timer. It starts a T3 agent in the
`cliproxyapi` project to repair `model-priority` for open `sync-upstream failed for <tag>` issues.
The next sync publishes a successful repair. The script starts at most one agent per failing head
and three per issue, with one repair running at a time. State lives in
`~/.local/state/cliproxyapi-rebase-agent/`.

Set `CPA_REBASE_PROJECT` to the T3 project directory if it differs from
`~/code/reuse/cliproxyapi`, or `T3CODE_CLI_PATH` if the CLI is installed outside
`~/.t3/bin/t3`.

## New Codex and Claude models before upstream lists them

`local-tools/cliproxyapi-codex-catalog` runs every 10 minutes from a user timer. It adds the models served by your Codex accounts to the plan and client catalogs. Official catalog entries take precedence.

It sets per-account `excluded_models` for models still rolling out, removing only exclusions it owns. Codex catalog ids served by no account are removed from both catalogs; official metadata remains authoritative for served models. If any enabled account lookup fails, catalogs and exclusions stay unchanged until the next successful run.

Claude model IDs come from `~/.t3/caches/claudeAgent.json`, populated by Claude Code, and
explicit IDs in the model-release-watch alert outbox. Custom cache entries and model
families without official templates are skipped. **The updater never makes HTTP requests
to Anthropic or the Claude proxy. Test Claude only with the `claude` CLI.**

Each proxy reloads its catalog without restarting. The updater runs
`~/.t3/bin/t3 provider refresh` after a catalog change when the command is available.
The updater needs Python 3 and uses its standard library. Use `--dry-run` to preview
without writing files or refreshing providers.

Back up the Codex and Claude proxy configs before enabling the timer.

```bash
mkdir -p ~/.local/bin ~/.config/systemd/user
install -m 755 local-tools/cliproxyapi-codex-catalog ~/.local/bin/
cp local-tools/cliproxyapi-codex-catalog.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cliproxyapi-codex-catalog.timer
```

To undo it, disable the timer, restore the pre-install configs for both proxies, and remove only the account exclusions recorded in `~/.cli-proxy-api/catalogs/state.json`.

## Install the local updater

`local-tools/cliproxyapi-update` runs hourly from a user timer. It installs the newest `-mp` release into `~/.cli-proxy-api/app/` only when both proxies (ports 8317 and 8318) have no open client connections, or after an update has waited 24 hours. It then restarts both proxies and checks that they are running and listening. If either check fails, it restores `cli-proxy-api.prev` and opens an issue. It never sends a request to either proxy.

Install the updater and repair timers from this checkout.

```bash
mkdir -p ~/.local/bin ~/.config/systemd/user
install -m 755 local-tools/cliproxyapi-update local-tools/cliproxyapi-rebase-agent ~/.local/bin/
cp local-tools/cliproxyapi-update.{service,timer} local-tools/cliproxyapi-rebase-agent.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cliproxyapi-update.timer cliproxyapi-rebase-agent.timer
```

To roll back by hand:

```
cp ~/.cli-proxy-api/app/cli-proxy-api.prev ~/.cli-proxy-api/app/cli-proxy-api && systemctl --user restart cliproxyapi cliproxyapi-claude
```
