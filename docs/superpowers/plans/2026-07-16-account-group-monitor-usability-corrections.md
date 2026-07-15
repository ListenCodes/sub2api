# Account And Group Monitor Usability Corrections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make account and group monitoring use the complete Sub2API inventory, shared pagination/platform behavior, immediate filters, stable detail dialogs, and seven/thirty-day group ranges.

**Architecture:** extensions-self reads the complete account inventory and account-group memberships through main-database security views, combines them with time-scoped extension facts, then filters, sorts, and paginates the complete candidate set. Vue surfaces share Sub2API platform metadata and configured page sizes, remove periodic refresh, and use cancellable debounced filtering. Group long ranges aggregate the existing 10-minute facts at query time.

**Tech Stack:** Go 1.24, PostgreSQL, `database/sql`, `sqlmock`, Vue 3, TypeScript, Vitest, Vue Test Utils, Tailwind CSS, Docker Compose, Playwright CLI.

---

## File Map

- `extensions-self/account-monitor/sql/main_source_views.sql`, `source.go`: safe account inventory and memberships.
- `extensions-self/account-monitor/http.go`, `admin_backend.go`: page-size/range validation, full inventory merge, detail fields, adaptive buckets.
- `extensions-self/account-monitor/*_test.go`: Go unit and PostgreSQL integration coverage.
- `deploy/ops/publish-custom.sh`, `deploy/tests/account-monitor-contract.test.mjs`: publish-time view verification.
- `frontend/src/utils/platformColors.ts`, `frontend/src/components/common/PlatformBadge.vue`: canonical platform options and badge.
- `frontend/src/components/common/Pagination.vue`: exact configured page sizes.
- `frontend/src/composables/useDebouncedAction.ts`: shared cancellable 300 ms debounce.
- `frontend/src/api/admin/accountMonitor.ts`: groups, long ranges, bucket and page-size contracts.
- `frontend/src/views/admin/account-monitor/`: complete inventory table, filters, and fixed detail dialog.
- `frontend/src/views/admin/group-monitor/`: immediate filters, long ranges, badge/card/detail changes.
- `frontend/src/views/admin/UserRiskControl*.vue`, `frontend/src/components/admin/UserRiskControlUserDrawer.vue`: immediate filters and backdrop close.
- `frontend/src/views/admin/ExtensionsCenterView.vue`: remove duplicate parent heading.
- `docs/ACCOUNT-MONITOR-CHECKLIST.md`, `deploy/RELEASE-RUNBOOK.md`: corrected release checks.

### Task 1: Add The Account-Group Security View And Source Reader

**Files:**
- Modify: `extensions-self/account-monitor/sql/main_source_views.sql`
- Modify: `extensions-self/account-monitor/source.go`
- Test: `extensions-self/account-monitor/source_test.go`
- Modify: `deploy/ops/publish-custom.sh`
- Test: `deploy/tests/account-monitor-contract.test.mjs`

- [ ] **Step 1: Write failing source-contract tests**

Add a test that requires the new safe query and columns:

```go
func TestSourceContractsIncludeAccountInventoryGroups(t *testing.T) {
    lower := strings.ToLower(accountGroupDimensionsQuery)
    if !strings.Contains(lower, "extensions_self_ro.account_group_dimension") {
        t.Fatalf("account groups query bypasses safe view: %s", accountGroupDimensionsQuery)
    }
    for _, column := range []string{"account_id", "group_id", "group_name", "group_platform", "group_status", "group_deleted_at"} {
        if !strings.Contains(lower, column) { t.Errorf("missing %s", column) }
    }
}
```

Require `create or replace view extensions_self_ro.account_group_dimension` in
`TestSourceViewSQLDoesNotExposeSensitiveColumns`, and forbid credentials, key
material, rates, and user columns.

- [ ] **Step 2: Run the tests and verify RED**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestSourceContractsIncludeAccountInventoryGroups|TestSourceViewSQLDoesNotExposeSensitiveColumns' -count=1
```

Expected: FAIL because the view/query is missing.

- [ ] **Step 3: Add the least-privilege view and reader**

```sql
CREATE OR REPLACE VIEW extensions_self_ro.account_group_dimension
WITH (security_barrier = true) AS
SELECT ag.account_id, g.id AS group_id, g.name AS group_name,
       g.platform AS group_platform, g.status AS group_status,
       g.deleted_at AS group_deleted_at
