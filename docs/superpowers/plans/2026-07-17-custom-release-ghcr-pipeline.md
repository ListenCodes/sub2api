# Custom Release GHCR Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace VPS-local scheduled builds with a durable administrator-triggered pipeline that validates official stable Releases, gates promotion on Actions, publishes two public GHCR images, deploys immutable digests, and automatically restores the previous digest pair after failed health checks.

**Architecture:** US-RN-66 owns the persistent release state machine, guarded branch promotion, backup, deployment, and rollback. GitHub Actions has read-only source access plus package write access and produces both images from one tested commit. The application creates a durable job and returns immediately; a `systemd.path` unit wakes the host orchestrator.

**Tech Stack:** Go, Vue 3/TypeScript/Vitest, Bash, PowerShell contract tests, Node `node:test`, GitHub Actions, Docker Buildx/GHCR, Docker Compose, systemd, PostgreSQL backup tools.

---

## File Map

**Create**

- `.github/workflows/custom-release.yml`: full custom-release validation and paired GHCR publication.
- `deploy/tests/custom-release-workflow-contract.test.mjs`: workflow, Dockerfile, Compose, and package metadata contracts.
- `deploy/ops/release-state.sh`: atomic release job/state helpers shared by host scripts.
- `deploy/ops/wait-for-actions.sh`: public GitHub check-run polling for one candidate SHA.
- `deploy/ops/verify-release-images.sh`: public registry/image metadata and digest verification.
- `deploy/ops/sub2api-release.path`: host trigger watcher.
- `deploy/ops/sub2api-release.service`: one-shot host orchestrator.
- `deploy/ops/tests/release-pipeline-fixture.sh`: fake Git/GitHub/GHCR/Docker executable test harness.
- `deploy/ops/tests/test-release-pipeline.sh`: three scenarios, base race, image rejection, and rollback tests.
- `docs/superpowers/specs/2026-07-17-custom-release-ghcr-pipeline-design.md`: approved architecture.
- `docs/superpowers/plans/2026-07-17-custom-release-ghcr-pipeline.md`: this execution plan.

**Modify**

- `Dockerfile`: OCI source, revision, and version labels.
- `extensions-self/Dockerfile`: matching OCI labels and build arguments.
- `deploy/docker-compose.yml`: `SUB2API_IMAGE` and `EXTENSIONS_SELF_IMAGE` substitutions; remove build-only production assumptions.
- `deploy/.env.example`: document image variables without production secrets.
- `deploy/ops/resolve-stable-release.sh`: require annotated tag object type and expose immutable API identity.
- `deploy/ops/sync-upstream.sh`: use durable states, preserve candidate evidence, and stop before promotion.
- `deploy/ops/sync-and-publish.sh`: orchestrate all three scenarios, Actions/image waits, guarded promotion, and digest publication.
- `deploy/ops/sync-trigger.sh`: enqueue and return immediately.
- `deploy/ops/publish-custom.sh`: pull/verify digests, backup, staged deployment, full health, rollback, and release-state write.
- `deploy/ops/tests/test-release-resolver.sh`: tag type and malformed API fixtures.
- `deploy/ops/tests/test-script-contract.ps1`: stable branch, no scheduled path, systemd, digest, and rollback invariants.
- `deploy/tests/account-monitor-contract.test.mjs`: preserve monitor backup/readiness order under digest publication.
- `deploy/tests/extensions-self-layout.test.mjs`: variable images and staged service recreation.
- `deploy/tests/risk-control-alias.test.mjs`: digest rollback naming and health invariants.
- `backend/internal/service/update_job.go`: durable status enum/schema and current-job lookup.
- `backend/internal/service/update_service.go`: always enqueue administrator release jobs without synchronous Release gating.
- `backend/internal/service/update_job_service_test.go`: state validation, current job, restart, and immediate-return tests.
- `backend/internal/handler/admin/system_handler.go`: current/latest status query and complete job response.
- `backend/internal/handler/admin/system_handler_test.go`: accepted job and optional job ID tests.
- `frontend/src/api/admin/system.ts`: state union and full release-job payload.
- `frontend/src/api/admin/__tests__/system.spec.ts`: terminal/non-terminal helpers and current status requests.
- `frontend/src/components/common/VersionBadge.vue`: state progress, refresh recovery, long-running polling, rollback/conflict display.
- `frontend/src/components/common/__tests__/VersionBadge.spec.ts`: state transitions and refresh recovery.
- `frontend/src/i18n/locales/en/common.ts`, `frontend/src/i18n/locales/zh/common.ts`: release state messages.
- `AGENTS.md`, `deploy/RELEASE-RUNBOOK.md`, `deploy/README.md`, `deploy/ops/README.md`: final branch, CI, GHCR, systemd, backup, rollback, and reporting contract.
- `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`: production operations contract after repository implementation is committed.

