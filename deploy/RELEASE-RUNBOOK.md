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
git push origin custom
```

The production deployment then uses the approved `origin/custom` commit. Do
not deploy an uncommitted worktree or an arbitrary upstream commit.

## VPS Fallback Release

Use this path when the local development machine is unavailable or an urgent
production fix is required. Execute all remote commands through `ssh-skill`.

Before changing the VPS:

1. Confirm the current container image, Git commit, and worktree status.
2. Create a database/configuration backup under `/root/backups/sub2api/`.
3. Confirm no other deployment is running.
4. Create `emergency/vps-YYYYMMDD` from the deployed `custom` branch.

After the change:

1. Run focused tests or at minimum a successful image build.
2. Build `sub2api:custom` from `/root/sub2api`.
3. Run `docker compose -f /root/sub2api/deploy/docker-compose.yml up -d sub2api`.
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
