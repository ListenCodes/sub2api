# Dual-Version Release Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an official/custom dual-version production ledger and two-stage complete-snapshot rollback without splitting the existing paired release unit.

**Architecture:** Additive custom Go and Vue modules read the production ledger, expose dual-version detection/history, and queue durable operations while pinned Stable hot files remain byte-identical to upstream. Host-side Bash remains the sole production mutation authority: it validates and atomically commits ledger records, prepares updates or rollbacks, and applies only locally available immutable artifacts after explicit confirmation. Vue renders the current/target pair and drives the same durable operation through prepare and apply.

**Tech Stack:** Go 1.26.5, Gin, Vue 3, TypeScript, Pinia, Vitest, Bash, jq, Docker Compose, Node test runner, PowerShell contracts.

---

## File And Ownership Map

- `backend/internal/service/custom_release_service.go`: all custom detection and operation queueing added as methods on the existing service without modifying its official source file.
- `backend/internal/service/release_ledger.go`: typed, read-mostly ledger store for current identity, operation pointer, and rollback eligibility.
- `backend/internal/service/update_job.go`: generic durable update/rollback operation record, status validation, operation creation, and legacy job rejection.
- `backend/internal/handler/admin/custom_release_handler.go`: authenticated/idempotent update and rollback prepare/apply methods added to `SystemHandler` outside its official source file.
- `backend/internal/server/routes/custom_extensions.go`: custom release routes.
- `deploy/ops/release-ledger.sh`: host ledger validation, atomic writes, publication, pointer changes, and rollback-list primitives.
- `deploy/ops/migrate-release-ledger.sh`: explicit idempotent baseline bootstrap for `aa2d24106cab0a03785330d8e0ff4e02b0474a0e = v1.0.0`.
- `deploy/ops/prepare-release.sh` / `apply-release.sh`: ledger-aware normal update preparation and application.
- `deploy/ops/prepare-rollback.sh` / `apply-rollback.sh`: complete-snapshot rollback preparation and application.
- `deploy/ops/release-common.sh`: manifest, source switching, Compose, health, and artifact helpers shared by update and rollback.
- `deploy/ops/release-state.sh`, `sync-trigger.sh`, `sync-and-publish.sh`: generic operation storage and dispatch with legacy fail-closed compatibility.
- `frontend/src/features/custom-release/api.ts`: dual identity, snapshot, and four-action API contracts.
- `frontend/src/features/custom-release/store.ts`: current/target official/custom identity cache.
- `frontend/src/features/custom-release/CustomReleaseBadge.vue`: compact badge and active operation coordinator.
- `frontend/src/features/custom-release/ReleaseRollbackPanel.vue`: last-three complete snapshot selection and rollback confirmation.
- `frontend/src/features/custom-release/releaseOperation.ts`: pure state labels, terminal/prepared predicates, and countdown helpers.
- `deploy/tests/custom-release-isolation.test.mjs`: executable conflict-budget contract that keeps upstream hot files equal to the pinned Stable baseline.

### Task 1: Isolate Existing Custom Updater From Upstream Hot Files

**Files:**
- Create: `backend/internal/service/custom_release_service.go`
- Create: `backend/internal/service/custom_release_service_test.go`
- Create: `backend/internal/handler/admin/custom_release_handler.go`
- Create: `backend/internal/handler/admin/custom_release_handler_test.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Modify: `backend/internal/server/routes/admin.go` (three legacy route handler substitutions only)
- Create: `frontend/src/features/custom-release/api.ts`
- Create: `frontend/src/features/custom-release/store.ts`
- Create: `frontend/src/features/custom-release/CustomReleaseBadge.vue`
- Create: `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue` (one import target only)
- Restore to pinned Stable content after extraction: `backend/internal/service/update_service.go`
- Restore to pinned Stable content after extraction: `backend/internal/handler/admin/system_handler.go`
- Restore to pinned Stable content after extraction: `frontend/src/components/common/VersionBadge.vue`
- Restore to pinned Stable content after extraction: `frontend/src/api/admin/system.ts`
- Restore to pinned Stable content after extraction: `frontend/src/stores/app.ts`
- Create: `deploy/tests/custom-release-isolation.test.mjs`

- [ ] **Step 1: Write failing behavior-parity and conflict-budget tests**

Move the existing two-phase updater tests into new custom-release test files before moving implementation. Preserve assertions for unified official/custom detection, prepare/apply separation, server-side job recovery, docs-only refusal, 15-minute prepared countdown, explicit confirmation, and no replay of terminal history.

Add a Node contract that reads `deploy/stable-release-baseline.json`, resolves its `commit_sha`, and runs `git diff --exit-code <stable-commit> --` for these five files:

```js
const stableOwnedHotFiles = [
  'backend/internal/service/update_service.go',
  'backend/internal/handler/admin/system_handler.go',
  'frontend/src/components/common/VersionBadge.vue',
  'frontend/src/api/admin/system.ts',
  'frontend/src/stores/app.ts'
]
```

The same test must assert `admin.go` maps `/update` to `PrepareUpdate`, maps old `/rollback` and `/rollback-versions` to `LegacyRollbackUnsupported`, and contains no ledger/version/state-machine logic. Assert `AppSidebar.vue` imports `@/features/custom-release/CustomReleaseBadge.vue` and contains no release state logic.

- [ ] **Step 2: Run focused tests and verify they fail**

```powershell
Set-Location backend
go test -tags=unit ./internal/service ./internal/handler/admin -run 'TestCustomRelease' -count=1
Set-Location ../frontend
pnpm vitest run src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts
Set-Location ..
node --test deploy/tests/custom-release-isolation.test.mjs
```

Expected: FAIL because additive custom modules do not exist and the five hot files still contain custom updater code.

- [ ] **Step 3: Extract backend behavior without changing dependency injection**

Define custom methods on the existing types from new files, so Wire providers and handler structs do not change:

```go
type customReleaseGitHubClient interface {
	FetchLatestRelease(context.Context, string) (*GitHubRelease, error)
	FetchCustomReleaseHead(context.Context, string, string) (*GitRef, error)
	CompareCommits(context.Context, string, string, string) ([]ChangedFile, error)
	FetchRefCommit(context.Context, string, string) (string, error)
}

func (s *UpdateService) CheckCustomRelease(ctx context.Context, force bool) (*CustomReleaseInfo, error)
func (s *UpdateService) PrepareUpdate(ctx context.Context) (*UpdateJob, error)
func (s *UpdateService) ApplyUpdate(ctx context.Context, jobID string) (*UpdateJob, error)