FROM public.account_groups AS ag
JOIN public.groups AS g ON g.id = ag.group_id;
```

Add:

```go
type AccountGroupDimension struct { AccountID int64; Group GroupDimension }
func (s *PostgresSource) ReadAccountDimensions(ctx context.Context) ([]AccountDimension, error)
func (s *PostgresSource) ReadAccountGroupDimensions(ctx context.Context) ([]AccountGroupDimension, error)
```

Both readers use the safe schema, source timeout, ordered results, and no raw
table fallback.

- [ ] **Step 4: Verify GREEN and the publish contract**

Add a publish transaction probe for
`extensions_self_ro.account_group_dimension`, then run:

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestSourceContractsIncludeAccountInventoryGroups|TestSourceViewSQLDoesNotExposeSensitiveColumns' -count=1
Set-Location ../..
node --test deploy/tests/account-monitor-contract.test.mjs
```

Expected: all pass.

- [ ] **Step 5: Commit**

```powershell
git add extensions-self/account-monitor/sql/main_source_views.sql extensions-self/account-monitor/source.go extensions-self/account-monitor/source_test.go deploy/ops/publish-custom.sh deploy/tests/account-monitor-contract.test.mjs
git commit -m "feat(monitor): expose safe account group inventory"
```

### Task 2: Build Full Account Candidates And Multi-Group Filtering

**Files:**
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Test: `extensions-self/account-monitor/admin_backend_test.go`

- [ ] **Step 1: Write failing backend tests**

Use separate sqlmock databases for extension facts and the main source. Seed:

```go
accounts := []AccountDimension{
    {ID: 1, Name: "idle", Platform: "grok", Status: "active"},
    {ID: 2, Name: "busy", Platform: "openai", Status: "active"},
    {ID: 3, Name: "multi", Platform: "anthropic", Status: "active"},
}
memberships := []AccountGroupDimension{
    {AccountID: 2, Group: GroupDimension{ID: 10, Name: "GPT", Platform: "openai", Status: "active"}},
    {AccountID: 3, Group: GroupDimension{ID: 11, Name: "Claude", Platform: "anthropic", Status: "active"}},
    {AccountID: 3, Group: GroupDimension{ID: 12, Name: "Shared", Platform: "anthropic", Status: "active"}},
}
```

Prove total 3, idle metrics are zero and risk unavailable, multi has two groups,
`group_id=11` returns multi once with both groups, `ungrouped` returns idle,
deleted groups do not prevent ungrouped matching, and model/result filters remove
zero-match accounts.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestAdminServiceAccountsUsesFullInventory|TestAdminServiceAccountsFiltersMultipleGroups' -count=1
```

Expected: FAIL because summaries have no groups and facts define candidates.

- [ ] **Step 3: Add focused inventory types and helpers**

```go
type AccountGroupSummary struct {
    GroupID int64 `json:"group_id"`; Name string `json:"name"`
    Platform string `json:"platform"`; Status string `json:"status"`
}
type accountInventory struct {
    Accounts map[int64]AccountDimension
    Groups map[int64][]AccountGroupSummary
}
```

Add `Groups []AccountGroupSummary` to `AccountSummary` and implement:

```go
func (s *AdminService) loadAccountInventory(ctx context.Context, rollup string) (accountInventory, error)
func filterAccountInventory(in accountInventory, query map[string]string) accountInventory
func mergeAccountStats(in accountInventory, stats []AccountSummary, requireFacts bool) []AccountSummary
func sortAccountSummaries(items []AccountSummary, sortBy, order string)
```

- [ ] **Step 4: Merge before paging**

Apply dimension filters first; query unpaged aggregate facts for candidate IDs;
require a fact row when a fact filter is active; add zero rows otherwise; evaluate
health in one batch; stable-sort the complete list; then page. Parent rollup unions
and de-duplicates child memberships. Keep the 5000 risk-candidate safety limit.

- [ ] **Step 5: Verify GREEN and commit**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestAdminServiceAccounts|TestAccountRisk|TestAdminServiceOverview' -count=1
Set-Location ../..
git add extensions-self/account-monitor/admin_backend.go extensions-self/account-monitor/admin_backend_test.go
git commit -m "feat(monitor): include complete account inventory"
```

### Task 3: Fix Page Sizes And Add Adaptive Group Ranges

