# Monitor Fixed Timeline Corrections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct account group presentation and trends, make monitor filters self-identifying, and make group monitoring return and render exactly 24 stable buckets for every supported range.

**Architecture:** Extend the existing read-only account-group view with the non-secret group rate, propagate it through the existing source and API types, and keep account presentation native Vue. For group timelines, aggregate exact display buckets from retained request facts using a server-owned range map, then reuse the existing zero-fill card builder for list and detail responses. UI components keep existing route/query behavior while adding explicit labels, a 24-column chart grid, calls-first ordering/filtering, and an explicit horizontal scroll surface.

**Tech Stack:** Go 1.24, PostgreSQL 17, Vue 3, TypeScript, Vitest, Tailwind CSS, Docker Compose

---

### Task 1: Propagate Group Rate Multipliers

**Files:**
- Modify: `extensions-self/account-monitor/sql/main_source_views.sql`
- Modify: `extensions-self/account-monitor/source.go`
- Modify: `extensions-self/account-monitor/source_test.go`
- Modify: `extensions-self/account-monitor/source_reader_test.go`
- Modify: `extensions-self/account-monitor/postgres_integration_test.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/admin_backend_test.go`
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorTable.vue`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts`

- [ ] **Step 1: Write failing safe-view and propagation tests**

Assert the view selects `g.rate_multiplier AS group_rate_multiplier`, the
restricted source reader scans it, JSON group summaries contain
`rate_multiplier`, and the account table renders `GPT Pro · 1.5x` without a
second platform badge in the group cell.

```go
if groups[0].RateMultiplier != 1.5 {
    t.Fatalf("rate_multiplier=%v, want 1.5", groups[0].RateMultiplier)
}
```

```ts
expect(wrapper.get('[data-testid="account-group-11"]').text()).toContain('GPT Pro · 1.5x')
expect(wrapper.get('[data-testid="account-group-11"]').findComponent(PlatformBadge).exists()).toBe(false)
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```powershell
Push-Location extensions-self/account-monitor
go test ./... -run 'AccountGroup|SourceViews'
Pop-Location
Push-Location frontend
npx vitest run src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts
Pop-Location
```

Expected: failures because the view, Go structs, JSON type, and Vue rendering do
not yet expose the rate.

- [ ] **Step 3: Implement the minimal data contract**

Add `RateMultiplier float64` to `GroupDimension` and
`AccountGroupSummary`, select/scan the value, and return it as
`rate_multiplier`. Render only the group name and formatted multiplier:

```vue
<span :data-testid="`account-group-${group.group_id}`">
  {{ group.name }} · {{ formatMultiplier(group.rate_multiplier) }}x
</span>
```

- [ ] **Step 4: Run focused tests and verify GREEN**

Run the commands from Step 2. Expected: all selected tests pass.

- [ ] **Step 5: Commit**

```powershell
git add extensions-self/account-monitor frontend/src
git commit -m "feat(monitor): show account group rate multipliers"
```

### Task 2: Make Account Trends And Filters Unambiguous

**Files:**
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/admin_backend_test.go`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorDrawer.vue`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorFilters.vue`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorDialogs.spec.ts`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorFilters.spec.ts`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorFilters.vue`
- Modify: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts`

- [ ] **Step 1: Write failing ordering and label tests**

Assert trend SQL uses descending bucket order and the drawer renders a later
timestamp before an earlier one. Assert visible labels exist for every account
and group filter while the existing select-immediate/input-debounced behavior
still fires once.

```ts
expect(wrapper.findAll('[data-testid="account-trend-row"]')[0].text()).toContain('2026/7/16')
expect(wrapper.get('[data-testid="account-filter-platform-label"]').text()).toBe('平台')
expect(wrapper.get('[data-testid="group-filter-call-status-label"]').text()).toBe('调用状态')
```

- [ ] **Step 2: Run focused tests and verify RED**

```powershell
Push-Location extensions-self/account-monitor
go test ./... -run 'Trend'
Pop-Location
Push-Location frontend
npx vitest run src/views/admin/account-monitor/__tests__/AccountMonitorDialogs.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorFilters.spec.ts src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts
Pop-Location
```

Expected: ordering and visible-label assertions fail.

