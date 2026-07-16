# Sub2API Release Runbook

This runbook defines how to change and publish Sub2API and its unified custom
extensions service.

## Release Units

| Unit | Source | Runtime image | Production path |
|---|---|---|---|
| Main application and risk hooks | `origin/custom` | `sub2api:custom` | `/root/sub2api` |
| Risk control, account monitor, and homepage | `origin/custom:extensions-self/` | `deploy-extensions-self` | `/root/sub2api/extensions-self` |

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
and extensions-self services, and verifies health and the running version.
The backup contains both `sub2api_db.dump` and `risk_control_db.dump`.

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
5. Check the public HTTPS endpoint and the extensions-self container.
6. Commit the change and push it to `origin`.
7. Record the commit, image ID, backup path, and validation result.

Do not use a force reset to hide local changes. If the VPS worktree is dirty,
stop and preserve the diff before proceeding.

## Extensions-Self Release

The unified extension service is built from the approved main repository checkout:

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
https://sub.ailisten.top/api/v1/extensions-self/homepage/
https://sub.ailisten.top/api/v1/admin/extensions-self/account-monitor/data-quality
```

`risk-control-postgres` and `risk_control_postgres_data` remain independent and
must not appear in application `up`, `rm`, or `down` commands.

For the one-time migration, keep the current `risk-control` container running,
change `RISK_CONTROL_URL` in `/root/sub2api/deploy/.env` to the extensions URL,
and run `publish-custom.sh`. The publisher starts and verifies
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
5. Build both images and recreate only `sub2api` and `extensions-self`.
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
- [ ] Intended main commit and risk image tag are recorded.
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

Rollback means restoring the previous Compose/image pair and restarting only
the affected service. Do not roll back by deleting the Git repository or by
resetting the production worktree without a backup.

The publisher records the previous images as
`sub2api:rollback-<timestamp>` and `deploy-extensions-self:rollback-<timestamp>`.
Retag the selected rollback image to its active Compose image name, restore
the matching configuration and previous inline homepage setting, and recreate
only the affected application service. Never delete the risk database during
rollback.

For an account-monitor-only rollback, set `ACCOUNT_MONITOR_ENABLED=false`,
restore the matching application images and environment, and recreate only
`sub2api` and `extensions-self`. Keep monitor tables and safe views for diagnosis.
Restore `risk_control_db.dump` only for confirmed schema/data corruption; a
normal code rollback must not discard newly collected risk or monitor data.

After rollback, verify the same health checklist and record the failed commit,
the rollback target, and the reason.

## Agent Rules

- A code task does not imply a production release.
- A release requires explicit user authorization unless the user explicitly
  requested the VPS emergency path.
- Other agents must read the repository `AGENTS.md` before editing.
- Report implementation commit, tests, deployment status, and rollback status
  as separate facts.
