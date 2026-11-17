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

## New Codex models before upstream lists them (pbox)

`pbox/cliproxyapi-codex-catalog` runs every 10 minutes from a user timer. It asks each Codex account which models it serves (the same `/codex/models` call Codex CLI makes) and writes `~/.cli-proxy-api/catalogs/models.json` and `codex_client_models.json`: the official catalogs plus any `gpt-*` slug an account serves that they lack. A new `models.json` entry is cloned from the newest entry of the same family (astra, sol, terra, luna); the Codex client entry is the backend's own. Once upstream lists a slug, the upstream entry is used.

It also sets `excluded_models` on each account file to the `gpt-*` slugs other accounts serve but that account does not, so a model still rolling out is never routed to an account without it. It removes only exclusions it added itself. On its first run it adds a `models:` block pointing `config.yaml` at the two local files, and it asks the proxy to reload them whenever they change.

To install it:

```
install -m 755 pbox/cliproxyapi-codex-catalog ~/.local/bin/
cp pbox/cliproxyapi-codex-catalog.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cliproxyapi-codex-catalog.timer
```

To undo it, disable the timer, remove the `models:` block from `config.yaml`, and delete the `excluded_models` entries it added (listed in `~/.cli-proxy-api/catalogs/state.json`).

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
