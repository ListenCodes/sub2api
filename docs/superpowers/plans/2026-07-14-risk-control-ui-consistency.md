# Risk Control Admin UI Consistency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the three risk-control admin pages functionally complete and visually consistent with account management on desktop and mobile.

**Architecture:** Keep the existing Vue views and API endpoints, replacing one-off markup with repository shared components. Extend the existing adapter and label formatter only where current data is discarded or remains unreadable.

**Tech Stack:** Vue 3, TypeScript, Tailwind CSS, Vitest, Vue Test Utils, Playwright.

---

### Task 1: Lock Data Presentation Behavior

**Files:**
- Modify: `frontend/src/utils/__tests__/userRiskControlLabels.spec.ts`
- Modify: `frontend/src/api/admin/__tests__/userRiskControlV2.spec.ts`
- Modify: `frontend/src/utils/userRiskControlLabels.ts`
- Modify: `frontend/src/api/admin/userRiskControlV2.ts`

- [ ] **Step 1: Write failing tests for legacy reason parsing**

```ts
expect(formatRiskReason('rule=login_failure_burst count=9 window=300'))
  .toBe('命中规则：登录失败爆发（5 分钟内失败 9 次）')
```

- [ ] **Step 2: Write failing tests for preserved event associations and actor names**

```ts
expect(detail.events[0]).toMatchObject({ ip: '198.51.100.10', device_id: 'chrome-124' })
expect(audit.items[0].actor).toBe('qa-admin')
```

- [ ] **Step 3: Run the two test files and verify the assertions fail for missing behavior**

Run: `pnpm --dir frontend exec vitest run src/utils/__tests__/userRiskControlLabels.spec.ts src/api/admin/__tests__/userRiskControlV2.spec.ts`

- [ ] **Step 4: Implement the minimal formatter and adapter changes**

Parse `rule`, `count`, and `window`; map known rule codes; retain `ip` and
`device_id`; prefer `actor_name` over `actor_id`.

- [ ] **Step 5: Re-run the two test files and verify they pass**

### Task 2: Standardize Scenario Rule Forms

**Files:**
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts`
- Modify: `frontend/src/views/admin/UserRiskControlRulesView.vue`

- [ ] **Step 1: Add failing assertions for `BaseDialog`, shared selects, toggle, selected template, and responsive `DataTable`**
- [ ] **Step 2: Run the rule view test and confirm failure**
- [ ] **Step 3: Replace the duplicate page heading and native create controls**
- [ ] **Step 4: Replace native edit controls while preserving revision, save, reload, and test behavior**
- [ ] **Step 5: Run the rule view test and verify pass**

### Task 3: Standardize User Risk Workspace And Drawer

**Files:**
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts`
- Modify: `frontend/src/views/admin/UserRiskControlUsersView.vue`
- Modify: `frontend/src/components/admin/UserRiskControlUserDrawer.vue`

- [ ] **Step 1: Add failing assertions for shared table layout, toggle, dialogs, textarea errors, semantic button variants, and association values**
- [ ] **Step 2: Run the user view test and confirm failure**
- [ ] **Step 3: Adopt `TablePageLayout` and `DataTable`, preserving server sorting, selection, row opening, and pagination**
- [ ] **Step 4: Replace batch and single-user confirmation overlays with `BaseDialog` and `TextArea`**
- [ ] **Step 5: Render distinct IP/device evidence in the drawer and verify the test passes**

### Task 4: Standardize Operation Audit Workspace

**Files:**
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts`
- Modify: `frontend/src/views/admin/UserRiskControlAuditView.vue`

- [ ] **Step 1: Add failing assertions for `DateRangePicker`, `DataTable`, actor preference, and neutral non-account status changes**
- [ ] **Step 2: Run the audit view test and confirm failure**
- [ ] **Step 3: Adopt shared layout, date range, table sorting, and mobile cards**
- [ ] **Step 4: Run the audit view test and verify pass**

### Task 5: Verify, Integrate, And Publish

**Files:**
- Modify only if verification exposes a scoped defect.

- [ ] **Step 1: Run all focused risk-control frontend tests**
- [ ] **Step 2: Run `pnpm --dir frontend typecheck`, build, lint check, and `git diff --check`**
- [ ] **Step 3: Browser-test 1440x900 and 390x844 with read-only API fixtures**
- [ ] **Step 4: Commit the feature branch and merge it into `custom` without force operations**
- [ ] **Step 5: Push `origin/custom`, publish the exact commit through `publish-custom.sh`, and verify container/internal/public health**
