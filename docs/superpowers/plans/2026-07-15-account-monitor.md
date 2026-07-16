# Extensions-Self Account Monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a production-ready account monitoring extension that records upstream account attempts and user final outcomes, aggregates account/model/user/error metrics, exposes an authenticated admin UI, and preserves the existing risk-control, homepage, database, and release contracts.

**Architecture:** Add an independent `extensions-self/account-monitor` Go module that is composed into the existing `extensions-self` process. It reads only deployment-owned `extensions_self_ro` PostgreSQL views through a dedicated read-only connection, writes idempotent facts and aggregates into `risk-control-postgres`, and serves signed admin APIs plus static admin assets through the existing Sub2API proxy. Official Sub2API changes remain limited to exact failed-attempt model attribution, an authenticated proxy allowlist, and a thin frontend route/menu shell.

**Tech Stack:** Go 1.26.5, PostgreSQL, `lib/pq`, Vue 3 + TypeScript, Vitest, Node contract tests, Docker Compose, PowerShell/Bash release checks.

**Baseline Note:** Before implementation, `extensions-self/risk-control` tests and the two existing deployment contract suites passed. The existing full backend handler/service suite and full frontend Vitest suite exceeded a five-minute local command timeout without reporting assertion failures; implementation therefore uses focused tests per task and a 20-minute final full-suite timeout.

---

## File Map

**New account-monitor module**

- `extensions-self/account-monitor/go.mod`: independent Go module consumed by the existing process.
- `extensions-self/account-monitor/config.go`: source DB, collection, retention, query, and web configuration.
- `extensions-self/account-monitor/model.go`: facts, filters, aggregates, thresholds, anomalies, jobs, and API response types.
- `extensions-self/account-monitor/classifier.go`: stable Chinese failure classification.
- `extensions-self/account-monitor/normalizer.go`: source rows to account-attempt and user-result facts.
- `extensions-self/account-monitor/repository.go`: extension DB schema, idempotent writes, aggregates, queries, retention, rebuild locks.
- `extensions-self/account-monitor/source.go`: read-only source view queries and cursors.
- `extensions-self/account-monitor/collector.go`: minute polling, five-minute lookback, transactions, backoff, rebuilds, retention.
- `extensions-self/account-monitor/anomaly.go`: threshold inheritance and explainable health evaluation.
- `extensions-self/account-monitor/http.go`: signed-admin API dispatcher and static account-monitor page handler.
- `extensions-self/account-monitor/schema.sql`: facts, aggregates, sync state, thresholds, and rebuild jobs.
- `extensions-self/account-monitor/sql/main_source_views.sql`: least-privilege main DB schema, views, and role grants.
- `extensions-self/account-monitor/web/index.html`, `app.js`, `styles.css`: responsive admin page body.
- `extensions-self/account-monitor/*_test.go`: unit, repository, HTTP, collector, and contract tests.
- `extensions-self/account-monitor/README.md`: statistics, data flow, configuration, operations, and limitations.

**Existing extensions process**

- `extensions-self/risk-control/go.mod`: require/replace the local account-monitor module.
- `extensions-self/risk-control/config.go`: load monitor configuration without weakening risk-control validation.
- `extensions-self/risk-control/main.go`: open the read-only source DB, apply monitor schema, start/stop collector, and compose handlers.
- `extensions-self/risk-control/http.go`: delegate `/api/v1/admin/account-monitor/*` and `/account-monitor/*` after existing signature/actor checks.
- `extensions-self/risk-control/http_test.go`, `config_test.go`, `homepage_test.go`: composition and regression tests.
- `extensions-self/Dockerfile`: build both Go modules and copy monitor web assets.

**Official backend, frontend, deployment, and docs**

