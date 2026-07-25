# Release Status Contract Design

## Problem

The versioned host scripts still persist legacy release statuses such as
`waiting_actions`, while the backend validates only the canonical release
status vocabulary. A valid in-progress operation is therefore rejected as
`RELEASE_OPERATION_INCONSISTENT` when the admin API reads it.

## Scope

This change standardizes every status written or accepted by `deploy/ops/` on
the canonical values declared in `backend/internal/service/update_job.go`.
Backend parsing is unchanged. Frontend support for legacy values remains so an
old response or cached fixture can still be rendered.

The current production operation is not modified in place. It was created by
the old scripts and may retain its legacy status until that operation reaches a
terminal state. The corrected contract applies after the versioned scripts are
installed by a later administrator-triggered release.

## Status Mapping

| Legacy script status | Canonical status |
|---|---|
| `checking_updates` | `resolving_target` |
| `checking_release` | `resolving_target` |
| `validating_tag` | `resolving_target` |
| `merging_release` | `resolving_target` |
| `waiting_actions` | `resolving_target` |
| `promoting_release` | `resolving_target` |
| `waiting_images` | `verifying_images` |
| `preparing_compose` | `rendering_compose` |
| `deploying_extensions` | `switching_extensions` |
| `deploying_main` | `switching_main` |

Messages and metadata continue to carry the detailed phase, such as checking a
Release tag or waiting for Actions. The status field expresses the stable API
state rather than every shell sub-step.

## Implementation

1. Initialize new jobs with `resolving_target`.
2. Replace every legacy literal passed to `release_job_update` with its
   canonical equivalent.
3. Restrict `release_valid_status` to the canonical backend vocabulary so a
   future script cannot persist another legacy or private state.
4. Add a Node contract test that compares script validation/emission against
   the backend constants and rejects all known legacy status literals in
   `deploy/ops/`.
5. Document the canonical-state rule in the release runbook.

## Verification

- Run the new release-status contract test and the existing deployment contract
  suite.
- Run shell syntax checks for every changed script.
- Run focused backend tests to confirm the API status contract remains intact.
- Review `git diff --check` and the final branch diff before merging.

## Release Boundary

Committing and pushing this change does not deploy it. Production installation
continues to require the administrator update action, successful Actions and
paired immutable images, backup, apply, and health gates.

## Production Follow-up

The first rollout after this design exposed a second host-script contract gap:
the new homepage and account inventory code depended on additions to
`extensions_self_ro`, but normal apply did not reapply the versioned source-view
SQL. Production was restored by applying `main_source_views.sql` in one
transaction after confirming the fresh release backup.

The durable fix refreshes these additive read-only views after the target source
checkout and backup validation, but before switching `extensions-self`. A SQL
failure settles the release through `SOURCE_VIEWS_FAILED`; the existing homepage
and data-quality health checks remain the post-switch verification gates.
