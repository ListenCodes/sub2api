# Release Notice And Rollback UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-admin cross-device unread release notices, a complete two-phase dual-version rollback UI, data-volume-only rollback history, and a staged removal of privileged Web container mounts.

**Architecture:** Custom-owned backend files compute and persist notice fingerprints beside the existing release ledger, while the custom frontend badge and rollback panel own all presentation and orchestration. The Web API lists only complete ledger snapshots; host scripts remain the fail-closed authority for Git, paired images, OCI identity, Compose, and backups. A transition validator must be deployed and installed on the host before the final strict no-mount release can advance to `origin/custom-release`.

**Tech Stack:** Go 1.26, Gin, Vue 3, TypeScript, Pinia, Vitest, Bash, jq, Node test runner, Docker Compose, GitHub Actions/GHCR.

---

## File Map

**Stage A transition release**

- Create `deploy/ops/tests/test-release-common-compose.sh`: fixture-level legacy/reduced/hybrid mount validation.
- Modify `deploy/ops/release-common.sh`: accept exactly the legacy or reduced Web mount shape during migration.
- Modify `deploy/ops/tests/test-release-pipeline.sh`: invoke the new validator fixture.
- Modify `deploy/RELEASE-RUNBOOK.md`: record the Stage A deployment and installed-script gate.

**Backend notice and rollback API**

- Create `backend/internal/service/custom_release_notice.go`: canonical fingerprint and atomic per-admin state store.
- Create `backend/internal/service/custom_release_notice_test.go`: fingerprint, persistence, isolation, pruning, and failure tests.
- Modify `backend/internal/service/custom_release_service.go`: expose target Official commit/fingerprint and remove Web Git/Docker eligibility.
- Modify `backend/internal/service/custom_release_service_test.go`: detection fingerprints and data-only history behavior.
- Modify `backend/internal/handler/admin/custom_release_handler.go`: decorate checks by auth subject and mark read best effort.
- Modify `backend/internal/handler/admin/custom_release_handler_test.go`: admin identity and endpoint behavior.
- Modify `backend/internal/server/routes/custom_extensions.go`: register the custom read endpoint.
- Modify `deploy/tests/backend-extension-route-contract.test.mjs`: enforce route ownership without touching central routes.

**Frontend**

- Modify `frontend/src/features/custom-release/api.ts`: notice fields/API and complete rollback job identity types.
- Modify `frontend/src/features/custom-release/store.ts`: independent unread state plus explicit current-release loading/error.
- Create `frontend/src/features/custom-release/__tests__/store.spec.ts`: store acknowledgement and current-release errors.
- Modify `frontend/src/features/custom-release/ReleaseRollbackPanel.vue`: official-style state-complete two-phase panel.
- Modify `frontend/src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts`: loading/error/retry/empty/select/prepared/expiry/apply coverage.
- Modify `frontend/src/features/custom-release/CustomReleaseBadge.vue`: unread-only collapsed styling and orchestration.
- Modify `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`: once-only notice and rollback integration coverage.
- Modify `frontend/src/features/custom-release/__tests__/api.spec.ts`: mark-read HTTP contract.

**Stage B host/deployment/docs**

- Modify `deploy/docker-compose.custom.yml`: remove source, Docker socket, and Docker binary mounts.
- Modify `deploy/ops/release-common.sh`: replace transition acceptance with strict reduced-mount validation.
- Modify `deploy/tests/compose-overlay-contract.test.mjs`: positively require data/trigger and reject privileged mounts.
- Modify `deploy/ops/tests/test-prepare-rollback.sh`: fail-closed Git/image/OCI/backup scenarios.
- Modify `AGENTS.md`, `docs/SUB2API-CUSTOM-OPERATIONS.md`, `deploy/RELEASE-RUNBOOK.md`, and `deploy/ops/README.md`: final ownership and migration contracts.
- Modify knowledge-base `03-应用与项目/Sub2API/README.md` and the Sub2API sections of the VPS rules/ledgers after repository behavior is final.

## Task 1: Stage A Transition Compose Validator

**Files:**

- Create: `deploy/ops/tests/test-release-common-compose.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/RELEASE-RUNBOOK.md`

- [ ] **Step 1: Write the failing standalone validator fixture**

Create JSON fixtures for these exact target sets:

```text
legacy: /app/data, /app/scripts/sync-upstream.sh, /repo,
        /var/run/docker.sock, /usr/bin/docker
reduced: /app/data, /app/scripts/sync-upstream.sh
hybrid-a: /app/data, /app/scripts/sync-upstream.sh, /repo
hybrid-b: /app/data, /app/scripts/sync-upstream.sh, /var/run/docker.sock
```

The test sources `release-common.sh`, calls
`release_validate_rendered_compose`, expects legacy and reduced to pass, and
expects both hybrids to fail. Each fixture includes exact main/extensions image
references, required health checks, networks, services, and named volumes.

- [ ] **Step 2: Run the fixture and verify reduced mode fails**

Run:

```bash
bash deploy/ops/tests/test-release-common-compose.sh
```

Expected: non-zero with `reduced compose was rejected` because the current
validator requires `/repo` and `/var/run/docker.sock`.

- [ ] **Step 3: Add an exact transition mount predicate**

Add this jq helper inside `release_validate_rendered_compose`:

```jq
def mount_targets:
  [.services.sub2api.volumes[]?.target] | unique | sort;
def legacy_mounts:
  mount_targets == ([
    "/app/data",
    "/app/scripts/sync-upstream.sh",
    "/repo",
    "/usr/bin/docker",
    "/var/run/docker.sock"
  ] | sort);
def reduced_mounts:
  mount_targets == (["/app/data", "/app/scripts/sync-upstream.sh"] | sort);
```

Implement the comparison with sorted arrays constructed before comparison so
jq syntax is valid. Require `legacy_mounts or reduced_mounts`; keep all existing
image, service, health, network, and named-volume checks.

- [ ] **Step 4: Add the fixture to the pipeline aggregator**

Insert:

```bash
bash "$ROOT_DIR/deploy/ops/tests/test-release-common-compose.sh"
```

in `test-release-pipeline.sh` before prepare/apply fixtures.

- [ ] **Step 5: Document the transition-only behavior**

Add a Stage A section to `deploy/RELEASE-RUNBOOK.md` stating that this commit
accepts both exact shapes only so the installed host script can prepare the
later no-mount target, and that the overlay remains unchanged in Stage A.

- [ ] **Step 6: Run Stage A tests**

Run:

```powershell
node --test deploy/tests/compose-overlay-contract.test.mjs deploy/tests/release-prepared-expiry.test.mjs
```

Run in Bash-capable CI/local environment:

```bash
bash deploy/ops/tests/test-release-common-compose.sh
bash deploy/ops/tests/test-release-pipeline.sh
```

Expected: all pass; current overlay still contains all three privileged mounts.

- [ ] **Step 7: Commit Stage A**

```bash
git add deploy/ops/release-common.sh deploy/ops/tests/test-release-common-compose.sh \
  deploy/ops/tests/test-release-pipeline.sh deploy/RELEASE-RUNBOOK.md
git commit -m "chore(release): accept reduced web mounts during migration"
```

## Task 2: Canonical Notice Fingerprint And State Store

**Files:**

- Create: `backend/internal/service/custom_release_notice.go`
- Create: `backend/internal/service/custom_release_notice_test.go`
- Modify: `backend/internal/service/custom_release_service.go`
- Test: `backend/internal/service/custom_release_service_test.go`

- [ ] **Step 1: Write failing fingerprint matrix tests**

Use a complete `CustomReleaseInfo` and assert the digest is unchanged for the
same five fields and changes independently for kind, Official version,
Official commit, and Custom commit:

```go
base := &CustomReleaseInfo{
    DetectionComplete: true,
    HasUpdate: true,
    UpdateKind: UpdateKindCombined,
    TargetOfficialVersion: "v0.1.169",
    TargetOfficialCommit: strings.Repeat("a", 40),
    TargetCustomCommit: strings.Repeat("b", 40),
}
fingerprint := customReleaseUpdateFingerprint(base)
require.Regexp(t, `^[0-9a-f]{64}$`, fingerprint)
require.Equal(t, fingerprint, customReleaseUpdateFingerprint(base))
```

Also assert incomplete, `has_update=false`, and `kind=none` return an empty
fingerprint.

- [ ] **Step 2: Write failing per-admin persistence tests**

Set `SUB2API_RELEASE_NOTICE_STATE_FILE` to a temp path. Assert:

```go
unread, err := customReleaseNoticeUnread(41, fingerprint)
require.NoError(t, err)
require.True(t, unread)
require.NoError(t, markCustomReleaseNoticeRead(41, fingerprint))
unread, err = customReleaseNoticeUnread(41, fingerprint)
require.NoError(t, err)
require.False(t, unread)
other, err := customReleaseNoticeUnread(42, fingerprint)
require.NoError(t, err)
require.True(t, other)
```

