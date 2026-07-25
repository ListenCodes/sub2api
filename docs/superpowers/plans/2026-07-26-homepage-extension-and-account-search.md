# Homepage Extension And Account Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the extension homepage use configured branding and live public group rates, restore brand navigation to `/home`, and add administrator account-monitor search by configured name or actual account identity.

**Architecture:** Keep anonymous homepage behavior in `extensions-self`: a dedicated security-barrier view feeds a narrowly typed source reader and an exact no-store JSON endpoint below the existing homepage proxy. Extend the existing signed account-monitor API with one derived identity field and server-side pre-pagination search. Official backend routes and CSP remain unchanged; native frontend edits are limited to the sidebar and existing account-monitor components.

**Tech Stack:** Go 1.26, PostgreSQL security views, net/http, Vue 3, TypeScript, Vitest, Node test runner, pnpm, Docker-based Go tests.

---

### Task 1: Track The Approved Design And Plan

**Files:**
- Add: `docs/superpowers/specs/2026-07-26-homepage-extension-live-branding-design.md`
- Add: `docs/superpowers/plans/2026-07-26-homepage-extension-and-account-search.md`

- [ ] **Step 1: Force-add the ignored planning artifacts**

Run:

```powershell
git add -f docs/superpowers/specs/2026-07-26-homepage-extension-live-branding-design.md docs/superpowers/plans/2026-07-26-homepage-extension-and-account-search.md
git diff --cached --check
```

Expected: both Markdown files are staged and no whitespace error is reported.

- [ ] **Step 2: Commit the approved design**

```powershell
git commit -m "docs: design extension homepage and account search"
```

Expected: one documentation commit on `feature/homepage-extension-live-branding`.

### Task 2: Extend The Safe Source Contract

**Files:**
- Modify: `extensions-self/account-monitor/sql/main_source_views.sql`
- Modify: `extensions-self/account-monitor/source.go`
- Modify: `extensions-self/account-monitor/source_test.go`
- Modify: `extensions-self/account-monitor/source_reader_test.go`
- Modify: `extensions-self/account-monitor/postgres_integration_test.go`

- [ ] **Step 1: Write failing source-contract tests**

Add tests that require `public_group_catalog`, verify its filter predicates, and require `account_identity` to be appended to `account_dimension`. Replace the existing blanket ban on the text `a.credentials` with structural assertions: require only the exact `NULLIF(a.credentials ->> 'email', '')` and parent-email expressions, and continue to reject raw `credentials` in any view output. Add the SQL mock test to `source_reader_test.go`, which already owns `newSourceMock`:

```go
func TestPostgresSourceReadPublicGroups(t *testing.T) {
    db, mock := newSourceMock(t)
    mock.ExpectQuery(regexp.QuoteMeta(publicGroupsQuery)).WillReturnRows(
        sqlmock.NewRows([]string{"name", "platform", "rate_multiplier", "peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier"}).
            AddRow("GPT Pro", "openai", 0.3, true, "14:00", "22:00", 1.2),
    )
    items, err := NewPostgresSource(db, time.Second, 100).ReadPublicGroups(context.Background())
    require.NoError(t, err)
    require.Equal(t, "GPT Pro", items[0].Name)
    require.InDelta(t, 0.3, items[0].RateMultiplier, 0.0001)
}
```

Extend integration fixtures with safe email-only identity extraction and assert the restricted role can read the derived value and public catalog but still cannot select `public.accounts` or raw `credentials`.

- [ ] **Step 2: Run the Go tests and verify RED**

Upload `extensions-self/account-monitor` to an exact temporary directory on `US-RN-66` with `ssh-skill`, then run:

```bash
docker run --rm -v /tmp/sub2api-homepage-tests/account-monitor:/src:ro -w /src golang:1.26.5 go test -count=1 ./...
```

Expected: FAIL because `publicGroupsQuery`, `ReadPublicGroups`, `PublicGroup`, `account_identity`, and `public_group_catalog` do not exist.

