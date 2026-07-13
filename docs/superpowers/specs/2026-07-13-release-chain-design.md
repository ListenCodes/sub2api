# Sub2API Release Chain Design

## Goal

Make the custom source update and production release paths separate, reviewable,
and recoverable. An upstream update must never rebase the production branch,
build a production image, deploy it, or force-push the user's fork.

## Source Of Truth

```text
Wei-Shaw/sub2api upstream/main
        |
        | local fetch and conflict resolution
        v
E:\Code\sub2api feature/integration branch
        |
        | tests, version check, review
        v
E:\Code\sub2api custom -> ListenCodes/sub2api origin/custom
        |
        | VPS fast-forward to the approved commit
        v
/root/sub2api custom -> sub2api:custom -> production
```

The legacy service at `/root/sub2api-risk-control` remains outside this chain.
The v2 risk-control service in `sub2api/risk-control` is released with the
main repository and must use the dedicated `risk-control-v2` network alias.

## Upstream Sync

The admin update action invokes a host-side sync script through the existing
container mount. The script will:

1. Acquire the existing lock and reject a dirty production worktree.
2. Fetch `upstream/main` and `origin/custom`.
3. Verify that the VPS `custom` branch matches `origin/custom`.
4. Create a temporary worktree from the approved custom commit.
5. Try a non-destructive merge of `upstream/main` into a unique integration
   branch.
6. On conflict, abort the temporary merge and report the exact conflict files.
7. On success, commit and push only `integration/upstream-<timestamp>` to the
   user's fork for local review.

The script never changes `custom`, builds an image, restarts a service, or
pushes `custom`.

## Production Publish

The publish script is a separate host operation. It accepts one approved
commit or uses `origin/custom` after verifying that the source tree is clean.
It creates a timestamped backup, records the previous image IDs, builds the
main image from `/root/sub2api`, recreates only the requested services, and
checks container and public health. It never fetches `upstream`, rebases,
force-pushes, or deploys an uncommitted worktree.

## Admin UI Contract

The existing asynchronous update job remains the transport, but its result is
now preparation-only. A successful job reports that an integration branch was
created and does not request a service restart. A failed job preserves the
script's conflict or validation message. Actual production publishing remains
an operator action outside the admin update button.

## Failure Handling

- Dirty VPS worktree: fail without stashing or modifying it.
- Upstream conflict: abort the temporary merge and report files.
- Push integration branch failure: retain the local temporary branch and
  report the remote error; do not touch `custom`.
- Build or health failure during publish: keep the previous image and backup
  available, then stop before declaring success.
- Any failed operation leaves production running on the previous container.

## Acceptance Criteria

- Repeated admin sync requests cannot run concurrently.
- A conflict does not change `/root/sub2api`'s `custom` branch or production.
- A clean upstream merge creates only an `origin/integration/*` branch.
- The UI does not show a restart action after a preparation-only sync.
- The publish path accepts only a clean, approved `origin/custom` commit.
- The latest production version is verified from the running binary after
  publish.