Recreate no in-memory object and repeat the read to prove file persistence.

- [ ] **Step 3: Write failing validation, pruning, and failure tests**

Cover invalid user IDs/fingerprints, symlink state targets, malformed JSON,
read-only parent/write failure, `0600` mode on non-Windows, and deterministic
oldest-entry pruning at 10,001 records.

- [ ] **Step 4: Implement the focused state module**

Define:

```go
const customReleaseNoticeSchemaVersion = 1
const customReleaseNoticeMaxAdmins = 10_000

type customReleaseNoticeAdminState struct {
    LastReadFingerprint string `json:"last_read_fingerprint"`
    ReadAt string `json:"read_at"`
}

type customReleaseNoticeState struct {
    SchemaVersion int `json:"schema_version"`
    Admins map[string]customReleaseNoticeAdminState `json:"admins"`
}
```

Implement canonical SHA-256 with an LF after every field, guarded reads/writes,
same-directory temporary file, `Sync`, atomic rename, and directory sync. Use a
package mutex only around the state-file read/modify/write transaction.

Expose the store through these `UpdateService` methods so the custom handler
interface and implementation stay consistent:

```go
func (s *UpdateService) CustomReleaseNoticeUnread(
    ctx context.Context, userID int64, fingerprint string,
) (bool, error)

func (s *UpdateService) MarkCustomReleaseNoticeRead(
    ctx context.Context, userID int64, fingerprint string,
) error
```

- [ ] **Step 5: Populate the target Official commit and fingerprint**

Add to `CustomReleaseInfo`:

```go
TargetOfficialCommit string `json:"target_official_commit,omitempty"`
UpdateFingerprint string `json:"update_fingerprint,omitempty"`
NoticeUnread bool `json:"notice_unread"`
NoticeWarning string `json:"notice_warning,omitempty"`
```

Set `TargetOfficialCommit` to the current Official commit unless a valid newer
Stable commit was resolved. At the end of `CheckCustomRelease`, set
`UpdateFingerprint = customReleaseUpdateFingerprint(info)`; do not read an
admin state in this context-free method.

- [ ] **Step 6: Run focused Go tests**

```bash
cd backend && go test -tags=unit ./internal/service \
  -run 'TestCustomRelease(UpdateFingerprint|Notice|Check)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the state module**

```bash
git add backend/internal/service/custom_release_notice.go \
  backend/internal/service/custom_release_notice_test.go \
  backend/internal/service/custom_release_service.go \
  backend/internal/service/custom_release_service_test.go
git commit -m "feat(release): persist per-admin update notice state"
```

## Task 3: Authenticated Notice HTTP Contract

**Files:**

- Modify: `backend/internal/handler/admin/custom_release_handler.go`
- Modify: `backend/internal/handler/admin/custom_release_handler_test.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Modify: `deploy/tests/backend-extension-route-contract.test.mjs`

- [ ] **Step 1: Extend the handler test stub and write failing check tests**

Add service-interface methods:

```go
CustomReleaseNoticeUnread(context.Context, int64, string) (bool, error)
MarkCustomReleaseNoticeRead(context.Context, int64, string) error
```

Set `middleware.AuthSubject{UserID: 41}` in Gin context. Assert the check handler
passes 41 and the computed fingerprint to the notice method and returns
`notice_unread=true`.

- [ ] **Step 2: Write advisory failure tests**

Make the stub notice read return an error. Assert HTTP 200, original
`has_update=true`, `notice_unread=true`, and non-empty `notice_warning`. Make
mark-read return an error and assert HTTP 200 with `persisted=false`.

- [ ] **Step 3: Write mark-read validation and admin-isolation tests**

Cover a valid 64-hex fingerprint, invalid JSON, invalid fingerprint, missing
auth subject, user 41, and user 42. No test may touch the official user model.

- [ ] **Step 4: Implement handler decoration and best-effort mark read**

In `CheckCustomRelease`, obtain the subject with
`middleware.GetAuthSubjectFromContext`. When the fingerprint is non-empty, set
unread from the store. On failure set unread true and an advisory warning.

Add `MarkCustomReleaseRead` that validates the body, obtains the same subject,
calls the service, and always returns success for persistence errors:

```go
response.Success(c, gin.H{
    "fingerprint": fingerprint,
    "persisted": err == nil,
})
```

