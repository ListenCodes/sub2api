# Extensions Center, Account Risk Score, and Group Monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver one native Vue extensions center containing user risk control, account monitoring, and group monitoring, backed by exact account risk scores and shared final-request facts with `group_id`, then validate, merge, publish, backfill, and reconcile production.

**Architecture:** Keep `extensions-self` as the only custom application container and `risk-control-postgres` as the only extension database. Extend the existing safe-source collector and final-request fact once, derive account health and group/model 10-minute aggregates from that shared fact, expose both through the existing signed administrator proxy, and render the results in native Vue under one parent route without an iframe or account-monitor static asset server.

**Tech Stack:** Go 1.24, PostgreSQL, Gin, Vue 3 Composition API, TypeScript, Vue Router, Vitest, Docker Compose, Node contract tests, in-app Browser QA, `ssh-skill` for production operations.

---

## Approved Inputs and Non-Negotiable Boundaries

- Design baseline: `a2ceec31` and `docs/superpowers/specs/2026-07-15-extensions-center-account-group-monitor-design.md`.
- Existing account-monitor facts, aggregates, thresholds, rebuild jobs, and signed admin API remain compatible.
- `risk-control-postgres` is never recreated or removed during publish or rollback.
- Main PostgreSQL is read only through `extensions_self_ro` and the `extensions_self_monitor` login.
- No iframe, no account-monitor static page, no `ServeWeb`, no account-monitor web directory setting, and no security-header exception for account monitor survive the implementation.
- A user final request is counted once. `actual_cost > 0` is success, zero-cost usage is a failure placeholder and is not counted independently, success overrides prior failures, and the last final failure wins when no success exists.
- The group dimension is the business `groups.id` stored as `group_id`; soft-deleted groups never reappear in cards.
- Production can only be published with `/opt/sub2api-custom/publish-custom.sh --commit <approved-origin-custom-commit>` after all gates pass.

## File and Responsibility Map

### Shared source, facts, aggregates, and APIs

- Modify `extensions-self/account-monitor/sql/main_source_views.sql`: expose `group_id` on usage/error sources and add the limited `group_dimension` view.
- Modify `deploy/ops/install-account-monitor-source.sql`: keep the role/login idempotent and grant the new view through the existing group role.
- Modify `extensions-self/account-monitor/model.go`: add group identity and quality fields to source rows and final request facts; add group dimension, bucket, summary, and API response types.
- Modify `extensions-self/account-monitor/source.go`: read `group_id` and group dimensions with query timeouts and stable cursor semantics.
- Modify `extensions-self/account-monitor/normalizer.go`: preserve the winning final result's group/model and fallback identity quality.
- Modify `extensions-self/account-monitor/schema.sql`: migrate request facts, create the dimension mirror and group/model 10-minute aggregate with indexes and retention support.
- Modify `extensions-self/account-monitor/repository.go`: upsert group-bearing request facts and dimensions transactionally, clean retained group aggregates, and support rebuild/backfill.
- Modify `extensions-self/account-monitor/aggregate.go`: refresh complete UTC 10-minute buckets idempotently from final request facts.
- Modify `extensions-self/account-monitor/collector.go`: synchronize dimensions without deleting the last good mirror, refresh affected complete buckets, and publish data-quality counters.
- Modify `extensions-self/account-monitor/anomaly.go`: calculate deterministic 0-100 risk scores and ordered contributions from resolved thresholds.
- Modify `extensions-self/account-monitor/admin_backend.go`: batch-score filtered account candidates before stable sorting/paging and query group card/detail aggregates.
- Modify `extensions-self/account-monitor/http.go`: keep signed JSON APIs, add group-monitor endpoints, and remove static file serving.

### Main application proxy and request attribution

- Modify `backend/internal/service/ops_upstream_context.go`: confirm each upstream error event carries the actual upstream model without exposing unsafe fields.
- Modify `backend/internal/service/risk_control_client.go`: retain signed JSON proxying and delete the account-monitor asset proxy.
- Modify `backend/internal/handler/admin/user_risk_control_proxy.go` and `backend/internal/server/routes/admin.go`: allowlist group-monitor JSON routes under the authenticated admin proxy.
- Modify `backend/internal/server/routes/account_monitor_routes_test.go` and `backend/internal/handler/admin/account_monitor_proxy_test.go`: prove authentication, compliance, signature, method, size, and path constraints.
- Restore `backend/internal/server/middleware/security_headers.go` and `security_headers_test.go` to the approved homepage-only framing behavior before implementation commits.

### Native Vue extensions center