- `backend/internal/service/ops_upstream_context.go`: add optional `upstream_model` and populate it from request context.
- `backend/internal/service/ops_upstream_context_test.go`: exact/legacy JSON tests.
- `backend/internal/handler/ops_error_logger.go`: expose the mapped upstream model to the shared context key.
- `backend/internal/handler/ops_error_logger_test.go`: context propagation test.
- `backend/internal/handler/admin/user_risk_control_proxy.go`: add a separate monitor allowlist and upstream prefix.
- `backend/internal/handler/admin/user_risk_control_proxy_test.go`: auth, method, path, body, and error proxy tests.
- `backend/internal/service/risk_control_client.go`: generalize signed admin proxy response handling without changing risk APIs.
- `backend/internal/service/risk_control_client_test.go`: signed monitor proxy contract.
- `backend/internal/server/routes/admin.go`: authenticated monitor proxy route.
- `backend/internal/handler/extensions_self_proxy.go`, `backend/internal/service/risk_control_client.go`, and tests: narrow static monitor asset proxy.
- `backend/internal/server/routes/auth.go`: authenticated static monitor route.
- `frontend/src/views/admin/AccountMonitorView.vue`: thin iframe shell.
- `frontend/src/router/index.ts`, `frontend/src/components/layout/AppSidebar.vue`: route and menu entry.
- `frontend/src/i18n/locales/zh/common.ts`, `frontend/src/i18n/locales/en/common.ts`: labels.
- `frontend/src/router/__tests__/feature-access.spec.ts`: admin route guard test.
- `deploy/docker-compose.yml`, `deploy/docker-compose.local.yml`, `deploy/.env.example`: read-only source DSN and monitor configuration.
- `deploy/ops/install-account-monitor-source.sql`, `deploy/ops/publish-custom.sh`: install/verify safe views before enabling monitor collection.
- `deploy/tests/account-monitor-contract.test.mjs`, `deploy/tests/extensions-self-layout.test.mjs`: single-container and release invariants.
- `AGENTS.md`, `extensions-self/README.md`, `extensions-self/risk-control/README.md`, `deploy/README.md`, `deploy/ops/README.md`, `deploy/RELEASE-RUNBOOK.md`, `docs/EXTENSIONS-SELF-ARCHITECTURE.md`, `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`, `docs/ACCOUNT-MONITOR-CHECKLIST.md`: ownership, data dictionary, deploy, rollback, rebuild, and handoff.
- `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`: VPS architecture/index update after repository work is merged.

## Task 1: Lock Baseline And Module Boundary

**Files:**
- Create: `extensions-self/account-monitor/go.mod`
- Create: `extensions-self/account-monitor/config.go`
- Create: `extensions-self/account-monitor/config_test.go`
- Modify: `extensions-self/risk-control/go.mod`

- [ ] **Step 1: Write the failing configuration tests**

```go
func TestConfigDefaults(t *testing.T) {
    cfg := LoadConfig(func(string) string { return "" })
    if cfg.PollInterval != time.Minute || cfg.Lookback != 5*time.Minute || cfg.DetailRetention != 90*24*time.Hour || cfg.DailyRetention != 365*24*time.Hour {
        t.Fatalf("unexpected defaults: %+v", cfg)
    }
}

func TestConfigRequiresSourceDatabaseWhenEnabled(t *testing.T) {
    cfg := Config{Enabled: true}
    if err := cfg.Validate(); !errors.Is(err, ErrSourceDatabaseRequired) {
        t.Fatalf("got %v", err)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test ./...`
Expected: FAIL because `Config`, `LoadConfig`, and validation errors do not exist.

- [ ] **Step 3: Add the module and minimal typed configuration**

```go
type Config struct {
    Enabled          bool
    SourceDatabaseURL string
    PollInterval     time.Duration
    Lookback         time.Duration
    BatchSize        int
    DetailRetention  time.Duration
    DailyRetention   time.Duration
    QueryTimeout     time.Duration
    WebDir           string
}
```

Use environment keys `ACCOUNT_MONITOR_ENABLED`, `ACCOUNT_MONITOR_SOURCE_DATABASE_URL`, `ACCOUNT_MONITOR_POLL_SECONDS`, `ACCOUNT_MONITOR_LOOKBACK_SECONDS`, `ACCOUNT_MONITOR_BATCH_SIZE`, `ACCOUNT_MONITOR_QUERY_TIMEOUT_MS`, and `EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR`. Keep disabled mode valid without a source DSN.

- [ ] **Step 4: Wire the local module dependency and rerun tests**

Add to `extensions-self/risk-control/go.mod`:

```go
require github.com/ListenCodes/sub2api-account-monitor v0.0.0
replace github.com/ListenCodes/sub2api-account-monitor => ../account-monitor
```

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions-self/account-monitor extensions-self/risk-control/go.mod extensions-self/risk-control/go.sum
git commit -m "feat(account-monitor): establish extension module"
```

## Task 2: Define The Safe Main-Database Read Layer

**Files:**
- Create: `extensions-self/account-monitor/sql/main_source_views.sql`
- Create: `extensions-self/account-monitor/source.go`
- Create: `extensions-self/account-monitor/source_test.go`
- Create: `deploy/ops/install-account-monitor-source.sql`

- [ ] **Step 1: Write source contract tests**

Test that source queries reference only `extensions_self_ro.usage_source`, `error_source`, `account_dimension`, `user_dimension`, and `api_key_dimension`; reject raw table names and credential/key columns.

```go
func TestSourceQueriesUseOnlySafeViews(t *testing.T) {
    for name, query := range SourceQueriesForTest() {
        if strings.Contains(query, "FROM usage_logs") || strings.Contains(query, "credentials") || strings.Contains(query, "api_key ") {
            t.Fatalf("unsafe query %s: %s", name, query)
        }
    }
}
```

- [ ] **Step 2: Run and verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestSource' ./...`
Expected: FAIL because source contracts do not exist.

