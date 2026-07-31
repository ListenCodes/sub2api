# Release Notice, Failure Recovery, And Stable Merge Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve runtime update attention and matching durable failures, expose exact Actions failure evidence, enforce canonical Stable merge history, and integrate official v0.1.169 without publishing production.

**Architecture:** The custom frontend derives attention from update class and restores the server's current durable operation, using local storage only as a job-ID accelerator. The existing backend job JSON gains additive evidence fields, while host scripts emit and persist one structured Actions result and validate canonical Stable merge identity before candidate push or promotion. No database, official UI, Wire, router, or Stable zero-overlap file changes.

**Tech Stack:** Vue 3, Pinia, TypeScript, Vitest, Go, Bash, jq, Node test runner, Git, GitHub Actions, GHCR.

---

## File Map

- `frontend/src/features/custom-release/{store.ts,api.ts,CustomReleaseBadge.vue}`: attention, recovery, evidence UI.
- `frontend/src/features/custom-release/__tests__/{store.spec.ts,CustomReleaseBadge.spec.ts}`: behavior contracts.
- `backend/internal/service/{update_job.go,update_job_service_test.go}`: additive durable evidence fields.
- `deploy/ops/{wait-for-actions.sh,prepare-release.sh,sync-upstream.sh,promote-release.sh}`: Actions and merge gates.
- `deploy/tests/wait-for-actions.test.mjs`: Actions outcome fixtures.
- `deploy/tests/custom-release-isolation.test.mjs`: first-parent Stable identity.
- `deploy/ops/tests/test-release-pipeline.sh`: prepare settlement and promotion fixtures.
- `AGENTS.md`, `docs/SUB2API-CUSTOM-OPERATIONS.md`, `deploy/RELEASE-RUNBOOK.md`, `deploy/ops/README.md`: operating contract.
- `deploy/stable-release-baseline.json`: v0.1.169 identity, only after the canonical merge.

### Task 1: Correct Documentation-Only And Runtime Attention

**Files:**
- Modify: `frontend/src/features/custom-release/store.ts`
- Modify: `frontend/src/features/custom-release/CustomReleaseBadge.vue`
- Test: `frontend/src/features/custom-release/__tests__/store.spec.ts`
- Test: `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

- [ ] **Step 1: Write failing tests**

Add table-driven badge cases for `official`, `custom`, and `combined`. Open and close the menu and assert `release-notice-indicator` remains present and `markCustomReleaseRead` is not called. Add a docs-only case that opens the menu, asserts `markCustomReleaseRead(fingerprint)`, removes amber attention, and retains the docs-only text/link.

- [ ] **Step 2: Verify RED**

Run: `cd frontend; npx --yes pnpm@10.28.2 exec vitest run src/features/custom-release/__tests__/store.spec.ts src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

Expected: runtime acknowledgement and amber-persistence assertions fail.

- [ ] **Step 3: Implement the minimal policy**

Add this computed value and use it for the collapsed button class, title, dot, and ping:

```ts
const releaseAttentionRequired = computed(() =>
  appStore.runtimeUpdate === true && hasUpdate.value
    ? true
    : updateKind.value === 'docs-only' && noticeUnread.value
)
```

Start `acknowledgeCurrentNotice` with `if (updateKind.value !== 'docs-only') return`. Apply the same `updateKind !== 'docs-only'` guard in `store.markCurrentNoticeRead` so other callers cannot acknowledge runtime targets.

- [ ] **Step 4: Verify GREEN and commit**

Run the Step 2 command, then commit `fix(frontend): keep runtime release attention durable` with only Task 1 files.

### Task 2: Restore Matching Durable Failures And Evidence

**Files:**
- Modify: `frontend/src/features/custom-release/api.ts`
- Modify: `frontend/src/features/custom-release/CustomReleaseBadge.vue`
- Test: `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

- [ ] **Step 1: Write failing recovery tests**

Use a failed update job with matching `stable_release_tag`, `stable_release_commit`, and `target_custom_commit`. Cover exact local job recovery, server-current recovery without local storage, server-newer replacement, close/reopen, stale target rejection, retry job replacement, success cleanup, and visible `failed_check`, `check_url`, `conclusion`, `error_code`, and `production_changed=false`.

- [ ] **Step 2: Verify RED**

Run: `cd frontend; npx --yes pnpm@10.28.2 exec vitest run src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

Expected: current code removes the job ID and clears terminal feedback.

- [ ] **Step 3: Add API fields and target matching**

Add optional `failed_check`, `check_url`, and `conclusion` to `UpdateJob`. Implement:

```ts
function jobMatchesDetectedTarget(job: UpdateJob): boolean {
  if (job.operation_kind !== 'update' || !hasUpdate.value) return false
  const officialMatches = !appStore.officialUpdate ||
    (job.stable_release_tag === appStore.targetOfficialVersion &&
      job.stable_release_commit === appStore.targetOfficialCommit)
  const customMatches = !appStore.customUpdate ||
    job.target_custom_commit === appStore.targetCustomCommit
  return officialMatches && customMatches
}
```

- [ ] **Step 4: Implement recovery and rendering**

Query the exact local job if present, then the no-ID server current job. Select the later valid `updated_at`, resume nonterminal/prepared work, restore only a matching update failure, and drop only a stale accelerator. Do not remove the key in `finishUpdateFailure`. Clear it on success/new target. Opening/closing must preserve the failure. Render evidence with a safe external link and label the action `Retry preparation` / `重试准备`.

- [ ] **Step 5: Verify GREEN and commit**

Run the Step 2 command, then commit `fix(frontend): restore durable release failures` with only Task 2 files.

### Task 3: Preserve Structured Evidence In The Backend API

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_job_service_test.go`

- [ ] **Step 1: Write a failing round-trip test**

Create a valid job with `FailedCheck: "deployment"`, `CheckURL: "https://github.com/ListenCodes/sub2api/actions/runs/1/job/2"`, and `Conclusion: "failure"`; write it, read it through `GetUpdateStatus`, and compare exact values.

- [ ] **Step 2: Verify RED**

Run: `cd backend; go test ./internal/service -run 'UpdateJob.*Evidence' -count=1`

Expected: compile failure because fields do not exist.

- [ ] **Step 3: Implement the minimal fields**

```go
FailedCheck string `json:"failed_check,omitempty"`
CheckURL    string `json:"check_url,omitempty"`
Conclusion  string `json:"conclusion,omitempty"`
```

- [ ] **Step 4: Verify GREEN and commit**

Run: `cd backend; go test ./internal/service -run 'UpdateJob|ReleaseOperation' -count=1`

Commit `feat(release): expose actions failure evidence`.

### Task 4: Return And Persist Exact Actions Evidence

**Files:**
- Create: `deploy/tests/wait-for-actions.test.mjs`
- Modify: `deploy/ops/wait-for-actions.sh`
- Modify: `deploy/ops/prepare-release.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`

- [ ] **Step 1: Write failing waiter fixtures**

Generate check-run JSON for all-success, `failure`, `cancelled`, `skipped`, missing check, incomplete check, malformed response, and missing images URL. Spawn Bash with `SUB2API_CHECKS_JSON_FILE` and assert one JSON object, exit code, `failed_check`, URL, conclusion, error code, and `production_changed=false`.

- [ ] **Step 2: Verify RED**

Run: `node --test deploy/tests/wait-for-actions.test.mjs`

Expected: current generic stderr/key-value output fails.

- [ ] **Step 3: Emit one JSON evidence object**

Success shape: `{"ok":true,"workflow_url":"https://..."}`.

Failure shape:

```json
{"ok":false,"message":"required check deployment concluded failure","error_code":"ACTIONS_REQUIRED_CHECK_FAILED","failed_check":"deployment","check_url":"https://...","conclusion":"failure","workflow_url":"https://...","published":false,"production_changed":false}
```

Live pending checks poll; fixture mode fails immediately rather than sleeping.

- [ ] **Step 4: Write failing prepare settlement fixture**

Inject a waiter that emits the failure object and exits one. Assert the durable operation retains every evidence field plus `published=false` and `production_changed=false`.

- [ ] **Step 5: Verify RED**

Run: `bash deploy/ops/tests/test-release-pipeline.sh`

Expected: current prepare settles generic `ACTIONS_FAILED`.

- [ ] **Step 6: Validate and persist evidence**

Allow `fail_prepare` to merge validated metadata while always forcing `published:false` and `production_changed:false`. Treat malformed waiter output as `ACTIONS_EVIDENCE_INVALID`.

- [ ] **Step 7: Verify GREEN and commit**

Run Steps 2 and 5. Commit `fix(release): persist required actions failure evidence`.

### Task 5: Enforce Canonical Stable Merge Creation And Promotion

**Files:**
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/promote-release.sh`
- Modify: `deploy/tests/custom-release-isolation.test.mjs`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`

- [ ] **Step 1: Write failing history fixtures**

Require the first-parent Stable merge to have the exact subject, exactly two parents, second parent equal to baseline commit, and first parent on the approved custom lineage. Add promotion rejection cases for generic subject, wrong second parent, wrong baseline, and changed remote base.

- [ ] **Step 2: Verify RED**

Run: `node --test deploy/tests/custom-release-isolation.test.mjs`

Run: `bash deploy/ops/tests/test-release-pipeline.sh`

Expected: source/fixtures fail because sync uses `--no-edit` and promotion checks ancestry only.

- [ ] **Step 3: Create and validate canonical merge**

Use:

```bash
git -C "$WORKTREE" merge --no-ff -m "merge: integrate stable Release $RELEASE_TAG" "$RELEASE_COMMIT"
```

Before candidate push, validate exact subject, two parents, first-parent custom lineage, exact second parent, and exact baseline identity.

- [ ] **Step 4: Revalidate before promotion**

Read baseline JSON from `TARGET_COMMIT`, find its canonical merge on first-parent history, validate subject/tag, second parent/baseline, first-parent ancestry from `BASE_COMMIT`, and unchanged remote base. Keep a normal push without force.

- [ ] **Step 5: Verify GREEN and commit**

Run Step 2 plus `bash -n deploy/ops/sync-upstream.sh deploy/ops/promote-release.sh`. Commit `fix(release): enforce canonical stable merge history`.

### Task 6: Update Operating Documentation

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/SUB2API-CUSTOM-OPERATIONS.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/ops/README.md`