- Create `frontend/src/views/admin/ExtensionsCenterView.vue`: the single `AppLayout` owner and first-level tabs.
- Create `frontend/src/views/admin/extensions/UserRiskControlPanel.vue`: second-level router tabs and child route outlet for the three existing risk pages.
- Refactor `frontend/src/views/admin/UserRiskControlUsersView.vue`, `UserRiskControlRulesView.vue`, and `UserRiskControlAuditView.vue`: remove their outer `AppLayout`, preserve behavior, synchronize filters/sort/page to query, and use the shared risk badge.
- Replace `frontend/src/views/admin/AccountMonitorView.vue`: compatibility wrapper/route target that renders the native account monitor panel.
- Create `frontend/src/views/admin/account-monitor/AccountMonitorPanel.vue`, `AccountMonitorOverview.vue`, `AccountMonitorFilters.vue`, `AccountMonitorTable.vue`, `AccountMonitorDrawer.vue`, `AccountMonitorThresholdDialog.vue`, and `AccountMonitorRebuildDialog.vue`.
- Create `frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts`: typed URL state, request cancellation, 60-second refresh, selection/drawer preservation, and cleanup.
- Create `frontend/src/views/admin/group-monitor/GroupMonitorPanel.vue`, `GroupMonitorFilters.vue`, `GroupMonitorCard.vue`, `GroupMonitorTimeline.vue`, and `GroupMonitorDetailDialog.vue`.
- Create `frontend/src/components/admin/RiskScoreBadge.vue`: shared score direction, level labels, colors, boundaries, and unavailable state.
- Create `frontend/src/api/admin/accountMonitor.ts`: typed account and group monitor client using the existing main `apiClient` admin proxy.
- Modify `frontend/src/router/index.ts` and `frontend/src/components/layout/AppSidebar.vue`: one extensions-center menu, nested native routes, and query-preserving compatibility redirects.

### Remove obsolete account-monitor static delivery

- Delete `extensions-self/account-monitor/web/index.html`, `app.js`, and `styles.css`.
- Delete `extensions-self/account-monitor/web_contract_test.go`.
- Modify `extensions-self/risk-control/http.go`, `account_monitor_runtime.go`, `main.go`, and their tests to remove `ServeWeb` and `WebDir` wiring while retaining account-monitor JSON APIs.
- Modify `extensions-self/Dockerfile`, `deploy/docker-compose.yml`, and `deploy/tests/account-monitor-contract.test.mjs` to remove the web copy/environment/health contract.

### Documentation and release operations

- Modify `AGENTS.md`, `deploy/RELEASE-RUNBOOK.md`, `deploy/README.md`, `deploy/ops/README.md`, `extensions-self/README.md`, `extensions-self/account-monitor/README.md`, `docs/EXTENSIONS-SELF-ARCHITECTURE.md`, `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`, and `docs/ACCOUNT-MONITOR-CHECKLIST.md`.
- Modify `deploy/ops/publish-custom.sh`: verify the new safe views, group aggregates, native admin API health, backups, image/commit metadata, and rollback targets without referencing static account-monitor assets.
- Add or modify deploy contract tests so release ordering, backups, Compose rendering, permissions, backfill commands, and rollback guarantees are executable assertions.

## Task 1: Remove the Uncommitted iframe Header Experiment and Establish the Baseline

**Files:**
- Restore: `backend/internal/server/middleware/security_headers.go`
- Restore: `backend/internal/server/middleware/security_headers_test.go`

- [ ] **Step 1: Record the two-file experiment diff**

Run:

```powershell
git diff -- backend/internal/server/middleware/security_headers.go backend/internal/server/middleware/security_headers_test.go
```

Expected: only `isExtensionsHomepageRoute` being broadened to account monitor plus two account-monitor framing tests.

- [ ] **Step 2: Restore only those two paths from `HEAD`**

Use `apply_patch` to reverse the recorded hunks. Do not use checkout/reset and do not touch any other path.

- [ ] **Step 3: Prove the worktree is clean and the approved design remains**

Run:

```powershell
git status --short --branch
git show --stat --oneline a2ceec31
```

Expected: clean `feature/account-monitor-20260715`; design document remains committed at `a2ceec31`.

- [ ] **Step 4: Run the focused middleware baseline**

Run:

```powershell
go test ./internal/server/middleware -run TestSecurityHeaders -count=1
```

from `backend`. Expected: PASS with homepage framing allowed and account-monitor framing denied.

## Task 2: Extend the Safe Source Contract with `group_id` and Group Dimensions

**Files:**
- Modify: `extensions-self/account-monitor/sql/main_source_views.sql`
- Modify: `deploy/ops/install-account-monitor-source.sql`
- Modify: `extensions-self/account-monitor/model.go`
- Modify: `extensions-self/account-monitor/source.go`
- Test: `extensions-self/account-monitor/source_test.go`
- Test: `extensions-self/account-monitor/source_reader_test.go`
- Test: `deploy/tests/account-monitor-contract.test.mjs`

- [ ] **Step 1: Write failing safe-view and source-reader tests**

Add assertions that both `usage_source` and `error_source` select `group_id`; `group_dimension` exposes exactly `id,name,platform,status,deleted_at`; the reader scans nullable group IDs; and no credential/key/body/header field appears in the view SQL.

Run:

```powershell
go test ./... -run "Source|SafeView|GroupDimension" -count=1
node --test deploy/tests/account-monitor-contract.test.mjs
```

Expected: FAIL because `group_id`, `group_dimension`, and their reader methods do not exist.

- [ ] **Step 2: Add the minimal safe SQL contract**

In `usage_source`, select `usage_logs.group_id`. In `error_source`, select `ops_error_logs.group_id`. Add:

```sql
CREATE OR REPLACE VIEW extensions_self_ro.group_dimension AS
SELECT id, name, platform, status, deleted_at
FROM public.groups;
```

