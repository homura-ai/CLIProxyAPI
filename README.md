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