**Delete**

- `deploy/ops/auto-update.sh`: scheduled updater.

---

### Task 1: Define the Actions and image contract

**Files:**
- Create: `deploy/tests/custom-release-workflow-contract.test.mjs`
- Create: `.github/workflows/custom-release.yml`
- Modify: `Dockerfile`
- Modify: `extensions-self/Dockerfile`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/.env.example`

- [ ] **Step 1: Write the failing workflow/image contract test**

Create a Node test that parses workflow YAML as text and asserts the exact production contract:

```js
test('custom-release workflow gates paired images on the full validation suite', () => {
  assert.match(workflow, /branches:[\s\S]*custom-release[\s\S]*integration\/release-/)
  assert.match(workflow, /permissions:[\s\S]*contents:\s*read[\s\S]*packages:\s*write/)
  for (const marker of [
    'make test-unit', 'make test-integration', 'golangci/golangci-lint-action',
    'pnpm run typecheck', 'pnpm run test:run', 'pnpm run build',
    'extensions-self/account-monitor', 'extensions-self/risk-control',
    'node --test deploy/tests/*.test.mjs', 'bash -n', 'docker/build-push-action'
  ]) assert.match(workflow, new RegExp(escapeRegExp(marker)))
  assert.match(workflow, /ghcr\.io\/listencodes\/sub2api-custom:custom-\$\{\{ github\.sha \}\}/)
  assert.match(workflow, /ghcr\.io\/listencodes\/sub2api-extensions:custom-\$\{\{ github\.sha \}\}/)
})
```

Also assert both Dockerfiles expose `IMAGE_REVISION`, `IMAGE_VERSION`, and the fork source label, and Compose uses `${SUB2API_IMAGE:?...}` and `${EXTENSIONS_SELF_IMAGE:?...}`.

- [ ] **Step 2: Run the contract test and verify RED**

Run:

```powershell
node --test deploy/tests/custom-release-workflow-contract.test.mjs
```

Expected: FAIL because the workflow and digest image variables do not exist.

- [ ] **Step 3: Add the minimum workflow and image metadata implementation**

Use one workflow with independent validation jobs and a paired image job:

```yaml
permissions:
  contents: read
  packages: write

jobs:
  images:
    needs: [backend, golangci_lint, frontend, extensions, deployment]
    if: github.event_name == 'push'
```

Compute `IMAGE_VERSION` from `deploy/stable-release-baseline.json`, pass the full `${{ github.sha }}` as `IMAGE_REVISION`, log in with `${{ secrets.GITHUB_TOKEN }}`, and use two `docker/build-push-action` steps. Build both images with `platforms: linux/amd64`, `push: true`, and the full-SHA tags.

Define Compose images exactly as:

```yaml
sub2api:
  image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}
extensions-self:
  image: ${EXTENSIONS_SELF_IMAGE:?EXTENSIONS_SELF_IMAGE is required}
