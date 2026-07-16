# Upstream Conflict Resolution And Reporting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Resolve the current upstream integration conflict without dropping either the custom risk-control routes or upstream video routes, and make the deployed admin update flow expose the existing structured conflict status.

**Architecture:** Keep `sync-upstream.sh` as a conservative three-way merge gate. Resolve only the two known source conflicts in an isolated branch by combining both sides, then retain the conflict-reporting backend/frontend changes so unresolved future conflicts are visible to administrators. Production remains unchanged until a separately authorized release.

**Tech Stack:** Git worktrees, Bash operation scripts, Go/Gin route tests, Vue 3, Vitest, Go test.

---

### Task 1: Establish the regression checks before merge resolution

**Files:**
- Test: `backend/internal/server/routes/gateway_test.go`
- Test: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`
- Test: `frontend/src/api/admin/__tests__/system.spec.ts`

- [ ] **Step 1: Add route assertions for both feature families**

Extend the existing gateway route test to assert that the router registers `/v1/videos/edits` and `/v1/videos/extensions`, while retaining the existing risk middleware coverage.

- [ ] **Step 2: Run the focused tests before merging upstream**

Run:

```bash
cd backend && go test ./internal/server/routes
cd ../frontend && npx vitest run src/components/common/__tests__/VersionBadge.spec.ts src/api/admin/__tests__/system.spec.ts
```

Expected: the new route assertions fail because the current custom branch has the risk-control routes but not the upstream video edit/extension routes; the conflict-reporting UI/API tests pass on the feature branch.

### Task 2: Resolve the two upstream conflicts in the isolated feature worktree

**Files:**
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `deploy/README.md`

- [ ] **Step 1: Merge `upstream/main` into `feature/conflict-reporting`**

Use a normal merge in `E:\Code\worktrees\sub2api-conflict-reporting`; do not use `-X ours`, `-X theirs`, rebase, or force push.

- [ ] **Step 2: Resolve `gateway.go` by preserving both behaviors**

Keep upstream `videoEditHandler`, `videoExtensionHandler`, and their `/videos/edits` and `/videos/extensions` registrations. Keep custom `RiskEventMiddleware` construction and apply it to the same authenticated gateway groups and aliases already covered by the custom side.

- [ ] **Step 3: Resolve `deploy/README.md` by combining documentation sections**

Retain upstream Apple container deployment documentation and custom risk-control/release-chain documentation. Reconcile overlapping deployment tables and environment instructions so each deployment method is documented once.

- [ ] **Step 4: Verify no unresolved conflict markers remain**

Run:

```bash
git diff --check
git diff --name-only --diff-filter=U
```

Expected: `git diff --name-only --diff-filter=U` prints no paths.

### Task 3: Verify the merged behavior and conflict reporting

**Files:**
- Verify: `deploy/ops/sync-upstream.sh`
- Verify: `deploy/ops/sync-trigger.sh`
- Verify: `backend/internal/service/update_job.go`
- Verify: `backend/internal/handler/admin/system_handler.go`
- Verify: `frontend/src/components/common/VersionBadge.vue`

- [ ] **Step 1: Run focused backend and frontend tests**

Run the route, update-job, admin-handler, conflict-reporting, and frontend system/version tests.

- [ ] **Step 2: Run deployment script contract and syntax checks**

Run the repository's existing operation-script contract test and Bash syntax checks for every script under `deploy/ops/`.

- [ ] **Step 3: Build the frontend and run the backend package checks**

Run the frontend typecheck/build and the relevant Go package tests.

### Task 4: Review and integrate source changes

- [ ] **Step 1: Review the final diff and commit the isolated branch**

Commit the merge resolution and any required regression assertions on `feature/conflict-reporting`.

- [ ] **Step 2: Request a code review before touching `custom`**

Review the merge for route preservation, risk middleware coverage, and release-script behavior.

- [ ] **Step 3: Fast-forward/merge the validated result into `custom` and push `origin/custom`**

Only after all verification passes. Do not publish production in this task unless explicitly authorized separately.

### Task 5: Production rollout decision

- [ ] **Step 1: If release is authorized, back up and install the versioned scripts**

Install the updated scripts, publish the exact `origin/custom` commit through `publish-custom.sh`, and verify application, risk-control, database, Redis, and public health.

- [ ] **Step 2: Verify the admin UI shows conflict details**

Confirm the status includes conflicted files, base/upstream commits, artifact path, and that production was unchanged when a conflict is intentionally reproduced.
