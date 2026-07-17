# Sub2API Release Runbook

This runbook defines how to change and publish Sub2API and its unified custom
extensions service.

Start with `docs/SUB2API-CUSTOM-OPERATIONS.md` for the Chinese project overview,
automation boundary, standard custom-code workflow, expected timing, and report
template. This runbook remains authoritative for production execution and
rollback details.

## Release Units

| Unit | Source | Runtime image | Production path |
|---|---|---|---|
| Main application and risk hooks | `origin/custom-release` | `ghcr.io/listencodes/sub2api-custom@sha256:<digest>` | `/root/sub2api` |
| Risk control, account monitor, and homepage | `origin/custom-release:extensions-self/` | `ghcr.io/listencodes/sub2api-extensions@sha256:<digest>` | `/root/sub2api/extensions-self` |

Publish the two units as a recorded pair when either one changes. The main
application's `RISK_CONTROL_URL` must point to the running risk service.

## Normal Release

Custom code follows this path:

```text
feature -> custom-release -> Actions + public paired GHCR images
-> administrator button -> systemd state machine -> digest deployment
```

Merge a tested feature into `custom-release`, push `origin/custom-release`, and
wait for the Custom Release workflow plus both full-SHA image tags. Pushing code
does not publish production. An administrator must explicitly start the durable
release job from the update button.

The production deployment uses the approved `origin/custom-release` commit.
The legacy `custom` branch is retained for history and `upstream/main`
compatibility testing and cannot auto-publish. Do not deploy an uncommitted
worktree or an arbitrary upstream commit. The VPS
host flow may promote a clean official Release candidate only after exact tag
validation, Actions, both image checks, and a base recheck.

The first production branch switch from `custom` to `custom-release` is a
manual migration and requires explicit publication authorization. Completing
code, pushing `origin/custom-release`, and publishing production are separate
states.

## Versioned Host Operations

Install the scripts from `deploy/ops/` to `/opt/sub2api-custom/`:

```text
sync-trigger.sh    container-mounted trigger; writes a job ID and returns
sync-upstream.sh   verifies a stable Release and prepares origin/integration/release-*
wait-for-actions.sh / verify-release-images.sh  Actions and image gates
promote-release.sh guarded remote-only branch promotion
sync-and-publish.sh durable host orchestrator
publish-custom.sh  internal backup/digest deploy/rollback component
sub2api-release.path / .service  administrator trigger consumer
```

Install scripts and units, then enable the path watcher:

```bash
install -m 0755 deploy/ops/*.sh /opt/sub2api-custom/
install -m 0644 deploy/ops/sub2api-release.path /etc/systemd/system/
install -m 0644 deploy/ops/sub2api-release.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now sub2api-release.path
```

`sub2api-release.path` watches the persistent `release-trigger` created by the
administrator action. The one-shot service calls only `sync-and-publish.sh`.
The health-monitor schedule remains independent.

The two public image repositories are:

```text
ghcr.io/listencodes/sub2api-custom:custom-<full-sha>
ghcr.io/listencodes/sub2api-extensions:custom-<full-sha>
```

Production `.env` pins `SUB2API_IMAGE` and `EXTENSIONS_SELF_IMAGE` to the
verified `ghcr.io/...@sha256:...` references. The VPS pulls them anonymously.
There is no release polling job or automatic source update.

For the one-time migration from legacy locally built images, a missing
`release-state.json` makes the publisher record the clean local commit, current
container image IDs, old Compose/environment, and rollback tags in the release
backup. A healthy publish creates the first formal digest state. A failed
migration restores the old Compose and local images and leaves formal state
absent; this bootstrap is not used after a healthy digest release.

Do not call `publish-custom.sh` directly for final acceptance. Trigger the same
durable job used by the administrator UI and monitor its persisted states.

If a stable Release merge conflicts, the update status reports the exact files,
approved branch base, Release tag/commit, and diagnostic artifact path. The
admin panel must show that production was not changed. Resolve the conflict in
a local `custom-release` worktree, run the normal tests, push
`origin/custom-release`, and retry the update;
do not use `git reset --hard` or a forced `ours`/`theirs` merge.

## VPS Fallback Release

Use this path when the local development machine is unavailable or an urgent
production fix is required. Execute all remote commands through `ssh-skill`.

Before changing the VPS:

1. Confirm the current container image, Git commit, and worktree status.
2. Confirm no other durable release job is running.
3. Create `emergency/vps-YYYYMMDD` from the deployed `custom-release` commit.

After an emergency change:

1. Run focused tests, commit, and push the emergency branch.
2. Reconcile it into `custom-release` without rewriting history and wait for
   the same Actions and paired GHCR images.