**Files:**
- Modify: `extensions-self/account-monitor/http.go`
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Test: `extensions-self/account-monitor/http_test.go`
- Test: `extensions-self/account-monitor/admin_backend_test.go`
- Test: `extensions-self/account-monitor/postgres_integration_test.go`

- [ ] **Step 1: Write page-size and range RED tests**

Accept sizes 5, 12, 20, 100, 1000 and reject 4/1001. Add handler cases:

```go
{path: "/group-monitor/groups?range=7d", duration: 7 * 24 * time.Hour, bucketSeconds: 3600},
{path: "/group-monitor/groups?range=30d", duration: 30 * 24 * time.Hour, bucketSeconds: 21600},
```

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestHandlerAcceptsConfiguredPageSizes|TestHandlerRoutesGroupMonitorRanges' -count=1
```

Expected: FAIL on 1000, 7d, and 30d.

- [ ] **Step 3: Implement bounded sizes and closed range metadata**

Add `BucketSeconds int` to `AdminRequest`. Parse any page size 5-1000. Preserve
missing defaults 20 (accounts) and 12 (groups). Use a switch mapping 1h/6h/12h/24h
to 600 seconds, 7d to 3600, and 30d to 21600; all other values return 400.

- [ ] **Step 4: Write adaptive aggregation RED tests**

Seed six adjacent 10-minute rows and assert one hourly bucket; seed 36 rows and
assert one six-hour bucket; prove card/model totals match and a PostgreSQL session
with `SET TIME ZONE 'Asia/Shanghai'` returns the same UTC instants.

- [ ] **Step 5: Implement parameterized query-time aggregation**

Use in list and detail SQL:

```sql
date_bin(make_interval(secs => $4), bucket_at,
         TIMESTAMPTZ '1970-01-01 00:00:00+00') AS display_bucket
```

Group cards by group/display bucket and details by model/display bucket. Return
`bucket_seconds` and complete missing buckets at that duration after normalizing
database timestamps with `UTC()`.

- [ ] **Step 6: Verify GREEN and commit**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestHandler|TestGroupMonitor|TestPostgresMigrationAggregationRebuildAndRetention' -count=1
Set-Location ../..
git add extensions-self/account-monitor/http.go extensions-self/account-monitor/http_test.go extensions-self/account-monitor/admin_backend.go extensions-self/account-monitor/admin_backend_test.go extensions-self/account-monitor/postgres_integration_test.go
git commit -m "feat(monitor): support configured pages and long ranges"
```

### Task 4: Correct Users/API-Key Detail Fields

**Files:**
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Test: `extensions-self/account-monitor/admin_backend_test.go`

- [ ] **Step 1: Write the failing response-contract test**

Require `email`, `api_key_name`, attempts, successes, failures, `success_rate`,
tokens, `user_cost`, and `last_attempted_at`; forbid `username` and
`masked_prefix` in every returned row.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run TestUsersDetailResponseFields -count=1
```

Expected: FAIL for missing/new and forbidden/old fields.

- [ ] **Step 3: Extend the aggregate and minimize the response**

Add `MAX(attempted_at)` to `usersSQL`, compute:

```go
successRate := 0.0
if attempts > 0 { successRate = float64(successes) / float64(attempts) }
```

Read only email and API-key name into the response. Keep IDs for row identity but
do not emit username or masked prefix.

- [ ] **Step 4: Verify GREEN and commit**

```powershell
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test ./... -run 'TestUsersDetail|TestSource' -count=1
Set-Location ../..
git add extensions-self/account-monitor/admin_backend.go extensions-self/account-monitor/admin_backend_test.go
git commit -m "refactor(monitor): simplify user key details"
```

### Task 5: Fix Shared Pagination And Add Shared Platform Badge

**Files:**
- Modify: `frontend/src/components/common/Pagination.vue`
- Create: `frontend/src/components/common/__tests__/Pagination.pageSizeOptions.spec.ts`
- Modify: `frontend/src/utils/platformColors.ts`
- Create: `frontend/src/components/common/PlatformBadge.vue`
- Create: `frontend/src/components/common/__tests__/PlatformBadge.spec.ts`

- [ ] **Step 1: Write Pagination RED tests**

Mount with `pageSizeOptions: [20, 100, 1000]`, assert exact Select options and
that choosing 1000 emits 1000. Mount with explicit `[12, 24]` and assert no
global option leaks into it:

```ts
expect(wrapper.findComponent(Select).props('options')).toEqual([
  { value: 20, label: '20' }, { value: 100, label: '100' }, { value: 1000, label: '1000' },
])
```

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/components/common/__tests__/Pagination.pageSizeOptions.spec.ts
```

