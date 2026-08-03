# Sidebar Home Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore `/home` as the destination for both the sidebar logo and site name.

**Architecture:** Keep the existing `router-link` bindings in `AppSidebar.vue` and change only their shared `homePath` value. Preserve the focused source-contract test so future Stable integrations cannot silently restore role-based dashboard routing.

**Tech Stack:** Vue 3, TypeScript, Vitest

---

### Task 1: Restore the sidebar home destination

**Files:**
- Modify: `frontend/src/components/layout/AppSidebar.vue:254`
- Test: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts:58`

- [x] **Step 1: Write the failing test**

Rename the header test to describe the public homepage behavior and assert:

```ts
expect(componentSource).toContain("const homePath = '/home'")
expect(componentSource).not.toContain("const homePath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))")
```

- [x] **Step 2: Run the focused test to verify it fails**

Run: `pnpm exec vitest run src/components/layout/__tests__/AppSidebar.spec.ts`

Expected: FAIL because `AppSidebar.vue` still defines the role-dependent dashboard destination.

- [x] **Step 3: Write the minimal implementation**

Replace the shared destination with:

```ts
const homePath = '/home'
```

- [x] **Step 4: Run focused verification**

Run the same Vitest file, `pnpm run typecheck`, and `git diff --check`.

Expected: all commands exit successfully.

- [x] **Step 5: Commit the implementation**

Stage the component, test, and this plan, then commit with `fix(frontend): restore sidebar home navigation`.
