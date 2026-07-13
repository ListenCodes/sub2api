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
v2 risk-control images, recreates only the affected services, and verifies
health. A publish failure is terminal for that run. The exact promoted commit
is retained in `sync-pending-publish` so a later run can retry that same
approved commit before attempting another upstream merge.

`sync-upstream.sh` is the preparation component. It never builds an image or
restarts a container by itself. `sync-trigger.sh` is the container-mounted
admin trigger and waits until the unified host flow has completed.

## Production Publish

`publish-custom.sh --commit <sha>` is the only normal VPS release entrypoint.
The SHA must equal the current `origin/custom` head. The script backs up the
database and production configuration, builds from `/root/sub2api`, recreates
only `sub2api` and the v2 `risk-control` service, then verifies health and the
running binary version.

The legacy `/root/sub2api-risk-control` service is not part of this publish
operation.

## Cron Installation

The daily job and the per-minute admin-trigger consumer must both use the
unified wrapper. The per-minute job removes the trigger marker only when it is
about to start a run; the wrapper lock prevents overlapping publish attempts.

```cron
0 3 * * * /bin/bash /opt/sub2api-custom/auto-update.sh >> /var/log/sub2api-update.log 2>&1
* * * * * DATA_DIR=/var/lib/docker/volumes/deploy_sub2api_data/_data; [ -f "$DATA_DIR/sync-trigger" ] && rm "$DATA_DIR/sync-trigger" && /bin/bash /opt/sub2api-custom/sync-and-publish.sh >> /var/log/sub2api-sync.log 2>&1
```