Expected: FAIL because `Pagination` ignores its prop.

- [ ] **Step 3: Implement exact option handling**

Use sanitized explicit options when supplied, otherwise global configured
options; include the current value only when needed. Normalize a selection
against that same list, not an unrelated global list.

- [ ] **Step 4: Write and implement PlatformBadge through RED/GREEN**

Test all supported labels/icons/classes and neutral unknown fallback. Export:

```ts
export const SUPPORTED_PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' }, { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' }, { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
] as const
```

Build `PlatformBadge.vue` from `PlatformIcon`, `platformBadgeLightClass`, and
`platformLabel`; do not introduce a new color map.

- [ ] **Step 5: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/components/common/__tests__/Pagination.pageSizeOptions.spec.ts src/components/common/__tests__/PlatformBadge.spec.ts
Set-Location ..
git add frontend/src/components/common/Pagination.vue frontend/src/components/common/PlatformBadge.vue frontend/src/components/common/__tests__/Pagination.pageSizeOptions.spec.ts frontend/src/components/common/__tests__/PlatformBadge.spec.ts frontend/src/utils/platformColors.ts
git commit -m "fix(frontend): share monitor paging and platform badges"
```

### Task 6: Add A Cancellable Debounce Primitive

**Files:**
- Create: `frontend/src/composables/useDebouncedAction.ts`
- Create: `frontend/src/composables/__tests__/useDebouncedAction.spec.ts`

- [ ] **Step 1: Write fake-timer RED tests**

Cover one execution after 300 ms, replacement of an earlier call, immediate
flush for selects, and cancellation on unmount:

```ts
const action = vi.fn()
const { schedule } = useDebouncedAction(action, 300)
schedule(); schedule()
await vi.advanceTimersByTimeAsync(299)
expect(action).not.toHaveBeenCalled()
await vi.advanceTimersByTimeAsync(1)
expect(action).toHaveBeenCalledTimes(1)
```

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/composables/__tests__/useDebouncedAction.spec.ts
```

Expected: FAIL because the composable is missing.

- [ ] **Step 3: Implement the composable**

```ts
export function useDebouncedAction(action: () => void | Promise<void>, delay = 300) {
  let timer: number | undefined
  const cancel = () => { if (timer !== undefined) window.clearTimeout(timer); timer = undefined }
  const runNow = () => { cancel(); return action() }
  const schedule = () => { cancel(); timer = window.setTimeout(() => { timer = undefined; void action() }, delay) }
  onBeforeUnmount(cancel)
  return { schedule, runNow, cancel }
}
```

- [ ] **Step 4: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/composables/__tests__/useDebouncedAction.spec.ts
Set-Location ..
git add frontend/src/composables/useDebouncedAction.ts frontend/src/composables/__tests__/useDebouncedAction.spec.ts
git commit -m "feat(frontend): add immediate filter debounce"
```

### Task 7: Correct Account Monitor Filters, Inventory, And Table

**Files:**
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Test: `frontend/src/api/admin/__tests__/accountMonitor.spec.ts`
- Modify: `frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorFilters.vue`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorPanel.vue`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorTable.vue`
- Test: `frontend/src/views/admin/account-monitor/__tests__/useAccountMonitorFilters.spec.ts`
- Test: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts`

- [ ] **Step 1: Write account UI RED tests**