func (h *SystemHandler) CheckCustomRelease(c *gin.Context)
func (h *SystemHandler) PrepareUpdate(c *gin.Context)
func (h *SystemHandler) ApplyUpdate(c *gin.Context)
func (h *SystemHandler) GetUpdateStatus(c *gin.Context)
func (h *SystemHandler) LegacyRollbackUnsupported(c *gin.Context)
```

The new handler file may access `SystemHandler`'s existing private service/lock fields because it remains in package `admin`. It type-asserts a focused custom interface and reuses existing idempotency, audit, compliance, and system-lock helpers. The service file uses environment-derived data/trigger paths and its own custom mutex; it must not require fields added to official `UpdateService`. Register all new routes in `custom_extensions.go`; the only official route-table changes are the three safe legacy handler substitutions described above.

- [ ] **Step 4: Extract frontend behavior and restore official hot files**

Move the custom API/store/badge behavior into `frontend/src/features/custom-release/`. Use component-local `useI18n({ useScope: 'local', messages })` messages so official locale files do not change. Change only the AppSidebar import:

```ts
import VersionBadge from '@/features/custom-release/CustomReleaseBadge.vue'
```

After parity tests pass, manually restore the five Stable-owned hot files to the exact blobs at the commit in `deploy/stable-release-baseline.json`. Do not use `git reset`, `git checkout --`, or overwrite unrelated files. Confirm the Node conflict-budget test passes.

- [ ] **Step 5: Run parity tests and commit the isolation refactor**

```powershell
Set-Location backend
go test -tags=unit ./internal/service ./internal/handler/admin -run 'TestCustomRelease' -count=1
Set-Location ../frontend
pnpm vitest run src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts
pnpm run typecheck
Set-Location ..
node --test deploy/tests/custom-release-isolation.test.mjs
git add backend/internal/service/custom_release_service.go backend/internal/service/custom_release_service_test.go backend/internal/handler/admin/custom_release_handler.go backend/internal/handler/admin/custom_release_handler_test.go backend/internal/server/routes/custom_extensions.go backend/internal/server/routes/admin.go backend/internal/service/update_service.go backend/internal/handler/admin/system_handler.go frontend/src/features/custom-release frontend/src/components/layout/AppSidebar.vue frontend/src/components/common/VersionBadge.vue frontend/src/api/admin/system.ts frontend/src/stores/app.ts deploy/tests/custom-release-isolation.test.mjs
git commit -m "refactor(release): isolate custom updater from upstream"
```

### Task 2: Go Ledger Store And Immutable Record Contracts

**Files:**
- Create: `backend/internal/service/release_ledger.go`
- Create: `backend/internal/service/release_ledger_test.go`
- Modify: `backend/internal/service/custom_release_service.go`

- [ ] **Step 1: Write failing tests for state, current release, eligibility, corruption, and last-three ordering**

Add table-driven unit tests that create `state.json` and `releases/*.json` under `t.TempDir()`. Use these exact public shapes in the fixtures:

```go
state := ReleaseLedgerState{
	SchemaVersion:          1,
	CurrentReleaseID:       "release-current",
	CustomVersionHighWater: 4,
	ActiveOperationID:      "",
	UpdatedAt:              "2026-07-23T08:00:00Z",
}
record := ReleaseRecord{
	SchemaVersion:         1,
	ReleaseID:             "release-current",
	OfficialVersion:       "v0.1.163",
	OfficialCommit:        strings.Repeat("a", 40),
	CustomVersion:         "v1.0.4",
	CustomVersionSequence: 4,
	CustomCommit:          strings.Repeat("b", 40),
	MainDigest:            "sha256:" + strings.Repeat("1", 64),
	ExtensionsDigest:      "sha256:" + strings.Repeat("2", 64),
	BaseComposeSHA256:     strings.Repeat("3", 64),
	CustomComposeSHA256:   strings.Repeat("4", 64),
	RenderedComposeSHA256: strings.Repeat("5", 64),
	EnvSHA256:             strings.Repeat("6", 64),
	BackupDir:             filepath.Join(root, "artifacts", "release-current"),
	BackupManifestSHA256:  strings.Repeat("7", 64),
	PublishedAt:           "2026-07-23T08:00:00Z",
	SourceKind:            "custom",
	OperationID:           "update-current",
}
```

Assert `CurrentRelease()` returns the pair, `ListRollbackReleases(3)` excludes current, sorts by `published_at` descending, excludes records with missing `target/SHA256SUMS`, and returns `LEDGER_INCONSISTENT` for a missing current record, malformed custom version, sequence/version mismatch, or bad digest.

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```powershell
Set-Location backend
go test -tags=unit ./internal/service -run 'TestReleaseLedger' -count=1
```

Expected: FAIL because `ReleaseLedgerState`, `ReleaseRecord`, and `newReleaseLedgerStore` do not exist.

- [ ] **Step 3: Implement the typed ledger reader and validators**

Create these types and methods, keeping filesystem details private:

```go
type ReleaseLedgerState struct {
	SchemaVersion          int    `json:"schema_version"`
	CurrentReleaseID       string `json:"current_release_id"`
	CustomVersionHighWater int    `json:"custom_version_high_water"`
	ActiveOperationID      string `json:"active_operation_id,omitempty"`
	UpdatedAt              string `json:"updated_at"`
}

type ReleaseRecord struct {
	SchemaVersion          int    `json:"schema_version"`
	ReleaseID              string `json:"release_id"`
	OfficialVersion        string `json:"official_version"`
	OfficialCommit         string `json:"official_commit"`
	CustomVersion          string `json:"custom_version"`
	CustomVersionSequence  int    `json:"custom_version_sequence"`
	CustomCommit           string `json:"custom_commit"`
	MainDigest             string `json:"main_digest"`
	ExtensionsDigest       string `json:"extensions_digest"`
	BaseComposeSHA256      string `json:"base_compose_sha256"`
	CustomComposeSHA256    string `json:"custom_compose_sha256"`
	RenderedComposeSHA256  string `json:"rendered_compose_sha256"`
	EnvSHA256              string `json:"env_sha256"`
	BackupDir              string `json:"backup_dir"`
	BackupManifestSHA256   string `json:"backup_manifest_sha256"`
	PublishedAt            string `json:"published_at"`
	SourceKind             string `json:"source_kind"`
	OperationID            string `json:"operation_id"`
}

type releaseLedgerStore struct {
	root string
}

func newReleaseLedgerStore(root string) *releaseLedgerStore
func (s *releaseLedgerStore) ReadState() (*ReleaseLedgerState, error)
func (s *releaseLedgerStore) CurrentRelease() (*ReleaseRecord, error)
func (s *releaseLedgerStore) ListRollbackReleases(limit int) ([]ReleaseRecord, error)
```

Validate `vMAJOR.MINOR.PATCH`, require `custom_version == fmt.Sprintf("v1.0.%d", sequence)`, reject negative/high-water regressions, validate SHA/digest/hash formats, canonicalize every artifact path below the configured artifact root, and never read `.env` contents.

- [ ] **Step 4: Re-run focused tests and verify they pass**

Run the Step 2 command. Expected: PASS with all ledger parsing and eligibility cases green.

- [ ] **Step 5: Commit the ledger reader**

```powershell
git add backend/internal/service/release_ledger.go backend/internal/service/release_ledger_test.go backend/internal/service/custom_release_service.go
git commit -m "feat(release): add production ledger reader"
```

### Task 3: Generic Durable Operation And Dual-Version Detection

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_job_service_test.go`
- Modify: `backend/internal/service/custom_release_service.go`
- Modify: `backend/internal/service/custom_release_service_test.go`

- [ ] **Step 1: Write failing operation and detection tests**

Add tests for both operation kinds and every new state:

```go
const (
	ReleaseOperationUpdate   = "update"
	ReleaseOperationRollback = "rollback"
	ReleasePhasePrepare      = "prepare"
	ReleasePhaseApply        = "apply"
)

valid := []string{
	"resolving_target", "resolving_snapshot", "verifying_snapshot",
	"verifying_images", "downloading_images", "rendering_compose",
	"backing_up", "validating_backup", "prepared", "apply_queued",
	"validating_manifest", "switching_extensions", "switching_main",
	"health_checking", "rolling_back", "success", "failed", "conflict",
	"expired", "drifted", "failed_rolled_back", "rollback_failed",
}
```

Extend detection fixtures so the current ledger record is
`Official v0.1.163 / Custom v1.0.4`. Assert target pairs:

```text
official  -> Official v0.1.164 / Custom v1.0.4
custom    -> Official v0.1.163 / Custom v1.0.5
combined  -> Official v0.1.164 / Custom v1.0.5
docs-only -> no candidate custom version and runtime_update=false
none      -> current pair unchanged and has_update=false
```

Assert an unavailable or inconsistent ledger makes `detection_complete=false` and never fabricates `v1.0.0`.

- [ ] **Step 2: Run focused tests and verify they fail**

```powershell
Set-Location backend
go test -tags=unit ./internal/service -run 'Test(UpdateJob|UpdateServiceCheckUpdate|ReleaseOperation)' -count=1
```

Expected: FAIL on missing operation-kind fields, statuses, and target version fields.

- [ ] **Step 3: Generalize the job record and detection response**

Keep `UpdateJob` as the compatibility Go name but add these fields:

```go
type UpdateJob struct {
	JobID                      string `json:"job_id"`
	OperationKind              string `json:"operation_kind"`
	Action                     string `json:"action"`
	Status                     string `json:"status"`
	BaseReleaseID              string `json:"base_release_id,omitempty"`
	TargetReleaseID            string `json:"target_release_id,omitempty"`
	CurrentOfficialVersion     string `json:"current_official_version,omitempty"`
	CurrentCustomVersion       string `json:"current_custom_version,omitempty"`
	TargetOfficialVersion      string `json:"target_official_version,omitempty"`
	TargetCustomVersion        string `json:"target_custom_version,omitempty"`
	ProposedCustomSequence     *int   `json:"proposed_custom_sequence,omitempty"`
	AdvancesCustomVersion      bool   `json:"advances_custom_version"`
}
```

Extend `CustomReleaseInfo` with the same current/target display fields and `ReleaseID`. Source current identity from `ledger.CurrentRelease()`, retain compatibility `current_version`/`latest_version` values without a `v` prefix inside the custom response, and derive custom candidates only from ledger high-water plus runtime update kind. Do not add these fields to the official `UpdateInfo`.

Generate IDs as `update-<unix-nanoseconds>-<random-hex>` or `rollback-<unix-nanoseconds>-<random-hex>`. Accept the old `update-<legacy-suffix>` path format for legacy reads, but reject any legacy record without `operation_kind` before a trigger is written.

- [ ] **Step 4: Re-run the focused tests and verify they pass**

Run the Step 2 command. Expected: PASS, including legacy fail-closed cases and dual-version matrix.

- [ ] **Step 5: Commit operation and detection contracts**

```powershell
git add backend/internal/service/update_job.go backend/internal/service/update_job_service_test.go backend/internal/service/custom_release_service.go backend/internal/service/custom_release_service_test.go
git commit -m "feat(release): add dual-version operation contracts"
```

### Task 4: Administrator API And Legacy Rollback Shutdown

**Files:**
- Modify: `backend/internal/service/custom_release_service.go`
- Modify: `backend/internal/service/custom_release_service_test.go`
- Modify: `backend/internal/handler/admin/custom_release_handler.go`
- Modify: `backend/internal/handler/admin/custom_release_handler_test.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`

- [ ] **Step 1: Write failing handler/service tests for four actions**

Register test routes and assert these requests:

```text
GET  /api/v1/admin/system/custom-release/check
GET  /api/v1/admin/system/release
GET  /api/v1/admin/system/releases/rollback
POST /api/v1/admin/system/update/prepare
POST /api/v1/admin/system/update/apply
POST /api/v1/admin/system/rollback/prepare   {"release_id":"release-v101"}
POST /api/v1/admin/system/rollback/apply     {"job_id":"rollback-prepared"}
```

Every POST must require an `Idempotency-Key`, use a distinct audit action, retain the system operation lock, and return HTTP 202 with the durable record. Assert `/system/update` remains prepare-only. Assert old `POST /system/rollback` and `GET /system/rollback-versions` return `LEGACY_ROLLBACK_UNSUPPORTED` without calling GitHub or binary replacement methods.

- [ ] **Step 2: Run handler and service tests and verify they fail**

```powershell
Set-Location backend
go test -tags=unit ./internal/service ./internal/handler/admin -run 'Test.*(Release|Rollback|Prepare|Apply)' -count=1
```

Expected: FAIL because complete-snapshot rollback methods and routes are missing.

- [ ] **Step 3: Implement service queueing and handlers**

Define the custom handler's private service surface in `custom_release_handler.go` without changing the official interface in `system_handler.go`:

```go
type customReleaseService interface {
	CheckCustomRelease(context.Context, bool) (*service.CustomReleaseInfo, error)
	CurrentRelease(context.Context) (*service.ReleaseRecord, error)
	ListRollbackReleases(context.Context) ([]service.ReleaseRecord, error)
	PrepareUpdate(context.Context) (*service.UpdateJob, error)
	ApplyUpdate(context.Context, string) (*service.UpdateJob, error)
	PrepareRollback(context.Context, string) (*service.UpdateJob, error)
	ApplyRollback(context.Context, string) (*service.UpdateJob, error)
	GetUpdateStatus(context.Context, string) (*service.UpdateJob, error)
}
```

Use one internal `queueOperation(ctx, kind, phase, targetReleaseID)` method. It must return the existing non-terminal/prepared operation for an idempotent duplicate, reject a different concurrent operation with `UPDATE_IN_PROGRESS`, reject expired apply with `UPDATE_PREPARATION_EXPIRED`, and never queue `none` or `docs-only` updates.

Use audit names `admin.system.update.prepare`, `admin.system.update.apply`, `admin.system.rollback.prepare`, and `admin.system.rollback.apply`. Include current/target release and dual-version fields in `updateJobResponse`.

- [ ] **Step 4: Re-run focused tests and verify they pass**

Run the Step 2 command. Expected: PASS with no invocation of `RollbackToVersion`, `applyReleaseAssets`, or `Rollback` from an HTTP handler.

- [ ] **Step 5: Commit the API migration**

```powershell
git add backend/internal/service/custom_release_service.go backend/internal/service/custom_release_service_test.go backend/internal/handler/admin/custom_release_handler.go backend/internal/handler/admin/custom_release_handler_test.go backend/internal/server/routes/custom_extensions.go
git commit -m "feat(admin): add complete release rollback API"
```

### Task 5: Host Ledger Primitives And Exact Baseline Migration

**Files:**
- Create: `deploy/ops/release-ledger.sh`
- Create: `deploy/ops/migrate-release-ledger.sh`
- Create: `deploy/ops/tests/test-release-ledger.sh`
- Modify: `deploy/ops/release-state.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing shell fixtures for ledger validation and migration**

Create fixtures that seed a production state, exact Compose/environment hashes, digest metadata, and backup `SHA256SUMS`. Assert the migration command:

```bash
migrate-release-ledger.sh \
  --expected-production-commit aa2d24106cab0a03785330d8e0ff4e02b0474a0e \
  --official-version v0.1.163 \
  --custom-version v1.0.0
```

creates one immutable bootstrap record, `state.json` with high-water `0`, and the compatibility fields. Re-running must return success without changing hashes. Wrong commit, running digest, Stable identity, Compose hash, environment hash, missing backup, or active job must fail without creating a partial ledger.

Add injected failures after release-record write and compatibility projection write; the fixture must prove deterministic recovery or a `LEDGER_INCONSISTENT` refusal.

- [ ] **Step 2: Run fixtures and verify they fail**

```powershell
bash deploy/ops/tests/test-release-ledger.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
```

Expected: FAIL because the ledger helper and migration script are absent.

- [ ] **Step 3: Implement validated atomic ledger functions**

Define and use these functions in `release-ledger.sh`:

```bash
ledger_state_path() { printf '%s/state.json\n' "$RELEASE_LEDGER_ROOT"; }
ledger_release_path() { printf '%s/releases/%s.json\n' "$RELEASE_LEDGER_ROOT" "$1"; }
ledger_operation_path() { printf '%s/operations/%s.json\n' "$RELEASE_LEDGER_ROOT" "$1"; }

ledger_validate_state() {
  jq -e '
    type == "object"
    and .schema_version == 1
    and (.current_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
    and (.custom_version_high_water | type == "number" and floor == . and . >= 0)
    and ((.active_operation_id == null) or (.active_operation_id | type == "string" and test("^(update|rollback)-[A-Za-z0-9-]+$")))
    and (.updated_at | fromdateiso8601 > 0)
  ' "$1" >/dev/null
}

ledger_validate_release() {
  jq -e --arg artifact_root "$RELEASE_BACKUP_ROOT/" '
    type == "object"
    and .schema_version == 1
    and (.release_id | test("^release-[A-Za-z0-9-]+$"))
    and (.official_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.official_commit | test("^[0-9a-f]{40}$"))
    and (.custom_version_sequence | type == "number" and floor == . and . >= 0)
    and .custom_version == ("v1.0." + (.custom_version_sequence | tostring))
    and (.custom_commit | test("^[0-9a-f]{40}$"))
    and (.main_digest | test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | test("^sha256:[0-9a-f]{64}$"))
    and (.base_compose_sha256 | test("^[0-9a-f]{64}$"))
    and (.custom_compose_sha256 | test("^[0-9a-f]{64}$"))
    and (.rendered_compose_sha256 | test("^[0-9a-f]{64}$"))
    and (.env_sha256 | test("^[0-9a-f]{64}$"))
    and (.backup_manifest_sha256 | test("^[0-9a-f]{64}$"))
    and (.backup_dir | startswith($artifact_root))
    and (.published_at | fromdateiso8601 > 0)
    and (.source_kind | IN("official", "custom", "combined", "bootstrap"))
  ' "$1" >/dev/null
}

ledger_atomic_write() {
  local path="$1" content="$2" directory temporary
  directory="$(dirname "$path")"
  mkdir -p "$directory"
  temporary="$(mktemp "$directory/.ledger.XXXXXX")"
  printf '%s\n' "$content" > "$temporary"
  chmod 0644 "$temporary"
  sync -f "$temporary"
  mv -f "$temporary" "$path"
  sync -f "$directory"
}
```

Implement `ledger_create_release` with a fully written and synced temporary file plus an atomic hard-link create so an existing immutable record can never be overwritten. Implement the active-operation and commit functions as locked read-validate-write operations. Publication order is immutable record, compatibility projection, then state pointer/high-water; rollback order is compatibility projection then state pointer with the original high-water. `ledger_recover_or_refuse` may finish only an exact operation/record/projection identity match.

Move new operations to `$DATA_DIR/release-ledger/operations`. Read the old `$DATA_DIR/release-jobs` only to emit `LEGACY_SINGLE_PHASE_UNSUPPORTED`; never migrate a legacy deployment state into apply.

- [ ] **Step 4: Implement the idempotent bootstrap**

Parse all three required command arguments, verify exact production/container/artifact identity, create an opaque `release-bootstrap-<timestamp>-aa2d24106` record with `source_kind:"bootstrap"`, and write:

```json
{
  "schema_version": 1,
  "current_release_id": "release-bootstrap-20260723T000000Z-aa2d24106",
  "custom_version_high_water": 0,
  "active_operation_id": null,
  "updated_at": "RFC3339"
}
```

Do not run Compose lifecycle commands, pull images, restore databases, or modify the production Git checkout.

- [ ] **Step 5: Run fixtures, verify pass, and commit**

```powershell
bash deploy/ops/tests/test-release-ledger.sh
bash deploy/ops/tests/test-release-pipeline.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
git add deploy/ops/release-ledger.sh deploy/ops/migrate-release-ledger.sh deploy/ops/release-state.sh deploy/ops/tests/test-release-ledger.sh deploy/ops/tests/test-release-pipeline.sh deploy/ops/tests/test-script-contract.ps1
git commit -m "feat(release): add atomic production ledger"
```

### Task 6: Ledger-Aware Update Preparation And Version Proposal

**Files:**
- Modify: `deploy/ops/prepare-release.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/release-state.sh`
- Create: `deploy/ops/tests/test-prepare-release-ledger.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing preparation fixtures**

Cover `official`, `custom`, `combined`, `docs-only`, and `none`. For runtime cases assert the manifest contains:

```json
{
  "operation_kind": "update",
  "base_release_id": "release-current",
  "base_custom_high_water": 4,
  "target_release_id": "release-immutable-candidate",
  "target_official_version": "v0.1.164",
  "target_custom_version": "v1.0.5",
  "proposed_custom_sequence": 5,
  "advances_custom_version": true,
  "target_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "main_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "extensions_digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
  "current_compose_sha256": "64-char hash",
  "target_base_compose_sha256": "64-char hash",
  "target_custom_compose_sha256": "64-char hash",
  "target_env_sha256": "64-char hash",
  "backup_manifest_sha256": "64-char hash",
  "prepared_at": "RFC3339",
  "expires_at": "RFC3339"
}
```

Assert official-only keeps sequence `4`; docs-only/none never creates a manifest or consumes a sequence. Fail the fixture on any Compose lifecycle command, production source switch, ledger pointer change, or `release-state.json` write.

- [ ] **Step 2: Run fixtures and verify they fail**

```powershell
bash deploy/ops/tests/test-prepare-release-ledger.sh
```

Expected: FAIL because current manifests contain no ledger/version proposal and render the production pair instead of a target checkout.

- [ ] **Step 3: Implement target staging and immutable release artifacts**

Under the existing release lock:

1. Read and validate the current ledger state/record.
2. Run existing Stable/custom resolution and scope classification.
3. Compute the version proposal from update kind and high-water.
4. Wait for all seven Actions jobs and verify paired OCI identities/digests.
5. Pull by digest during prepare only.
6. Create a temporary detached worktree for `target_commit`.
7. Copy the current production `.env` to a private staged file and pin both digest variables there.
8. Render target `deploy/docker-compose.yml` plus `deploy/docker-compose.custom.yml` using project `deploy`, `config --quiet`, and `config --format json`.
9. Store the target Compose pair, staged `.env`, rendered JSON, and their SHA256 values under `$BACKUP_DIR/target/`.
10. Back up and verify both current databases and all existing production evidence.

Use a manifest constructor whose jq object has exactly the fields asserted in Step 1 plus Stable identity, workflow URL, source/target commit, old digest pair, and the existing 15-minute expiry.

- [ ] **Step 4: Implement expiry reuse without stale backup reuse**

Permit reuse only when target commit, Stable identity, OCI labels, and both immutable digests match cached verified evidence. Always re-read ledger/current hashes and create a new backup directory, `prepared_at`, `expires_at`, manifest, and backup checksums. Reject a high-water or current-release change.

- [ ] **Step 5: Run fixtures and commit**

```powershell
bash deploy/ops/tests/test-prepare-release-ledger.sh
bash deploy/ops/tests/test-release-pipeline.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
git add deploy/ops/prepare-release.sh deploy/ops/release-common.sh deploy/ops/release-state.sh deploy/ops/tests/test-prepare-release-ledger.sh deploy/ops/tests/test-release-pipeline.sh deploy/ops/tests/test-script-contract.ps1
git commit -m "feat(release): propose custom versions during prepare"
```

### Task 7: Update Apply, Ledger Publication, And Crash Recovery

**Files:**
- Modify: `deploy/ops/apply-release.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/release-ledger.sh`
- Create: `deploy/ops/tests/test-apply-release-ledger.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing apply/drift/allocation fixtures**

Test success plus expiry, current-release drift, high-water drift, origin drift, Compose drift, `.env` drift, digest drift, backup drift, missing local image, dirty worktree, extension failure, main failure, health failure, projection-write failure, ledger-state-write failure, and duplicate apply.

Every fixture must fail if apply invokes `git fetch`, GitHub URLs, Actions waiters, image verification, `docker pull`, `pg_dump`, `pg_restore`, or a Compose lifecycle command without `--pull never`. On pre-mutation failure, assert no state changes. On post-mutation failure, assert the base pair and projections return and the custom high-water does not advance.

- [ ] **Step 2: Run fixtures and verify they fail**

```powershell
bash deploy/ops/tests/test-apply-release-ledger.sh
```

Expected: FAIL because apply does not validate ledger proposal or create an immutable release record.

- [ ] **Step 3: Implement local exact-commit switching and full drift gates**

Add source helpers that never use `git reset --hard`:

```bash
release_source_snapshot() {
  SOURCE_HEAD="$(git -C "$REPO" rev-parse HEAD)"
  SOURCE_REF="$(git -C "$REPO" symbolic-ref --quiet --short HEAD || true)"
}

release_checkout_exact_commit() {
  local target="$1"
  [[ -z "$(git -C "$REPO" status --porcelain --untracked-files=all)" ]]
  git -C "$REPO" cat-file -e "$target^{commit}"
  git -C "$REPO" switch --detach "$target"
}
```

Before calling it, verify manifest SHA/expiry, base release ID, high-water, current production/ledger projection, origin head, current and target Compose/environment hashes, backup SHA, and local RepoDigests. Recreate only `extensions-self`, wait healthy, then recreate `sub2api`, using the explicit pair and `--pull never`.

- [ ] **Step 4: Commit the new release only after health**

Construct a `ReleaseRecord` JSON from the prepared manifest and health timestamp, then call one ledger transaction that:

1. creates the immutable record exclusively;
2. writes compatibility `release-state.json` with release ID and both versions;
3. updates `state.json` current pointer and high-water;
4. settles the operation and clears `active_operation_id`.

Route any failure after source/container mutation through automatic restoration of the base source, target artifact pair, old `.env`, and old digests. A process interrupted after a partial metadata commit must be recovered only when release record, projection, operation, and running identities agree exactly; otherwise return `LEDGER_INCONSISTENT`.

- [ ] **Step 5: Run fixtures and commit**

```powershell
bash deploy/ops/tests/test-apply-release-ledger.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
git add deploy/ops/apply-release.sh deploy/ops/release-common.sh deploy/ops/release-ledger.sh deploy/ops/tests/test-apply-release-ledger.sh deploy/ops/tests/test-script-contract.ps1
git commit -m "feat(release): publish ledger after healthy apply"
```

### Task 8: Complete-Snapshot Rollback Preparation

**Files:**
- Create: `deploy/ops/prepare-rollback.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/release-ledger.sh`
- Create: `deploy/ops/tests/test-prepare-rollback.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing rollback preparation fixtures**

Seed five records, mark one current, make only three historical records artifact-complete, and request one by exact `release_id`. Assert preparation verifies record/artifact hashes, checks local source object, conditionally pulls only a missing historical digest, renders `$TARGET_RECORD.backup_dir/target/` Compose and `.env`, creates a fresh backup of the current release, and writes a 15-minute manifest.

Assert invalid/current/ineligible release IDs, corrupted target artifacts, current-ledger drift, failed fresh backup, or database dump validation failure settle without container or state changes.

- [ ] **Step 2: Run fixture and verify it fails**

```powershell
bash deploy/ops/tests/test-prepare-rollback.sh
```

Expected: FAIL because `prepare-rollback.sh` does not exist.

- [ ] **Step 3: Implement immutable rollback preparation**

The manifest must contain:

```json
{
  "operation_kind": "rollback",
  "base_release_id": "release-current",
  "target_release_id": "release-historical",
  "base_custom_high_water": 7,
  "target_official_version": "v0.1.162",
  "target_custom_version": "v1.0.3",
  "target_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "main_digest": "sha256:historical-main",
  "extensions_digest": "sha256:historical-extensions",
  "target_artifact_manifest_sha256": "64-char hash",
  "backup_dir": "fresh current backup",
  "backup_manifest_sha256": "64-char hash",
  "prepared_at": "RFC3339",
  "expires_at": "RFC3339"
}
```

Do not wait for Actions. Do not query GitHub. A `docker pull` is permitted only if `docker image inspect <repo>@<digest>` proves the immutable target missing locally. Reuse the same complete backup helper as update prepare so both databases, current config, Nginx/certificates, metadata, and rollback tags are verified.

- [ ] **Step 4: Re-run fixture and verify pass**

Run the Step 2 command. Expected: PASS, including zero lifecycle changes and conditional pull assertions.

- [ ] **Step 5: Commit rollback prepare**

```powershell
git add deploy/ops/prepare-rollback.sh deploy/ops/release-common.sh deploy/ops/release-ledger.sh deploy/ops/tests/test-prepare-rollback.sh deploy/ops/tests/test-script-contract.ps1
git commit -m "feat(release): prepare complete snapshot rollback"
```

### Task 9: Complete-Snapshot Rollback Apply And Reversibility

**Files:**
- Create: `deploy/ops/apply-rollback.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/release-ledger.sh`
- Create: `deploy/ops/tests/test-apply-rollback.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing rollback apply fixtures**

Assert success restores the historical source, target Compose/`.env`, paired digests, and displayed versions while keeping `custom_version_high_water` unchanged. The former current release must become rollback-eligible after success.

Add expiry, current pointer/high-water drift, target-record drift, artifact/backup drift, missing local image, dirty source, extension failure, main failure, health failure, and metadata write failure. Every apply fixture must reject GitHub/network access, pulls, Actions waits, and database backup/restore. On post-mutation failure, assert automatic restoration of the release current at preparation time.

- [ ] **Step 2: Run fixture and verify it fails**

```powershell
bash deploy/ops/tests/test-apply-rollback.sh
```

Expected: FAIL because `apply-rollback.sh` does not exist.

- [ ] **Step 3: Implement local-only rollback apply**

Reuse `release_checkout_exact_commit`, install the historical `target/` Compose pair and private `.env`, pin the historical digest pair, switch extension then main with `--pull never`, and run the same complete health suite as update apply.

On success call `ledger_commit_rollback "$TARGET_RELEASE_ID" "$JOB_ID"`, which writes the target compatibility projection and changes only `current_release_id`; it must preserve the existing high-water. Record an immutable rollback operation event with base and target release IDs.

- [ ] **Step 4: Implement automatic restoration of the pre-rollback release**

After any mutation failure, use the fresh preparation backup and base release record to restore source/config/digests, extension first then main, rerun health, restore the base compatibility projection and current pointer, and settle as `failed_rolled_back`. If restoration health or metadata fails, settle `rollback_failed` with `production_changed:true` and the exact artifact path. Never restore either database automatically.

- [ ] **Step 5: Run fixtures and commit**

```powershell
bash deploy/ops/tests/test-apply-rollback.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
git add deploy/ops/apply-rollback.sh deploy/ops/release-common.sh deploy/ops/release-ledger.sh deploy/ops/tests/test-apply-rollback.sh deploy/ops/tests/test-script-contract.ps1
git commit -m "feat(release): apply reversible complete rollback"
```

### Task 10: Generic Dispatcher, Trigger, And Operation Recovery

**Files:**
- Modify: `deploy/ops/sync-trigger.sh`
- Modify: `deploy/ops/sync-and-publish.sh`
- Modify: `deploy/ops/release-state.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`
- Modify: `deploy/tests/custom-release-workflow-contract.test.mjs`

- [ ] **Step 1: Write failing dispatch and legacy fixtures**

Assert the trigger contains only `<phase> <job-id>`, while the dispatcher reads `operation_kind` from the immutable operation file and maps:

```text
update + prepare   -> prepare-release.sh
update + apply     -> apply-release.sh
rollback + prepare -> prepare-rollback.sh
rollback + apply   -> apply-rollback.sh
```

Assert one release lock is acquired before claim, duplicate triggers are idempotent, terminal operations clear `active_operation_id`, prepared operations retain it, and missing/mismatched kind/action fails before executor invocation. Old `release-jobs` records must settle or report `LEGACY_SINGLE_PHASE_UNSUPPORTED` and never reach an executor.

- [ ] **Step 2: Run contracts and verify they fail**

```powershell
bash deploy/ops/tests/test-release-pipeline.sh
pwsh -File deploy/ops/tests/test-script-contract.ps1
node --test deploy/tests/custom-release-workflow-contract.test.mjs
```

Expected: FAIL on absent rollback dispatch and new operation directory contract.

- [ ] **Step 3: Implement operation-aware dispatch**

Keep the existing systemd path/service and release lock. Validate `update-*` and `rollback-*` IDs, read only `$DATA_DIR/release-ledger/operations/<job-id>.json` for new work, verify trigger phase equals record action, export job ID, then `exec` the exact executor from the mapping above. `publish-custom.sh` remains a fail-closed shim and no dispatcher branch calls it.

- [ ] **Step 4: Re-run all dispatch contracts and verify pass**

Run the Step 2 commands. Expected: PASS; trigger helper returns immediately and no cron/scheduled publisher appears.

- [ ] **Step 5: Commit dispatch changes**

```powershell
git add deploy/ops/sync-trigger.sh deploy/ops/sync-and-publish.sh deploy/ops/release-state.sh deploy/ops/tests/test-release-pipeline.sh deploy/ops/tests/test-script-contract.ps1 deploy/tests/custom-release-workflow-contract.test.mjs
git commit -m "feat(release): dispatch update and rollback operations"
```

### Task 11: Frontend API, Pinia Identity, And Pure State Helpers

**Files:**
- Modify: `frontend/src/features/custom-release/api.ts`
- Create: `frontend/src/features/custom-release/__tests__/api.spec.ts`
- Modify: `frontend/src/features/custom-release/store.ts`
- Create: `frontend/src/features/custom-release/__tests__/store.spec.ts`
- Create: `frontend/src/features/custom-release/releaseOperation.ts`
- Create: `frontend/src/features/custom-release/__tests__/releaseOperation.spec.ts`

- [ ] **Step 1: Write failing TypeScript/Vitest contracts**

Define fixtures with these exact API types:

```ts
export interface ReleaseIdentity {
  release_id: string
  official_version: string
  official_commit: string
  custom_version: string
  custom_version_sequence: number
  custom_commit: string
  published_at: string
}

export type ReleaseOperationKind = 'update' | 'rollback'
export type ReleaseOperationAction = 'prepare' | 'apply'
```

Assert distinct calls and independent idempotency keys for `prepareUpdate()`, `applyUpdate(jobID)`, `prepareRollback(releaseID)`, and `applyRollback(jobID)`. Assert `getCurrentRelease()`, `getRollbackReleases()`, and status without a job ID. Store tests must preserve current official/custom identity through cached reads and clear target identity after successful refresh.

- [ ] **Step 2: Run focused tests and verify they fail**

```powershell
Set-Location frontend
pnpm vitest run src/features/custom-release/__tests__/api.spec.ts src/features/custom-release/__tests__/store.spec.ts src/features/custom-release/__tests__/releaseOperation.spec.ts
```

Expected: FAIL on missing types, endpoints, store refs, and helpers.

- [ ] **Step 3: Implement API and store contracts**

Use these endpoint functions:

```ts
prepareUpdate(): POST /admin/system/update/prepare
applyUpdate(jobID): POST /admin/system/update/apply
prepareRollback(releaseID): POST /admin/system/rollback/prepare
applyRollback(jobID): POST /admin/system/rollback/apply
getCurrentRelease(): GET /admin/system/release
getRollbackReleases(): GET /admin/system/releases/rollback
getUpdateStatus(jobID?): GET /admin/system/update/status
```

Remove the frontend's official binary `rollback(version?)` and long request timeout. Extend Pinia with `currentOfficialVersion`, `currentCustomVersion`, `currentReleaseID`, `targetOfficialVersion`, and `targetCustomVersion`, while keeping `currentVersion` as the compatibility official version without `v`.

Implement pure helpers for terminal/settled states, operation-specific confirm labels, and `remainingSeconds(expiresAt, now)` so timers are deterministic in tests.

- [ ] **Step 4: Re-run focused tests and verify pass**

Run the Step 2 command. Expected: PASS with four distinct idempotent actions.

- [ ] **Step 5: Commit frontend data contracts**

```powershell
git add frontend/src/features/custom-release/api.ts frontend/src/features/custom-release/store.ts frontend/src/features/custom-release/releaseOperation.ts frontend/src/features/custom-release/__tests__/api.spec.ts frontend/src/features/custom-release/__tests__/store.spec.ts frontend/src/features/custom-release/__tests__/releaseOperation.spec.ts
git commit -m "feat(frontend): add dual release operation contracts"
```

### Task 12: Dual-Version Badge And Complete Rollback Panel

**Files:**
- Create: `frontend/src/features/custom-release/ReleaseRollbackPanel.vue`
- Create: `frontend/src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts`
- Modify: `frontend/src/features/custom-release/CustomReleaseBadge.vue`
- Modify: `frontend/src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts`

- [ ] **Step 1: Write failing component tests**

Cover:

- compact badge and popup display `Official v0.1.163` plus `Custom v1.0.4`;
- official/custom/combined target pairs and custom short SHA;
- docs-only notice without prepare action;
- all update and rollback prepare/apply progress states;
- 15-minute countdown, expired/drifted state, and no automatic apply;
- last three snapshots excluding current;
- rollback selection by `release_id`, `准备回退`, then `确认回退` only after prepared;
- duplicate click suppression;
- refresh/re-login recovery from server active operation with no localStorage;
- terminal historical success/failure not replayed when the popup reopens;
- automatic rollback and rollback-failure messages.

Use stable `data-testid` values such as `current-official-version`, `current-custom-version`, `target-version-pair`, `prepare-update`, `confirm-update`, `prepare-rollback`, and `confirm-rollback` instead of locating buttons by translated text.

- [ ] **Step 2: Run focused component tests and verify they fail**

```powershell
Set-Location frontend
pnpm vitest run src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts
```

Expected: FAIL because the popup renders one version and old official rollback behavior.

- [ ] **Step 3: Implement the rollback panel and badge orchestration**

`ReleaseRollbackPanel.vue` receives the current release, eligible records, active operation, loading/error flags, and emits `prepare(releaseID)` / `apply(jobID)`. It displays both versions, short custom commit, publication time, target eligibility, progress, and expiry without manual shell commands.

`CustomReleaseBadge.vue` owns one active operation poller. On mount/open it calls status without an ID first; localStorage is only a fallback pointer. It stops at `prepared`, starts apply only from explicit update/rollback confirmation, refreshes the ledger identity after success, and clears terminal feedback when closed. Keep icon buttons/tooltips and the existing compact visual language.

- [ ] **Step 4: Add exact Chinese/English state strings**

Add component-local Chinese/English labels for current/target official and custom versions, official/custom/combined detection, resolving/verifying snapshots, image download, backup/validation, prepared expiry, environment drift, update/rollback confirmation, switching extension/main, health checks, successful update/rollback, conflict, automatic restoration, and restoration failure. Do not modify official locale dictionaries and do not add explanatory feature-tour copy.

- [ ] **Step 5: Run frontend checks and commit**

```powershell
Set-Location frontend
pnpm vitest run src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts
pnpm run typecheck
git add src/features/custom-release/CustomReleaseBadge.vue src/features/custom-release/ReleaseRollbackPanel.vue src/features/custom-release/__tests__/CustomReleaseBadge.spec.ts src/features/custom-release/__tests__/ReleaseRollbackPanel.spec.ts
git commit -m "feat(frontend): show dual versions and complete rollback"
```

### Task 13: Deployment Contracts And Operator Documentation

**Files:**
- Modify: `deploy/tests/compose-overlay-contract.test.mjs`
- Modify: `deploy/tests/custom-release-workflow-contract.test.mjs`
- Modify: `deploy/ops/tests/test-script-contract.ps1`
- Modify: `deploy/ops/README.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `docs/SUB2API-CUSTOM-OPERATIONS.md`

- [ ] **Step 1: Add failing cross-layer deployment contracts**

Require all four executors, ledger/migration scripts, explicit Compose pair, target artifact pair, digest pinning, `--pull never` in both apply scripts, no lifecycle management of PostgreSQL/Redis/risk database, no implicit override file, and no GitHub/pull/backup commands in either apply script.

Require the docs to name both versions, global high-water behavior, exact baseline, last-three complete snapshots, two-stage rollback, normal no-database-restore policy, legacy API shutdown, and the separate production migration authorization.

- [ ] **Step 2: Run contracts and verify they fail**

```powershell
node --test deploy/tests/*.test.mjs
pwsh -File deploy/ops/tests/test-script-contract.ps1
```

Expected: FAIL on missing ledger/rollback documentation and cross-layer assertions.

- [ ] **Step 3: Update all three operator documents**

Document the exact paths, JSON identities, migration command, update and rollback state machines, 15-minute expiry, version allocation rules, drift refusal, artifact eligibility/retention, recovery/error codes, and report format. Explicitly state:

```text
Official v0.1.163 / Custom v1.0.0 belongs to production commit aa2d24106cab0a03785330d8e0ff4e02b0474a0e.
The first successful custom runtime release after bootstrap becomes v1.0.1.
Rollback restores the historical display pair but never lowers/reuses the high-water number.
Normal rollback never restores either database.
```

Preserve the prohibition on direct `publish-custom.sh`, implicit Compose overrides, `git reset --hard`, force push, Docker cache cleanup, and production actions without administrator authorization.

- [ ] **Step 4: Render the real Compose pair and run contracts**

```powershell
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --quiet
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --format json | Out-Null
node --test deploy/tests/*.test.mjs
pwsh -File deploy/ops/tests/test-script-contract.ps1
```

Expected: all commands exit `0` and no implicit override is loaded.

- [ ] **Step 5: Commit contracts and documentation**

```powershell
git add deploy/tests/compose-overlay-contract.test.mjs deploy/tests/custom-release-workflow-contract.test.mjs deploy/ops/tests/test-script-contract.ps1 deploy/ops/README.md deploy/RELEASE-RUNBOOK.md docs/SUB2API-CUSTOM-OPERATIONS.md
git commit -m "docs(release): document dual-version rollback operations"
```

### Task 14: Full Feature Verification And Independent Review

**Files:**
- Review every file changed since `381ac7db4`.

- [ ] **Step 1: Run complete backend verification**

```powershell
Set-Location backend
make test-unit
make test-integration
```

Expected: both commands exit `0` with no skipped release-ledger unit package caused by build tags.

- [ ] **Step 2: Run complete frontend verification**

```powershell
Set-Location frontend
pnpm run test:run
pnpm run typecheck
pnpm run build
```

Expected: Vitest, `vue-tsc`, and Vite production build all exit `0`.

- [ ] **Step 3: Run extensions and deployment verification**

```powershell
Set-Location extensions-self/account-monitor
go test ./...
Set-Location ../risk-control
go test ./...
Set-Location ../..
node --test deploy/tests/*.test.mjs
pwsh -File deploy/ops/tests/test-script-contract.ps1
bash deploy/ops/tests/test-release-resolver.sh
bash deploy/ops/tests/test-release-pipeline.sh
bash deploy/ops/tests/test-release-ledger.sh
bash deploy/ops/tests/test-prepare-release-ledger.sh
bash deploy/ops/tests/test-apply-release-ledger.sh
bash deploy/ops/tests/test-prepare-rollback.sh
bash deploy/ops/tests/test-apply-rollback.sh
Get-ChildItem deploy/ops -Recurse -Filter *.sh | ForEach-Object { bash -n $_.FullName; if ($LASTEXITCODE -ne 0) { throw "bash syntax failed: $($_.FullName)" } }
```

Expected: every command exits `0`.

- [ ] **Step 4: Verify Compose, diff hygiene, and secrets boundary**

```powershell
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --quiet
git diff --check 381ac7db4...HEAD
git status --short
git diff --name-only 381ac7db4...HEAD
```

Expected: Compose and diff checks pass; only intended source/tests/docs appear; no production `.env`, certificate, key, dump, or credential file is tracked.

- [ ] **Step 5: Request review, fix findings, and create the final feature commit if needed**

Use `superpowers:requesting-code-review` against the approved design and the full `381ac7db4...HEAD` diff. Fix all critical/important findings with failing regression tests first, rerun affected focused suites, then rerun Steps 1-4. Do not squash away the TDD commits unless the user explicitly requests it.

### Task 15: Merge, Exact-Merge Verification, Push, And CI/GHCR Observation

**Files:**
- No source edits unless exact-merge verification exposes a regression.

- [ ] **Step 1: Verify branch/worktree prerequisites**

Confirm the feature worktree is clean. Confirm `E:\Code\sub2api` is clean and `custom-release` has not moved unexpectedly. Fetch normally if necessary; never use reset or force.

- [ ] **Step 2: Merge locally and record the exact merge commit**

```powershell
Set-Location E:\Code\sub2api
git merge --no-ff feature/dual-version-release-ledger-20260723 -m "merge: add dual-version release ledger"
git rev-parse HEAD
```

Expected: one local merge commit and no unresolved conflicts.

- [ ] **Step 3: Re-run all Task 14 verification on the exact merge commit**

Run Task 14 Steps 1-4 from `E:\Code\sub2api`. Any fix must be committed on a feature/fix branch and merged normally; do not edit directly on `custom-release` and leave an ad hoc commit.

- [ ] **Step 4: Push without rewriting history and observe Actions/GHCR**

```powershell
git push origin custom-release
```

Wait for the exact merge SHA's `Custom Release` jobs `backend`, `golangci`, `frontend`, `extensions`, `deployment`, `metadata`, and `images`. Record the workflow URL and verify both public image tags/digests plus OCI revision/version/source for that SHA.

- [ ] **Step 5: Report delivery and production boundary separately**

Report design commit, plan commit, feature commits, merge commit, complete local results, push result, seven Actions jobs, both GHCR digests, and OCI labels. State explicitly that this implementation task does not install VPS scripts, run baseline migration, create a production backup, click prepare/apply/rollback, change production containers, or perform a production rollback.
