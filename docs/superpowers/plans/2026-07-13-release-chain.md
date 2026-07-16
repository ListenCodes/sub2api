# Release Chain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate upstream integration from production publishing and make the admin update action safe.

**Architecture:** Version host scripts in `deploy/ops`. The sync script uses a temporary worktree and only publishes an integration branch. The publish script validates an approved `origin/custom` commit, backs up production, builds, deploys, and verifies health. The existing async API reports preparation status without requesting a restart.

**Tech Stack:** Bash, Git worktrees, Docker Compose, Go, Vue 3, TypeScript, Vitest, Go test.

---

### Task 1: Add script contract tests and versioned operations scripts

**Files:**
- Create: `deploy/ops/sync-upstream.sh`
- Create: `deploy/ops/publish-custom.sh`
- Create: `deploy/ops/README.md`
- Test: `deploy/ops/tests/test-script-contract.ps1`

- [ ] **Step 1: Write the failing contract test**

Assert that the sync script contains no `git rebase`, `docker build`,
`docker compose up`, `git push origin custom`, or force-push operation, and
that the publish script requires `origin/custom`, creates a backup directory,
and uses an explicit Compose project name.

- [ ] **Step 2: Run the contract test and verify it fails**

Run `pwsh -File deploy/ops/tests/test-script-contract.ps1`.
Expected: FAIL because the scripts do not exist.

- [ ] **Step 3: Implement the sync script**

Implement lock acquisition, clean-tree validation, upstream/origin fetch,
temporary worktree merge, conflict-file reporting, integration branch commit,
and non-force push to `origin/integration/upstream-*`. Write the existing
`sync-status` and `sync-result` files without changing `custom`.

- [ ] **Step 4: Implement the publish script**

Require a clean `/root/sub2api` tree and an approved commit reachable from
`origin/custom`. Back up PostgreSQL, `.env`, Compose, Nginx, and current image
metadata. Build `sub2api:custom`, recreate only selected services with
`--project-name deploy`, and verify health and binary version.

- [ ] **Step 5: Run the contract test and shell syntax checks**

Run `pwsh -File deploy/ops/tests/test-script-contract.ps1`,
`bash -n deploy/ops/sync-upstream.sh`, and
`bash -n deploy/ops/publish-custom.sh`.
Expected: PASS with zero failures.

- [ ] **Step 6: Commit the scripts**

Run `git add deploy/ops && git commit -m "ops: separate upstream sync from production publish"`.

### Task 2: Make the asynchronous update status preparation-only

**Files:**
- Modify: `backend/internal/service/update_job.go`
- Modify: `backend/internal/service/update_service.go`
- Modify: `backend/internal/handler/admin/system_handler.go`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/api/admin/system.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Test: `backend/internal/service/update_job_service_test.go`
- Test: `frontend/src/components/common/__tests__/VersionBadge.spec.ts`

- [ ] **Step 1: Add failing status assertions**

Assert that update jobs expose `need_restart: false` for preparation jobs and
that the component renders a preparation message without a restart button.

- [ ] **Step 2: Run focused tests and verify failure**

Run `go test ./internal/service -run UpdateJob -count=1` from `backend` and
`pnpm vitest run frontend/src/components/common/__tests__/VersionBadge.spec.ts`.
Expected: FAIL because the status has no preparation contract.

- [ ] **Step 3: Implement the status contract**

Add a `NeedRestart` field to `UpdateJob`, return `false` from
`PerformUpdate`, and make the UI use the status field instead of assuming
every successful update needs a restart. Add Chinese and English copy for
integration-branch preparation.

- [ ] **Step 4: Run focused tests**

Run the same Go and frontend tests. Expected: PASS.

- [ ] **Step 5: Commit the UI/API change**

Run `git add backend frontend && git commit -m "fix: make upstream update preparation-only"`.

### Task 3: Document and install the new production workflow

**Files:**
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `AGENTS.md`
- Modify: `deploy/README.md`
- Modify: `deploy/docker-compose.yml`

- [ ] **Step 1: Document exact local, fork, VPS, and rollback paths**

State that `upstream/main` is read-only input, `origin/custom` is the approved
release source, and the VPS only fast-forwards to that commit.

- [ ] **Step 2: Mount only the versioned sync wrapper**

Keep the container mount path stable at `/app/scripts/sync-upstream.sh`, but
document the installation of `deploy/ops/sync-upstream.sh` to the host path.

- [ ] **Step 3: Validate Compose and documentation references**

Run the YAML parser, `git diff --check`, and a repository search confirming no
official workflow instructs a direct `rebase upstream/main` or force-push.

- [ ] **Step 4: Commit documentation**

Run `git add AGENTS.md deploy && git commit -m "docs: define approved release workflow"`.

### Task 4: Merge the current upstream version locally

**Files:**
- Modify: upstream-conflicted files selected by Git
- Test: backend and frontend focused suites plus Compose validation

- [ ] **Step 1: Fetch latest upstream and origin**

Run `git fetch upstream` and `git fetch origin` in the isolated worktree.

- [ ] **Step 2: Merge upstream into an integration branch**

Run `git merge upstream/main` and preserve the v2 risk-control integration,
the `risk-control-v2` alias, and the new release scripts while accepting
unrelated upstream improvements.

- [ ] **Step 3: Verify the expected version**

Read `backend/cmd/server/VERSION` and confirm it is the latest upstream
version, currently expected to be `0.1.152` or higher.

- [ ] **Step 4: Run focused backend, frontend, risk-control, and Compose checks**

Run `go test ./...` in `backend`, `pnpm test -- --run` in `frontend` using the
repository's supported command, `go test ./...` in `risk-control`, and parse
both production Compose files.

- [ ] **Step 5: Merge the validated integration branch into custom**

Fast-forward local `custom`, inspect `git diff upstream/main...custom`, and
push `custom` to `origin` without force.

### Task 5: Install, publish, and verify production

**Files:**
- Remote: `/opt/sub2api-custom/sync-upstream.sh`
- Remote: `/opt/sub2api-custom/publish-custom.sh`
- Remote: `/opt/sub2api-custom/auto-update.sh`

- [ ] **Step 1: Back up the current production state**

Create a new `/root/backups/sub2api/<timestamp>/` containing the database
dump, `.env`, Compose, Nginx, cron, scripts, and current image metadata.

- [ ] **Step 2: Install the versioned scripts**

Copy the approved scripts to `/opt/sub2api-custom`, mark them executable, and
replace the old auto-update behavior with a check-only wrapper.

- [ ] **Step 3: Fast-forward VPS source to approved origin/custom**

Run `git fetch origin custom` and `git merge --ff-only origin/custom`; do not
run `git fetch upstream`, `git rebase`, or `git reset --hard`.

- [ ] **Step 4: Build and publish the approved commit**

Run the versioned publish script with the approved commit and explicit
`--project-name deploy`; recreate only the main/v2 services affected.

- [ ] **Step 5: Verify production**

Check binary version, container health, v2 DNS/health, risk event rows, public
`/health`, and the absence of new upstream-sync errors.

- [ ] **Step 6: Record the release**

Record the commit, image IDs, backup directory, version, and rollback tags in
the final report.
