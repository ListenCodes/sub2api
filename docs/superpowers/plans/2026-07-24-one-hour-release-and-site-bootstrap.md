# One-Hour Release And Site Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make prepared update and rollback confirmations valid for one hour, clear the PostCSS security gate, and add safe Docker commands for exporting, migrating, or freshly deploying the custom site.

**Architecture:** Keep all runtime changes inside additive `deploy/ops` scripts and contract tests. Reuse the existing immutable-image verifier, explicit Compose pair, release ledger helpers, backup contract, and systemd watcher; do not change the five Stable-owned hotspot files or production automatically.

**Tech Stack:** Bash, Docker Compose, Git, jq, PostgreSQL dump/restore, Node.js test runner, pnpm, GitHub Actions.

---

### Task 1: One-hour prepared-operation lifetime

**Files:**
- Create: `deploy/tests/release-prepared-expiry.test.mjs`
- Modify: `deploy/ops/prepare-release.sh`
- Modify: `deploy/ops/prepare-rollback.sh`

- [ ] **Step 1: Write the failing contract test**

Create a Node test that reads both prepare scripts and asserts each computes
`expires_at` using `+60 minutes`; also assert neither file contains
`+15 minutes`.

- [ ] **Step 2: Verify RED**

Run: `node --test deploy/tests/release-prepared-expiry.test.mjs`

Expected: FAIL because both scripts still use 15 minutes.

- [ ] **Step 3: Implement the minimal lifetime change**

Change only these expressions:

```bash
expires_at="$(date -u -d '+60 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d "$prepared_at +60 minutes" '+%Y-%m-%dT%H:%M:%SZ')"
```

- [ ] **Step 4: Verify GREEN and shell syntax**

Run:

```bash
node --test deploy/tests/release-prepared-expiry.test.mjs
bash -n deploy/ops/prepare-release.sh deploy/ops/prepare-rollback.sh
```

Expected: all checks pass.

- [ ] **Step 5: Commit**

```bash
git add deploy/tests/release-prepared-expiry.test.mjs deploy/ops/prepare-release.sh deploy/ops/prepare-rollback.sh
git commit -m "feat(release): extend prepared confirmation to one hour"
```