- [ ] **Step 1: Correct the contract**

Document docs-only acknowledgement, runtime attention until `has_update=false`, server-current matching failure recovery, target replacement, exact Actions evidence, canonical merge validation, and production-not-authorized status. Remove statements that all targets are acknowledged or all terminal failures are hidden.

- [ ] **Step 2: Verify and commit**

Run: `rg -n "notice_unread|terminal|canonical|production_changed" AGENTS.md docs/SUB2API-CUSTOM-OPERATIONS.md deploy/RELEASE-RUNBOOK.md deploy/ops/README.md`

Run: `git diff --check`

Commit `docs(release): document durable update attention`.

### Task 7: Full Local Verification And Independent Review

**Files:** No new files.

- [ ] **Step 1: Frontend verification**

Run `npx --yes pnpm@10.28.2 typecheck`, `test:run`, and `build` from `frontend/`.

- [ ] **Step 2: Backend verification**

Run `go test ./... -count=1` from `backend/`.

- [ ] **Step 3: Deploy verification**

Run `node --test deploy/tests/*.test.mjs`, `pwsh -NoProfile -File deploy/ops/tests/test-script-contract.ps1`, `bash deploy/tests/site-bootstrap-test.sh`, `bash deploy/ops/tests/test-release-pipeline.sh`, and `bash -n deploy/ops/*.sh`.

- [ ] **Step 4: Boundaries and hygiene**

Run `node --test deploy/tests/custom-overlap-budget.test.mjs`, `git diff --check origin/custom-release...HEAD`, and inspect `git status --short --branch`.

- [ ] **Step 5: Independent review**

Provide design path, plan path, base/head SHAs, test evidence, zero-overlap boundary, and production prohibition to a fresh read-only reviewer. Fix every Critical/Important issue and rerun affected/full tests.

### Task 8: Integrate Stable v0.1.169 And Advance Custom Release

**Files:**
- Official merge updates Stable-owned files.
- Modify: `deploy/stable-release-baseline.json`

- [ ] **Step 1: Verify immutable identity**

Run `git cat-file -t v0.1.169` and `git rev-parse 'v0.1.169^{}'`.

Expected: annotated `tag`; peeled commit `26d894ef4f50645a4bf1030e378ac892f17d0223`.

- [ ] **Step 2: Create canonical first-parent merge**

Run: `git merge --no-ff -m "merge: integrate stable Release v0.1.169" 26d894ef4f50645a4bf1030e378ac892f17d0223`

Resolve only genuine conflicts through custom-owned files. Do not use blanket `ours`/`theirs`; Stable zero-overlap files must retain official v0.1.169 bytes.

- [ ] **Step 3: Record baseline identity**

Write repository, tag, tag object SHA, peeled commit, and publication timestamp to `deploy/stable-release-baseline.json`. Commit `chore: record stable Release v0.1.169`.

- [ ] **Step 4: Verify merged result**

Assert canonical subject in first-parent history, exact second parent and baseline, then rerun all Task 7 checks.

- [ ] **Step 5: Fast-forward the production branch ref**

Run `git fetch origin custom-release`, require `git merge-base --is-ancestor origin/custom-release HEAD`, then `git push origin HEAD:custom-release`. Never force.

- [ ] **Step 6: Verify Actions and paired public GHCR images**

For the pushed full SHA, require `backend`, `golangci`, `frontend`, `extensions`, `deployment`, `metadata`, and `images` all `success`. Verify both full-SHA public image tags and OCI revision equal the pushed commit.

- [ ] **Step 7: Record the non-deployment boundary**

Report implementation commits, canonical merge, baseline commit, tests, push, Actions URL, both digests, and explicitly record: administrator prepare/apply not triggered; SSH/deployment not executed; production commit/containers and installed `/opt` scripts unchanged.