- [ ] **Step 3: Implement the safe view and reader**

Append `account_identity` to the existing view and add the dedicated public view:

```sql
CREATE OR REPLACE VIEW extensions_self_ro.account_dimension
WITH (security_barrier = true) AS
SELECT
    a.id, a.parent_account_id, a.name, a.platform, a.status, a.schedulable, a.deleted_at,
    COALESCE(
        NULLIF(a.extra ->> 'email_address', ''),
        NULLIF(a.extra ->> 'email', ''),
        NULLIF(a.credentials ->> 'email', ''),
        NULLIF(parent.credentials ->> 'email', ''),
        ''
    ) AS account_identity
FROM public.accounts AS a
LEFT JOIN public.accounts AS parent ON parent.id = a.parent_account_id;

CREATE OR REPLACE VIEW extensions_self_ro.public_group_catalog
WITH (security_barrier = true) AS
SELECT name, platform, rate_multiplier, peak_rate_enabled, peak_start, peak_end,
       peak_rate_multiplier, sort_order
FROM public.groups
WHERE status = 'active' AND deleted_at IS NULL AND is_exclusive = FALSE;
```

Add `AccountIdentity string` to `AccountDimension`, add the following public type and method, and append the identity scan in both account-dimension readers:

```go
type PublicGroup struct {
    Name               string  `json:"name"`
    Platform           string  `json:"platform"`
    RateMultiplier     float64 `json:"rate_multiplier"`
    PeakRateEnabled    bool    `json:"peak_rate_enabled"`
    PeakStart          string  `json:"peak_start"`
    PeakEnd            string  `json:"peak_end"`
    PeakRateMultiplier float64 `json:"peak_rate_multiplier"`
}

func (s *PostgresSource) ReadPublicGroups(ctx context.Context) ([]PublicGroup, error) {
    if s == nil || s.db == nil {
        return nil, errors.New("account monitor source database is nil")
    }
    ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
    defer cancel()
    rows, err := s.db.QueryContext(ctx, publicGroupsQuery)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    result := make([]PublicGroup, 0)
    for rows.Next() {
        var item PublicGroup
        if err := rows.Scan(
            &item.Name,
            &item.Platform,
            &item.RateMultiplier,
            &item.PeakRateEnabled,
            &item.PeakStart,
            &item.PeakEnd,
            &item.PeakRateMultiplier,
        ); err != nil {
            return nil, err
        }
        result = append(result, item)
    }
    return result, rows.Err()
}

const publicGroupsQuery = `
SELECT name, platform, rate_multiplier, peak_rate_enabled, peak_start, peak_end,
       peak_rate_multiplier
FROM extensions_self_ro.public_group_catalog
ORDER BY sort_order, LOWER(platform), LOWER(name)`
```

The SQL query orders by `sort_order`, `LOWER(platform)`, then `LOWER(name)` and does not select `sort_order` into the public DTO.

- [ ] **Step 4: Run the Go tests and verify GREEN**

Repeat the temporary upload and Docker test command.

Expected: account-monitor unit and integration-contract tests PASS. Database-backed integration tests may skip only when their documented DSN is absent.

- [ ] **Step 5: Commit the source contract**

```powershell
git add extensions-self/account-monitor/sql/main_source_views.sql extensions-self/account-monitor/source.go extensions-self/account-monitor/source_test.go extensions-self/account-monitor/postgres_integration_test.go
git commit -m "feat: expose safe homepage group catalog"
```

### Task 3: Add Account Identity Search To The Extension Service

**Files:**
- Modify: `extensions-self/account-monitor/admin_backend.go`
- Modify: `extensions-self/account-monitor/admin_backend_test.go`

- [ ] **Step 1: Write failing search tests**

Add focused tests around `filterAccountInventory` and `mergeAccountStats`:

```go
func TestFilterAccountInventoryMatchesNameOrIdentity(t *testing.T) {
    inventory := accountInventory{
        Accounts: map[int64]AccountDimension{
            1: {ID: 1, Name: "Primary OpenAI", AccountIdentity: "owner@example.com"},
            2: {ID: 2, Name: "Backup Claude", AccountIdentity: "backup@example.com"},
        },
        Members: map[int64][]AccountDimension{
            1: {{ID: 1, Name: "Primary OpenAI", AccountIdentity: "owner@example.com"}},
            2: {{ID: 2, Name: "Backup Claude", AccountIdentity: "backup@example.com"}},
        },
        Groups: map[int64][]AccountGroupSummary{},
    }
    require.Contains(t, filterAccountInventory(inventory, map[string]string{"query": "PRIMARY"}).Accounts, int64(1))
    require.Contains(t, filterAccountInventory(inventory, map[string]string{"query": "OWNER@EXAMPLE"}).Accounts, int64(1))
}
```

Add a parent-rollup case where a child identity retains the parent row, and assert `AccountSummary.AccountIdentity` is returned.

- [ ] **Step 2: Run account-monitor tests and verify RED**

Expected: FAIL because inventory filtering ignores `query` and the response has no `account_identity`.

- [ ] **Step 3: Implement pre-pagination search**

Add the field:

```go
AccountIdentity string `json:"account_identity,omitempty"`
```

In `filterAccountInventory`, normalize `request.Query["query"]` with `strings.ToLower(strings.TrimSpace(...))` and retain a rollup row when any member name or identity contains it. In `mergeAccountStats`, copy the selected dimension's identity to the response. Keep all filtering before stats merge, sorting, total calculation, and pagination.

- [ ] **Step 4: Run account-monitor tests and verify GREEN**

Expected: all account-monitor tests PASS, including physical and parent-rollup search.

- [ ] **Step 5: Commit account search backend**

```powershell
git add extensions-self/account-monitor/admin_backend.go extensions-self/account-monitor/admin_backend_test.go
git commit -m "feat: search monitored accounts by identity"
```

### Task 4: Add The Public Homepage Group Endpoint

**Files:**
- Create: `extensions-self/risk-control/homepage_groups.go`
- Modify: `extensions-self/risk-control/http.go`
- Modify: `extensions-self/risk-control/homepage_test.go`
- Modify: `extensions-self/risk-control/account_monitor_runtime.go`
- Modify: `extensions-self/risk-control/main.go`
- Modify: `extensions-self/risk-control/account_monitor_test.go`

- [ ] **Step 1: Write failing HTTP and runtime tests**

Define a small fake implementing the desired reader and assert:

```go
func TestHomepagePublicGroups(t *testing.T) {
    server := newHomepageServer(t)
    server.publicGroups = staticPublicGroupReader{items: []accountmonitor.PublicGroup{{Name: "GPT Pro", Platform: "openai", RateMultiplier: 0.3}}}
    request := httptest.NewRequest(http.MethodGet, "/homepage/api/public-groups", nil)
    recorder := httptest.NewRecorder()
    server.ServeHTTP(recorder, request)
    require.Equal(t, http.StatusOK, recorder.Code)
    require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
    require.JSONEq(t, `{"groups":[{"name":"GPT Pro","platform":"openai","rate_multiplier":0.3,"peak_rate_enabled":false,"peak_start":"","peak_end":"","peak_rate_multiplier":0}]}`, recorder.Body.String())
}
```

Add HEAD/no-body, POST/405, missing-reader/503, reader-error/503 generic-body, and runtime-with-DSN-but-monitor-disabled tests.

- [ ] **Step 2: Run risk-control tests and verify RED**

Upload the whole local `extensions-self` tree to the temporary remote directory because risk-control replaces the sibling account-monitor module. Run:

```bash
docker run --rm -v /tmp/sub2api-homepage-tests/extensions-self:/src:ro -w /src/risk-control golang:1.26.5 go test -count=1 ./...
```

Expected: FAIL because `publicGroups` and the exact route do not exist and the runtime is still coupled to `ACCOUNT_MONITOR_ENABLED`.

