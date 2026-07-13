# Sub2API Release Runbook

This runbook defines how to change and publish Sub2API and the independent
risk-control service.

## Release Units

| Unit | Source | Runtime image | Production path |
|---|---|---|---|
| Main application and risk hooks | `origin/custom` | `sub2api:custom` | `/root/sub2api` |
| Independent risk-control service | approved risk release | `sub2api-risk-control:<release>` | `/root/sub2api-risk-control` |

Publish the two units as a recorded pair when either one changes. The main
application's `RISK_CONTROL_URL` must point to the running risk service.

## Normal Local Release

Run from the local repository after the user authorizes a release:

```bash
git status --short --branch
git fetch upstream
git fetch origin

# Integrate upstream in a feature or integration branch.
# Resolve conflicts locally and run tests before merging to custom.
git switch custom
git merge --ff-only integration/upstream-YYYYMMDD
git push origin custom
```

The production deployment uses the approved `origin/custom` commit. Do not
deploy an uncommitted worktree or an arbitrary upstream commit. The VPS
unified flow may promote a clean integration branch and publish it
automatically, but only after the base commit, clean-tree, backup, build, and
health checks pass.

## Versioned VPS Operations

Install the scripts from `deploy/ops/` to `/opt/sub2api-custom/`:

```text
sync-trigger.sh    container-mounted trigger; waits for the cron result
sync-upstream.sh   fetches upstream and prepares origin/integration/*
sync-and-publish.sh shared trigger/scheduled sync-then-publish wrapper
auto-update.sh     scheduled wrapper for sync-and-publish.sh
publish-custom.sh  approved production release entrypoint
```

The normal publish command is:

```bash
/opt/sub2api-custom/publish-custom.sh --commit "$(git rev-parse origin/custom)"
```

It backs up production, builds the approved source, recreates only the main
and v2 risk-control services, and verifies health and the running version.

The admin trigger and the daily scheduled job both call
`sync-and-publish.sh`. A conflict or changed custom base stops before
`origin/custom` and production are changed. A clean merge promotes the
integration branch and calls the same publish entrypoint automatically.

The production crontab must keep the per-minute admin-trigger consumer on the
same wrapper; calling `sync-upstream.sh` there produces preparation-only
behavior and will not deploy the result:

```cron
0 3 * * * /bin/bash /opt/sub2api-custom/auto-update.sh >> /var/log/sub2api-update.log 2>&1
* * * * * DATA_DIR=/var/lib/docker/volumes/deploy_sub2api_data/_data; [ -f "$DATA_DIR/sync-trigger" ] && rm "$DATA_DIR/sync-trigger" && /bin/bash /opt/sub2api-custom/sync-and-publish.sh >> /var/log/sub2api-sync.log 2>&1
```

If an upstream merge conflicts, the update status reports the exact files,
both commit IDs, and the diagnostic artifact path. The admin panel must show
that production was not changed. Resolve the conflict in a local `custom`
worktree, run the normal tests, push `origin/custom`, and retry the update;
do not use `git reset --hard` or a forced `ours`/`theirs` merge.

## VPS Fallback Release

Use this path when the local development machine is unavailable or an urgent
production fix is required. Execute all remote commands through `ssh-skill`.

Before changing the VPS:

1. Confirm the current container image, Git commit, and worktree status.
2. Create a database/configuration backup under `/root/backups/sub2api/`.
3. Confirm no other deployment is running.
4. Create `emergency/vps-YYYYMMDD` from the deployed `custom` branch.

After an emergency change:

1. Run focused tests or at minimum a successful image build.
2. Build and publish with the versioned `publish-custom.sh` after committing
   the emergency change and pushing the approved commit to `origin`.
3. Do not run the upstream preparation script as a production release.
4. Wait for the container health check and `http://127.0.0.1:8081/health`.
5. Check the public HTTPS endpoint and the risk-control container.
6. Commit the change and push it to `origin`.
7. Record the commit, image ID, backup path, and validation result.

Do not use a force reset to hide local changes. If the VPS worktree is dirty,
stop and preserve the diff before proceeding.

## Risk-Control Release

The independent service is intentionally outside the main Git worktree:

```text
/root/sub2api-risk-control/deploy/docker-compose.prod.yml
/root/sub2api-risk-control/.env
```

The main application must use the dedicated network alias
`http://risk-control-v2:8090`. The legacy risk service may still share the
Docker network under the `risk-control` name during migration, so do not use
the ambiguous legacy alias for the v2 integration.

The admin product contract is defined in
`docs/RISK-CONTROL-ADMIN-SPEC.md`. A risk-control release is not accepted when
the pages only render a skeleton. Before production, verify the three admin
flows together: a user row includes account identity, a Chinese risk reason,
event evidence and account status; a rule can be created, validated, tested
and audited; and an administrator can perform a reason-required single or
batch status action with per-target results. Sort and filter controls must
change the data query or stable result order, not only the visual state.

The `.env` file is production-only and must never be committed. Update the
service image and Compose file together, then validate:

```bash
docker compose \
  -f /root/sub2api-risk-control/deploy/docker-compose.prod.yml \
  --env-file /root/sub2api-risk-control/.env \
  config --quiet
```

Keep the main application in shadow/review mode until registration events,
user identity, risk reason, action records, and administrator visibility have
been verified with real traffic.

## Verification Checklist

- [ ] Git worktree is clean after commit.
- [ ] Intended main commit and risk image tag are recorded.
- [ ] `sub2api` container is healthy.
- [ ] `sub2api-risk-control` container is healthy.
- [ ] PostgreSQL and Redis are healthy.
- [ ] Main `/health` returns success.
- [ ] Public HTTPS endpoint returns success.
- [ ] Admin risk pages load and show the expected user identity and reason.
- [ ] Admin risk pages render Chinese labels for risk types, levels, actions,
      statuses and audit results; raw enum values are not the primary text.
- [ ] User page supports current-page selection, batch action confirmation,
      required reason, partial-failure reporting and sortable columns.
- [ ] Rule page supports creating a rule and records create/update actions.
- [ ] Audit page shows administrator, target, reason, result and failure detail.
- [ ] No unexpected Nginx configuration warnings exist.
- [ ] Backup path and rollback image are recorded.

## Rollback

Rollback means restoring the previous Compose/image pair and restarting only
the affected service. Do not roll back by deleting the Git repository or by
resetting the production worktree without a backup.

After rollback, verify the same health checklist and record the failed commit,
the rollback target, and the reason.

## Agent Rules

- A code task does not imply a production release.
- A release requires explicit user authorization unless the user explicitly
  requested the VPS emergency path.
- Other agents must read the repository `AGENTS.md` before editing.
- Report implementation commit, tests, deployment status, and rollback status
  as separate facts.