Use a zero-data account and a two-group account. Assert both render, platform and
group badges render, the total is the complete inventory, no auto-refresh toggle
exists, page size 1000 survives parse/serialize/change, text waits 300 ms,
platform/group selects query immediately, and `group_id=ungrouped` serializes.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/api/admin/__tests__/accountMonitor.spec.ts src/views/admin/account-monitor/__tests__/useAccountMonitorFilters.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts
```

Expected: FAIL on groups, 1000, badge, and timer behavior.

- [ ] **Step 3: Extend API and route types**

```ts
export interface AccountGroupSummary { group_id: number; name: string; platform: string; status: string }
// AccountMonitorAccount.groups: AccountGroupSummary[]
// AccountFilters.groupID?: number | 'ungrouped'
```

Use numeric page sizes validated 5-1000, serialize `group_id`, and read choices
from `getConfiguredTablePageSizeOptions()`.

- [ ] **Step 4: Implement immediate filters and manual refresh only**

Delete `autoRefresh`, interval state, interval setup, and toggle markup.
`AccountMonitorFilters` receives all current groups, uses all supported platform
options, schedules text/number changes, runs selects immediately, removes Apply,
and keeps Reset. Every filter change resets page to 1 and updates the route.

- [ ] **Step 5: Render multi-group and platform badges**

Use `PlatformBadge` in the platform cell. Add a group column that displays all
memberships in platform/name order and `未分组` for an empty array. Never collapse
a multi-group account to the selected filter group.

- [ ] **Step 6: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/api/admin/__tests__/accountMonitor.spec.ts src/views/admin/account-monitor/__tests__
npm run typecheck
Set-Location ..
git add frontend/src/api/admin/accountMonitor.ts frontend/src/api/admin/__tests__/accountMonitor.spec.ts frontend/src/views/admin/account-monitor
git commit -m "feat(frontend): show complete grouped account inventory"
```

### Task 8: Stabilize And Simplify Account Detail

**Files:**
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorDrawer.vue`
- Modify: `frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts`
- Test: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts`
- Test: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorDialogs.spec.ts`

- [ ] **Step 1: Write dialog RED tests**

Assert no Attempts tab/request; user columns include success rate and recent time
but exclude username/prefix; the shell is fixed at 80vh; content owns overflow;
table headers are sticky; backdrop/Escape/close emit close while inner clicks do
not.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorDialogs.spec.ts
```

Expected: FAIL on current six-tab and flexible-height behavior.

- [ ] **Step 3: Implement the fixed five-tab dialog**

Remove `attempts` from `AccountDetailTab`, tab definitions, dispatch, and mocks.
Pass `:close-on-click-outside="true"`. Use an
`h-[80vh] max-h-[80vh] flex flex-col` shell, a
`min-h-0 flex-1 overflow-auto` content region, and `sticky top-0 z-10` headers.
Format success rate, cost, and `last_attempted_at` explicitly.

- [ ] **Step 4: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/account-monitor/__tests__
Set-Location ..
git add frontend/src/views/admin/account-monitor/AccountMonitorDrawer.vue frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts frontend/src/views/admin/account-monitor/__tests__
git commit -m "fix(frontend): stabilize account detail dialog"
```

### Task 9: Correct Group Monitor Filters, Cards, And Long Ranges

**Files:**
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Modify: `frontend/src/views/admin/group-monitor/useGroupMonitorFilters.ts`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorFilters.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorPanel.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorCard.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorTimeline.vue`
- Modify: `frontend/src/views/admin/group-monitor/GroupMonitorDetailDialog.vue`
- Test: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorPanel.spec.ts`
- Test: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorCard.spec.ts`
- Test: `frontend/src/views/admin/group-monitor/__tests__/GroupMonitorDetailDialog.spec.ts`

- [ ] **Step 1: Write group UI RED tests**

Assert 7d/30d route state, 1000 persistence, all supported platforms, 300 ms text
debounce, immediate selects, no Apply/auto-refresh, `text-base` group names,
shared badge, and one-hour/six-hour accessibility labels from `bucket_seconds`.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/group-monitor/__tests__ src/api/admin/__tests__/accountMonitor.spec.ts
```

Expected: FAIL on long range, paging, and presentation contracts.

- [ ] **Step 3: Implement filters and manual refresh only**

Extend `GroupRange` with `7d|30d`; remove interval/toggle code; use global page
sizes and all supported platforms; debounce group name and immediately run
selects; remove Apply and retain Reset.

- [ ] **Step 4: Implement card/detail presentation**

Use `PlatformBadge`; set names to `text-base font-semibold`; pass
`bucket_seconds` into timeline/detail labels; enable backdrop close. Preserve the
approved platform/name ordering.

- [ ] **Step 5: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/group-monitor/__tests__ src/api/admin/__tests__/accountMonitor.spec.ts
npm run typecheck
Set-Location ..
git add frontend/src/api/admin/accountMonitor.ts frontend/src/views/admin/group-monitor
git commit -m "feat(frontend): improve group monitor range and filters"
```

### Task 10: Align User-Risk Filters And Remove Parent Heading