- [ ] **Step 5: Register only the custom authenticated route**

Add:

```go
admin.POST("/system/custom-release/read", h.Admin.System.MarkCustomReleaseRead)
```

to `registerCustomAdminRoutes`. Extend the Node ownership contract to require
this exact path in `custom_extensions.go` and forbid it in the central router.

- [ ] **Step 6: Run focused tests**

```bash
cd backend && go test -tags=unit ./internal/handler/admin \
  -run 'TestCustomRelease(Check|MarkRead)' -count=1
cd .. && node --test deploy/tests/backend-extension-route-contract.test.mjs
```

Expected: PASS.

- [ ] **Step 7: Commit the HTTP contract**

```bash
git add backend/internal/handler/admin/custom_release_handler.go \
  backend/internal/handler/admin/custom_release_handler_test.go \
  backend/internal/server/routes/custom_extensions.go \
  deploy/tests/backend-extension-route-contract.test.mjs
git commit -m "feat(release): expose per-admin notice acknowledgement"
```

## Task 4: Data-Volume-Only Rollback History

**Files:**

- Modify: `backend/internal/service/custom_release_service.go`
- Modify: `backend/internal/service/custom_release_service_test.go`
- Test: `backend/internal/service/release_ledger_test.go`

- [ ] **Step 1: Replace runtime-filter tests with a no-runtime-dependency test**

Create current plus four complete historical fixtures. Set `PATH` to an empty
directory and `SUB2API_REPO` to a missing path. Assert the service returns the
newest three historical records and excludes current:

```go
releases, err := svc.ListRollbackReleases(context.Background())
require.NoError(t, err)
require.Equal(t, []string{"release-4", "release-3", "release-2"}, releaseIDs(releases))
```

- [ ] **Step 2: Verify the test fails under the current runtime filter**

```bash
cd backend && go test -tags=unit ./internal/service \
  -run TestCustomReleaseRollbackCandidatesNeedNoRuntimeTools -count=1
```

Expected: FAIL with an empty result.

- [ ] **Step 3: Remove Web runtime eligibility**

Change only:

```go
return newCustomReleaseLedgerStore().ListRollbackReleases(3, nil)
```

Delete `rollbackReleaseRuntimeEligible`, `rollbackSourceAvailable`,
`rollbackImageAvailable`, their mutable test hooks, and now-unused `os/exec`
dependencies/constants. Do not weaken `releaseLedgerStore` artifact validation.

- [ ] **Step 4: Run service and ledger tests**

```bash
cd backend && go test -tags=unit ./internal/service \
  -run 'Test(CustomReleaseRollback|ReleaseLedger)' -count=1
```

Expected: PASS, including incomplete snapshot exclusion and three-item limit.

- [ ] **Step 5: Commit the responsibility split**

```bash
git add backend/internal/service/custom_release_service.go \
  backend/internal/service/custom_release_service_test.go \
  backend/internal/service/release_ledger_test.go
git commit -m "fix(release): list rollback snapshots without docker"
```

## Task 5: Frontend Notice And Current-Release Store

**Files:**

- Modify: `frontend/src/features/custom-release/api.ts`
- Modify: `frontend/src/features/custom-release/store.ts`
- Create: `frontend/src/features/custom-release/__tests__/store.spec.ts`
- Modify: `frontend/src/features/custom-release/__tests__/api.spec.ts`

- [ ] **Step 1: Write failing API tests**

Assert `markCustomReleaseRead(fingerprint)` posts to
`/admin/system/custom-release/read` with the exact fingerprint and returns
`{ fingerprint, persisted }`. Extend version fixtures with
`target_official_commit`, `update_fingerprint`, and `notice_unread`.

- [ ] **Step 2: Write failing independent-state store tests**

Cover:

```ts
expect(store.hasUpdate).toBe(true)
expect(store.noticeUnread).toBe(false)
expect(store.updateFingerprint).toBe(fingerprint)
```

after an acknowledged server response. Then return a new fingerprint with
`notice_unread=true` and assert the indicator becomes unread while `hasUpdate`
remains true in both cases.

- [ ] **Step 3: Write failing acknowledgement failure tests**

Mock the mark API to reject. Assert `markCurrentNoticeRead()` resolves without
throwing and optimistically changes only `noticeUnread` to false. A later
`fetchVersion(true)` must reconcile it from the server.

- [ ] **Step 4: Write failing current-release error tests**

Mock `getCurrentRelease` to reject and assert:

```ts
expect(store.currentRelease).toBeNull()
expect(store.currentReleaseLoading).toBe(false)
expect(store.currentReleaseError).toContain('current release')
```

Retry with success and assert the error clears and identity is populated.

- [ ] **Step 5: Implement API types and store state**

Add:

```ts
export interface NoticeReadResult { fingerprint: string; persisted: boolean }
export async function markCustomReleaseRead(fingerprint: string): Promise<NoticeReadResult>
```

Extend `VersionInfo` and `UpdateJob` with the target identity fields returned by
the backend. Add store refs `updateFingerprint`, `noticeUnread`,
`noticeWarning`, `currentReleaseLoading`, and `currentReleaseError`.

`fetchCurrentRelease` must set loading before the call, clear stale identity on
failure, expose a normalized message, and clear loading in `finally`.

- [ ] **Step 6: Run focused frontend tests**

```bash
cd frontend && pnpm exec vitest run \
  src/features/custom-release/__tests__/api.spec.ts \
  src/features/custom-release/__tests__/store.spec.ts
```

Expected: PASS.

- [ ] **Step 7: Commit the frontend data contract**

```bash
git add frontend/src/features/custom-release/api.ts \
  frontend/src/features/custom-release/store.ts \
  frontend/src/features/custom-release/__tests__/api.spec.ts \
  frontend/src/features/custom-release/__tests__/store.spec.ts
git commit -m "feat(frontend): track unread release targets"
```

## Task 6: Complete Official-Style Rollback Panel

**Files:**

- Modify: `frontend/src/features/custom-release/ReleaseRollbackPanel.vue`
- Modify: `frontend/src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts`

- [ ] **Step 1: Write state-complete rendering tests**

Add separate tests for loading spinner, current/history error with retry,
empty-state copy, and a populated current plus three targets. Assert every
target shows Official, Custom, eight-character commit, and localized time.

- [ ] **Step 2: Write selection and prepared-state tests**

Click the second target and assert amber border/radio classes plus one `prepare`
event. Supply a prepared rollback job, attempt to click another target, and
assert selection is locked and the confirm event uses the prepared `job_id`.

- [ ] **Step 3: Write expiry and recovery tests**

Use fake timers with an expiry two seconds ahead. Assert countdown reaches zero,
confirm disables, and exactly one `expired(job_id)` event fires. For a terminal
error, assert the target remains visible and retry emits once.

- [ ] **Step 4: Implement explicit props and events**

Use:

```ts
const props = defineProps<{
  current?: ReleaseIdentity | null
  releases: ReleaseIdentity[]
  operation?: UpdateJob | null
  currentLoading?: boolean
  historyLoading?: boolean
  error?: string
}>()
const emit = defineEmits<{
  retry: []
  prepare: [releaseID: string]
  apply: [jobID: string]
  expired: [jobID: string]
}>()
```

Guard expiry emission by job ID so the one-second timer cannot emit repeatedly.

- [ ] **Step 5: Implement the visual states**

Copy the official component's Tailwind language: primary spinner, red bordered
error plus full-width retry, centered muted empty state, bordered full-width
radio cards, amber selected state, emerald prepared summary, and full-width
amber action. Keep all logic in this custom component and use the shared `Icon`
component.

- [ ] **Step 6: Run panel tests and typecheck**

```bash
cd frontend && pnpm exec vitest run \
  src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts
pnpm run typecheck
```

Expected: PASS.

- [ ] **Step 7: Commit the panel**

```bash
git add frontend/src/features/custom-release/ReleaseRollbackPanel.vue \
  frontend/src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts
git commit -m "feat(frontend): complete two-phase rollback panel"
```

## Task 7: Badge Acknowledgement And Rollback Orchestration

**Files:**