Keep all grants on `extensions_self_monitor_ro`; do not grant base-table access.

- [ ] **Step 3: Add typed source rows and dimension reads**

Use nullable `GroupID *int64` on source/fact types and a `GroupDimension` with `ID`, `Name`, `Platform`, `Status`, `DeletedAt`, and `SyncedAt`. Query dimensions with `ORDER BY id` and the existing query timeout.

- [ ] **Step 4: Run the focused tests and security scans**

Run:

```powershell
go test ./... -run "Source|SafeView|GroupDimension" -count=1
node --test deploy/tests/account-monitor-contract.test.mjs
rg -n "credentials|api_keys\.key|request_body|request_headers|oauth|cookie" extensions-self/account-monitor/sql/main_source_views.sql
```

Expected: tests PASS; sensitive names occur only in negative/permission assertions, never projected by the safe views.

- [ ] **Step 5: Commit the safe-source contract**

```powershell
git add extensions-self/account-monitor/sql/main_source_views.sql deploy/ops/install-account-monitor-source.sql extensions-self/account-monitor/model.go extensions-self/account-monitor/source.go extensions-self/account-monitor/source_test.go extensions-self/account-monitor/source_reader_test.go deploy/tests/account-monitor-contract.test.mjs
git commit -m "feat: expose safe group monitoring sources"
```

## Task 3: Persist Shared Final-Request Group Facts and the Dimension Mirror

**Files:**
- Modify: `extensions-self/account-monitor/schema.sql`
- Modify: `extensions-self/account-monitor/model.go`
- Modify: `extensions-self/account-monitor/normalizer.go`
- Modify: `extensions-self/account-monitor/repository.go`
- Modify: `extensions-self/account-monitor/collector.go`
- Test: `extensions-self/account-monitor/schema_test.go`
- Test: `extensions-self/account-monitor/normalizer_test.go`
- Test: `extensions-self/account-monitor/repository_test.go`
- Test: `extensions-self/account-monitor/collector_test.go`

- [ ] **Step 1: Write failing normalization and migration tests**

Cover: success carries its `group_id`; success overrides earlier failed group/model; final failure uses the latest `(created_at, source_id)` group/model; fallback request identity retains `identity_quality=fallback`; repeated upserts update group/model rather than duplicate the request; dimension sync upserts active/inactive rows and preserves the prior mirror on source failure.

Run:

```powershell
go test ./... -run "GroupID|FinalRequest|GroupDimension|Schema" -count=1
```

Expected: FAIL on missing columns/types/upsert statements.

- [ ] **Step 2: Add idempotent schema migration version 2**

Add nullable `group_id` to `account_monitor_request_facts`, indexes on `(group_id, occurred_at)` and `(occurred_at, group_id)`, and:

```sql
CREATE TABLE IF NOT EXISTS account_monitor_group_dimensions (
  group_id BIGINT PRIMARY KEY,
  name TEXT NOT NULL,
  platform TEXT NOT NULL,
  status TEXT NOT NULL,
  deleted_at TIMESTAMPTZ,
  synced_at TIMESTAMPTZ NOT NULL
);
```

Record migration version `2` with `ON CONFLICT DO NOTHING` so startup is idempotent.

- [ ] **Step 3: Implement winner-preserving request upserts**

Pass group/model/attribution through `Normalize`; keep one `request:<api_key_id>:<request_id>` row; let a valid success overwrite a failure; otherwise compare source order and retain the last final failure. Preserve `identity_quality` and do not generate group IDs for ungrouped requests.

- [ ] **Step 4: Synchronize dimensions transactionally**

Upsert the complete safe dimension snapshot with `synced_at`, including inactive and soft-deleted rows. Do not delete the mirror when the source read fails; API visibility filters soft-deleted rows later.

- [ ] **Step 5: Run the focused suite and commit**

```powershell
go test ./... -run "GroupID|FinalRequest|GroupDimension|Schema|Collector|Repository" -count=1
git add extensions-self/account-monitor
git commit -m "feat: persist grouped final request facts"
```

Expected: PASS; duplicate collection does not increase final-request counts.

## Task 4: Build Idempotent 10-Minute Group/Model Aggregates, Retention, and Backfill

**Files:**
- Modify: `extensions-self/account-monitor/schema.sql`
- Modify: `extensions-self/account-monitor/aggregate.go`
- Modify: `extensions-self/account-monitor/repository.go`
- Modify: `extensions-self/account-monitor/collector.go`
- Modify: `extensions-self/account-monitor/rebuild_test.go`
- Test: `extensions-self/account-monitor/aggregate_test.go`
- Test: `extensions-self/account-monitor/retention_test.go`
- Create: `extensions-self/account-monitor/group_aggregate_test.go`

- [ ] **Step 1: Write failing bucket, attribution, retention, and rebuild tests**

Test UTC flooring to 10 minutes, exclusion of the current incomplete bucket, unique `(bucket_at,group_id,actual_model)` rows, exact/estimated counters in one model row, `total=successes+failures`, idempotent reruns, late data replacing affected buckets, 90-day cleanup, and segmented rebuild ranges that never exceed 31 days.

Run:

```powershell
go test ./... -run "GroupAggregate|TenMinute|Retention|Rebuild" -count=1
```

Expected: FAIL because the table and refresh path do not exist.

- [ ] **Step 2: Add the aggregate schema**

Create `account_monitor_group_model_10m` with `bucket_at`, `group_id`, `actual_model`, `total_requests`, `successes`, `failures`, `exact_model_requests`, `estimated_model_requests`, a composite primary key, and range indexes for group/time queries.

- [ ] **Step 3: Implement complete-bucket refresh**

For the affected range, floor start/end to UTC 10-minute boundaries, cap end at the current boundary, delete existing aggregate rows within the transaction, and insert grouped counts from final request facts where `group_id IS NOT NULL`. Use the final model fallback order and `未知实际模型` only when all model fields are absent.

- [ ] **Step 4: Extend cleanup and rebuild**

Delete group aggregates older than 90 days. Rebuild group facts and aggregates with the existing advisory lock and job progress fields, keeping old aggregates until each transactional segment succeeds.

- [ ] **Step 5: Run aggregate/rebuild tests and commit**

```powershell
go test ./... -run "Aggregate|GroupAggregate|TenMinute|Retention|Rebuild" -count=1
git add extensions-self/account-monitor
git commit -m "feat: aggregate group model health windows"
```

## Task 5: Implement the Exact Account Risk Score and Stable Account Paging

