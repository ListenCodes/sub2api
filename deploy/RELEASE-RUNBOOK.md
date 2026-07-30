# Sub2API Release Runbook

Dual-version baseline: Official v0.1.163 / Custom v1.0.0 belongs to production
commit `aa2d24106cab0a03785330d8e0ff4e02b0474a0e`. The first successful custom
runtime release is v1.0.1. Official-only releases retain the custom version;
combined releases advance it once. Failed, expired, drifted, unconfirmed, and
docs-only operations allocate no number. Complete rollback uses prepare then
explicit apply within one hour, only the last three complete snapshots
excluding current, never lowers/reuses high-water, and normally restores neither
database. Legacy single-stage and official binary rollback endpoints fail closed.

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

Commits limited to Markdown, `AGENTS.md`, or any `.gitignore` are documentation-only:
the Custom Release workflow ignores those pushes, and the host classifier marks
the durable job `docs_only=true` without waiting for Actions, verifying GHCR, or
changing production. Any runtime path in the full production-to-target diff uses
the normal validation and paired-image gates.

## Versioned Host Operations

Install the scripts from `deploy/ops/` to `/opt/sub2api-custom/`:

```text
sync-trigger.sh    container-mounted trigger; writes a job ID and returns
sync-upstream.sh   verifies a stable Release and prepares origin/integration/release-*
wait-for-actions.sh / verify-release-images.sh  Actions and image gates
promote-release.sh guarded remote-only branch promotion
sync-and-publish.sh durable host orchestrator
prepare-release.sh creates the immutable manifest, backup, and rollback evidence
apply-release.sh   validates and applies an approved prepared manifest
publish-custom.sh  deprecated fail-closed compatibility shim; never an entrypoint
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

The installed `/opt/sub2api-custom/` copy is a separate release artifact; a Git
checkout fast-forward does not update it. After a successful release whose
production diff includes `deploy/ops/`, wait until `sub2api-release.service` is
inactive, confirm `/root/sub2api` is clean at the ledger's current commit, rerun
the install commands above, reload systemd, and compare the installed files
with the deployed source before accepting another administrator trigger. Never
replace the installed script set while a release operation is active.

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

### Host Artifact Retention And Cleanup

Host cleanup is manual maintenance, not part of release apply and not a cron
job. It requires explicit operator authorization and must not change the
production commit, release projection, database volumes, or running container
set. Do not hardcode a "current" version in operating documents; read
`release-state.json`, the ledger state, both running OCI revisions, and the
container version output at the start of each operation.

Before changing installed scripts or deleting artifacts:

1. Acquire `/var/lock/sub2api-release.lock` without waiting. Stop if it is busy.
2. Require `release-ledger/state.json.active_operation_id` to be null, no
   prepared operation awaiting confirmation, `sub2api-release.service` to be
   inactive, and `/root/sub2api` to be clean at the ledger commit.
3. Back up `/opt/sub2api-custom/` and both systemd units under
   `release-host-backups/`, including a `SHA256SUMS` manifest. Reinstall from the
   deployed `deploy/ops/` source, reload systemd, run `bash -n`, and compare all
   installed scripts and units byte for byte. Preserve host-only files such as
   `health-monitor.sh`.

Use this retention contract:

| Artifact | Retention |
|---|---|
| `release-ledger/` and compatible legacy `release-jobs/` | Keep the complete audit history. |
| `release-backups/` | Keep the three backup directories referenced by the newest successful release records. Validate each retained `SHA256SUMS` before deleting an older directory. These directories are the three complete snapshots excluding current. |
| Current and rollback images | Keep the current main/extensions digest pair plus every pair and rollback tag recorded by the three retained backups. Also protect every image ID used by any running container. |
| `release-prepared/` | Never remove an active or confirmable manifest. With no active operation, keep the directories associated with the three retained successful release records. |
| `sync-conflicts/` | Keep the newest three diagnostic snapshots; the ledger remains the permanent operation audit. |
| `release-host-backups/` | Keep the newest three verified host-script backups. |
| `/root/backups/sub2api/` | Keep the newest three superseded manual backups plus every explicitly pinned, unique migration, or emergency backup. Delete only after confirming the retained release backups cover current rollback needs. |
| Legacy root `.prepared-update-*`, `sync-job-id`, `sync-status`, and `sync-result` | Remove only after the ledger path is active, no operation is running, and current code no longer reads them. |

Resolve every recursive deletion target with `realpath`, require it to be a
direct child of the intended root, and log the exact target before deletion.
For Docker, build a protected image-ID set from running containers, the current
digest pair, and all three rollback snapshots. Remove only unprotected Sub2API
or obsolete VPS-build images. Do not use `docker image prune -a`,
`docker system prune`, or any volume prune; images belonging to other
applications are outside this procedure.

After cleanup, revalidate all retained backup checksums and digest references,
render the explicit Compose pair with `config --quiet`, check the main,
extensions, PostgreSQL, Redis, and risk database containers, exercise public
health and homepage endpoints, and confirm the path unit is active while the
service is inactive. Record the UTC maintenance log path, retained counts,
removed counts, Docker storage, filesystem usage, and whether deleted material
is locally recoverable. Cleanup does not create a Git commit unless this
versioned policy or implementation changes.

### Release status contract

Versioned host scripts must persist only the canonical status values declared
by the backend `ReleaseStatus*` constants in
`backend/internal/service/update_job.go`:

```text
resolving_target resolving_snapshot verifying_snapshot verifying_images
downloading_images rendering_compose backing_up validating_backup prepared
apply_queued validating_manifest switching_extensions switching_main
health_checking rolling_back success failed conflict expired drifted
failed_rolled_back rollback_failed
```

Shell sub-steps such as checking the stable Release, validating its tag,
merging a candidate, or waiting for Actions belong in the operation message and
metadata. They must not introduce private status values. The deployment
contract tests compare `deploy/ops/release-state.sh` and literal script writes
with the backend constants.

Installing a newer script set does not rewrite an already-running or historical
operation. Let an operation created by older scripts reach a terminal state
before starting another update.

Production always uses an explicit Compose pair. The base
`deploy/docker-compose.yml` is the unmodified file from the recorded official
Stable Release. All production-only services, digest image substitutions,
mounts, environment, and the dedicated risk database volume live in
`deploy/docker-compose.custom.yml`. Never use an implicit
`docker-compose.override.yml`; every production `config`, `up`, and health
validation command supplies the base first and custom file second.

For the one-time migration from legacy locally built images, a missing
`release-state.json` makes the publisher record the clean local commit, current
container image IDs, the matching Compose pair/environment, and rollback tags in the release
backup. A healthy publish creates the first formal digest state. A failed
migration restores the backed-up Compose pair and local images and leaves formal state
absent; this bootstrap is not used after a healthy digest release.

When the current clean production commit predates
`docker-compose.custom.yml`, the publisher pairs its old base file with a
temporary empty overlay for current-state rendering and backup. The approved
target commit must contain the tracked custom overlay before backup or mutation
can proceed. This also supports a pre-overlay deployment that already has a
valid `release-state.json`.

`publish-custom.sh` always rejects direct use. For final acceptance, trigger
the same durable job used by the administrator UI and monitor its persisted
states.

## New Site And Full-Site Migration

Use only a clean Linux amd64 checkout whose `custom-release` HEAD exactly equals
`origin/custom-release`. A new host must have none of the target containers,
named volumes, installed release units, or production `.env`. Keep the input
secret file outside the checkout with mode `0600`, and run `--check-only`
before the real command:

```bash
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE --check-only
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE
```

This initializes the recorded Official Stable, Custom v1.0.0, and custom
high-water zero. It verifies the paired public GHCR images and explicit Compose
render, then starts PostgreSQL, Redis, risk-control PostgreSQL,
`extensions-self`, and `sub2api` in dependency order.

For migration, first export a healthy source site to a new absolute directory:

```bash
deploy/ops/export-custom-site.sh \
  --output /root/sub2api-site-export --confirm EXPORT-SITE