### Task 2: PostCSS security advisory

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/pnpm-lock.yaml`

- [ ] **Step 1: Preserve the failing security reproduction**

Run the workflow-equivalent audit and checker:

```bash
cd frontend
pnpm audit --prod --audit-level=high --json > audit.json || true
python ../tools/check_pnpm_audit_exceptions.py --audit audit.json --exceptions ../.github/audit-exceptions.yml
```

Expected: FAIL for `postcss` / `GHSA-6g55-p6wh-862q` with locked version
`8.5.6`.

- [ ] **Step 2: Add the minimal transitive floor**

Add this entry to the existing `pnpm.overrides` object:

```json
"postcss@<8.5.12": ">=8.5.12"
```

Run `pnpm install --lockfile-only` to update only dependency metadata.

- [ ] **Step 3: Verify GREEN**

Run the same audit/checker and `pnpm why postcss --prod`.

Expected: the exception checker prints `Audit exceptions validated.` and all
resolved PostCSS copies are `8.5.12` or later. Do not add an audit exception.

- [ ] **Step 4: Commit**

```bash
git add frontend/package.json frontend/pnpm-lock.yaml
git commit -m "fix(frontend): upgrade vulnerable postcss"
```

### Task 3: Bootstrap and migration contracts

**Files:**
- Create: `deploy/tests/site-bootstrap-contract.test.mjs`
- Create: `deploy/ops/export-custom-site.sh`
- Create: `deploy/ops/bootstrap-custom-site.sh`

- [ ] **Step 1: Write failing static contracts**

The Node test must require the scripts to contain:

```text
fresh | migrate
FRESH-EMPTY-SITE | RESTORE-MIGRATION
custom-release and origin/custom-release identity checks
ghcr.io/...@sha256 digest references
verify-release-images.sh
docker compose --project-name deploy -f base -f custom --env-file
postgres -> redis -> risk-control-postgres -> extensions-self -> sub2api order
release-ledger and release-state validation
install -m 0755 deploy/ops scripts
systemctl enable --now sub2api-release.path
SHA256SUMS plus pg_restore --list for both dumps
```

It must reject broad operations by asserting neither script contains
`docker compose down`, `docker system prune`, `docker volume prune`,
`git reset --hard`, mutable application tags, or direct SSH commands.

- [ ] **Step 2: Verify RED**

Run: `node --test deploy/tests/site-bootstrap-contract.test.mjs`

Expected: FAIL because both scripts are absent.

### Task 4: Complete migration bundle exporter

**Files:**
- Modify: `deploy/ops/export-custom-site.sh`
- Modify: `deploy/tests/site-bootstrap-contract.test.mjs`

- [ ] **Step 1: Implement fail-closed argument and source validation**

Support exactly:

```bash
export-custom-site.sh --output /absolute/empty/directory --confirm EXPORT-SITE
```

Require root, a clean exact `custom-release` checkout matching
`origin/custom-release`, a valid idle ledger, healthy paired containers, an
absolute empty output directory outside the repository, readable Compose/env/
Nginx/certificate files, and digest-pinned current state.

- [ ] **Step 2: Export current state**

Under the output directory, copy the ledger, release-state projection, complete
release-backups tree, Compose pair, `.env`, Nginx and certificates, source and
image metadata. Create fresh custom-format dumps of `sub2api` and
`risk_control`, validate both with `pg_restore --list`, then generate one exact
`SHA256SUMS` covering every regular file except the manifest itself.

- [ ] **Step 3: Verify script contracts and syntax**

Run:

```bash
node --test deploy/tests/site-bootstrap-contract.test.mjs
bash -n deploy/ops/export-custom-site.sh
```

Expected: exporter assertions pass; bootstrap assertions remain RED until Task 5.

- [ ] **Step 4: Commit**

```bash
git add deploy/ops/export-custom-site.sh deploy/tests/site-bootstrap-contract.test.mjs
git commit -m "feat(deploy): export complete custom site bundles"
```

### Task 5: Docker fresh and migrate bootstrap

**Files:**
- Modify: `deploy/ops/bootstrap-custom-site.sh`
- Modify: `deploy/tests/site-bootstrap-contract.test.mjs`

- [ ] **Step 1: Implement common preflight**

Support exactly:

```bash
bootstrap-custom-site.sh fresh --env-file /absolute/site.env --confirm FRESH-EMPTY-SITE
bootstrap-custom-site.sh migrate --bundle /absolute/bundle --confirm RESTORE-MIGRATION
```

Require Linux root; Docker Compose, Git, jq, curl, sha256sum and flock; a clean
exact `custom-release` checkout; no existing `deploy` containers or named
volumes; no active ledger; and an `amd64` host. Refuse relative paths, symlinks,
world/group-readable secret files, unknown flags, or the wrong confirmation.

- [ ] **Step 2: Implement immutable image and Compose preparation**

For `fresh`, derive Stable identity from `deploy/stable-release-baseline.json`,
call `verify-release-images.sh` for the exact HEAD, write only the returned
digest references to a staged mode-0600 `.env`, and render the explicit base
then custom Compose pair. For `migrate`, verify the bundle's exact
`SHA256SUMS`, ledger, release-state, dumps, Compose pair, environment and image
metadata before copying anything.

- [ ] **Step 3: Implement safe service order and data initialization**

Create only the exact `deploy` resources for the new site. Start `postgres`,
then `redis`, then `risk-control-postgres`. In `migrate`, restore both dumps
while application writers are absent. Start and health-check `extensions-self`
before starting and health-checking `sub2api`. Never recreate an existing
database container or volume.

- [ ] **Step 4: Implement ledger/bootstrap finalization**

For `fresh`, create a complete baseline snapshot under the standard artifact
root and atomically initialize Official from the verified Stable file, Custom
`v1.0.0`, high-water `0`, source kind `bootstrap`, exact commit, dual digests,
Compose/env hashes, and backup manifest hash. For `migrate`, restore the copied
ledger and projection only after all paths resolve under the new host's standard
artifact root and all referenced release artifacts validate.

- [ ] **Step 5: Install host operations and report**

Install versioned scripts to `/opt/sub2api-custom`, install both systemd units,
run `systemctl daemon-reload`, enable the path watcher, and print exact commit,
Stable/custom versions, digests, ledger path, backup/bundle path, container
health, and watcher status. Do not modify DNS.

- [ ] **Step 6: Verify GREEN and syntax**

Run:

```bash
node --test deploy/tests/site-bootstrap-contract.test.mjs
bash -n deploy/ops/bootstrap-custom-site.sh
```

Expected: all contracts and syntax checks pass.

- [ ] **Step 7: Commit**

```bash
git add deploy/ops/bootstrap-custom-site.sh deploy/tests/site-bootstrap-contract.test.mjs
git commit -m "feat(deploy): add safe Docker site bootstrap"
```

### Task 6: Fixture-driven fail-closed tests

**Files:**
- Create: `deploy/tests/site-bootstrap-test.sh`
- Create: `deploy/tests/fixtures/bootstrap/bin/docker`
- Create: `deploy/tests/fixtures/bootstrap/bin/systemctl`
- Create: `deploy/tests/fixtures/bootstrap/bin/curl`

- [ ] **Step 1: Write failing fixture scenarios**

Use a temporary repository/data root and command shims. Assert both modes stop
before mutation for wrong confirmation, dirty checkout, non-digest images,
nonempty target resources, checksum drift, corrupt dump listing, active ledger,
and backup paths outside the artifact root. Assert a valid `--check-only` fresh
or migrate run records the exact expected ordered commands without executing
Docker mutations.

- [ ] **Step 2: Verify RED**

Run: `bash deploy/tests/site-bootstrap-test.sh`

Expected: FAIL until the bootstrap script exposes fixture-safe `--check-only`
and environment overrides.

- [ ] **Step 3: Add minimal testability hooks**

Add only environment overrides already used by release scripts
(`SUB2API_REPO`, `SUB2API_DATA_DIR`, `SUB2API_ENV_FILE`, Compose paths, helper
paths) plus `--check-only`. Check-only performs every read/identity/render/hash
validation and prints the mutation plan, but never starts services, restores a
database, writes the ledger, or installs systemd units.

- [ ] **Step 4: Verify GREEN**

Run fixture tests, both Node contract tests, and shell syntax.

- [ ] **Step 5: Commit**

```bash
git add deploy/ops/bootstrap-custom-site.sh deploy/tests/site-bootstrap-test.sh deploy/tests/fixtures/bootstrap
git commit -m "test(deploy): cover fresh and migration bootstrap gates"
```

### Task 7: Operator documentation

**Files:**
- Modify: `deploy/README.md`
- Modify: `deploy/ops/README.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `docs/SUB2API-CUSTOM-OPERATIONS.md`