- Modify: `frontend/src/features/custom-release/CustomReleaseBadge.vue`
- Modify: `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

- [ ] **Step 1: Write unread-only styling tests**

Mount with `hasUpdate=true`, `noticeUnread=false`; assert update content exists
after opening but the collapsed button has no amber background, dot, or ping.
Mount with `noticeUnread=true`; assert all three unread visuals exist.

- [ ] **Step 2: Write once-only acknowledgement tests**

Assert opening a closed dropdown calls `markCurrentNoticeRead` once. Closing and
reopening the already acknowledged fingerprint must not issue a second call.
Returning a new fingerprint/unread state must permit one new call.

- [ ] **Step 3: Write operation acknowledgement tests**

For update prepare/apply and rollback prepare/apply, assert mark-read is invoked
before the operation API and a rejected mark-read promise does not suppress the
operation call.

- [ ] **Step 4: Write rollback error/expiry integration tests**

Assert a failed current-release request renders the panel error and retry calls
both current release and history APIs. On `expired`, call the existing
`applyRollback(jobID)` refusal path, ignore its expected expiry error, and resume
polling until terminal `expired`; then clear the operation and reload identity.

- [ ] **Step 5: Implement the badge split**

Add computed `noticeUnread` and use it only in the collapsed button class,
indicator, and title. Keep `hasUpdate` in all update content and capability
branches. Track the last locally acknowledged fingerprint so repeated toggles
do not post again before the next server fetch.

- [ ] **Step 6: Wire complete panel states**

Pass `currentReleaseLoading`, `historyLoading`, combined error, current identity,
history, and operation to `ReleaseRollbackPanel`. Implement `retry`, `prepare`,
`apply`, and `expired` handlers without moving logic into official
`VersionBadge.vue`.

- [ ] **Step 7: Run badge suite**

```bash
cd frontend && pnpm exec vitest run \
  src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts \
  src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts \
  src/features/custom-release/__tests__/store.spec.ts \
  src/features/custom-release/__tests__/api.spec.ts
pnpm run typecheck
```

Expected: PASS.

- [ ] **Step 8: Commit badge integration**

```bash
git add frontend/src/features/custom-release/CustomReleaseBadge.vue \
  frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts
git commit -m "feat(frontend): acknowledge release targets once per admin"
```

## Task 8: Strengthen Host Rollback Fail-Closed Tests

**Files:**

- Modify: `deploy/ops/tests/test-prepare-rollback.sh`
- Test: `deploy/ops/prepare-rollback.sh`
- Test: `deploy/ops/release-common.sh`

- [ ] **Step 1: Add unavailable-Git and paired-image refusal scenarios**

Add `missing-git`, `main-pull-failure`, and `extensions-pull-failure`. The fake
tools return 127 for Git or fail both local inspect and pull for the selected
digest. Each scenario must assert failed operation, cleared active pointer,
unchanged release projection checksum, and no Compose lifecycle call.

- [ ] **Step 2: Add wrong OCI revision refusal**

For `wrong-revision`, return `OTHER_COMMIT` from the main image OCI revision
label. Assert prepare fails before fresh production backup or manifest creation.

- [ ] **Step 3: Keep and clarify corrupt backup refusal**

Retain `corrupt-target` and `dump-validation-failure`; assert neither creates a
prepared manifest and both keep production state unchanged.

- [ ] **Step 4: Run host fixtures**

```bash
bash deploy/ops/tests/test-prepare-rollback.sh
bash deploy/ops/tests/test-apply-rollback.sh
bash deploy/ops/tests/test-release-pipeline.sh
```

Expected: PASS for all success and refusal scenarios.

- [ ] **Step 5: Commit the host evidence**

```bash
git add deploy/ops/tests/test-prepare-rollback.sh
git commit -m "test(release): prove rollback preparation fails closed"
```

## Task 9: Stage B Strict Reduced-Mount Contract

**Files:**

- Modify: `deploy/docker-compose.custom.yml`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/tests/test-release-common-compose.sh`
- Modify: `deploy/tests/compose-overlay-contract.test.mjs`

- [ ] **Step 1: Change tests from transition to strict expectations**

The shell validator fixture must now accept only the reduced shape and reject
legacy plus both hybrids. The Node test must reject these literal overlay
markers:

```text
/root/sub2api:/repo:rw
/var/run/docker.sock:/var/run/docker.sock
/usr/bin/docker:/usr/bin/docker:ro
```

The rendered test must require exactly `/app/data` and
`/app/scripts/sync-upstream.sh`, with the trigger read-only.

- [ ] **Step 2: Run tests and verify legacy overlay fails**

```powershell
node --test deploy/tests/compose-overlay-contract.test.mjs
```

```bash
bash deploy/ops/tests/test-release-common-compose.sh
```

Expected: FAIL while the overlay and transition validator remain legacy-compatible.

- [ ] **Step 3: Remove privileged mounts from the overlay**

Leave only:

```yaml
volumes:
  - /opt/sub2api-custom/sync-trigger.sh:/app/scripts/sync-upstream.sh:ro
```

The unmodified base Compose supplies `sub2api_data:/app/data`.

- [ ] **Step 4: Make the installed validator strict**