```

Transfer that directory using an operator-approved secure channel, clone the
same exact `custom-release` commit on the empty target, and run:

```bash
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION --check-only
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION
```

The export contains fresh validated dumps of both databases, `.env`, the
Compose pair, Nginx/TLS files, the full dual-version ledger, release backups,
runtime metadata, and exact checksums. Migration restores both databases before
application writers start and preserves the historical display and high-water.
Neither command changes DNS/CDN. Review and activate Nginx/TLS routing
separately. Later releases use the normal administrator update button; prepared
update and rollback manifests require explicit apply within one hour.

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
/root/sub2api/deploy/docker-compose.custom.yml
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
  -f /root/sub2api/deploy/docker-compose.custom.yml \
  --env-file /root/sub2api/deploy/.env \
  config --quiet
```

Use the same pair with `config --format json` when inspecting effective
services, images, mounts, environment, networks, and volumes. Compose mapping
keys are overridden by the custom file, while lists such as mounts are merged;
the rendered configuration, not either source file alone, is the runtime
contract.

Keep the main application in shadow/review mode until registration events,
user identity, risk reason, action records, and administrator visibility have
been verified with real traffic.

## Account Monitor Release

The account monitor collector and admin API are disabled unless
`ACCOUNT_MONITOR_ENABLED=true`. `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` is also
used by the public homepage live-rate reader and is therefore required even
when collection is disabled if that homepage is enabled. The source DSN must
use the dedicated `extensions_self_monitor` login on the main `postgres`
service; the login only inherits `extensions_self_monitor_ro` and must never
be the main DB owner.

For the first enabled release, use this order:

1. Record the approved commit and current image IDs.
2. Back up and verify the main database and `risk-control-postgres`; back up both Compose files,
   `.env`, Nginx vhost, origin certificate/key, container/image metadata and rollback tags.