- [ ] **Step 3: Implement the reader dependency and endpoint**

Create a local interface and handler:

```go
type publicGroupReader interface {
    ReadPublicGroups(context.Context) ([]accountmonitor.PublicGroup, error)
}

func (s *HTTPServer) handlePublicGroups(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    if r.Method != http.MethodGet && r.Method != http.MethodHead {
        w.Header().Set("Allow", "GET, HEAD")
        writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
        return
    }
    if s.publicGroups == nil {
        writeError(w, http.StatusServiceUnavailable, errors.New("public groups unavailable"))
        return
    }
    items, err := s.publicGroups.ReadPublicGroups(r.Context())
    if err != nil {
        log.Printf("read public groups: %v", err)
        writeError(w, http.StatusServiceUnavailable, errors.New("public groups unavailable"))
        return
    }
    if r.Method == http.MethodHead {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"groups": items})
}
```

Intercept only `/homepage/api/public-groups` before the static homepage prefix branch. Extend `accountMonitorRuntime` with `source *accountmonitor.PostgresSource`; open the source whenever the DSN is configured, build monitor handler/collector only when enabled, and start the collector only when non-nil. Wire `server.publicGroups = runtime.source` without adding an official main route.

- [ ] **Step 4: Run risk-control and account-monitor tests and verify GREEN**

Expected: both Go modules PASS in the remote one-shot containers.

- [ ] **Step 5: Commit the endpoint**

```powershell
git add extensions-self/risk-control/homepage_groups.go extensions-self/risk-control/http.go extensions-self/risk-control/homepage_test.go extensions-self/risk-control/account_monitor_runtime.go extensions-self/risk-control/main.go extensions-self/risk-control/account_monitor_test.go
git commit -m "feat: serve live public group rates from extension"
```

### Task 5: Replace Homepage Hardcoding With Dynamic Branding And Rates

**Files:**
- Modify: `extensions-self/homepage/index.html`
- Modify: `deploy/tests/extensions-self-layout.test.mjs`

- [ ] **Step 1: Write failing static homepage contract tests**

Require all of the following:

```js
assert.doesNotMatch(homepage, /fonts\.googleapis\.com|\/logo\.png/)
assert.match(homepage, /fetch\(['"]\/api\/v1\/settings\/public['"]/)
assert.match(homepage, /fetch\(['"]api\/public-groups['"]/)
assert.match(homepage, /href="\/home"[^>]*target="_top"/)
assert.doesNotMatch(homepage, /Claude \u7279\u4ef7|GPT PLUS \u7279\u4ef7|0\.001x/)
```

Also require explicit loading, empty, and unavailable states and safe DOM text insertion.

- [ ] **Step 2: Run Node tests and verify RED**

```powershell
node --test deploy/tests/extensions-self-layout.test.mjs
```

Expected: FAIL on Google Fonts, `/logo.png`, `/` brand link, missing fetches, and hardcoded rates.

- [ ] **Step 3: Implement the approved B layout**

Remove the font import and hardcoded rate rows. Use a system font stack and stable responsive dimensions. Add IDs/data attributes for brand, subtitle, logo, hero eyebrow, rate container, and status. On `DOMContentLoaded`, run independent branding and group requests with `cache: 'no-store'`.

Validate logo sources as same-origin paths or `data:image/...`; otherwise use `/logo.svg`. Set document title, navigation name/subtitle, hero eyebrow, and footer through `textContent`. Group live rows by platform, build every admin-controlled string with `createElement` plus `textContent`, format base multipliers to at most four decimals, and render peak time/factor only when enabled. Preserve `target="_top"`, set the brand link to `/home`, and keep the dashboard CTA unchanged.

- [ ] **Step 4: Run Node tests and verify GREEN**

Expected: extension layout tests PASS with no hardcoded rate names.

- [ ] **Step 5: Commit the homepage**

```powershell
git add extensions-self/homepage/index.html deploy/tests/extensions-self-layout.test.mjs
git commit -m "feat: render homepage branding and live rates"
```