**Files:**
- Modify: `extensions-self/account-monitor/model.go`
- Modify: `extensions-self/account-monitor/anomaly.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Test: `extensions-self/account-monitor/anomaly_test.go`
- Test: `extensions-self/account-monitor/admin_backend_test.go`

- [ ] **Step 1: Write table-driven failing formula tests**

Cover every signal at trigger, growth, cap, cumulative cap 100, `math.Round` half-away-from-zero behavior, 19/20, 39/40, 69/70 levels, stable equal-contribution order, and unavailable data returning score 0 with `risk_score_available=false`.

Use these contribution functions in test expectations:

```go
auth := min(90, 70+5*(count-threshold))
successRate := min(60, 40+round(20*(threshold-rate)/threshold))
consecutive := min(60, 40+4*(count-threshold))
throttle := min(35, 20+round(15*(ratio-threshold)/(1-threshold)))
```

Run:

```powershell
go test ./... -run "RiskScore|RiskLevel|ReasonOrder" -count=1
```

Expected: FAIL because health returns only the existing threshold level/reasons.

- [ ] **Step 2: Implement one resolved-signal evaluation**

Return `RiskScore`, `RiskScoreAvailable`, `Level`, and reasons derived from the same resolved thresholds and signal booleans. Add the fixed 25/20/20/15 contributions only once per signal, sort reasons by contribution descending and then the fixed signal order, and clamp the sum to 100.

- [ ] **Step 3: Write failing batch sort/filter/overview tests**

Test `min_risk_score`, `max_risk_score`, `sort_by=risk_score`, unavailable-last in both directions, final `account_id ASC` tie-break, scoring before page slicing, `average_risk_score` over available scores only, and `high_risk_accounts` for 70-100.

- [ ] **Step 4: Implement bounded batch scoring**

Fetch all business-filtered candidate account IDs and aggregate signal inputs in bounded set queries, resolve thresholds by scope in bulk, score once per candidate, then filter/sort/page in memory. Do not issue a query per account and do not score only the SQL page.

- [ ] **Step 5: Run focused and full module tests; commit**

```powershell
go test ./... -run "RiskScore|RiskLevel|ReasonOrder|AccountList|Overview" -count=1
go test ./...
git add extensions-self/account-monitor
git commit -m "feat: calculate explainable account risk scores"
```

## Task 6: Add Group Monitor JSON APIs and the Authenticated Main Proxy

**Files:**
- Modify: `extensions-self/account-monitor/model.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/http.go`
- Modify: `backend/internal/service/risk_control_client.go`
- Modify: `backend/internal/handler/admin/user_risk_control_proxy.go`
- Modify: `backend/internal/server/routes/admin.go`
- Test: `extensions-self/account-monitor/admin_backend_test.go`
- Test: `extensions-self/account-monitor/http_test.go`
- Test: `backend/internal/handler/admin/account_monitor_proxy_test.go`
- Test: `backend/internal/server/routes/account_monitor_routes_test.go`

- [ ] **Step 1: Write failing repository/API tests for group cards**

Cover `range=1h|6h|12h|24h`, active default, all non-deleted groups, platform/name/status/call-status filters, page sizes 12/24/48, stable `LOWER(platform),LOWER(name),id` ordering, complete-bucket timelines, the five card states, and data-quality metadata.

- [ ] **Step 2: Write failing detail and boundary tests**

Cover model-name ordering, exact/estimated counts, 24-hour bucket completeness, inactive-after-open status, soft-delete returning not found, invalid range/page size rejection, query timeout, no request/error detail in JSON, and cancellation.

- [ ] **Step 3: Implement signed extension handlers**

Add:

```text
GET /api/v1/admin/account-monitor/group-monitor/groups
GET /api/v1/admin/account-monitor/group-monitor/groups/:id
```

Keep existing signature, actor, body-size, and timeout middleware. Summarize cards from the 10-minute table and dimensions; never expose raw facts.

- [ ] **Step 4: Extend the main admin proxy allowlist**

Expose only `/api/v1/admin/extensions-self/account-monitor/group-monitor/groups` and numeric group detail with GET. Prove unauthenticated/non-admin/compliance-locked calls are rejected, signatures are generated server-side, traversal and unsupported methods fail, and JSON size limits remain.

- [ ] **Step 5: Run both Go suites and commit**

```powershell
go test ./...
go test ./internal/handler/admin ./internal/server/routes ./internal/service -run "AccountMonitor|GroupMonitor" -count=1
git add extensions-self/account-monitor backend/internal/service/risk_control_client.go backend/internal/handler/admin backend/internal/server/routes
git commit -m "feat: expose authenticated group monitor APIs"
```

Run the first command from `extensions-self/account-monitor` and the second from `backend`.

## Task 7: Create the Shared Risk Badge and the Native Extensions Center Routes

**Files:**
- Create: `frontend/src/components/admin/RiskScoreBadge.vue`
- Create: `frontend/src/components/admin/__tests__/RiskScoreBadge.spec.ts`
- Create: `frontend/src/views/admin/ExtensionsCenterView.vue`
- Create: `frontend/src/views/admin/extensions/UserRiskControlPanel.vue`
- Modify: `frontend/src/views/admin/UserRiskControlUsersView.vue`
- Modify: `frontend/src/views/admin/UserRiskControlRulesView.vue`
- Modify: `frontend/src/views/admin/UserRiskControlAuditView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Test: `frontend/src/__tests__/integration/navigation.spec.ts`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts`

- [ ] **Step 1: Write failing badge tests**

Assert 0-19 normal, 20-39 attention, 40-69 abnormal, 70-100 critical, user-risk explicit level compatibility, and unavailable rendering `暂无评分` without a zero-normal label.

- [ ] **Step 2: Implement `RiskScoreBadge`**

Accept `score`, `available`, and optional explicit user-risk level; centralize Chinese labels and restrained semantic colors; expose an accessible label such as `风险分 72，严重`.

- [ ] **Step 3: Write failing route/menu/query tests**

Assert one sidebar item `扩展中心`; nested routes under `/admin/extensions/...`; first-level and user-risk second-level tabs; old user-risk/account-monitor routes preserving all query entries; no separate account-monitor or three risk menu items.

- [ ] **Step 4: Implement the parent route and refactor shells**

Make `ExtensionsCenterView` the only `AppLayout` owner. Use router links/tabs for first and second levels. Remove the three child pages' outer `AppLayout`, retain their data actions, and synchronize filter/sort/page state with `route.query` using `router.replace` for automatic changes and `router.push` for explicit tab navigation.

- [ ] **Step 5: Use the badge in user risk and run tests**

```powershell
pnpm --dir frontend exec vitest run src/components/admin/__tests__/RiskScoreBadge.spec.ts src/views/admin/__tests__/UserRiskControlUsersView.spec.ts src/__tests__/integration/navigation.spec.ts
git add frontend/src
git commit -m "feat: add native extensions center navigation"
```

## Task 8: Replace the Account Monitor iframe with Native Vue

**Files:**
- Replace: `frontend/src/views/admin/AccountMonitorView.vue`
- Create: `frontend/src/api/admin/accountMonitor.ts`
- Create: `frontend/src/api/admin/__tests__/accountMonitor.spec.ts`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorPanel.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorOverview.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorFilters.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorTable.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorDrawer.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorThresholdDialog.vue`
- Create: `frontend/src/views/admin/account-monitor/AccountMonitorRebuildDialog.vue`
- Create: `frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts`
- Replace tests: `frontend/src/views/admin/__tests__/AccountMonitorView.spec.ts`
- Create tests under: `frontend/src/views/admin/account-monitor/__tests__/`

- [ ] **Step 1: Write failing API and URL-state tests**

Define typed responses for overview/accounts/details/models/users/errors/trends/attempts/data-quality/thresholds/rebuild and group APIs. Assert query serialization, cancellation, 401/423 propagation, page-size/range validation, and exact restoration of filters/sort/risk range/page/tab from the URL.

- [ ] **Step 2: Implement the typed API and filter composable**

Use the existing `apiClient`; create one abort controller per request family; cancel superseded requests; expose `refresh`, `setFilters`, `resetFilters`, `setPage`, and `dispose`; refresh every 60 seconds without changing route state.

- [ ] **Step 3: Write failing panel/table/drawer tests**

Cover overview and data quality, service-side risk sorting/filtering, stable paging, empty/unavailable/error states, manual refresh, preserved selected account, all six detail tabs, tab-local retry, mobile full-screen drawer, threshold input retention on failure, and rebuild progress/error fields.

- [ ] **Step 4: Implement the native components with existing primitives**

Compose `TablePageLayout`, `DataTable`, `Pagination`, `DateRangePicker`, `Select`, `SearchInput`, `Toggle`, `BaseDialog`, `EmptyState`, `Icon`, and `RiskScoreBadge`. Keep page-level horizontal overflow absent and confine dense tables/timelines to their own intentional scroll container.