**Files:**
- Modify: `frontend/src/views/admin/ExtensionsCenterView.vue`
- Modify: `frontend/src/views/admin/UserRiskControlUsersView.vue`
- Modify: `frontend/src/views/admin/UserRiskControlAuditView.vue`
- Modify: `frontend/src/views/admin/UserRiskControlRulesView.vue`
- Modify: `frontend/src/components/admin/UserRiskControlUserDrawer.vue`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts`
- Test: `frontend/src/router/__tests__/user-risk-control-routes.spec.ts`
- Test: `frontend/src/router/__tests__/account-monitor-route.spec.ts`

- [ ] **Step 1: Write heading/filter/backdrop RED tests**

Assert RouterView renders without `扩展中心` or `用户安全与运行质量`; user
search/score inputs debounce 300 ms; selects/toggles query immediately; audit
actor/date filters follow the same rule; Rules command buttons remain explicit;
and detail/editor dialogs opt into backdrop close.

- [ ] **Step 2: Run and verify RED**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/__tests__/UserRiskControlUsersView.spec.ts src/views/admin/__tests__/UserRiskControlRulesView.spec.ts src/views/admin/__tests__/UserRiskControlAuditView.spec.ts src/router/__tests__/user-risk-control-routes.spec.ts src/router/__tests__/account-monitor-route.spec.ts
```

Expected: FAIL for parent heading and non-debounced user inputs.

- [ ] **Step 3: Implement shared immediate behavior**

Remove parent header and dependent top padding. In Users, search/score inputs
schedule the shared debounce while selects/toggles run now; each run resets page
and syncs route. Apply the same pattern to Audit text/date filters. Preserve
Save/Create/Test in Rules because they are commands, not filters. Pass
`closeOnClickOutside=true` to user detail and rule editor dialogs.

- [ ] **Step 4: Verify and commit**

```powershell
Set-Location frontend
npm run test:run -- src/views/admin/__tests__/UserRiskControlUsersView.spec.ts src/views/admin/__tests__/UserRiskControlRulesView.spec.ts src/views/admin/__tests__/UserRiskControlAuditView.spec.ts src/router/__tests__/user-risk-control-routes.spec.ts src/router/__tests__/account-monitor-route.spec.ts
Set-Location ..
git add frontend/src/views/admin/ExtensionsCenterView.vue frontend/src/views/admin/UserRiskControlUsersView.vue frontend/src/views/admin/UserRiskControlRulesView.vue frontend/src/views/admin/UserRiskControlAuditView.vue frontend/src/components/admin/UserRiskControlUserDrawer.vue frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts frontend/src/router/__tests__/user-risk-control-routes.spec.ts frontend/src/router/__tests__/account-monitor-route.spec.ts
git commit -m "fix(frontend): align extension filter interactions"
```

### Task 11: Update Operational Documentation And Contracts

**Files:**
- Modify: `docs/ACCOUNT-MONITOR-CHECKLIST.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`

- [ ] **Step 1: Add documentation contract assertions first**

Require checklist/runbook text for `account_group_dimension`, all-account count,
multi-group samples, page size 1000, 7d/30d reconciliation, and manual refresh.

- [ ] **Step 2: Run contracts and verify RED**

```powershell
node --test deploy/tests/account-monitor-contract.test.mjs
```

Expected: FAIL on missing new runbook/checklist phrases.

- [ ] **Step 3: Update exact release checks**

Document safe-view allow/deny queries, inventory count versus fact-active count,
multi-group sampling, adaptive-range totals, browser checks, and rollback. Remove
requirements for periodic auto refresh and six account-detail tabs.

- [ ] **Step 4: Verify and commit**

```powershell
node --test deploy/tests/account-monitor-contract.test.mjs
git add docs/ACCOUNT-MONITOR-CHECKLIST.md deploy/RELEASE-RUNBOOK.md deploy/tests/account-monitor-contract.test.mjs
git commit -m "docs: add monitor correction release checks"
```

### Task 12: Full Local Verification And Independent Review

**Files:**
- Modify only exact files implicated by confirmed review findings; commit each
  accepted finding with its explicit paths.

- [ ] **Step 1: Run extensions-self PostgreSQL integration tests**

