# Risk Control Rules And Audit UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the rules and audit pages compact, consistent, automatically filtered, and free of the untranslated Apply button.

**Architecture:** Keep all backend and API contracts intact. Recompose the two Vue views with existing `SearchInput`, `Select`, `Pagination`, and `Icon` components, and cover the behavior through their existing view tests.

**Tech Stack:** Vue 3, TypeScript, Tailwind CSS, Vitest, Vue Test Utils

---

### Task 1: Audit Filter Toolbar And Pagination

**Files:**
- Modify: `frontend/src/views/admin/UserRiskControlAuditView.vue`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts`

- [ ] **Step 1: Write failing interaction tests**

Add assertions that `common.apply` and `apply-audit-filters` are absent, changing action/result calls `listAudit` automatically, reset clears active filters, and changing the shared pagination page size sends the selected `pageSize`.

- [ ] **Step 2: Verify the tests fail for the old UI**

Run:

```powershell
pnpm --dir frontend exec vitest run src/views/admin/__tests__/UserRiskControlAuditView.spec.ts
```

Expected: failures caused by the old Apply button and fixed page size.

- [ ] **Step 3: Implement the compact toolbar**

Use this component structure:

```vue
<section class="border-y border-gray-200 py-3 dark:border-dark-700">
  <SearchInput ... @search="applyFilters" />
  <Select ... @update:model-value="setFilter('action', $event)" />
  <button class="btn-ghost btn-icon" @click="resetFilters"><Icon name="x" /></button>
</section>
<Pagination :page-size="pageSize" @update:page-size="changePageSize" />
```

Keep an `activeFilters` snapshot so stale debounced requests cannot overwrite newer filter results, matching the risk-user page pattern.

- [ ] **Step 4: Verify the audit tests pass**

Run the command from Step 2 and expect all audit-view tests to pass.

### Task 2: Compact Rules Table

**Files:**
- Modify: `frontend/src/views/admin/UserRiskControlRulesView.vue`
- Test: `frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts`

- [ ] **Step 1: Write failing structure tests**

Assert that the page contains `risk-rules-table`, exposes `edit-rule-<id>`, hides the edit controls initially, and reveals `rule-editor-<id>` after the edit command.

- [ ] **Step 2: Verify the tests fail for the card list**

Run:

```powershell
pnpm --dir frontend exec vitest run src/views/admin/__tests__/UserRiskControlRulesView.spec.ts
```

Expected: the old card list has no compact table or explicit expandable editor.

- [ ] **Step 3: Implement the table and expandable editor**

Render one summary row per rule and one conditional editor row:

```vue
<table data-testid="risk-rules-table" class="w-full min-w-[1040px] table-fixed">
  <tbody>
    <template v-for="rule in rules" :key="rule.id">
      <tr>...<button :data-testid="`edit-rule-${rule.id}`" @click="toggleEditor(rule.id)">...</button></tr>
      <tr v-if="expandedRuleId === rule.id" :data-testid="`rule-editor-${rule.id}`">...</tr>
    </template>
  </tbody>
</table>
```

Move the existing window, threshold, score, level, action, save, test, and conflict controls into the expanded row without changing their API payloads.

- [ ] **Step 4: Verify the rules tests pass**

Run the command from Step 2 and expect all rules-view tests to pass.

### Task 3: Focused Validation And Delivery

**Files:**
- Verify: `frontend/src/views/admin/UserRiskControlAuditView.vue`
- Verify: `frontend/src/views/admin/UserRiskControlRulesView.vue`

- [ ] **Step 1: Run focused automated validation**

```powershell
pnpm --dir frontend exec vitest run src/views/admin/__tests__/UserRiskControlAuditView.spec.ts src/views/admin/__tests__/UserRiskControlRulesView.spec.ts
pnpm --dir frontend typecheck
git diff --check
```

Expected: all commands exit zero.

- [ ] **Step 2: Run browser smoke checks**

Check rules and audit routes at 1440x900 and 390x844. Confirm meaningful content, no framework overlay, no relevant console errors, internal table scrolling, automatic filter interaction, and expandable rule editing.

- [ ] **Step 3: Commit and deliver**

Commit the tested branch, fast-forward `custom`, push `origin/custom`, and publish the approved commit through `/opt/sub2api-custom/publish-custom.sh` under the shared release lock.