### Task 6: Restore Native Brand Navigation

**Files:**
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`

- [ ] **Step 1: Change the test expectation first**

Require a role-independent home route:

```ts
expect(componentSource).toContain("const homePath = '/home'")
expect(componentSource.match(/:to="homePath"/g)).toHaveLength(2)
```

- [ ] **Step 2: Run the focused test and verify RED**

```powershell
pnpm test:run src/components/layout/__tests__/AppSidebar.spec.ts
```

Expected: FAIL because `homePath` still selects admin/user dashboards.

- [ ] **Step 3: Implement the one-line destination change**

Replace the role-aware computed destination with:

```ts
const homePath = '/home'
```

Keep both existing router links and mobile click handling unchanged.

- [ ] **Step 4: Run sidebar tests and verify GREEN**

Expected: AppSidebar and logo-sanitization tests PASS.

- [ ] **Step 5: Commit native navigation**

```powershell
git add frontend/src/components/layout/AppSidebar.vue frontend/src/components/layout/__tests__/AppSidebar.spec.ts
git commit -m "fix: restore sidebar brand home navigation"
```

### Task 7: Add Account Search To The Native Monitor UI

**Files:**
- Modify: `frontend/src/api/admin/accountMonitor.ts`
- Modify: `frontend/src/api/admin/__tests__/accountMonitor.spec.ts`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorFilters.vue`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorPanel.vue`
- Modify: `frontend/src/views/admin/account-monitor/AccountMonitorTable.vue`
- Modify: `frontend/src/views/admin/account-monitor/useAccountMonitorFilters.ts`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorFilters.spec.ts`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts`
- Modify: `frontend/src/views/admin/account-monitor/__tests__/useAccountMonitorFilters.spec.ts`

- [ ] **Step 1: Write failing API, state, filter, and table tests**

Require `query` in API serialization and URL state, require the filter label and placeholder, and mount the real table with `account_identity`:

```ts
expect(apiClient.get).toHaveBeenCalledWith(
  '/admin/extensions-self/account-monitor/accounts',
  expect.objectContaining({ params: expect.objectContaining({ query: 'owner@example.com' }) }),
)
expect(wrapper.get('[data-testid="account-filter-query-label"]').text()).toBe('账号')
expect(wrapper.get('input[placeholder="搜索账号名称或实际账号"]')).toBeTruthy()
expect(wrapper.text()).toContain('owner@example.com')
```

Verify a debounced change emits `apply` with `{ query }`, route round-trip preserves it, and panel requests reset to page 1 after search.

- [ ] **Step 2: Run focused frontend tests and verify RED**

```powershell
pnpm test:run src/api/admin/__tests__/accountMonitor.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorFilters.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts src/views/admin/account-monitor/__tests__/useAccountMonitorFilters.spec.ts
```

Expected: FAIL because the type, parameter, state, control, and table field are missing.

- [ ] **Step 3: Implement frontend query and identity display**

Add `account_identity?: string` to `AccountMonitorAccount`, `query?: string` to `AccountFilters`, and `query: filters.query` to `accountParams`. Parse and serialize route key `query`; include `state.query` in `requestFilters`.

Add this control before platform filters:

```vue
<label class="flex w-full items-center gap-2 sm:w-auto">
  <span class="input-label !mb-0 shrink-0" data-testid="account-filter-query-label">账号</span>
  <SearchInput :model-value="draft.query || ''" class="w-full sm:w-64"
    placeholder="搜索账号名称或实际账号"
    @update:model-value="updateQuery" @search="runImmediate" />