3. Use the administrator-triggered durable release path.
4. Wait for container and full internal/public health checks.
5. Record the commit, both digests, backup path, and rollback evidence.

Do not use a force reset to hide local changes. If the VPS worktree is dirty,
stop and preserve the diff before proceeding.

## Extensions-Self Release

The unified extension service is sourced from the approved main repository checkout:

```text
/root/sub2api/extensions-self/risk-control
/root/sub2api/extensions-self/account-monitor
/root/sub2api/extensions-self/homepage
/root/sub2api/deploy/docker-compose.yml
/root/sub2api/deploy/.env
```

The main application must use `http://extensions-self:8090`. The extension Go
process serves signed risk/account-monitor APIs and the static `/homepage/` route.
The browser reaches them only through same-origin main application proxies:

```text
/api/v1/extensions-self/homepage/
/api/v1/admin/extensions-self/account-monitor/data-quality
```

`risk-control-postgres` and `risk_control_postgres_data` remain independent and
must not appear in application `up`, `rm`, or `down` commands.

For the one-time migration, keep the current `risk-control` container running,
change `RISK_CONTROL_URL` in `/root/sub2api/deploy/.env` to the extensions URL,
and run the administrator release job. The publisher starts and verifies
`extensions-self` before removing the old application container. It does not
touch the risk database container or volume.

After the public proxy returns 200, set the administration system's custom
homepage content to the absolute iframe URL above. Back up the previous inline
HTML first. Restart the main application only if the direct database update was
used and the settings cache was not invalidated through the admin API.

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
  --project-name deploy \
  -f /root/sub2api/deploy/docker-compose.yml \
  --env-file /root/sub2api/deploy/.env \
  config --quiet
```

Keep the main application in shadow/review mode until registration events,
user identity, risk reason, action records, and administrator visibility have
been verified with real traffic.

## Account Monitor Release

The account monitor is disabled unless `ACCOUNT_MONITOR_ENABLED=true`. Its
source DSN must use the dedicated `extensions_self_monitor` login on the main
`postgres` service; the login only inherits `extensions_self_monitor_ro` and
must never be the main DB owner.

For the first enabled release, use this order:

1. Record the approved commit and current image IDs.
2. Back up and verify the main database and `risk-control-postgres`; back up Compose,
   `.env`, Nginx vhost, origin certificate/key, container/image metadata and rollback tags.
3. Run `deploy/ops/install-account-monitor-source.sql` as the main DB owner.
4. Verify the NOLOGIN role and TCP login can read `extensions_self_ro.usage_source`
   `extensions_self_ro.group_dimension`, and `extensions_self_ro.account_group_dimension`,
   while full keys and credentials are denied.
5. Verify the paired GHCR digests and recreate only `extensions-self`, then `sub2api`.
6. Verify `/admin/extensions/account-monitor`, `/admin/extensions/group-monitor`,
   signed `data-quality`, risk pages, and custom homepage.
7. Reconcile sampled success, failure, retry-after-failure, model, cost, and
   media counts. Record the actual available historical range.

After step 6, read `available_from/to` from signed `data-quality`, then run:

```bash
/root/sub2api/deploy/ops/backfill-account-monitor.sh \
  --from <available-from-RFC3339> --to <available-to-RFC3339> \
  --record-dir /root/backups/sub2api/<release-id>
```

The script must finish every non-overlapping segment as `completed`; do not continue after
`failed` or timeout. Preserve `backfill-jobs.tsv`, `processed_rows`, and the final quality JSON.

The publisher enforces steps 2 through 6 when the monitor is enabled. A rebuild
range may not exceed 31 days. Facts/minute aggregates are retained for 90 days;
daily aggregates for 365 days. Existing main error history may be shorter, so
historical gaps must be reported rather than backfilled with zeros.

### Inventory And Group Reconciliation

Run the allow probes with the dedicated source login. All three must succeed:

```sql
SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1;
SELECT 1 FROM extensions_self_ro.group_dimension LIMIT 1;
SELECT 1 FROM extensions_self_ro.account_group_dimension LIMIT 1;
```

Run the following deny probes with the same login. Both must fail with
`permission denied`; a returned row is a release blocker:

```sql
SELECT key FROM public.api_keys LIMIT 1;
SELECT credentials FROM public.accounts LIMIT 1;
```

Record the main-source “全量非删除账号数” separately from the extension
database's 30-day “事实活跃账号数”. With `rollup=physical`, the account-monitor
API total must equal the first count, including zero-call accounts; the second
count is diagnostic only and must not drive inventory paging.

```sql
-- Main database, through the restricted source login.
SELECT count(*) AS full_non_deleted_accounts
FROM extensions_self_ro.account_dimension
WHERE deleted_at IS NULL;

