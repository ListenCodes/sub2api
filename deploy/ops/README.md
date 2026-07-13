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
health. A publish failure is terminal for that run; it is not silently retried.

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