</label>
```

Use the existing 300 ms debounced action. In the account cell render identity when present, then retain `ID N` and parent metadata on a separate tertiary line.

- [ ] **Step 4: Run focused tests and verify GREEN**

Expected: all focused account-monitor tests PASS.

- [ ] **Step 5: Run frontend typecheck and commit**

```powershell
pnpm typecheck
git add frontend/src/api/admin/accountMonitor.ts frontend/src/api/admin/__tests__/accountMonitor.spec.ts frontend/src/views/admin/account-monitor
git commit -m "feat: search account monitor by actual account"
```

Expected: typecheck PASS and the frontend changes are committed.

### Task 8: Update Security Documentation And Run Regression Tests

**Files:**
- Modify: `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`
- Modify: `docs/ACCOUNT-MONITOR-CHECKLIST.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`

- [ ] **Step 1: Write failing documentation contract assertions**

Require all operator documents to name `public_group_catalog`, describe `account_identity` as a derived email-only administrator field, require allow probes for both views, and preserve raw table/credential deny probes.

- [ ] **Step 2: Run deployment contract tests and verify RED**

```powershell
node --test deploy/tests/account-monitor-contract.test.mjs
```

Expected: FAIL because the new view and identity checks are undocumented.

- [ ] **Step 3: Update documentation and release verification**

Document the two appended contracts, the exact extraction whitelist, the anonymous/admin boundary, owner reapplication of `install-account-monitor-source.sql`, restricted-role allow probes, and raw credential/table deny probes. Do not include any production email or secret value.

- [ ] **Step 4: Run all focused suites**

```powershell
node --test deploy/tests/extensions-self-layout.test.mjs deploy/tests/account-monitor-contract.test.mjs deploy/tests/backend-extension-route-contract.test.mjs
pnpm test:run src/components/layout/__tests__/AppSidebar.spec.ts src/components/layout/__tests__/siteLogoSanitization.spec.ts src/api/admin/__tests__/accountMonitor.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorFilters.spec.ts src/views/admin/account-monitor/__tests__/AccountMonitorPanel.spec.ts src/views/admin/account-monitor/__tests__/useAccountMonitorFilters.spec.ts
pnpm typecheck
git diff --check
```

Upload the final `extensions-self` tree to the remote temporary test directory and run both Go modules with one-shot containers. Expected: every focused suite PASS with no type or whitespace error.

- [ ] **Step 5: Commit documentation and contracts**

```powershell
git add docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md docs/ACCOUNT-MONITOR-CHECKLIST.md deploy/RELEASE-RUNBOOK.md deploy/tests/account-monitor-contract.test.mjs
git commit -m "docs: record homepage and account identity source contracts"
```

### Task 9: Visual Verification And Final Review

**Files:**
- No production files expected

- [ ] **Step 1: Start a local same-origin mock server**

Serve the real homepage file plus deterministic JSON for `/api/v1/settings/public` and `/api/v1/extensions-self/homepage/api/public-groups`. Use configured-name, subtitle, data-logo, empty, peak, and unknown-platform fixtures. Start on an unused localhost port and keep the process attached to a tracked session.

- [ ] **Step 2: Verify desktop and mobile in the browser**

At 1440x900 and 390x844, confirm nonblank rendering, non-zero logo natural dimensions, visible configured name/subtitle, no overlap, correct live rates/peak text, unknown-platform fallback, and `/home` top navigation. Capture screenshots and inspect the console for CSP, font, image, or JavaScript errors.

- [ ] **Step 3: Review the complete diff**

```powershell
git status --short --branch
git diff origin/custom-release...HEAD --stat
git diff origin/custom-release...HEAD --check
git log --oneline origin/custom-release..HEAD
```

Expected: only planned extension, focused frontend, tests, and documentation files changed; no secrets, generated dependencies, or unrelated metadata are tracked.

- [ ] **Step 4: Clean exact temporary test artifacts**

Remove only the task-created `/tmp/sub2api-homepage-tests` directory through `ssh-skill` after verifying its resolved absolute path. Stop the local mock server. Leave the feature worktree and commits intact for review.

- [ ] **Step 5: Report separate completion states**

Report implementation commit list and test evidence. State explicitly that `origin/custom-release`, CI/GHCR images, the source view, and production remain unchanged until the user separately authorizes push and release.