- [ ] **Step 3: Implement newest-first trends and compact visible labels**

Change the trend query to `ORDER BY 1 DESC`. Wrap each filter control in a
compact label block with a visible `text-xs text-gray-500` caption; preserve
existing models, test IDs, event handlers, widths, and responsive wrapping.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run the commands from Step 2. Expected: all selected tests pass.

- [ ] **Step 5: Commit**

```powershell
git add extensions-self/account-monitor frontend/src/views/admin/account-monitor frontend/src/views/admin/group-monitor
git commit -m "fix(monitor): clarify filters and trend order"
```

### Task 3: Enforce Four Exact 24-Bucket Ranges

**Files:**
- Modify: `extensions-self/account-monitor/http.go`
- Modify: `extensions-self/account-monitor/http_test.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/admin_backend_test.go`
- Modify: `extensions-self/account-monitor/postgres_integration_test.go`
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Modify: `frontend/src/views/admin/group-monitor/useGroupMonitorFilters.ts`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorFilters.vue`
- Modify: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts`

- [ ] **Step 1: Write failing range-contract tests**

Test this exact closed map and assert both list and detail contain 24 buckets:

```go
tests := []struct {
    rangeValue string
    duration   time.Duration
    bucket     int
}{
    {"6h", 6 * time.Hour, 15 * 60},
    {"24h", 24 * time.Hour, 60 * 60},
    {"7d", 7 * 24 * time.Hour, 7 * 60 * 60},
    {"30d", 30 * 24 * time.Hour, 30 * 60 * 60},
}
```

Also assert `1h` and `12h` return 400, invalid route values normalize to `6h`,
and request-fact aggregation preserves exact/estimated model counts and range
totals.

- [ ] **Step 2: Run Go and Vue tests and verify RED**

```powershell
Push-Location extensions-self/account-monitor
go test ./... -run 'GroupMonitor|GroupRange'
Pop-Location
Push-Location frontend
npx vitest run src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts
Pop-Location
```

Expected: old ranges, bucket sizes, 168/120 bucket counts, and old query source
cause failures.

- [ ] **Step 3: Implement the exact server-owned range map**

Map `6h/24h/7d/30d` to `900/3600/25200/108000` seconds. Truncate `to` to the
selected bucket size and set `from = to - 24 * bucket`. Change list and detail
queries to aggregate `account_monitor_request_facts` with `date_bin`, deriving
successes, failures, exact counts, and estimated counts from request facts.
Keep SQL bucket expressions parameterized and never accept a client SQL value.

- [ ] **Step 4: Run focused and PostgreSQL integration tests**

```powershell
Push-Location extensions-self/account-monitor
go test ./...
$env:ACCOUNT_MONITOR_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/postgres?sslmode=disable'
go test -tags=integration ./... -run TestPostgresIntegration -count=1
Pop-Location
```

Expected: all tests pass with 24 buckets and exact totals. If the configured
integration database is not running, start the repository's existing test
PostgreSQL service before rerunning; do not skip the test.

- [ ] **Step 5: Commit**

```powershell
git add extensions-self/account-monitor frontend/src/api/admin/accountMonitor.ts frontend/src/views/admin/group-monitor
git commit -m "fix(group-monitor): return fixed 24-bucket ranges"
```

### Task 4: Add Calls-First Results And Stable Timeline Layout