- [ ] **Step 3: Add idempotent least-privilege SQL**

The SQL must create `extensions_self_ro`, revoke public access, create/alter `extensions_self_monitor_ro` with `NOLOGIN` by default, and expose only the fields in spec sections 3 and 7. `error_source.upstream_errors` must rebuild a safe JSON array containing only timestamp, platform, account identity, `upstream_model`, status, kind, and already-sanitized message/detail. `api_key_dimension` exposes ID, name, and a fixed masked prefix only.

- [ ] **Step 4: Implement typed paged readers**

```go
type Source interface {
    ReadUsage(ctx context.Context, after Cursor, from time.Time, limit int) ([]UsageSourceRow, error)
    ReadErrors(ctx context.Context, after Cursor, from time.Time, limit int) ([]ErrorSourceRow, error)
    Dimensions(ctx context.Context, ids DimensionIDs) (Dimensions, error)
}
```

Order by `(created_at, id)`, use a statement timeout derived from config, and never request a page larger than the configured batch size.

- [ ] **Step 5: Verify SQL contract and commit**

Run: `rg -n "credentials|oauth|cookie|request_body|request_headers" extensions-self/account-monitor/sql deploy/ops/install-account-monitor-source.sql`
Expected: only comments/explicit `REVOKE` safety checks, never selected columns.

```bash
git add extensions-self/account-monitor/sql extensions-self/account-monitor/source* deploy/ops/install-account-monitor-source.sql
git commit -m "feat(account-monitor): add safe source views"
```

## Task 3: Add Extension Facts, Aggregates, And Control Schema

**Files:**
- Create: `extensions-self/account-monitor/schema.sql`
- Create: `extensions-self/account-monitor/repository.go`
- Create: `extensions-self/account-monitor/repository_test.go`

- [ ] **Step 1: Write schema contract tests**

Assert the schema contains unique `event_key` and `request_key`, all six aggregate tables, cursor state, thresholds, rebuild jobs, retention indexes, and no credential/full-key/body/header columns.

- [ ] **Step 2: Run and verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestSchema' ./...`
Expected: FAIL because the embedded schema is absent.

- [ ] **Step 3: Add idempotent schema**

Use `account_monitor_schema_migrations`; create `account_monitor_attempt_facts`, `account_monitor_request_facts`, account/account-model minute and daily aggregates, account-user and account-error daily aggregates, `account_monitor_sync_state`, `account_monitor_rebuild_jobs`, and `account_monitor_thresholds`. Store IDs and masked display snapshots only; never store credentials or full API keys.

- [ ] **Step 4: Implement transaction boundary**

```go
type Batch struct {
    Attempts []AttemptFact
    Requests []RequestFact
    UsageCursor Cursor
    ErrorCursor Cursor
}

func (r *Repository) CommitBatch(ctx context.Context, batch Batch) error {
    // one transaction: upsert facts -> refresh affected minute/daily buckets -> advance cursors
}
```

Use `ON CONFLICT (event_key) DO UPDATE` for corrected late facts and `ON CONFLICT (request_key) DO UPDATE` for final request state.