Replace `legacy_mounts or reduced_mounts` with the exact reduced target set.
Additionally assert the trigger mount is read-only and has source
`/opt/sub2api-custom/sync-trigger.sh`. Do not accept extra bind mounts.

- [ ] **Step 5: Update all rendered Compose fixtures**

Change release prepare/apply/rollback fixture JSON to contain the data and
read-only trigger mounts only. This proves every host path can validate final
snapshots and paired restores without Web Docker/source access.

- [ ] **Step 6: Run deployment suites**

```powershell
node --test deploy/tests/*.test.mjs
```

```bash
bash deploy/ops/tests/test-release-common-compose.sh
bash deploy/ops/tests/test-prepare-release-ledger.sh
bash deploy/ops/tests/test-apply-release-ledger.sh
bash deploy/ops/tests/test-prepare-rollback.sh
bash deploy/ops/tests/test-apply-rollback.sh
bash deploy/ops/tests/test-release-pipeline.sh
```

Expected: PASS with no fixture or production contract requiring `/repo`, Docker
socket, or Docker binary in the Web container.

- [ ] **Step 7: Commit final privilege reduction**

```bash
git add deploy/docker-compose.custom.yml deploy/ops/release-common.sh \
  deploy/ops/tests deploy/tests/compose-overlay-contract.test.mjs
git commit -m "security(release): remove web docker and source mounts"
```

## Task 10: Repository And Knowledge-Base Documentation

**Files:**

- Modify: `AGENTS.md`
- Modify: `docs/SUB2API-CUSTOM-OPERATIONS.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/ops/README.md`
- Modify outside repository: `03-应用与项目/Sub2API/README.md`
- Modify outside repository: `01-总览与台账/服务器清单/VPS服务器清单.md`
- Modify outside repository: `01-总览与台账/服务与域名总览/站点与服务访问台账.md`
- Modify outside repository: `02-基础设施/主机与系统/VPS专项/VPS运维规则.md`

- [ ] **Step 1: Update repository ownership rules**

Document the exact state file, per-admin fingerprint fields, data-only Web list,
host-only validation, final two-mount rendered contract, and the prohibition on
reintroducing source/Docker mounts. Preserve every zero-overlap rule.

- [ ] **Step 2: Update operator workflow**

State that opening the dropdown acknowledges only the visual notice; it does
not change update availability. Describe rollback loading/error/retry/empty,
prepare, one-hour confirmation, expiry recovery, and no normal database restore.

- [ ] **Step 3: Preserve the Stage A/Stage B gate in the runbook**

Record evidence fields for Stage A commit, Actions, paired digests, production
release, `/opt` sync, then Stage B commit/Actions/digests/release/final `/opt`
sync. Explicitly prohibit advancing Stage B on `origin/custom-release` before
the Stage A production host gate.

- [ ] **Step 4: Update knowledge-base summaries without dynamic values**

Change only Sub2API operational paragraphs. Do not write fixed current versions,
digests, passwords, tokens, or a machine-specific repository path as policy.

- [ ] **Step 5: Validate documentation and commit repository docs**

```bash
git diff --check
git add AGENTS.md docs/SUB2API-CUSTOM-OPERATIONS.md \
  deploy/RELEASE-RUNBOOK.md deploy/ops/README.md
git commit -m "docs(release): document notice and rollback safety"
```

The knowledge-base files are not part of this Git commit and must be reported
separately as local documentation updates.

## Task 11: Full Verification And Diff Audit

**Files:** No new files.

- [ ] **Step 1: Format source**

```bash
cd backend && gofmt -w internal/service/custom_release_notice.go \
  internal/service/custom_release_notice_test.go \
  internal/service/custom_release_service.go \
  internal/service/custom_release_service_test.go \
  internal/handler/admin/custom_release_handler.go \
  internal/handler/admin/custom_release_handler_test.go
```