**Files:**
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/admin_backend_test.go`
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Modify: `frontend/src/views/admin/group-monitor/useGroupMonitorFilters.ts`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorFilters.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorTimeline.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorDetailDialog.vue`
- Modify: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorCard.spec.ts`
- Modify: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorDetailDialog.spec.ts`
- Modify: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts`

- [ ] **Step 1: Write failing filter, order, chart, and scrollbar tests**

Test `has_calls` against normal, partial, failed, and idle cards; assert all
nonzero cards precede zero cards without changing their existing relative
order. Mount card and detail components with 24 buckets and assert a 24-track
grid, semantic status classes, `overflow-x-scroll`, and stable scrollbar gutter.

```ts
expect(wrapper.findAll('[data-testid="group-timeline-bar"]')).toHaveLength(24)
expect(wrapper.get('[data-testid="group-timeline"]').classes()).toContain('grid-cols-24')
expect(wrapper.get('[data-testid="group-model-timeline-scroll"]').classes()).toContain('overflow-x-scroll')
```

- [ ] **Step 2: Run focused tests and verify RED**

```powershell
Push-Location extensions-self/account-monitor
go test ./... -run 'GroupMonitor.*(Order|Calls|Filter)'
Pop-Location
Push-Location frontend
npx vitest run src/views/admin/group-monitor/__tests__
Pop-Location
```

Expected: `has_calls`, calls-first ordering, fixed tracks, and explicit scrollbar
assertions fail.

- [ ] **Step 3: Implement filter/order and presentation**

Recognize `has_calls` as `card.TotalRequests > 0`; otherwise preserve detailed
status equality. Use a stable sort that compares only the zero/nonzero
partition because cards were assembled in platform/name/ID order. Render the
timeline as `grid h-16 w-full grid-cols-[repeat(24,minmax(0,1fr))]` and keep the
existing gray/green/amber/red mapping. Set the detail wrapper to horizontal
scroll with `scrollbar-gutter: stable` and enough table width for 24 columns.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run the commands from Step 2. Expected: all selected tests pass.

- [ ] **Step 5: Commit**

```powershell
git add extensions-self/account-monitor frontend/src
git commit -m "fix(group-monitor): prioritize calls and stabilize timelines"
```

### Task 5: Verify, Review, Integrate, And Release

**Files:**
- Modify if findings require it: files listed in Tasks 1-4
- Record: `docs/superpowers/plans/2026-07-16-monitor-fixed-timeline-corrections.md`

- [ ] **Step 1: Run local verification**

```powershell
Push-Location extensions-self/account-monitor
go test ./...
Pop-Location
Push-Location backend
go test ./...
Pop-Location
Push-Location frontend
npm run test:unit -- --run
npm run type-check
npm run lint
npm run build
Pop-Location
git diff --check
```

Run the repository's existing deploy/ops and Compose contract commands from the
July 16 usability-corrections plan. Expected: every command exits 0.

- [ ] **Step 2: Perform browser acceptance locally**

At `1440x900`, `1920x1080`, and `390x844`, verify filter identity, group
multiplier presentation, newest-first trends, calls-first ordering,
`has_calls`, all four ranges, 24 equal bars, semantic colors, and detail
horizontal scrolling. Confirm no console errors, failed requests, overlap, or
unintended page-level overflow.

- [ ] **Step 3: Request independent code review and fix confirmed findings**

Use `superpowers:requesting-code-review`, inspect every finding against the
actual diff, fix confirmed issues with failing tests first, and rerun the
affected suites plus `git diff --check`.

- [ ] **Step 4: Verify clean feature HEAD and push**

```powershell
git status --short --branch
git push origin feature/account-monitor-20260715
```

Expected: clean branch and remote feature ref equals local HEAD.

- [ ] **Step 5: Merge into unchanged custom and push**

In `E:\Code\sub2api`, fetch `origin`, verify `custom` is clean and still equals
the previously observed `origin/custom`, merge the feature branch without
force or reset, rerun merge-sensitive tests, and push `origin/custom`.

- [ ] **Step 6: Back up and publish using the approved production path**

Use `ssh-skill` for every operation on the production host resolved from the external inventory. Confirm no parallel publish,
verify `/root/sub2api` is clean, verify `origin/custom` equals the approved merge
commit, validate Compose, and then run exactly:

```text
/opt/sub2api-custom/publish-custom.sh --commit <approved origin/custom SHA>
```

The publisher must back up both PostgreSQL databases, Compose, `.env`, Nginx,
certificates, container/image metadata, and rollback tags before rebuilding.

- [ ] **Step 7: Complete production data and browser acceptance**

Verify image/commit identity, five container health states, `/health`, extension
health, safe-view allow/deny permissions including the new rate column, data
quality, account/group samples, fixed 24-bucket totals, public HTTPS, absence of
legacy iframe/static/container delivery, and authenticated Edge behavior at all
three viewports. Record backup path, final commit, image IDs, reconciliation
samples, and rollback targets. Roll back with the matching retained images and
configuration if any publish or health gate fails.