- [ ] **Step 5: Run tests and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestSchema|TestRepository' ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/schema.sql extensions-self/account-monitor/repository*
git commit -m "feat(account-monitor): persist monitoring facts"
```

## Task 4: Normalize Attempts And User Results

**Files:**
- Create: `extensions-self/account-monitor/model.go`
- Create: `extensions-self/account-monitor/normalizer.go`
- Create: `extensions-self/account-monitor/normalizer_test.go`

- [ ] **Step 1: Write table-driven failing tests**

Cover: successful usage, final failure, retry then success, multiple failed accounts, synthetic failure when `upstream_errors` is empty, pre-routing/security denial with no account attempt, missing request ID fallback, and exact vs estimated model attribution.

```go
{
    name: "retry then success",
    errors: []ErrorSourceRow{{ID: 8, RequestID: "r1", APIKeyID: 2, UpstreamErrors: []UpstreamError{{AccountID: 10, UpstreamModel: "gpt-5.4", Status: 429}}}},
    usage: []UsageSourceRow{{ID: 9, RequestID: "r1", APIKeyID: 2, AccountID: 11, UpstreamModel: "gpt-5.4", ActualCost: decimal("0.2")}},
    wantAttempts: []Result{Failed, Succeeded},
    wantFinal: Succeeded,
}
```

- [ ] **Step 2: Verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestNormalize' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement formulas exactly**

Successful usage requires the repository's established billable-success condition (`actual_cost > 0`) so zero-cost error placeholders are excluded. Failure events use `ops:<log>:event:<index>`; synthetic failures use `ops:<log>:synthetic`; user results use `request:<api_key_id>:<request_id>` and fall back to source IDs with `identity_quality=fallback`.

- [ ] **Step 4: Verify and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestNormalize' ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/model.go extensions-self/account-monitor/normalizer*
git commit -m "feat(account-monitor): normalize account attempts"
```

## Task 5: Classify Failures In Stable Chinese Categories

**Files:**
- Create: `extensions-self/account-monitor/classifier.go`
- Create: `extensions-self/account-monitor/classifier_test.go`

- [ ] **Step 1: Write precedence tests**

Cover provider codes, 401/403 authentication, quota codes, 429 limiting, 529 overload, network errors, timeouts, 5xx upstream, 4xx request errors, content/security blocking, no-account routing, and unknown fallback. Assert provider code wins over status, status wins over text.

- [ ] **Step 2: Run and verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestClassify' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement deterministic classification**

```go
type ErrorCategory string
const (
    ErrorRateLimited ErrorCategory = "限流"
    ErrorOverloaded ErrorCategory = "上游过载"
    ErrorAuth ErrorCategory = "账号认证失效"
    ErrorQuota ErrorCategory = "账号额度不足"
    ErrorModelUnavailable ErrorCategory = "模型不可用"
    ErrorNetwork ErrorCategory = "网络连接失败"
    ErrorTimeout ErrorCategory = "请求超时"
    ErrorUpstream ErrorCategory = "上游服务错误"
    ErrorInvalidRequest ErrorCategory = "请求参数错误"
    ErrorSafety ErrorCategory = "内容或安全拦截"
    ErrorNoAccount ErrorCategory = "无可用账号"
    ErrorUnknown ErrorCategory = "未知错误"
)
```

- [ ] **Step 4: Verify and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestClassify' ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/classifier*
git commit -m "feat(account-monitor): classify upstream failures"
```

## Task 6: Implement Incremental Collection, Retention, And Rebuilds

**Files:**
- Create: `extensions-self/account-monitor/collector.go`
- Create: `extensions-self/account-monitor/collector_test.go`
- Modify: `extensions-self/account-monitor/repository.go`

- [ ] **Step 1: Write failing collector tests**

Use fake source/repository clocks to cover five-minute lookback, separate usage/error cursors, cursor advancement only after commit, duplicate re-read, capped exponential backoff, restart from cursor, 90-day fact/minute cleanup, one-year daily cleanup, 31-day rebuild limit, and advisory-lock rejection of overlapping jobs.

- [ ] **Step 2: Verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestCollector|TestRebuild|TestRetention' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement lifecycle**

```go
func (c *Collector) Run(ctx context.Context) {
    timer := time.NewTimer(0)
    defer timer.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-timer.C:
            err := c.SyncOnce(ctx)
            timer.Reset(c.nextDelay(err))
        }
    }
}
```

`SyncOnce` reads both sources from `cursor_time - lookback`, normalizes, commits one transaction, records data-quality counters, and then returns. Rebuilds use bounded ranges, `pg_try_advisory_lock`, upsert facts, and keep old aggregates until replacement succeeds.

- [ ] **Step 4: Verify and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestCollector|TestRebuild|TestRetention' ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/collector* extensions-self/account-monitor/repository.go
git commit -m "feat(account-monitor): collect and rebuild facts"
```

## Task 7: Aggregate Metrics And Evaluate Explainable Anomalies

**Files:**
- Create: `extensions-self/account-monitor/anomaly.go`
- Create: `extensions-self/account-monitor/anomaly_test.go`
- Modify: `extensions-self/account-monitor/repository.go`

- [ ] **Step 1: Write failing aggregation and anomaly tests**