- [ ] **Step 5: Remove iframe behavior tests and run the native suite**

```powershell
pnpm --dir frontend exec vitest run src/api/admin/__tests__/accountMonitor.spec.ts src/views/admin/__tests__/AccountMonitorView.spec.ts src/views/admin/account-monitor/__tests__
git add frontend/src
git commit -m "feat: render account monitoring natively"
```

Expected: no `iframe`, `frameKey`, `window.open`, or `/api/v1/extensions-self/account-monitor/` static URL in frontend source.

## Task 9: Implement Native Group Monitoring Cards and Model Detail

**Files:**
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Create: `frontend/src/views/admin/group-monitor/GroupMonitorPanel.vue`
- Create: `frontend/src/views/admin/group-monitor/GroupMonitorFilters.vue`
- Create: `frontend/src/views/admin/group-monitor/GroupMonitorCard.vue`
- Create: `frontend/src/views/admin/group-monitor/GroupMonitorTimeline.vue`
- Create: `frontend/src/views/admin/group-monitor/GroupMonitorDetailDialog.vue`
- Create tests under: `frontend/src/views/admin/group-monitor/__tests__/`

- [ ] **Step 1: Write failing card/filter/paging tests**

Cover platform, name, active/inactive/all, call status, 1/6/12/24 hours, page sizes 12/24/48, deterministic server order, one-to-four responsive columns, volume-height bars, state colors, no-data versus recently-idle, and query restoration.

- [ ] **Step 2: Implement filters, cards, and independent list errors**

Render only group name, platform, dimension status, current call status, range success rate, and aggregate timeline. Preserve the last successful cards and timestamp on refresh error; never display request/error text or account identity.

- [ ] **Step 3: Write failing detail-dialog tests**

Cover summary totals, actual model count, alphabetical model rows, success/total cells, pointer/focus bucket details, fixed model column and time header, 24-hour horizontal scrolling, mobile full-screen mode, inactive state change, soft-delete close-and-refresh, and independent detail errors.

- [ ] **Step 4: Implement detail and refresh lifecycle**

Keep the open group ID across automatic refresh; cancel detail requests when the dialog closes or route changes; clear the interval and abort all requests on unmount.

- [ ] **Step 5: Run group UI tests and commit**

```powershell
pnpm --dir frontend exec vitest run src/views/admin/group-monitor/__tests__ src/api/admin/__tests__/accountMonitor.spec.ts
git add frontend/src
git commit -m "feat: add native group monitoring"
```

## Task 10: Delete Static Account Monitor Delivery and Update Runtime Contracts

