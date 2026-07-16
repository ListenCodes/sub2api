# Automatic Upstream Publish Chain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the admin upstream trigger and daily job automatically publish a conflict-free upstream integration while stopping safely for conflicts or release failures.

**Architecture:** Keep `sync-upstream.sh` as the temporary-worktree integration component. Add `sync-and-publish.sh` as the single host entrypoint for the container trigger and scheduled job; it owns an end-to-end lock, promotes only an unchanged-base integration branch to `origin/custom`, and invokes the existing backup/build/health gate in `publish-custom.sh`. Extend the status contract so the UI distinguishes prepared, published, and failed outcomes.

**Tech Stack:** Bash, Git worktrees, Docker Compose, Go, Vue 3, TypeScript, Vitest, PowerShell contract tests.

---

### Task 1: Update operational rules and documentation

**Files:**
- Modify: `AGENTS.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `deploy/README.md`
- Modify: `deploy/ops/README.md`
- Modify: `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`

- [x] State that clean upstream integrations may be promoted and published automatically by the unified wrapper.
- [x] State that conflicts, base drift, dirty worktrees, failed backups, failed builds, and failed health checks stop without publishing.
- [x] Document `sync-and-publish.sh` as the entrypoint for both the admin trigger and daily schedule.
- [x] Keep `publish-custom.sh` as the only image-build/deploy implementation.

### Task 2: Add the unified host flow

**Files:**
- Create: `deploy/ops/sync-and-publish.sh`
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/auto-update.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`

- [x] Add a failing shell-contract assertion that both scheduled and trigger paths call `sync-and-publish.sh`.
- [x] Add a deferred-result mode so the trigger result is written only after automatic publication finishes.
- [x] Record the integration base commit and branch in sync status.
- [x] Under one end-to-end lock, run integration, verify the base is unchanged, fast-forward `custom`, push `origin/custom`, and call `publish-custom.sh --commit <HEAD>`.
- [x] On conflict or any publish failure, preserve the integration branch and write a terminal failure result; a later run may retry only the exact pending approved commit.
- [x] Run the contract test and `bash -n` before committing.

### Task 3: Extend update status and UI

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Test: `backend/internal/service/update_job_service_test.go`
- Test: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`

- [x] Add optional `published` and `published_commit` fields to the update job contract.
- [x] Make successful published jobs render “published” without a restart button; preparation-only status remains supported for direct script checks.
- [x] Keep admin-only access, idempotency, polling, and failure messages.
- [x] Run focused Go and frontend tests before full verification.

### Task 4: Install, test, and publish

**Files:**
- Remote: `/opt/sub2api-custom/sync-and-publish.sh`
- Remote: `/opt/sub2api-custom/sync-upstream.sh`
- Remote: `/opt/sub2api-custom/auto-update.sh`
- Remote: crontab

- [x] Back up the current production state before replacing scripts or cron.
- [x] Install the unified wrapper root-owned and executable.
- [x] Change both the daily cron and trigger-processing cron to call the wrapper.
- [x] Run local frontend, backend-focused, risk-control, shell, and Compose checks.
- [x] Push the approved `custom` commit and invoke the publish script with its exact SHA.
- [x] Verify source equality, container health, `risk-control-v2`, database/Redis, public `/health`, risk counts, backup, rollback tags, and sync status.

### Task 5: Report unresolved upstream conflicts

**Files:**
- Create: `docs/superpowers/specs/2026-07-13-conflict-reporting-design.md`
- Modify: `deploy/ops/sync-upstream.sh`
- Modify: `deploy/ops/sync-trigger.sh`
- Modify: `deploy/ops/tests/test-script-contract.ps1`
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `backend/internal/service/update_job_service_test.go`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Test: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`

- [x] Add a failing shell assertion for conflict metadata and saved artifacts.
- [x] Add failing backend and frontend tests for conflict status fields.
- [x] Capture conflict files, commits, and a diagnostic snapshot before aborting.
- [x] Expose conflict metadata through the admin update API.
- [x] Render actionable conflict details in the failure panel.
- [x] Run focused tests, full frontend checks, and script syntax checks.