Cover account/model/user/error aggregates, parent-account rollup, tokens/cost/duration/image/video fields, P95, threshold inheritance (`account > parent > platform > global`), minimum sample rules, consecutive model failures, auth/quota severity, 429/529 ratio, no-success, user concentration, traffic baseline, latency baseline, and human-readable evidence.

- [ ] **Step 2: Verify failure**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestAggregate|TestAnomaly|TestThreshold' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement aggregate refresh and health reasons**

```go
type Health struct {
    Level string `json:"level"`
    Reasons []string `json:"reasons"`
}
// Example reason: 近 1 小时调用 82 次，失败 19 次，成功率 76.8%，低于 90% 阈值；主要原因：限流 12 次。
```

All list queries use aggregates for older ranges and recent facts only for the latest-detail pane. Whitelist sort columns and cap page size at 100.

- [ ] **Step 4: Verify and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test -run 'TestAggregate|TestAnomaly|TestThreshold' ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/anomaly* extensions-self/account-monitor/repository.go
git commit -m "feat(account-monitor): aggregate health metrics"
```

## Task 8: Expose Signed Admin APIs And Static Page Assets

**Files:**
- Create: `extensions-self/account-monitor/http.go`
- Create: `extensions-self/account-monitor/http_test.go`
- Create: `extensions-self/account-monitor/web/index.html`
- Create: `extensions-self/account-monitor/web/app.js`
- Create: `extensions-self/account-monitor/web/styles.css`
- Create: `extensions-self/account-monitor/web_contract_test.go`

- [ ] **Step 1: Write failing API tests**

Cover all spec routes, actor requirement supplied by the parent server, time range validation, pagination 20/50/100, sort whitelist, query timeout, threshold update, rebuild creation/status, unavailable data as explicit error, and response redaction.

- [ ] **Step 2: Write failing web contract tests**

Assert the page contains overview, filters, accounts, detail tabs, data quality, thresholds, and rebuild controls; API Key display must be masked; auto-refresh state is stored independently from filter/page/drawer state.

- [ ] **Step 3: Implement API dispatcher**

```go
func (h *Handler) ServeAdmin(w http.ResponseWriter, r *http.Request, relativePath string, actorID int64)
func (h *Handler) ServeWeb(w http.ResponseWriter, r *http.Request, relativePath string)
```

Implement `GET overview/accounts/accounts/:id/models/users/errors/attempts/data-quality/thresholds/rebuild-jobs/:id`, `PUT thresholds`, and `POST rebuild-jobs`.

- [ ] **Step 4: Implement responsive static UI**

Use semantic HTML, CSS grid/table overflow containers, stable control sizes, 8px-or-less radii, icon buttons with tooltips, a 60-second refresh timer, and a drawer that preserves state. The page calls only `/api/v1/admin/extensions-self/account-monitor/*` on the same origin.

- [ ] **Step 5: Verify and commit**

Run: `cd extensions-self/account-monitor && D:\Go\bin\go.exe test ./...`
Expected: PASS.

```bash
git add extensions-self/account-monitor/http* extensions-self/account-monitor/web
git commit -m "feat(account-monitor): add admin API and page"
```

## Task 9: Compose Account Monitor Into The Existing Single Process

**Files:**
- Modify: `extensions-self/risk-control/config.go`
- Modify: `extensions-self/risk-control/config_test.go`
- Modify: `extensions-self/risk-control/main.go`
- Modify: `extensions-self/risk-control/http.go`
- Modify: `extensions-self/risk-control/http_test.go`
- Modify: `extensions-self/risk-control/homepage_test.go`
- Modify: `extensions-self/Dockerfile`

- [ ] **Step 1: Write failing composition tests**

Assert disabled mode starts with no source DB, enabled mode requires source DB, monitor admin requests still require signature and actor, web assets are GET/HEAD only, `/healthz` reports monitor state without failing the whole service for a temporary source outage, and risk/homepage routes remain unchanged.

- [ ] **Step 2: Verify failure**

Run: `cd extensions-self/risk-control && D:\Go\bin\go.exe test ./...`
Expected: FAIL because monitor composition is absent.

- [ ] **Step 3: Wire lifecycle and handler delegation**

Open the extension DB as before. When enabled, open a second `sql.DB` with the read-only DSN, apply the monitor extension schema to the extension DB, create handler/collector, start the collector under the process signal context, and close the source DB at shutdown. Add handler fields to `HTTPServer`; do not change risk repository interfaces.

- [ ] **Step 4: Update Docker build context**

Copy both module manifests first for cacheable downloads, then both sources, build the existing binary, and copy `account-monitor/web` to `/app/account-monitor`.

- [ ] **Step 5: Verify regressions and commit**

Run: `cd extensions-self/risk-control && D:\Go\bin\go.exe test ./...`
Expected: PASS.

Run: `D:\nodejs\node.exe --test deploy/tests/extensions-self-layout.test.mjs`
Expected: PASS.

```bash
git add extensions-self/risk-control extensions-self/Dockerfile
git commit -m "feat(account-monitor): wire single-container runtime"
```

## Task 10: Record Exact Failed-Attempt Upstream Models

**Files:**
- Modify: `backend/internal/service/ops_upstream_context.go`
- Create or Modify: `backend/internal/service/ops_upstream_context_test.go`
- Modify: `backend/internal/handler/ops_error_logger.go`
- Modify: `backend/internal/handler/ops_error_logger_test.go`

- [ ] **Step 1: Write failing event JSON tests**

```go
func TestAppendOpsUpstreamErrorCopiesMappedModel(t *testing.T) {
    c, _ := gin.CreateTestContext(httptest.NewRecorder())
    c.Set(OpsUpstreamModelKey, "gpt-5.4")
    appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 7})
    got, _ := c.Get(OpsUpstreamErrorsKey)
    if got.([]*OpsUpstreamErrorEvent)[0].UpstreamModel != "gpt-5.4" { t.Fatal("missing model") }
}
```

Also parse historical JSON without the field and assert it remains valid.

- [ ] **Step 2: Verify failure**

Run: `cd backend && D:\Go\bin\go.exe test ./internal/service -run 'TestAppendOpsUpstreamError|TestParseOpsUpstreamErrors'`
Expected: FAIL.

- [ ] **Step 3: Add the shared context key and optional field**

Add `OpsUpstreamModelKey`, set it in `setOpsEndpointContext` from the already-mapped `upstreamModel`, and in `appendOpsUpstreamError` fill `ev.UpstreamModel` only when the event did not provide a more specific model. Trim it before storage.

- [ ] **Step 4: Run focused coverage across request types**

Run: `cd backend && D:\Go\bin\go.exe test ./internal/service ./internal/handler -run 'OpsUpstream|OpsEndpoint|ErrorLogger' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/ops_upstream_context* backend/internal/handler/ops_error_logger*
git commit -m "feat(ops): record failed attempt upstream model"
```

## Task 11: Add Narrow Main-App Proxies

**Files:**
- Modify: `backend/internal/handler/admin/user_risk_control_proxy.go`
- Modify: `backend/internal/handler/admin/user_risk_control_proxy_test.go`
- Modify: `backend/internal/handler/extensions_self_proxy.go`
- Modify: `backend/internal/handler/extensions_self_proxy_test.go`
- Modify: `backend/internal/service/risk_control_client.go`
- Modify: `backend/internal/service/risk_control_client_homepage_test.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/routes/auth.go`

- [ ] **Step 1: Write failing proxy allowlist tests**

Test exact methods and shapes for monitor endpoints, reject traversal/unknown endpoints, cap request/response bodies, preserve upstream JSON errors, and ensure non-admin requests never reach the proxy route.

- [ ] **Step 2: Verify failure**

Run: `cd backend && D:\Go\bin\go.exe test ./internal/handler/admin ./internal/handler ./internal/service -run 'AccountMonitor|ExtensionsSelf' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement distinct proxy prefixes**

Keep risk-control at `/api/v1/admin/*`. Add monitor admin forwarding from `/api/v1/admin/extensions-self/account-monitor/*` to `/api/v1/admin/account-monitor/*`. Add static forwarding from `/api/v1/extensions-self/account-monitor/*` to `/account-monitor/*`, GET/HEAD only. Reuse signing and response limits; do not expose arbitrary extension paths.

- [ ] **Step 4: Verify and commit**

Run: `cd backend && D:\Go\bin\go.exe test ./internal/handler/admin ./internal/handler ./internal/service -run 'RiskControl|AccountMonitor|ExtensionsSelf' -count=1`
Expected: PASS.

```bash
git add backend/internal/handler backend/internal/service/risk_control_client* backend/internal/server/routes
git commit -m "feat(account-monitor): proxy signed admin endpoints"
```

## Task 12: Add The Thin Vue Route And Menu Entry

**Files:**
- Create: `frontend/src/views/admin/AccountMonitorView.vue`
- Create: `frontend/src/views/admin/__tests__/AccountMonitorView.spec.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`
- Modify: `frontend/src/router/__tests__/feature-access.spec.ts`

- [ ] **Step 1: Write failing route and shell tests**

Assert admin-only route `/admin/account-monitor`, title keys, sidebar item, iframe source `/api/v1/extensions-self/account-monitor/`, sandbox/referrer policy, loading/error state, and no monitor business logic in Vue.

- [ ] **Step 2: Verify failure**

Run: `cd frontend && D:\nodejs\pnpm.cmd exec vitest run src/router/__tests__/feature-access.spec.ts src/views/admin/__tests__/AccountMonitorView.spec.ts`
Expected: FAIL.

- [ ] **Step 3: Implement the thin shell**

Use a full-width iframe inside the normal admin content area, with `min-height: calc(100vh - var(--header-height))`, an accessible title, reload icon, and an external-open icon. Do not duplicate filters or API types in official frontend code.

- [ ] **Step 4: Verify and commit**

Run: `cd frontend && D:\nodejs\pnpm.cmd exec vitest run src/router/__tests__/feature-access.spec.ts src/views/admin/__tests__/AccountMonitorView.spec.ts`
Expected: PASS.

Run: `cd frontend && D:\nodejs\pnpm.cmd typecheck`
Expected: PASS.

```bash
git add frontend/src/views/admin/AccountMonitorView.vue frontend/src/views/admin/__tests__ frontend/src/router frontend/src/components/layout/AppSidebar.vue frontend/src/i18n
git commit -m "feat(account-monitor): add admin entry shell"
```

## Task 13: Make Deployment And Release Contracts Explicit

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/.env.example`
- Modify: `deploy/ops/publish-custom.sh`
- Create: `deploy/tests/account-monitor-contract.test.mjs`
- Modify: `deploy/tests/extensions-self-layout.test.mjs`

- [ ] **Step 1: Write failing deployment contract tests**

Assert one `extensions-self` app container, unchanged `risk-control-postgres`, read-only source DSN, monitor web path, safe view installer, source privilege probe, build of both modules, and publisher health checks for monitor static/API readiness.

- [ ] **Step 2: Verify failure**

Run: `D:\nodejs\node.exe --test deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs`
Expected: FAIL.

- [ ] **Step 3: Add environment and compose wiring**

Add `ACCOUNT_MONITOR_ENABLED=false` by default, `ACCOUNT_MONITOR_SOURCE_DATABASE_URL`, polling/lookback/batch/query values, and web directory. The source user must be a dedicated read-only login; never reuse the main application DB owner.

- [ ] **Step 4: Add publisher gates**

Before building an enabled monitor, execute the source SQL as the DB owner, verify `SET ROLE extensions_self_monitor_ro; SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1`, verify denied access to credentials/full keys, then build. Preserve existing backup order and do not recreate `risk-control-postgres`.

- [ ] **Step 5: Verify and commit**

Run: `D:\nodejs\node.exe --test deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs`
Expected: PASS.

Run: `docker compose -f deploy/docker-compose.local.yml --env-file deploy/.env.example config --quiet`
Expected: exit 0.

```bash
git add deploy
git commit -m "feat(account-monitor): add deployment contracts"
```

## Task 14: Complete Operational And Handoff Documentation

**Files:**
- Modify: `extensions-self/README.md`
- Modify: `extensions-self/risk-control/README.md`
- Modify: `extensions-self/account-monitor/README.md`
- Create: `docs/EXTENSIONS-SELF-ARCHITECTURE.md`
- Create: `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`
- Create: `docs/ACCOUNT-MONITOR-CHECKLIST.md`
- Modify: `AGENTS.md`
- Modify: `deploy/README.md`
- Modify: `deploy/ops/README.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify after merge: `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`

- [ ] **Step 1: Write a documentation contract test**

Extend `deploy/tests/account-monitor-contract.test.mjs` to require statistics formulas, source/extension DB boundaries, routes, environment variables, rebuild/retention, data-quality caveats, deployment order, rollback, and troubleshooting commands.

- [ ] **Step 2: Verify failure**

Run: `D:\nodejs\node.exe --test deploy/tests/account-monitor-contract.test.mjs`
Expected: FAIL with missing documentation markers.

- [ ] **Step 3: Update repository docs**

Document that code completion is not production release; record the single-container architecture, independent source read role, exact/estimated model attribution, retry semantics, 90-day/one-year retention, enable/disable sequence, rebuild limit, backup, rollback, and failure diagnosis.

- [ ] **Step 4: Update VPS knowledge base**

After the repository branch is merged, use `apply_patch` on `E:\BaiduSyncdisk\Private\VPS\AGENTS.md` to add account-monitor paths, DB role/view ownership, release order, and rollback notes. Do not add secrets.

- [ ] **Step 5: Verify and commit**

Run: `D:\nodejs\node.exe --test deploy/tests/account-monitor-contract.test.mjs`
Expected: PASS.

```bash
git add AGENTS.md extensions-self docs deploy
git commit -m "docs(account-monitor): document operations and handoff"
```

## Task 15: Full Verification, Merge, And Production Release

**Files:**
- Review: all changed files
- Production scripts: `deploy/ops/publish-custom.sh`, `/opt/sub2api-custom/publish-custom.sh`

- [ ] **Step 1: Run focused and full repository checks**

```powershell
cd extensions-self/account-monitor
$env:GOPROXY='https://goproxy.cn,direct'
& 'D:\Go\bin\go.exe' test -race ./...

cd ..\risk-control
& 'D:\Go\bin\go.exe' test -race ./...

cd ..\..\backend
& 'D:\Go\bin\go.exe' test ./internal/service ./internal/handler/... -count=1 -timeout 20m

cd ..\frontend
& 'D:\nodejs\pnpm.cmd' exec vitest run --reporter=verbose
& 'D:\nodejs\pnpm.cmd' typecheck
& 'D:\nodejs\pnpm.cmd' build

cd ..
& 'D:\nodejs\node.exe' --test deploy/tests/*.test.mjs
git diff --check
git status --short
```

Expected: all exit 0; worktree contains only intended committed changes.

- [ ] **Step 2: Build and inspect containers locally**

Run: `docker compose -f deploy/docker-compose.local.yml --env-file deploy/.env.example config --quiet`
Expected: exit 0 and exactly one `extensions-self` application service plus independent `risk-control-postgres`.

Run: `docker build -f extensions-self/Dockerfile extensions-self -t deploy-extensions-self:account-monitor-test`
Expected: successful image build.

- [ ] **Step 3: Browser verification**

Start the local stack or frontend dev server with a test account-monitor API fixture. Verify 1440x900 and 390x844: page nonblank, no console errors, filters/sort/pagination, drawer tabs, API Key expansion, anomaly reasons, thresholds, rebuild progress, state-preserving auto-refresh, no overlap/horizontal page overflow/nested-scroll trap.

- [ ] **Step 4: Review official-code conflict surface**

Run: `git diff --check custom...HEAD -- backend frontend`, then manually confirm official changes are limited to spec section 17. Compare touched official files against `upstream/main` and stop if an unrelated refactor appears.

- [ ] **Step 5: Commit final fixes and obtain independent review**

Use `superpowers:requesting-code-review`, address verified findings, rerun affected tests, and ensure the branch is clean.

- [ ] **Step 6: Merge into custom and push**

From the canonical local repository, fetch `origin` and `upstream`, confirm `custom` has not changed unexpectedly, fast-forward or merge the reviewed feature branch into `custom`, run `git diff --check`, and push `origin/custom`. Record feature commit and resulting `custom` commit separately.

- [ ] **Step 7: Back up production through ssh-skill**

Use only `C:\Users\llx93\.cc-switch\skills\ssh-skill\scripts\ssh_execute.py` or the skill's documented daemon client. Confirm `/root/sub2api` is clean, current deployed commit/image IDs, no active publisher, and create a dated PostgreSQL/Compose/config/Nginx/extensions DB backup under `/root/backups/sub2api/<timestamp>/`.

- [ ] **Step 8: Install safe views and publish the approved commit**

Through ssh-skill, update `/root/sub2api` to the exact `origin/custom` commit, install/verify the read-only views and grants, keep `ACCOUNT_MONITOR_ENABLED=false` for schema/collector dry validation, then enable it and run:

```bash
/opt/sub2api-custom/publish-custom.sh --commit "<approved-origin-custom-commit>"
```

The publisher must back up first, build `sub2api:custom` and `deploy-extensions-self`, recreate only application services, and preserve `risk-control-postgres` identity.

- [ ] **Step 9: Production health and data reconciliation**

Verify container health, `http://127.0.0.1:8081/health`, extensions `/healthz`, public HTTPS, authenticated account-monitor page/API, recent sync time, source read status, cursors, attribution quality, and a production read-only sample for one account/model/user/recovered failure. Confirm risk-control pages and custom homepage still work.

- [ ] **Step 10: Record release and rollback state**

Record approved commit, image IDs, backup path, safe-view version, health results, browser results, and previous rollback image tags. If any release gate fails, stop, restore the matching prior images/config through the runbook, re-run health checks, and report implementation, merge, deployment, and rollback as separate facts.
