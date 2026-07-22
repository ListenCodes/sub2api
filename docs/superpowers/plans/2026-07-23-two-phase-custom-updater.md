# Two-Phase Custom Updater Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a durable prepare/apply Sub2API updater that detects official and custom changes independently and cannot mutate production before explicit confirmation.

**Architecture:** Extend the existing update service with a read-only unified detector and two administrator commands. Retain one systemd path/service, but make its host dispatcher route immutable jobs to separate prepare and apply executors backed by a read-only prepared manifest.

**Tech Stack:** Go/Gin, Vue 3/TypeScript/Pinia/Vitest, Bash/jq/Docker Compose, Node contract tests, PowerShell contract tests.

---

### Task 1: Durable Job And API Contracts

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_job_service_test.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/service/update_service_test.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `backend/internal/handler/admin/system_handler_test.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/api/admin/__tests__/system.spec.ts`

- [ ] Add failing Go and TypeScript tests for the new statuses, `action`, prepared manifest metadata, separate prepare/apply calls, legacy `/update` alias, duplicate commands, and apply-only-from-prepared validation.
- [ ] Run focused tests and confirm failures are caused by missing two-stage symbols or routes:

```powershell
go test -tags=unit ./internal/service ./internal/handler/admin
pnpm --dir frontend vitest run src/api/admin/__tests__/system.spec.ts
```

- [ ] Add `PrepareUpdate`, `ApplyUpdate`, and expanded `UpdateJob` types. Make `prepared` a polling-settled state, preserve the same job ID for apply, and emit different idempotency/audit operation names.
- [ ] Keep `PerformUpdate` as a prepare-only compatibility wrapper and reject legacy deployment-phase jobs with `LEGACY_SINGLE_PHASE_UNSUPPORTED`.
- [ ] Re-run the focused tests and commit the API/state contract.

### Task 2: Unified Read-Only Detection

**Files:**
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/service/update_service_test.go`
- Modify: `backend/internal/repository/github_release_service.go`
- Modify: `backend/internal/repository/github_release_service_test.go`
- Modify: `backend/internal/repository/update_cache.go`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/stores/app.ts`
- Modify: `frontend/src/api/admin/__tests__/system.spec.ts`

- [ ] Add failing matrix tests for `none`, `official`, `custom`, `combined`, and `docs-only`, including current Stable with a different custom SHA and a partial-source warning that cannot report `none`.
- [ ] Add GitHub client tests for exact `custom-release` ref resolution and compare-file pagination; authorization must remain restricted to exact `https://api.github.com` requests.
- [ ] Implement `UpdateKind`, release-state parsing, custom ref lookup, compare-scope classification, target short SHA, and runtime/docs-only flags. Cache only Stable data; always reload production and custom facts.
- [ ] Extend the Pinia store without removing the existing version/build fields used elsewhere.
- [ ] Run service, repository, store, and API tests and commit unified detection.

### Task 3: Host State And Trigger Contract

**Files:**
- Modify: `deploy/ops/release-state.sh`
- Modify: `deploy/ops/sync-trigger.sh`
- Modify: `deploy/ops/sync-and-publish.sh`
- Create: `deploy/ops/release-common.sh`
- Create: `deploy/ops/prepare-release.sh`
- Create: `deploy/ops/apply-release.sh`
- Modify: `deploy/ops/publish-custom.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] Add failing shell/PowerShell assertions for action-bearing triggers, every new status, prepared manifest fields, dispatcher separation, and absence of `publish-custom.sh` from all executable entry paths.
- [ ] Add failing legacy fixtures proving an old single-stage job never reaches Compose lifecycle commands.
- [ ] Implement atomic action/job triggers, expanded validation, polling-settled/terminal semantics, and dispatch under the existing release lock.
- [ ] Make `publish-custom.sh` a fail-closed compatibility shim after the separate executors own the active path.
- [ ] Run both contract suites and commit host state/dispatch behavior.

### Task 4: Preparation Executor And Immutable Manifest

**Files:**
- Modify: `deploy/ops/prepare-release.sh`
- Modify: `deploy/ops/release-common.sh`
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Create: `deploy/ops/tests/test-prepare-release.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] Build fake Git, GitHub, Docker, Compose, pg_dump, and pg_restore fixtures that fail if prepare invokes `compose up/down/rm`, moves the production checkout, or writes `release-state.json`.
- [ ] Add failing scenarios for official, custom, combined, docs-only, Actions failure, image failure, backup failure, and a same-target expired retry that reuses image evidence but creates a new backup.
- [ ] Implement target locking, seven-job verification, paired OCI/digest verification, guarded official promotion, digest pull, temporary target worktree, staged explicit Compose pair, backup, SHA256 verification, and an immutable 15-minute manifest.
- [ ] Assert the manifest includes production/target/Stable identities, both digests, current and staged Compose/`.env` hashes, backup identity, timestamps, and reusable image evidence.
- [ ] Run preparation fixtures plus shell syntax checks and commit prepare behavior.