-- Extension database.
SELECT count(DISTINCT account_id) AS fact_active_accounts
FROM account_monitor_attempt_facts
WHERE attempted_at >= now() - interval '30 days';
```

Select and record a “多分组账号样本” before browser acceptance. Each sampled
account must appear once in the account list, display every active membership,
and match a filter for any of its groups:

```sql
SELECT ag.account_id, array_agg(ag.group_id ORDER BY ag.group_id) AS group_ids
FROM extensions_self_ro.account_group_dimension AS ag
JOIN extensions_self_ro.account_dimension AS a ON a.id = ag.account_id
WHERE a.deleted_at IS NULL AND ag.group_deleted_at IS NULL
GROUP BY ag.account_id
HAVING count(DISTINCT ag.group_id) > 1
LIMIT 10;
```

For group monitoring, exercise `6h/24h/7d/30d`. Every list card and model detail
must return exactly 24 个时间桶, using 15 分钟, 1 小时, 7 小时, and 30 小时
respectively. Reconcile every API range against the same `[from,to)` interval in
`account_monitor_request_facts`; verify `total_requests=successes+failures` for
each group and that card/detail totals agree. Keep the exact UTC boundaries with
the release evidence.

Browser acceptance must select `page_size=1000`, exercise platform/group/text
filters, and use the “手动刷新” button on account and group monitor. Exercise
input/select filtering on user risk pages. When the page is idle, the network
log must show no monitor polling request. Account detail uses five tabs, a
fixed-height scrolling body, sticky table headers, stays open across queries,
and closes when its backdrop is clicked.

## Verification Checklist

- [ ] Git worktree is clean after commit.
- [ ] Intended commit and both GHCR digests are recorded.
- [ ] `sub2api` container is healthy.
- [ ] `extensions-self` container is healthy.
- [ ] Public extensions homepage returns success and is the configured iframe.
- [ ] Native `/admin/extensions/account-monitor` and `/admin/extensions/group-monitor` load.
- [ ] `extensions_self_ro.account_group_dimension` allow probe and raw key/credential deny probes passed.
- [ ] “全量非删除账号数” equals account-monitor total; “事实活跃账号数” is recorded separately.
- [ ] A “多分组账号样本” displays all memberships and matches every applicable group filter.
- [ ] `page_size=1000`, `6h/24h/7d/30d` fixed 24-bucket views, and “手动刷新” browser checks passed without background polling.
- [ ] Signed account-monitor `data-quality` reports a recent sync or an explicit source error.
- [ ] `data_as_of`, both cursors, available history, missing-group and exact/estimated counts are recorded.
- [ ] Segmented backfill covers only the available interval and every recorded job completed.
- [ ] Source login reads only `extensions_self_ro` and cannot read credentials/full keys.
- [ ] Sample account-attempt and user-final counts reconcile, including retry-after-failure.
- [ ] The retired `risk-control` application container is absent.
- [ ] `risk-control-postgres` kept the same container/volume identity.
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

Deployment or health failure triggers automatic paired rollback. The publisher
restores the backed-up `SUB2API_IMAGE` and `EXTENSIONS_SELF_IMAGE`, recreates
`extensions-self` then `sub2api`, and runs the complete health suite. Do not
roll back by deleting the Git repository or resetting the production worktree.

The publisher records the previous images as
`sub2api:rollback-<timestamp>` and `deploy-extensions-self:rollback-<timestamp>`.
The rollback tags are additional recovery evidence; normal rollback uses the
previous immutable digest pair and matching `.env`/Compose backup. Never delete
or recreate the risk database during rollback.

For an account-monitor-only rollback, set `ACCOUNT_MONITOR_ENABLED=false`,
restore the matching application images and environment, and recreate only
`sub2api` and `extensions-self`. Keep monitor tables and safe views for diagnosis.
Database restore is not automatic. Restore `risk_control_db.dump` only for
separately confirmed schema/data corruption; a normal code rollback must not
discard newly collected risk or monitor data.

After rollback, verify the same health checklist and record the failed commit,
the rollback target, and the reason.

## Agent Rules

- A code task does not imply a production release.
- A release requires explicit user authorization unless the user explicitly
  requested the VPS emergency path.
- Other agents must read the repository `AGENTS.md` before editing.
- Report implementation commit, tests, deployment status, and rollback status
  as separate facts.
- Report implementation, local tests, `origin/custom-release` push,
  Actions/GHCR, production backup, deployment, health, trigger migration, and
  rollback material separately.