3. Run `deploy/ops/install-account-monitor-source.sql` as the main DB owner for
   initial installation or manual repair. Normal release apply automatically
   reapplies the equivalent `main_source_views.sql` in one transaction after
   backup and before switching the extensions container.
4. Verify the NOLOGIN role and TCP login can read `extensions_self_ro.usage_source`,
   `extensions_self_ro.group_dimension`, `extensions_self_ro.account_group_dimension`,
   `extensions_self_ro.account_dimension.account_identity`, and
   `extensions_self_ro.public_group_catalog`,
   while full keys and credentials are denied.
5. Verify the paired GHCR digests and recreate only `extensions-self`, then `sub2api`.
6. Verify `/admin/extensions/account-monitor`, `/admin/extensions/group-monitor`,
   signed `data-quality`, risk pages, the custom homepage, and its public-groups
   endpoint. The apply health gate must fail if the live-rate endpoint is unavailable.
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

Normal release apply refreshes `main_source_views.sql` automatically and must
be used for every release that changes these views. Run the full
`deploy/ops/install-account-monitor-source.sql` only for initial installation
or an explicit manual repair, and keep its password argument synchronized with
the deployed source DSN. After the release, run the allow probes with the
dedicated source login; all five must succeed:

```sql
SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1;
SELECT 1 FROM extensions_self_ro.group_dimension LIMIT 1;
SELECT 1 FROM extensions_self_ro.account_group_dimension LIMIT 1;
SELECT account_identity FROM extensions_self_ro.account_dimension LIMIT 1;
SELECT 1 FROM extensions_self_ro.public_group_catalog LIMIT 1;
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
filters, search account-monitor rows by both configured name and actual account
`account_identity`, and use the “手动刷新” button on account and group monitor. Exercise
input/select filtering on user risk pages. When the page is idle, the network
log must show no monitor polling request. Account detail uses five tabs, a
fixed-height scrolling body, sticky table headers, stays open across queries,
and closes when its backdrop is clicked.

Homepage acceptance must call
`/api/v1/extensions-self/homepage/api/public-groups`, verify `Cache-Control: no-store`,
and reconcile every returned row with `extensions_self_ro.public_group_catalog`. Confirm
the page uses the configured site name, subtitle and logo, and that no raw account,
subscription or credential field appears in the response.

## Verification Checklist

- [ ] Git worktree is clean after commit.
- [ ] Intended commit and both GHCR digests are recorded.
- [ ] `sub2api` container is healthy.
- [ ] `extensions-self` container is healthy.
- [ ] Public extensions homepage returns success, uses configured branding, and its `/api/v1/extensions-self/homepage/api/public-groups` request returns only the live public catalog.
- [ ] Native `/admin/extensions/account-monitor` and `/admin/extensions/group-monitor` load.
- [ ] `extensions_self_ro.account_group_dimension`, `account_dimension.account_identity`, and `extensions_self_ro.public_group_catalog` allow probes and raw key/credential deny probes passed.
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
The additive `extensions_self_ro.public_group_catalog` view and appended
`account_identity` column remain in place; do not drop or replace them with raw-table grants.
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

## Two-Phase Update Contract

The administrator update action is deliberately split into two durable jobs on
the same `job_id`:

```text
check-updates (read-only)
  -> prepare: Actions + paired GHCR digests + explicit Compose render + backups
  -> prepared (60-minute expiry, immutable manifest)
  -> administrator confirmation
  -> apply: local drift gate + --pull never + extensions-self -> sub2api
  -> health checks -> atomic release-state.json
```

`prepare-release.sh` may fetch and verify the locked target, but it never moves
the production checkout, writes `.env` or `release-state.json`, or runs Compose
`up`, `down`, `rm`, or `restart`. It backs up both databases, the matching
Compose pair, environment, Nginx/certificates, old digests and metadata, then
stores `release-prepared/<job-id>/manifest.json` and its SHA256. The manifest
contains the production and target commits, Stable identity, both immutable
digests, Compose and environment hashes, backup directory, `prepared_at`, and
`expires_at`. A retry after expiry may reuse verified image evidence but must
create a fresh backup and manifest.

`apply-release.sh` accepts only a prepared manifest. It does not contact GitHub,
wait for Actions, pull images, or repeat database backups. It refuses to run on
production/origin/Compose/.env/digest/backup drift or an expired/corrupt
manifest. Every lifecycle command uses the explicit Compose pair and
`--pull never`; PostgreSQL, Redis, and `risk-control-postgres` are never
recreated. A failed extension, main, or health step restores the old local
digest pair and matching prepared Compose/backup evidence before reporting
failure. `publish-custom.sh` is a deprecated fail-closed shim and is not a
release entry point.

The `/admin/system/update` endpoint remains a prepare-only compatibility alias.
Legacy single-phase jobs are rejected with
`LEGACY_SINGLE_PHASE_UNSUPPORTED`. Opening the admin version popup only shows
current detection or an active/prepared durable job; a previous success,
failure, or rollback result is historical and is not replayed.