- [ ] **Step 2: Run backend verification**

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler/admin -count=1
go test ./internal/server/... -count=1
go test ./... -count=1
golangci-lint run ./...
```

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

```bash
cd frontend && pnpm run lint:check
pnpm run typecheck
pnpm exec vitest run
pnpm run build
```

Expected: PASS.

- [ ] **Step 4: Run deployment and extension verification**

```bash
cd extensions-self/account-monitor && go test ./... -count=1
cd ../risk-control && go test ./... -count=1
cd ../..
node --test deploy/tests/*.test.mjs
pwsh -NoProfile -File deploy/ops/tests/test-script-contract.ps1
bash deploy/tests/site-bootstrap-test.sh
bash deploy/ops/tests/test-release-pipeline.sh
```

Expected: PASS; environment-specific Docker render tests may skip locally but
must run in Actions.

- [ ] **Step 5: Prove Stable boundaries and final mounts**

```bash
node --test deploy/tests/custom-overlap-budget.test.mjs
git diff --exit-code "$(jq -r .commit_sha deploy/stable-release-baseline.json)" -- \
  backend/cmd/server/wire_gen.go \
  backend/internal/handler/wire.go \
  backend/internal/handler/gateway_handler.go \
  backend/internal/handler/openai_gateway_handler.go \
  backend/internal/server/router.go \
  backend/internal/server/routes/gateway.go \
  backend/internal/handler/admin/user_handler.go \
  backend/internal/server/middleware/security_headers.go \
  frontend/src/router/index.ts \
  deploy/docker-compose.local.yml
rg -n '/root/sub2api:/repo|docker.sock:/var/run/docker.sock|/usr/bin/docker:/usr/bin/docker' \
  deploy/docker-compose.custom.yml deploy/ops deploy/tests
```

Expected: overlap tests pass; zero-overlap diff is empty; the final `rg` returns
only explicit rejection tests and migration documentation, never an accepted
final Compose fixture.

- [ ] **Step 6: Review commits and worktree**

```bash
git diff --check origin/custom-release...HEAD
git log --oneline --decorate origin/custom-release..HEAD
git status --short --branch
```

Expected: intended commits only and a clean worktree.

## Task 12: Stage A Push, Production Gate, And Stage B Final Push

**Files:** No new files.

- [ ] **Step 1: Identify the Stage A commit exactly**

```bash
STAGE_A_COMMIT=$(git log --format=%H --grep='^chore(release): accept reduced web mounts during migration$' -n 1)
test -n "$STAGE_A_COMMIT"
test "$(git show -s --format=%s "$STAGE_A_COMMIT")" = \
  'chore(release): accept reduced web mounts during migration'
git show --stat "$STAGE_A_COMMIT"
```

Record the printed full SHA in the execution evidence and use that exact value
for the merge, Actions, image, and production comparisons.

- [ ] **Step 2: Advance only Stage A to `custom-release`**

Use the clean primary worktree. Merge or fast-forward the design and Stage A
commits only, then:

```bash
git push origin custom-release
```

Expected: remote branch contains the design and transition validator but none
of the Stage B no-mount/functionality commits.

- [ ] **Step 3: Wait for Stage A Actions and paired GHCR images**

Verify the `Custom Release` workflow head SHA equals Stage A and all required
jobs succeed. Verify both public `custom-<full-sha>` images and OCI revision.

- [ ] **Step 4: Stop for explicit production authorization**

Report Stage A commit, tests, push, Actions, and digests. Do not click the admin
update control, execute SSH, deploy production, or sync `/opt` without explicit
authorization. Stage B remains on the feature branch.

- [ ] **Step 5: After authorization, deploy and synchronize Stage A**

All remote commands use `ssh-skill`. Use the administrator two-phase update,
wait for health, confirm production commit, back up and install `/opt` scripts,
reload systemd, run `bash -n`, and compare installed files byte-for-byte. Record
the backup and verification evidence.

- [ ] **Step 6: Advance Stage B only after the host gate**

Merge the reviewed feature remainder into `custom-release`, preserving commits
without rewriting history, then:

```bash
git push origin custom-release
```

Expected: remote head equals the final reviewed implementation commit.

- [ ] **Step 7: Wait for final Actions/GHCR without publishing production**

Verify final workflow/jobs and paired images. Report production as still Stage A
unless a second explicit production authorization is given. Do not imply that a
successful push or image build is a production deployment.

## Completion Evidence

The task is complete only when all of these are separately recorded:

- design commit and implementation-plan commit;
- Stage A transition commit, local tests, `origin/custom-release` push,
  Actions, paired GHCR identities, authorized production deployment, and
  verified `/opt` synchronization;
- Stage B backend/frontend/deploy/docs commits and complete local/CI tests;
- final `origin/custom-release` head and final Actions/paired GHCR identities;
- knowledge-base file updates;
- explicit production status showing whether Stage B is unpublished or, after
  separate authorization, its backup/deployment/health/final `/opt` evidence;
- no actual rollback unless separately requested; and
- a clean final worktree with `git diff --check` and Stable overlap tests passing.
