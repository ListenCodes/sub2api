# Release Operations

These scripts are the versioned source for the VPS operations under
`/opt/sub2api-custom/`.

## Unified Upstream Flow

`sync-and-publish.sh` is the entrypoint used by both the admin trigger and the
scheduled job. It runs `sync-upstream.sh` in a temporary worktree first. A
conflict, changed `origin/custom` base, or dirty VPS tree stops the flow and
leaves the integration branch for manual resolution.

When the merge is clean and the base is unchanged, it fast-forwards `custom`,
pushes `origin/custom` without force, and invokes `publish-custom.sh` with the
exact new commit. The publish script backs up production, builds the main and
extensions-self images, recreates only the affected services, and verifies
health. A publish failure is terminal for that run. The exact promoted commit
is retained in `sync-pending-publish` so a later run can retry that same
approved commit before attempting another upstream merge.

`sync-upstream.sh` is the preparation component. It never builds an image or
restarts a container by itself. `sync-trigger.sh` is the container-mounted
admin trigger and waits until the unified host flow has completed.

When Git cannot safely merge upstream, the status includes `conflict_files`,
`conflict_base`, `conflict_upstream`, `conflict_log`, and `resolution_hint`.
The host stores a diagnostic snapshot under
`/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/<job-id>/`;
`conflict_log` points to that host-side `metadata.json` path, not the container
mount path. The admin panel shows the conflicted files and states that production was not
changed. The script never resolves conflicts with `ours` or `theirs` silently.

## Production Publish

`publish-custom.sh --commit <sha>` is the only normal VPS release entrypoint.
The SHA must equal the current `origin/custom` head. The script backs up the
database and production configuration, builds from `/root/sub2api`, recreates
only `sub2api` and the unified `extensions-self` service, then verifies the
main API, risk API, public homepage proxy, and running binary version.

When `ACCOUNT_MONITOR_ENABLED=true`, the publisher additionally:

1. parses the rendered `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` and requires the
   `extensions_self_monitor` login on the `postgres` service;
2. backs up both `sub2api-postgres` and `risk-control-postgres` before changes;
3. runs `install-account-monitor-source.sql` and verifies
   `SET ROLE extensions_self_monitor_ro` can read the safe views;
4. proves the login cannot read `public.api_keys.key` or
   `public.accounts.credentials`;
5. checks the account-monitor static page, signed `data-quality` API, and main
   authenticated proxy route after recreation.

Any failure stops publication. The script never prints the source password and
never manages the `risk-control-postgres` lifecycle.

The retired standalone `/root/sub2api-risk-control` deployment must not be
reintroduced. The canonical source is `/root/sub2api/extensions-self`; its
single Go process serves both risk APIs and `/homepage/`. The publisher removes
the previous `risk-control` application container only after `extensions-self`
and the public homepage proxy are healthy. It never removes or recreates
`risk-control-postgres`.

## Cron Installation

The daily job and the per-minute admin-trigger consumer must both use the
unified wrapper. The per-minute job removes the trigger marker only when it is
about to start a run; the wrapper lock prevents overlapping publish attempts.

```cron
0 3 * * * /bin/bash /opt/sub2api-custom/auto-update.sh >> /var/log/sub2api-update.log 2>&1
* * * * * DATA_DIR=/var/lib/docker/volumes/deploy_sub2api_data/_data; [ -f "$DATA_DIR/sync-trigger" ] && rm "$DATA_DIR/sync-trigger" && /bin/bash /opt/sub2api-custom/sync-and-publish.sh >> /var/log/sub2api-sync.log 2>&1
```