### Task 5: Apply Executor, Drift Gates, And Rollback

**Files:**
- Modify: `deploy/ops/apply-release.sh`
- Modify: `deploy/ops/release-common.sh`
- Create: `deploy/ops/tests/test-apply-release.sh`
- Modify: `deploy/ops/tests/test-release-pipeline.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] Add fixtures that fail if apply invokes GitHub/curl, `git fetch`, Actions/image verification, `docker pull`, pg_dump, pg_restore, or any Compose command without `--pull never`.
- [ ] Add failing expiration, production-commit drift, origin-head drift, Compose drift, `.env` drift, digest drift, manifest corruption, duplicate apply, extensions failure, main failure, health failure, and rollback failure cases.
- [ ] Implement manifest/backup verification, local source fast-forward, staged configuration installation, extensions-first/main-second `--pull never` deployment, complete health checks, and post-health atomic production-state write.
- [ ] Implement source/config/image rollback without `git reset --hard`, database restore, or lifecycle management of PostgreSQL, Redis, or `risk-control-postgres`.
- [ ] Run all apply fixtures and commit apply/rollback behavior.

### Task 6: VersionBadge Two-Stage UI

**Files:**
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/stores/app.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`

- [ ] Add failing component tests for all five detection kinds, custom short SHA, docs-only no-action behavior, preparation stages, prepared countdown, explicit confirmation, no automatic apply, expiry, drift, conflict, rollback, duplicate clicks, and server-current-job recovery without localStorage.
- [ ] Add failing API tests proving prepare and apply use separate endpoints and distinct Idempotency-Key requests.
- [ ] Implement the two buttons and state presentation while retaining the existing compact VersionBadge visual style and rollback panel.
- [ ] Make server current-job lookup authoritative on mount and localStorage a fallback; stop polling at prepared and restart only after explicit apply.
- [ ] Run focused Vitest and typecheck, then commit frontend behavior.

### Task 7: Compose And Deployment Contracts

**Files:**
- Modify: `deploy/docker-compose.custom.yml`
- Modify: `deploy/tests/compose-overlay-contract.test.mjs`
- Modify: `deploy/tests/custom-release-workflow-contract.test.mjs`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [ ] Add failing assertions for the action-aware trigger mount, explicit Compose pair, immutable digest variables, prepared target rendering, and absence of implicit override discovery.
- [ ] Implement only the mount/config changes needed by the two backend actions.
- [ ] Render the real pair with the fixture environment:

```powershell
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --quiet
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --format json
```

- [ ] Run deployment Node and PowerShell contracts and commit Compose wiring.

### Task 8: Operator Documentation

**Files:**
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `docs/SUB2API-CUSTOM-OPERATIONS.md`
- Modify: `deploy/ops/README.md`

- [ ] Replace the single-stage description with detection, prepare, manifest expiry/reuse, explicit apply, drift refusal, and rollback evidence.
- [ ] Document the deprecated `/update` alias and legacy job fail-closed behavior.
- [ ] State that pushing, preparing, applying, production backup, production health, and rollback are separately reported facts.
- [ ] Run documentation/deployment contract tests and commit the docs.

### Task 9: Feature Verification And Review

**Files:**
- Review all changed files.

- [ ] Run backend `go test ./...` with the repository's required build tags/environment.
- [ ] Run the complete frontend Vitest suite, typecheck, and production build.
- [ ] Run both `extensions-self` Go suites, all deployment Node contracts, the PowerShell contract, release resolver, prepare/apply shell fixtures, real Compose rendering, and shell syntax checks.
- [ ] Run `git diff --check`, inspect the complete diff for secrets/unrelated changes, and verify the worktree contains no production `.env`.
- [ ] Request an independent code review against the approved design; fix every critical or important finding and rerun affected tests.
- [ ] Create the feature commit only after fresh verification passes.

### Task 10: Local Merge, Exact-Commit Verification, And Push

**Files:**
- No new source files; operate on Git history only.

- [ ] Confirm the main `custom-release` worktree is clean and still based on the expected production commit or reconcile non-destructively if it advanced.
- [ ] Merge the feature branch into local `custom-release` without force, record the exact merge commit, and rerun the complete Task 9 verification on that commit.
- [ ] Push the exact merge commit to `origin/custom-release` without force.
- [ ] Wait for `backend`, `golangci`, `frontend`, `extensions`, `deployment`, `metadata`, and `images` on that SHA and record the workflow URL.
- [ ] Resolve and record both public GHCR immutable digests and OCI identities for the exact merge SHA.
- [ ] Report explicitly that no VPS scripts were installed, no production backup was created by this task, no update button was clicked, production remains at its prior commit, and no rollback was executed.