```powershell
docker run --rm -d --name sub2api-monitor-test-postgres -e POSTGRES_PASSWORD=postgres -p 55432:5432 postgres:17-alpine
$env:ACCOUNT_MONITOR_TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/postgres?sslmode=disable'
Set-Location extensions-self/account-monitor
& 'D:\Go\bin\go.exe' test -count=1 ./...
Set-Location ../risk-control
& 'D:\Go\bin\go.exe' test -count=1 ./...
Set-Location ../..
docker stop sub2api-monitor-test-postgres
```

Expected: both suites PASS and the PostgreSQL test does not skip.

- [ ] **Step 2: Run complete backend tests**

```powershell
Set-Location backend
& 'D:\Go\bin\go.exe' test -count=1 -p 1 ./...
Set-Location ..
```

Expected: PASS.

- [ ] **Step 3: Run complete frontend validation**

```powershell
Set-Location frontend
npm run test:run
npm run typecheck
npm run lint:check
npm run build
Set-Location ..
```

Expected: all exit 0.

- [ ] **Step 4: Run deployment and repository checks**

```powershell
node --test deploy/tests/*.test.mjs
git diff --check
git status --short
```

Expected: contracts pass, diff check has no output, and no unintended file is
present.

- [ ] **Step 5: Run local browser acceptance**

Start the existing QA API harness on `127.0.0.1:8080` and Vite on 4174, then use
one Playwright CLI session at 1440x900, 1920x1080, and 390x844. Verify all five
platforms, a complete inventory fixture including zero rows, multi-group filter,
page size 1000, fixed/sticky detail, backdrop close, immediate filters, no
auto-refresh, 7d/30d, no parent heading, no console errors, successful API
requests, no overlap, and no page-level horizontal overflow. Save screenshots
under `$env:TEMP\sub2api-monitor-corrections-qa` and close both local services.

- [ ] **Step 6: Request independent code review**

Use `superpowers:requesting-code-review` with both approved specifications, the
implementation commit range, and test/browser evidence. Use
`superpowers:receiving-code-review` for findings, apply only technically
confirmed issues, commit them with their exact files, and rerun affected suites.

### Task 13: Push, Merge, Publish, Reconcile, And Verify Production

**Files:**
- No source edits unless a failed gate produces a separately tested fix.

- [ ] **Step 1: Verify feature branch and push**

Use `superpowers:verification-before-completion`. Confirm clean status, then:

```powershell
git push origin feature/account-monitor-20260715
```

- [ ] **Step 2: Merge into unchanged custom**

In `E:\Code\sub2api`, fetch origin, verify `custom` is clean and its remote
baseline did not change, merge the feature branch without reset/rebase, rerun
merge-sensitive Go/frontend/deploy tests, then:

```powershell
git push origin custom
```

- [ ] **Step 3: Verify production gates through ssh-skill**

Resolve the final commit locally:

```powershell
$finalCommit = (git ls-remote origin refs/heads/custom).Split("`t")[0]
```

Through ssh-skill, verify `/root/sub2api` is clean, no publish process is active,
the fetched `origin/custom` equals `$finalCommit`, and Compose validates. Never
use direct SSH/SCP.

- [ ] **Step 4: Publish only through the approved entrypoint**

After backups of both databases, Compose, `.env`, Nginx, certificates,
container/image metadata, and rollback tags succeed, invoke through ssh-skill:

```text
/opt/sub2api-custom/publish-custom.sh --commit $finalCommit
```

- [ ] **Step 5: Reconcile production data**

Verify five containers, main/extension health, safe-view allow/deny permissions,
signed data quality, and legacy-container absence. Compare main non-deleted
account count with monitor total, sample multi-group accounts, compare 7d/30d
group totals with 10-minute facts, and record backup path, images, commit,
backfill jobs, and rollback targets.

- [ ] **Step 6: Run authenticated production browser acceptance**

Verify User Risk Users/Rules/Audit, Account Monitor, and Group Monitor at all
three viewports. Exercise immediate filtering, Reset, manual Refresh, 1000 rows,
multi-group display, platform badges, account detail scrolling/sticky headers,
backdrop closing, group details, and 7d/30d. Check console, network, overlap, and
overflow.

- [ ] **Step 7: Complete or roll back**

If every check passes, record the release healthy and call
`update_goal(status: "complete")`. If build, migration, reconciliation, or
health fails, stop or restore both approved rollback images/configuration,
preserve facts/cursors unless corruption is proven, retain diagnostics, and
report rollback separately.
