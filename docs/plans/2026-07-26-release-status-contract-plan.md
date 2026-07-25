# Canonical Release Status Implementation Plan

**Goal:** Make every versioned host release script persist only backend-approved
canonical release states and prevent future contract drift.

**Architecture:** The Go backend constants remain the source of truth. Shell
scripts use that vocabulary for initialization, validation, and updates. A Node
contract test statically compares the shell contract with the Go constants and
rejects known legacy literals.

## Task 1: Add a failing cross-language contract test

**Files:**

- Create: `deploy/tests/release-status-contract.test.mjs`
- Reference: `backend/internal/service/update_job.go`
- Reference: `deploy/ops/release-state.sh`

1. Parse canonical `ReleaseStatus*` string constants from the Go source.
2. Parse the shell `release_valid_status` case list and require exact equality.
3. Scan `deploy/ops/*.sh` and reject every known legacy status literal.
4. Extract literal `release_job_update` status arguments and require membership
   in the backend canonical set.
5. Run the test and confirm it fails on the current legacy script states.

## Task 2: Standardize all versioned script states

**Files:**

- Modify: `deploy/ops/release-state.sh`
- Modify: `deploy/ops/prepare-release.sh`
- Modify: `deploy/ops/sync-upstream.sh`

1. Change the initial job state from `checking_updates` to
   `resolving_target`.
2. Remove legacy values from `release_valid_status` and retain the complete
   canonical set.
3. Replace detailed legacy update states with canonical states while preserving
   phase-specific messages and metadata.
4. Run the new test and confirm it passes.

## Task 3: Document the contract

**Files:**

- Modify: `deploy/RELEASE-RUNBOOK.md`

1. Add the canonical state list and identify Go constants as the contract.
2. Explain that detailed shell phases belong in message/metadata, not new
   status values.
3. Explain that existing operations are not rewritten in place.

## Task 4: Verify and publish the code change

1. Run `node --test deploy/tests/*.test.mjs`.
2. Run `bash -n` over `deploy/ops/*.sh` in a Bash-capable environment.
3. Run the focused Go service tests if the local Go toolchain is available.
4. Run `git diff --check`, inspect the complete diff, and commit the fix.
5. Merge the feature branch into `custom-release` without rewriting history.
6. Push `origin/custom-release` and verify the local and remote commit IDs.
7. Report Actions/image and production deployment as separate states; do not
   trigger deployment.
