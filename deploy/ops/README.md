# Release Operations

These scripts are the versioned source for the VPS operations under
`/opt/sub2api-custom/`.

## Upstream Sync

`sync-upstream.sh` fetches the official `upstream/main`, checks that the VPS
`custom` branch matches `origin/custom`, and tries a merge in a temporary
worktree. A clean merge is pushed only as `origin/integration/upstream-*` for
local review. A conflict reports the exact files and leaves production alone.

It never builds an image, restarts a container, changes `custom`, or force-pushes.

## Production Publish

`publish-custom.sh --commit <sha>` is the only normal VPS release entrypoint.
The SHA must equal the current `origin/custom` head. The script backs up the
database and production configuration, builds from `/root/sub2api`, recreates
only `sub2api` and the v2 `risk-control` service, then verifies health and the
running binary version.

The legacy `/root/sub2api-risk-control` service is not part of this publish
operation.