```

- [ ] **Step 4: Run contract and deployment tests GREEN**

Run:

```powershell
node --test deploy/tests/custom-release-workflow-contract.test.mjs deploy/tests/*.test.mjs
```

Expected: all Node deployment tests pass.

- [ ] **Step 5: Commit the image contract**

```powershell
git add .github/workflows/custom-release.yml Dockerfile extensions-self/Dockerfile deploy/docker-compose.yml deploy/.env.example deploy/tests/custom-release-workflow-contract.test.mjs
git commit -m "ci: publish paired custom release images"
```

---

### Task 2: Make update jobs durable and asynchronous

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/service/update_job_service_test.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `backend/internal/handler/admin/system_handler_test.go`

- [ ] **Step 1: Write failing service and handler tests**

Add table tests for every valid state:

```go
var validUpdateStatuses = []string{
  UpdateStatusCheckingRelease, UpdateStatusValidatingTag, UpdateStatusMergingRelease,
  UpdateStatusWaitingActions, UpdateStatusWaitingImages, UpdateStatusPromotingRelease,
  UpdateStatusBackingUp, UpdateStatusDeployingExtensions, UpdateStatusDeployingMain,
  UpdateStatusHealthChecking, UpdateStatusRollingBack,
  UpdateStatusSuccess, UpdateStatusFailed, UpdateStatusConflict,
}
```

Tests must prove that `GetUpdateStatus(ctx, "")` reads `release-current-job-id`, a specific job reads `release-jobs/<job-id>.json`, an unknown job returns not found, all writes are atomic, and `PerformUpdate` returns without calling `CheckUpdate` or waiting for the trigger helper.

Change the handler test from `RequiresJobID` to `ReturnsCurrentJobWithoutJobID`.

- [ ] **Step 2: Run focused Go tests and verify RED**

Run from `backend`:

```bash
go test -tags=unit ./internal/service ./internal/handler/admin -run 'Update(Job|Status)|SystemHandler(PerformUpdate|GetUpdateStatus)' -count=1
```

Expected: FAIL on missing states, paths, and optional status lookup.

- [ ] **Step 3: Implement the durable job schema**

Use these default paths:

```go
const (
  defaultReleaseJobsDir = "/app/data/release-jobs"
  defaultCurrentReleaseJobPath = "/app/data/release-current-job-id"
  defaultUpdateScriptPath = "/app/scripts/sync-upstream.sh"
)
```

Extend `UpdateJob` with `UpdatedAt`, `TargetCommit`, `WorkflowURL`, both digests,
`ProductionChanged`, `ErrorCode`, `ArtifactPath`, and a nested rollback object.
Implement `IsTerminalUpdateStatus` and a single allowlist validator. Keep atomic
temporary-file-and-rename writes.

`PerformUpdate` must only reject a non-terminal existing job, allocate/write a
new job in `checking_release`, write the current ID, start the trigger helper,
and return. The host, not the binary version check, owns no-change detection.

- [ ] **Step 4: Run focused tests GREEN**

Run the same command. Expected: focused service and handler tests pass.

- [ ] **Step 5: Commit the backend state contract**

```powershell
git add backend/internal/service/update_job.go backend/internal/service/update_service.go backend/internal/service/update_job_service_test.go backend/internal/handler/admin/system_handler.go backend/internal/handler/admin/system_handler_test.go
git commit -m "feat(update): persist release job state"
```

---

### Task 3: Resume release progress in the administrator UI

**Files:**
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/api/admin/__tests__/system.spec.ts`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`
- Modify: `frontend/src/i18n/locales/zh/common.ts`

- [ ] **Step 1: Write failing API and component tests**

Add API tests for the full state union and optional `getUpdateStatus(jobID?)`.
Use fake timers in the component tests to prove:

```text
checking_release -> waiting_actions -> waiting_images -> health_checking -> success
```

remains in progress, `conflict` is terminal, `rolling_back -> failed` shows the
rollback result, and mounting the component calls current status and resumes a
persisted job. Advance fake time past 15 minutes and verify polling is still
active; use a 90-minute deadline or a server terminal state.

- [ ] **Step 2: Run Vitest and verify RED**

Run:

```powershell
pnpm --dir frontend exec vitest run src/api/admin/__tests__/system.spec.ts src/components/common/__tests__/VersionBadge.spec.ts
```

Expected: FAIL on missing state types, optional query, and refresh recovery.

- [ ] **Step 3: Implement state rendering and resume behavior**

Export:

```ts
export type UpdateJobStatus =
  | 'checking_release' | 'validating_tag' | 'merging_release'
  | 'waiting_actions' | 'waiting_images' | 'promoting_release'
  | 'backing_up' | 'deploying_extensions' | 'deploying_main'
  | 'health_checking' | 'rolling_back' | 'success' | 'failed' | 'conflict'
```

Persist only the opaque job ID in `localStorage`, clear it at a terminal state,
query current status on mount when no stored ID exists, and render server
messages plus translated state labels. Do not introduce background polling when
no non-terminal release job exists.

- [ ] **Step 4: Run focused frontend tests GREEN**

Run the same Vitest command. Expected: all focused tests pass.

- [ ] **Step 5: Commit the frontend state UX**

```powershell
git add frontend/src/api/admin/system.ts frontend/src/api/admin/__tests__/system.spec.ts frontend/src/components/common/VersionBadge.vue frontend/src/components/common/__tests__/VersionBadge.spec.ts frontend/src/i18n/locales/en/common.ts frontend/src/i18n/locales/zh/common.ts
git commit -m "feat(frontend): resume release job progress"
```

---

### Task 4: Implement the Release and Actions orchestrator

**Files:**
- Create: `deploy/ops/release-state.sh`
- Create: `deploy/ops/wait-for-actions.sh`
- Create: `deploy/ops/tests/release-pipeline-fixture.sh`
- Create: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/resolve-stable-release.sh`
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/sync-and-publish.sh`
- Modify: `deploy/ops/sync-trigger.sh`
- Modify: `deploy/ops/tests/test-release-resolver.sh`

- [ ] **Step 1: Extend resolver fixtures and verify RED**

Add ref fixtures with `object.type=commit`, missing `published_at`, duplicate
output fields, and a mismatched annotated tag SHA. Require the resolver to emit:

```text
release_tag=vX.Y.Z
release_published_at=RFC3339
release_tag_object_sha=<40 hex>
release_tag_object_type=tag
```

Run `bash deploy/ops/tests/test-release-resolver.sh`. Expected: FAIL because tag
object type is not validated or emitted.

- [ ] **Step 2: Write fake executable pipeline tests and verify RED**

The harness must isolate `REPO`, `DATA_DIR`, remotes, worktrees, and executable
dependencies. Cover:

1. no Release and no custom commit -> `success`, no Docker/Compose call;
2. undeployed custom commit -> wait checks/images, no merge, publish digests;
3. new Release -> exact annotated tag, integration push, Actions success,
   image verification, base recheck, ff-only promotion, publish;
4. draft/prerelease/tag mismatch/base race -> `failed`, production unchanged;
5. merge conflict -> `conflict` with files and artifact path;
6. trigger helper returns immediately and never loops or sleeps.

Run `bash deploy/ops/tests/test-release-pipeline.sh`. Expected: FAIL on missing
state helper, Actions waiter, and current synchronous promotion.

- [ ] **Step 3: Implement atomic state helpers**

`release-state.sh` must expose `release_job_path`, `read_current_job_id`, and
`write_release_job`. Every update preserves the complete JSON schema and sets
`updated_at`; only terminal states set `finished_at`.

- [ ] **Step 4: Implement exact candidate gating**

Refactor `sync-upstream.sh` to stop after pushing the integration branch and to
write `conflict` rather than generic `failed`. Refactor `sync-and-publish.sh` to:

```text
checking_release -> validating_tag -> [merge or current custom target]
-> waiting_actions -> waiting_images -> promoting_release -> publisher
```

Poll the public GitHub checks API at a rate that stays within anonymous limits.
Require all custom-release workflow checks to complete successfully. Re-fetch
and compare `origin/custom-release` immediately before an ff-only push.

- [ ] **Step 5: Run resolver and orchestrator tests GREEN**

Run:

```bash
bash deploy/ops/tests/test-release-resolver.sh
bash deploy/ops/tests/test-release-pipeline.sh
```

Expected: all fixture scenarios pass and no test calls a real remote or Docker.

- [ ] **Step 6: Commit the orchestrator**

```powershell
git add deploy/ops/release-state.sh deploy/ops/wait-for-actions.sh deploy/ops/resolve-stable-release.sh deploy/ops/sync-upstream.sh deploy/ops/sync-and-publish.sh deploy/ops/sync-trigger.sh deploy/ops/tests/test-release-resolver.sh deploy/ops/tests/release-pipeline-fixture.sh deploy/ops/tests/test-release-pipeline.sh
git commit -m "feat(ops): gate releases on actions and images"
```

---

### Task 5: Publish immutable digests and auto-restore failures

**Files:**
- Create: `deploy/ops/verify-release-images.sh`
- Modify: `deploy/ops/publish-custom.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`
- Modify: `deploy/tests/extensions-self-layout.test.mjs`
- Modify: `deploy/tests/risk-control-alias.test.mjs`

- [ ] **Step 1: Write failing digest and rollback tests**

Use fake Docker responses to test wrong revision, wrong version, wrong source,
missing `linux/amd64`, registry/local digest mismatch, missing previous digest,
extension deployment failure, main health failure, successful automatic
rollback, and rollback failure evidence.

Assert the publisher interface is:

```text
publish-custom.sh --commit <sha> --main-digest sha256:<hex> --extensions-digest sha256:<hex>
```

and that no `docker build`, `compose build`, or database lifecycle command is
present.

- [ ] **Step 2: Run deployment tests and verify RED**

Run:

```powershell
node --test deploy/tests/*.test.mjs
pwsh -File deploy/ops/tests/test-script-contract.ps1
```

and `bash deploy/ops/tests/test-release-pipeline.sh` in Bash. Expected: FAIL on
mutable tags, local builds, combined recreation, and missing rollback.

- [ ] **Step 3: Implement image verification before backup**

The verifier must anonymously resolve and pull:

```text
ghcr.io/listencodes/sub2api-custom:custom-<full SHA>
ghcr.io/listencodes/sub2api-extensions:custom-<full SHA>
```

It returns canonical `name@sha256:...` references only after manifest platform,
OCI labels, and local `RepoDigest` all match.

- [ ] **Step 4: Refactor the publisher around a mutation boundary**

Before mutation, validate branch, commit, Release tag/commit, both images,
Compose, current digests, and both database backups. Record old digests and all
required artifacts. Atomically write the two image variables, then:

```text
deploying_extensions -> verify extension
deploying_main -> health_checking -> write release-state.json -> success
```

Any failure after image-variable mutation enters `rolling_back`, restores the
backed-up environment/configuration, recreates extensions then main, runs health
checks, and records rollback success/failure. Never restore databases here.

- [ ] **Step 5: Run all deployment tests GREEN**

Run the commands from Step 2. Expected: all pass.

- [ ] **Step 6: Commit digest publication**

```powershell
git add deploy/ops/verify-release-images.sh deploy/ops/publish-custom.sh deploy/ops/tests/test-release-pipeline.sh deploy/ops/tests/test-script-contract.ps1 deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs
git commit -m "feat(ops): publish and restore release digests"
```

---

### Task 6: Replace cron with systemd and remove scheduled code

**Files:**
- Create: `deploy/ops/sub2api-release.path`
- Create: `deploy/ops/sub2api-release.service`
- Delete: `deploy/ops/auto-update.sh`
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/sync-and-publish.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write failing lifecycle contract assertions**

Assert the path unit watches the host data volume trigger, the service calls only
`/opt/sub2api-custom/sync-and-publish.sh`, `auto-update.sh` is absent, and no
operations source contains `--scheduled`, `scheduled-`, or `prepare_scheduled_status`.

- [ ] **Step 2: Run the script contract and verify RED**

Run `pwsh -File deploy/ops/tests/test-script-contract.ps1`. Expected: FAIL while
scheduled files and code remain.

- [ ] **Step 3: Add systemd units and delete scheduled behavior**

Use:

```ini
[Path]
PathExists=/var/lib/docker/volumes/deploy_sub2api_data/_data/release-trigger
Unit=sub2api-release.service
```

The service is `Type=oneshot`, sets `DATA_DIR`, runs the unified wrapper, and is
ordered after Docker/network. Remove all scheduled arguments and status branches.

- [ ] **Step 4: Run contract and shell syntax tests GREEN**

Run the PowerShell contract and `bash -n deploy/ops/*.sh`. Expected: pass.

- [ ] **Step 5: Commit lifecycle migration**

```powershell
git add -A deploy/ops
git commit -m "ops: trigger releases with systemd path"
```

---

### Task 7: Replace the release documentation contract

**Files:**
- Modify: `AGENTS.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/README.md`
- Modify: `deploy/ops/README.md`
- Modify: `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`

- [ ] **Step 1: Write failing documentation assertions**

Extend the Node/PowerShell contracts to require `custom-release`, both GHCR
names, digest pinning, administrator-only triggering, systemd units, and
automatic paired rollback. Forbid daily cron, per-minute trigger cron,
`auto-update.sh`, VPS-local builds, and publication from `custom`.

- [ ] **Step 2: Run documentation contracts and verify RED**

Run Node deployment tests and the PowerShell script contract. Expected: FAIL on
old cron/local-build language.

- [ ] **Step 3: Update repository documentation**

Document this only production path:

```text
feature -> custom-release -> Actions + public paired GHCR images
-> admin button -> systemd state machine -> digest deploy
```

Document selective `cherry-pick -x` to `custom`, exact backup/health/rollback
evidence, no database restore by default, and separate implementation/push/
deployment reporting.

- [ ] **Step 4: Update the external VPS operating contract**

After the repository implementation is committed, apply the same authoritative
paths and invariants to `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`. This external
file is not included in the repository commit and must be reported separately.

- [ ] **Step 5: Run documentation contracts GREEN and commit repository docs**

```powershell
git add AGENTS.md deploy/RELEASE-RUNBOOK.md deploy/README.md deploy/ops/README.md
git commit -m "docs: define digest release operations"
```

---

### Task 8: Complete local verification and independent review

**Files:** all implementation files above.

- [ ] **Step 1: Install dependencies without changing lockfiles**

```powershell
pnpm --dir frontend install --frozen-lockfile
```

Run `go mod download` in `backend`, `extensions-self/account-monitor`, and
`extensions-self/risk-control` where a Go runtime is available.

- [ ] **Step 2: Run the complete local suites**

```text
backend: make test-unit; make test-integration; golangci-lint run --timeout=30m
frontend: pnpm run typecheck; pnpm run test:run; pnpm run build
extensions: go test ./... in both modules
deployment: node --test deploy/tests/*.test.mjs
contracts: pwsh test-script-contract.ps1; Bash resolver/pipeline tests
shell: bash -n deploy/ops/*.sh
images: build both Dockerfiles when Docker is available
repository: git diff --check
```

Record unavailable local runtimes separately; the matching Actions jobs must be
green before branch promotion.

- [ ] **Step 3: Review requirements and request independent code review**

Compare every numbered objective requirement against a file/test/evidence item.
Dispatch a read-only reviewer with base `b079ceec3bd883f4847e3861cd34e6233cbcf190`
and the implementation head. Fix every Critical or Important finding and rerun
affected plus full tests.

- [ ] **Step 4: Commit any verification fixes**

Use focused commit messages and leave the feature worktree clean.

---

### Task 9: Merge, push, validate GHCR, and publish production

**Files:** Git refs, GitHub Actions/GHCR state, and US-RN-66 runtime state.

- [ ] **Step 1: Merge the feature into local `custom-release`**

Fetch `origin/custom-release`, confirm it has not moved unexpectedly, merge the
feature without rewriting history, run final checks, and push
`origin/custom-release`. Report the feature commit and push result separately.

- [ ] **Step 2: Wait for Actions and record both image digests**

Verify every required job is green. Record the two full-SHA tags, manifest
digests, `linux/amd64`, and all three OCI labels. Set each GHCR package to Public
using a local administrative GitHub API credential; verify anonymous manifests
and pulls. Never copy that credential to US-RN-66.

- [ ] **Step 3: Inspect production through ssh-skill**

Use only `ssh_execute.py US-RN-66`. Confirm branch/commit/dirty state, containers,
current images, database identities, crontab, existing scripts, disk space, and
health before mutation.

- [ ] **Step 4: Install versioned scripts and systemd units**

Use ssh-skill upload/execute helpers, preserve existing files in the release
backup, install scripts under `/opt/sub2api-custom`, install/enable the path unit,
remove only the obsolete update cron lines, and keep health-monitor scheduling.

- [ ] **Step 5: Trigger the administrator release path**

Trigger the same persistent job path used by the button. Do not call the
publisher directly for the final acceptance. Monitor states through success.
Do not intentionally force a rollback failure or health failure.

- [ ] **Step 6: Verify production evidence**

Record backup directory, `release-state.json`, production commit, two digests,
running container RepoDigests/labels, database container IDs, internal health,
public HTTPS, native monitor pages, signed data-quality, source allow/deny
probes, systemd path/service status, and absence of update cron/auto-update.

- [ ] **Step 7: Report the required facts separately**

Report implementation commit, local tests, `origin/custom-release` push,
Actions and GHCR digests, production backup, deployed commit, container/public
health, scheduled update removal, and rollback material completeness. State
explicitly that no rollback drill and no Docker cache cleanup were performed.