**Files:**
- Delete: `extensions-self/account-monitor/web/index.html`
- Delete: `extensions-self/account-monitor/web/app.js`
- Delete: `extensions-self/account-monitor/web/styles.css`
- Delete: `extensions-self/account-monitor/web_contract_test.go`
- Modify: `extensions-self/account-monitor/http.go`
- Modify: `extensions-self/account-monitor/config.go`
- Modify: `extensions-self/risk-control/http.go`
- Modify: `extensions-self/risk-control/account_monitor_runtime.go`
- Modify: `extensions-self/risk-control/main.go`
- Modify: `extensions-self/risk-control/account_monitor_test.go`
- Modify: `extensions-self/risk-control/account_monitor_docker_test.go`
- Modify: `extensions-self/Dockerfile`
- Modify: `deploy/docker-compose.yml`
- Modify: `backend/internal/service/risk_control_client.go`
- Modify: `backend/internal/service/risk_control_client_homepage_test.go`
- Modify: `backend/internal/server/middleware/security_headers_test.go`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`

- [ ] **Step 1: Invert static/iframe contract tests so they fail**

Assert the Dockerfile does not copy `account-monitor/web`, Compose has no `EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR`, the Go runtime has no `ServeWeb`/`WebDir`, the main service has no account-monitor asset proxy, account-monitor remains frame-denied, and signed admin JSON APIs still exist.

- [ ] **Step 2: Run the red contract tests**

```powershell
go test ./... -run "AccountMonitor|Homepage" -count=1
go test ./internal/service ./internal/server/middleware -run "AccountMonitor|Homepage|SecurityHeaders" -count=1
node --test deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs
```

Expected: FAIL on the current web copy, web directory environment, static proxy, and `ServeWeb` route.

- [ ] **Step 3: Remove only obsolete static delivery**

Delete the web files and wiring, keep `/homepage/` static serving unchanged, keep account-monitor signed JSON routes unchanged, and keep the homepage-only same-origin framing exception.

- [ ] **Step 4: Run the focused runtime/contract suites**

```powershell
go test ./... -run "AccountMonitor|Homepage" -count=1
go test ./internal/service ./internal/server/middleware -run "AccountMonitor|Homepage|SecurityHeaders" -count=1
node --test deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs
rg -n "account-monitor/web|EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR|ServeWeb|<iframe|frameKey|window\.open" extensions-self backend frontend deploy
```

Expected: tests PASS; the scan has no obsolete account-monitor static/iframe results.

- [ ] **Step 5: Commit the removal**

```powershell
git add -A extensions-self backend frontend deploy
git commit -m "refactor: remove account monitor iframe delivery"
```

## Task 11: Update Data Quality, Documentation, Publisher, and Backfill Contracts

**Files:**
- Modify: `extensions-self/account-monitor/model.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `deploy/ops/publish-custom.sh`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`
- Modify: `AGENTS.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/README.md`
- Modify: `deploy/ops/README.md`
- Modify: `extensions-self/README.md`
- Modify: `extensions-self/account-monitor/README.md`
- Modify: `docs/EXTENSIONS-SELF-ARCHITECTURE.md`
- Modify: `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`
- Modify: `docs/ACCOUNT-MONITOR-CHECKLIST.md`
- Modify: `E:/BaiduSyncdisk/Private/VPS/AGENTS.md` only after repository changes are finalized and without committing credentials.

- [ ] **Step 1: Write failing data-quality and deploy contract tests**

Assert both account/group APIs return `data_as_of`, lag, usage/error cursor state, recent source error, missing-group final-request count, exact/estimated model counts, available historical range, and stale-data warning. Assert the publisher backs up both databases, Compose, `.env`, Nginx, certificate files, and container/image metadata before installing views/building; validates `group_dimension`; records backfill range and rollback images; and never recreates the risk database.

- [ ] **Step 2: Implement quality reporting and segmented backfill commands**

Expose one shared quality snapshot. Add a documented operator command that submits non-overlapping rebuild segments of at most 31 days, polls each job, stops on failure, and records processed rows and the source-available interval. Do not synthesize missing history or zero buckets.

- [ ] **Step 3: Replace old iframe/static documentation**

Document native routes, extensions-center navigation, shared final-request facts, `group_id`, risk formula and unavailable state, dimension mirror, 10-minute aggregation, retention, data quality, backfill, permission probes, publish gates, and rollback. Keep homepage iframe documentation where it describes the custom homepage; remove only account-monitor iframe/static statements.

- [ ] **Step 4: Run contract and documentation scans**

```powershell
node --test deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs
rg -n "账号监控.*iframe|account-monitor/web|EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR|静态账号监控" AGENTS.md deploy extensions-self docs frontend backend
git diff --check
```

Expected: tests PASS; no obsolete account-monitor iframe/static contract remains; homepage iframe references remain intentional.

- [ ] **Step 5: Commit repository documentation and contracts**

```powershell
git add AGENTS.md deploy extensions-self docs
git commit -m "docs: define native monitor release operations"
```

Update the external VPS `AGENTS.md` separately after the feature commit so it reflects the final production procedure without becoming part of the repository commit.

## Task 12: Full Local Verification and Three-Viewport Browser Acceptance

**Files:**
- Modify only when a failing test has a reproduced defect and a new regression test is written first.
- Store screenshots/traces outside the repository.

- [ ] **Step 1: Run complete automated verification**

```powershell
go test ./...
go test ./...
pnpm --dir frontend exec vitest run
pnpm --dir frontend typecheck
pnpm --dir frontend lint:check
pnpm --dir frontend build
node --test deploy/tests/*.test.mjs
docker compose --project-name sub2api-verify -f deploy/docker-compose.local.yml config --quiet
git diff --check
```

Run the first Go command from `extensions-self/account-monitor`, the second from `backend`, and the remaining commands from the repository root. Expected: every command exits 0 with no test failures.

- [ ] **Step 2: Run real PostgreSQL migration/idempotency/backfill tests**

Start the repository's isolated local Compose test database, apply `schema.sql` twice, apply `main_source_views.sql` twice as the owner, seed success/failure/retry/group fixtures, run collection twice, run overlapping and segmented rebuilds, and assert identical fact/aggregate counts after the second run. Verify 10-minute complete-bucket exclusion and 90-day cleanup with SQL assertions.

- [ ] **Step 3: Start the local app and define browser flows**

The flows under test are:

```text
/admin/extensions/user-risk/users -> switch all risk sub-tabs -> query state survives refresh
/admin/extensions/account-monitor -> risk filter/sort -> drawer six tabs -> threshold/rebuild states
/admin/extensions/group-monitor -> filters/paging -> group card -> model detail timeline
```

Use the in-app Browser skill first. Keep the same authenticated tab and record console errors/warnings and network failures.

- [ ] **Step 4: Verify 1440x900, 1920x1080, and 390x844**

At each viewport prove page identity, nonblank content, no framework overlay, no relevant console errors, no page-level horizontal overflow, no overlaps, no nested scroll trap, and the target interaction state change. On mobile prove full-screen account detail and group detail. Exercise manual refresh and observe one 60-second auto-refresh retaining filters, page, selection, drawer/dialog, and tab.

- [ ] **Step 5: Fix only reproduced defects with TDD and rerun all affected checks**

For each defect, add a failing Vitest/Go test first, run it red, implement the smallest correction, run it green, and repeat the exact browser action. Save final screenshots outside the repository.

## Task 13: Independent Code Review, Feature Push, and `custom` Integration

**Files:**
- Review the complete range from plan commit parent to feature HEAD.

- [ ] **Step 1: Run `requesting-code-review` with an isolated reviewer**

Provide the approved design, this plan, base SHA `7296f692`, feature HEAD, exact test commands/results, and ask for security, counting semantics, query/paging, cancellation, UI state, migration, publish, and rollback findings with `file:line` evidence.

- [ ] **Step 2: Resolve every confirmed Critical/Important finding with TDD**

For each accepted finding, reproduce with a failing test, implement, rerun the focused suite, and record why any disputed finding is not valid with code/test evidence.

- [ ] **Step 3: Run `verification-before-completion` fresh**

Repeat the complete commands in Task 12 and inspect full outputs before creating the final feature commit.

- [ ] **Step 4: Commit and push the clean feature branch**

```powershell
git status --short --branch
git diff --check
git add -A
git commit -m "feat: add native extension monitoring center"
git push -u origin feature/account-monitor-20260715
```

Record the final feature SHA and verify the worktree is clean.

- [ ] **Step 5: Recheck and merge into `custom` safely**

In `E:\Code\sub2api`, fetch `origin`, confirm `custom` is clean and still matches the recorded approved baseline, merge the feature branch without rebase/force, resolve conflicts file by file, rerun all necessary Go/frontend/deploy/build checks, then push `origin/custom`. Record the merge SHA and verify `git rev-parse origin/custom` equals it.

## Task 14: Production Backup, Publish, Backfill, Reconciliation, and Browser Acceptance

**Files/Systems:**
- Production host: resolve from the external VPS inventory
- Production source: `/root/sub2api`
- Publisher: `/opt/sub2api-custom/publish-custom.sh`
- Public site: use the currently configured public origin

- [ ] **Step 1: Pass all release gates through `ssh-skill`**

Using only `ssh-skill` with the production alias from the external inventory, verify no publish/sync process or lock is active, `/root/sub2api` is clean on `custom`, production HEAD and `origin/custom` match the approved merge SHA, and Compose config renders successfully.

- [ ] **Step 2: Create and verify the release backup before mutation**

Back up main PostgreSQL, `risk-control-postgres`, `docker-compose.yml`, `.env`, Nginx vhost, origin certificate/key files, container metadata, image IDs, and current Git commit under one timestamped `/root/backups/sub2api/<release-id>/` directory. Verify both dumps with `pg_restore --list`, generate checksums, and record rollback tags/targets.

- [ ] **Step 3: Publish only the approved commit**

Run exactly:

```text
/opt/sub2api-custom/publish-custom.sh --commit <final-origin-custom-sha>
```

If backup, view installation, Compose validation, build, migration, or health checks fail, stop. Use the recorded matching image/config rollback only when production was already changed; never delete extension facts or recreate `risk-control-postgres`.

- [ ] **Step 4: Verify runtime identity and health**

Record main/extension image IDs and embedded commits. Check `sub2api`, `extensions-self`, PostgreSQL, Redis, and `risk-control-postgres` health; main `/health`; extension `/healthz`; signed account/group quality APIs; safe view reads; denied credentials/full-key reads; homepage; and public HTTPS. Confirm the old static account-monitor route, iframe UI, account-monitor web assets, and legacy `risk-control` application container are absent.

- [ ] **Step 5: Run segmented historical backfill and data-quality checks**

Backfill only the source-available interval in segments of at most 31 days. Poll each job, stop on error, record job IDs/row counts/ranges, confirm cursors are not incorrectly advanced, and verify missing-group plus exact/estimated attribution counters.

- [ ] **Step 6: Reconcile account and group samples**

For sampled windows, compare raw safe views to final request facts and group/model 10-minute aggregates. Verify success, final failure, retry-then-success, ungrouped requests, model fallback, account totals, group totals, and `total=successes+failures`; document expected source-retention gaps separately from counting errors.

- [ ] **Step 7: Perform authenticated production Browser acceptance**

With the existing signed-in browser session, verify extensions center, all three user-risk subpages, native account monitor, group monitor cards, filters/paging/auto-refresh, account drawer, thresholds/rebuild status, and model detail at 1440x900, 1920x1080, and 390x844. Check page identity, console, network, overlap, horizontal overflow, and interaction state.

- [ ] **Step 8: Record release and rollback status**

Record plan commit, feature commit, review result, `custom` merge SHA, `origin/custom`, backup path/checksums, main/extension image IDs, backfill jobs/ranges, reconciliation samples, browser screenshots, and rollback target. Report rollback as “not executed; target retained” only when all production checks pass; otherwise record the executed rollback and post-rollback health evidence.

## Final Verification Matrix

| Gate | Evidence |
|---|---|
| Worktree and ancestry | clean status, feature SHA, merge SHA, `origin/custom` equality |
| Extensions Go | full `go test ./...` output from `extensions-self/account-monitor` |
| Backend Go | full `go test ./...` output from `backend` |
| Frontend | full Vitest, typecheck, lint, and build outputs |
| Deploy contracts | all Node contract tests and Compose `config --quiet` |
| PostgreSQL | migration twice, collection twice, aggregate/backfill/retention assertions |
| Static removal | repository scan plus runtime route/container checks |
| Browser | three viewport screenshots, DOM/console/network and interaction evidence |
| Review | independent findings with every Critical/Important disposition |
| Production | backup/checksums, publisher exit, images/commits, health, reconciliation |
| Rollback | retained target or executed rollback with fresh health checks |