- [ ] **Step 1: Update one-hour wording**

Replace only release update/rollback prepared-operation references from 15
minutes to one hour. Leave unrelated account-monitor 15-minute buckets and
email-code expiry untouched.

- [ ] **Step 2: Document exact commands**

Add the export, `fresh`, and `migrate` commands; required 0600 input file;
empty-target requirement; exact digest/ledger semantics; DNS/TLS boundary;
failure cleanup boundary; and later left-corner prepare/confirm update flow.

- [ ] **Step 3: Extend the expiry contract test to documentation**

Assert the four release documents contain the one-hour rule and no longer
describe prepared update/rollback as 15 minutes.

- [ ] **Step 4: Run tests and commit**

```bash
node --test deploy/tests/release-prepared-expiry.test.mjs deploy/tests/site-bootstrap-contract.test.mjs
git add deploy/README.md deploy/ops/README.md deploy/RELEASE-RUNBOOK.md docs/SUB2API-CUSTOM-OPERATIONS.md deploy/tests/release-prepared-expiry.test.mjs
git commit -m "docs: document one-hour and Docker bootstrap workflows"
```

### Task 8: Full verification and review

**Files:** All changed files.

- [ ] **Step 1: Run deployment and security gates**

```bash
node --test deploy/tests/*.test.mjs
bash deploy/tests/site-bootstrap-test.sh
bash -n deploy/ops/*.sh deploy/tests/*.sh
cd frontend
pnpm install --frozen-lockfile
pnpm audit --prod --audit-level=high --json > audit.json || true
python ../tools/check_pnpm_audit_exceptions.py --audit audit.json --exceptions ../.github/audit-exceptions.yml
pnpm typecheck
pnpm test:run
pnpm build
```

- [ ] **Step 2: Run repository regression gates**

Run:

```bash
(cd backend && make test-unit && make test-integration)
(cd extensions-self/account-monitor && go test ./...)
(cd extensions-self/risk-control && go test ./...)
pwsh -File deploy/ops/tests/test-script-contract.ps1
bash deploy/ops/tests/test-release-resolver.sh
bash deploy/ops/tests/test-release-pipeline.sh
docker compose --project-name deploy -f deploy/docker-compose.yml -f deploy/docker-compose.custom.yml --env-file deploy/tests/fixtures/compose.env config --quiet
node --test deploy/tests/custom-release-isolation.test.mjs
git diff --check
git diff --stat origin/custom-release...HEAD
git diff origin/custom-release...HEAD
```

Confirm these files have no diff against the pinned Stable commit:

```bash
git diff d0bdd7e771636a8d315f542cafd39484f39bd60c -- \
  backend/internal/service/update_service.go \
  backend/internal/handler/admin/system_handler.go \
  frontend/src/components/common/VersionBadge.vue \
  frontend/src/api/admin/system.ts \
  frontend/src/stores/app.ts
```

- [ ] **Step 3: Use requesting-code-review and verification-before-completion**

Perform an independent review of the full feature diff, fix only evidenced
issues using TDD, rerun affected and complete gates, and record fresh outputs.

- [ ] **Step 4: Commit any verification-only corrections**

Use a focused commit message; leave the feature worktree clean.

### Task 9: Merge, push, and Actions verification

- [ ] **Step 1: Use finishing-a-development-branch**

Confirm the main `custom-release` worktree is clean and still at the expected
base. Merge the feature normally without force or reset.

- [ ] **Step 2: Verify the exact merge commit**

Rerun the complete validation set on the precise merge SHA.

- [ ] **Step 3: Push without force**

Push `origin/custom-release`, then wait for both `Custom Release` and
`Security Scan` on that exact SHA. Require all seven Custom Release jobs,
backend-security, and frontend-security to succeed, and verify both public GHCR
digests plus OCI revision/version/source labels.

- [ ] **Step 4: Stop before production mutation**

Report implementation commits, merge SHA, tests, push, Actions and GHCR. Do not
click prepare/apply, install the bootstrap script on a VPS, restore a database,
or change production without a separate explicit deployment instruction.
