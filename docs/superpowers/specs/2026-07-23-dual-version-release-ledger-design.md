# Dual-Version Release Ledger And Complete Rollback Design

## Goal

Add a production-owned release ledger that displays an official version and a
custom version while preserving one indivisible deployment unit. Updates still
resolve the latest official Stable Release and latest approved
`origin/custom-release` commit. Preparation, backup, apply, health validation,
automatic rollback, and administrator-selected rollback always operate on the
complete source, image, Compose, and environment snapshot.

The initial production identity is:

```text
Official v0.1.163
Custom v1.0.0
Production commit aa2d24106cab0a03785330d8e0ff4e02b0474a0e
```

This design extends the existing two-phase updater. It does not create separate
official and custom deployment controls, does not introduce Git tags as the
custom-version authority, and does not make database restoration part of normal
rollback.

## Confirmed Product Rules

- The UI displays official and custom versions together.
- Update detection still selects the latest eligible official Stable Release
  and latest `origin/custom-release` runtime commit.
- An official-only update changes the official version and keeps the custom
  version unchanged.
- A custom runtime update increments the custom patch version.
- A combined official and custom runtime update changes the official version
  and increments the custom patch version once.
- A documentation-only difference may be reported but neither creates a
  release nor consumes a custom version.
- Failed, expired, drifted, cancelled, or unconfirmed jobs do not consume a
  custom version.
- Custom versions use a global monotonic high-water mark. Rollback never lowers
  it and a previously used version is never reused.
- Rollback restores the historical displayed official/custom pair, but the next
  custom runtime publication uses the next global patch number.
- The rollback selector shows the latest three successful complete release
  snapshots, excluding the current release.
- Rollback is two-stage: prepare rollback, then explicit administrator
  confirmation.
- Rollback preparation creates a fresh backup of the current release, making a
  failed rollback automatically recoverable.
- Normal rollback does not restore either database.
- Opening the version/update popup does not replay a previous operation's
  success or failure. Only current detection and an active or prepared operation
  are restored.

## Chosen Architecture

The production persistent data volume owns a release ledger:

```text
/app/data/release-ledger/
  state.json
  releases/<release-id>.json
  operations/<job-id>.json
```

On the host this resolves under the existing `deploy_sub2api_data` volume. The
ledger is authoritative for version identity and complete rollback snapshots.
`release-state.json` remains a compatibility projection for existing scripts,
diagnostics, and APIs.

This was selected over two alternatives:

1. A version file committed to Git would make official-only merges and rollback
   history depend on source-history conventions, and could reuse numbers after
   branch movement.
2. Git tags would require a new remote mutation and tag governance path, while
   still failing to bind production-only Compose, environment, digest, and
   backup evidence.

The production ledger directly records what actually passed production health
checks and preserves the separation between source history and deployment
history.

## Upstream Conflict Isolation

Custom release behavior must live in additive files so later Stable Release
merges do not repeatedly conflict in official update and sidebar code. The
implementation first moves the already deployed two-phase custom updater out of
the following upstream-owned hot files, then restores those files to the exact
current Stable baseline wherever no unrelated custom behavior remains:

- `backend/internal/service/update_service.go`;
- `backend/internal/handler/admin/system_handler.go`;
- `frontend/src/components/common/VersionBadge.vue`;
- `frontend/src/api/admin/system.ts`;
- `frontend/src/stores/app.ts`.

New release behavior belongs in focused additive modules such as
`custom_release_service.go`, `custom_release_handler.go`, a custom release API
and Pinia store, and `features/custom-release/CustomReleaseBadge.vue`. Host
release scripts and their fixtures are already custom-owned and remain the
production mutation boundary.

The unavoidable official-code touchpoints are deliberately small:

1. the existing system route table redirects the legacy `/update` route to the
   custom prepare-only handler and makes the two old binary rollback routes fail
   closed;
2. the sidebar imports the custom release badge in place of the official badge;
3. the existing custom-extension registration call remains the single backend
   route integration point.

No ledger schema, state machine, GitHub detection, backup, rollback, or UI state
logic may be added to those touchpoints. Deployment contracts compare the five
restored hot files with the pinned Stable commit and reject renewed custom logic
there. This isolation is subordinate only to security: a small route-table
change is preferable to leaving the old single-binary update or rollback path
capable of bypassing the complete-snapshot workflow.

## Ledger State

`state.json` is the mutable pointer and global counter. Its logical schema is:

```json
{
  "schema_version": 1,
  "current_release_id": "release-...",
  "custom_version_high_water": 0,
  "active_operation_id": null,
  "updated_at": "RFC3339 timestamp"
}
```

`custom_version_high_water: 0` corresponds to `v1.0.0`. A custom runtime
candidate is rendered as `v1.0.<high-water + 1>`. The counter advances only in
the same locked commit that makes a newly published release current.

Only one release operation may be active or prepared at a time. The existing
release lock protects operation claiming, version proposal validation, release
record publication, and current-pointer changes.

## Immutable Release Record

Every successful runtime publication creates one immutable release record. A
successful administrator rollback changes the current pointer to an existing
record and writes an operation event; it does not mutate or duplicate the
historical release record.

Each release record binds at least:

```json
{
  "schema_version": 1,
  "release_id": "release-...",
  "official_version": "v0.1.163",
  "official_commit": "full SHA",
  "custom_version": "v1.0.0",
  "custom_version_sequence": 0,
  "custom_commit": "full SHA",
  "main_digest": "sha256:...",
  "extensions_digest": "sha256:...",
  "base_compose_sha256": "...",
  "custom_compose_sha256": "...",
  "rendered_compose_sha256": "...",
  "env_sha256": "...",
  "backup_dir": "/root/backups/sub2api/...",
  "backup_manifest_sha256": "...",
  "published_at": "RFC3339 timestamp",
  "source_kind": "official|custom|combined|bootstrap",
  "operation_id": "job id"
}
```

The record contains hashes and paths, never environment values or credentials.
Its referenced backup contains the matching explicit Compose pair, `.env`,
Nginx/certificates, old digest pair, rollback tags, container/image metadata,
and verified database dumps according to the existing backup contract.

The release ID is an opaque unique deployment identity, not a version string.
The official and custom versions are display identities; the release ID
distinguishes publications even if a rollback later returns to an older pair.

## Compatibility Projection

After a successful publication or rollback, `release-state.json` continues to
contain its current fields and adds:

```json
{
  "release_id": "release-...",
  "official_version": "v0.1.163",
  "custom_version": "v1.0.0",
  "custom_version_sequence": 0
}
```

The ledger is authoritative when both exist. A mismatch is drift and blocks a
new prepare or apply; code must not silently reconstruct one from the other
after initial migration.

## Baseline Migration

Migration is explicit, idempotent, and fail-closed. Before the first
ledger-aware runtime release is applied, the host migration step records the
currently healthy production release at commit
`aa2d24106cab0a03785330d8e0ff4e02b0474a0e` as:

```text
Official v0.1.163
Custom v1.0.0
custom_version_sequence = 0
```

The migration verifies the existing `release-state.json`, clean production
commit, running container digest pair, Stable tag/commit, explicit Compose pair,
`.env` hash, and matching backup evidence. It refuses to guess missing or
contradictory identity. It does not recreate containers or restore databases.

Rollout must seed this baseline before applying the first runtime commit that
contains the ledger feature. That first custom runtime publication therefore
becomes `Custom v1.0.1`, rather than relabelling the new commit as `v1.0.0`.
Installing or executing the migration on production requires a separate,
explicitly authorized deployment task; repository implementation alone does
not perform it.

## Version Allocation

Update preparation reads the ledger under the release lock and writes the
following immutable proposal into the prepared manifest:

- `base_release_id` and current high-water value;
- target official version/commit;
- target custom commit;
- update kind;
- proposed official and custom display versions;
- whether successful apply must advance the custom high-water value.

For `custom` and `combined` runtime updates, the proposed custom sequence is
`high-water + 1`. For `official`, it equals the current release's custom
sequence. `none` and `docs-only` cannot create a runtime prepared manifest.

Apply rechecks the current release ID and high-water value. Any change rejects
the manifest as drifted and requires a new preparation. The proposed number is
committed only after all health checks pass. This gives the administrator a
stable preview without consuming a number on failure.

A rollback never changes the high-water value. If production rolls back from
`v1.0.4` to a release displaying `v1.0.2`, the next successful custom runtime
publication is `v1.0.5`.

## Operation Records

`operations/<job-id>.json` is the durable audit/status record for both update
and rollback. It contains an operation kind, requested target, phase, status,
timestamps, idempotency identity, prepared-manifest identity, base and target
release IDs, error code, production-changed flag, and automatic rollback result.

An operation may be one of:

```text
kind: update | rollback
phase: prepare | apply
```

The update prepare states are:

```text
queued -> resolving_target -> validating_tag -> merging_release
-> waiting_actions -> verifying_images -> downloading_images
-> rendering_compose -> backing_up -> validating_backup -> prepared
```

The rollback prepare states are:

```text
queued -> resolving_snapshot -> verifying_snapshot
-> downloading_images (only when a historical digest is missing locally)
-> rendering_compose -> backing_up -> validating_backup -> prepared
```

Both apply paths use:

```text
apply_queued -> validating_manifest -> switching_extensions
-> switching_main -> health_checking -> success
```

Settled exceptional states are `expired`, `drifted`, `conflict`, and `failed`.
After any production mutation, a failure enters `rolling_back`; the final
record distinguishes `failed_rolled_back` from `rollback_failed`.

`prepared` is durable and resumable but never auto-transitions to apply.
Repeated requests with the same action and Idempotency-Key return the same
operation. Duplicate apply clicks cannot write a second trigger or execute a
second switch.

## Administrator API

The administrator system API provides:

- read-only update detection;
- current dual-version release identity;
- current active/prepared operation;
- the latest three eligible complete rollback snapshots;
- `prepare update` and `apply update` as distinct POST actions;
- `prepare rollback` and `apply rollback` as distinct POST actions.

Rollback preparation accepts an immutable `release_id`, not an official version
string. Apply accepts the prepared operation ID and does not allow the target to
be changed. Every POST action keeps administrator authentication, audit,
compliance, rate limiting, the system operation lock, and a required
`Idempotency-Key`.

The existing two-phase update endpoints remain compatible. The old official
binary rollback endpoint and version-string request are deprecated and fail
closed for this deployment mode; they must not bypass the complete-snapshot
workflow. The older single-stage update compatibility endpoint remains
prepare-only. Legacy jobs that imply a direct container mutation are never
resumed.

## Update Preparation And Apply

Normal update behavior continues to follow the approved two-phase updater:

- detection is read-only;
- prepare locks production, official Stable, target custom commit, paired OCI
  identities/digests, Compose/environment hashes, backup, and version proposal;
- prepare may access GitHub, wait for the seven Actions jobs, verify GHCR, pull
  immutable digests, and create a complete fresh backup;
- prepare does not switch the production Git checkout, recreate containers, or
  change either state file;
- the prepared manifest expires after 15 minutes;
- apply rejects commit, origin head, current release, high-water, Compose,
  `.env`, digest, local-image, manifest, or backup drift;
- apply does not access GitHub, wait for Actions, pull images, or repeat a
  database backup;
- apply uses the explicit Compose pair and `--pull never`, switches
  `extensions-self` before `sub2api`, and does not lifecycle-manage PostgreSQL,
  Redis, or `risk-control-postgres`.

The new ledger publication occurs only after the complete internal, public,
native administration, extension route, and signed data-quality health suite
passes.

## Complete-Snapshot Rollback

The rollback list is computed from immutable successful release records whose
required source object, digest pair, matching Compose/environment backup, and
checksums remain available. It excludes `current_release_id` and returns the
three most recently published eligible records.

Rollback preparation:

1. Locks the current release and chosen historical `release_id`.
2. Verifies the historical record and all referenced artifact checksums.
3. Ensures the historical source commit is available locally.
4. Verifies both historical digests and pulls a digest only when it is absent
   locally.
5. Renders and validates the historical explicit Compose pair with the target
   `.env` and digest contract.
6. Creates and verifies a fresh backup of the current dual databases,
   Compose/environment, Nginx/certificates, digest pair, metadata, and rollback
   tags.
7. Writes a 15-minute immutable rollback prepared manifest.

Rollback apply is local-only. It rechecks the ledger pointer, high-water,
source, manifest, backup, current and target digest availability, Compose, and
environment hashes. It moves the clean production checkout to the exact locked
historical commit without `git reset --hard`, installs the historical matching
Compose pair and `.env`, then recreates only `extensions-self` and `sub2api`
using `--pull never` and the normal health sequence.

On success, the ledger's current pointer and compatibility projection change to
the historical release. Its old official/custom display versions return. The
global high-water value remains unchanged and a rollback operation event is
written.

If rollback fails after mutation, automatic rollback restores the release that
was current when rollback preparation began, using the fresh preparation backup
and old digest pair. No normal rollback path restores either database.

## Atomicity And Recovery

Every JSON write uses a same-directory temporary file, canonical serialization,
SHA256 verification where applicable, file `fsync`, atomic rename, and parent
directory `fsync`. Immutable release records are created with exclusive
semantics and are never overwritten.

For a successful new publication, the locked commit order is:

1. Create and sync the immutable release record.
2. Atomically write the compatibility `release-state.json` projection.
3. Atomically update ledger `state.json` with the new current release ID and,
   when applicable, the advanced high-water value.
4. Settle the operation record as success and clear the active operation.

Recovery code recognizes a release record plus compatibility projection written
before the final pointer change and completes that exact pending commit only
when every identity matches. Any contradictory partial state blocks further
operations and reports `LEDGER_INCONSISTENT`; it is never repaired by selecting
the newest timestamp.

For successful rollback, the same sequence is used without creating a release
record or changing the high-water value. The existing target record is checked
again before changing the projections.

## UI Design

`VersionBadge` keeps the official compact style and displays both current
identities, for example:

```text
Official v0.1.163
Custom v1.0.0
```

Detection shows the target pair and identifies whether the available update is
official, custom, combined, or docs-only. Custom targets also show the short
commit SHA; semver is never used to detect custom source changes.

The update action remains `下载更新`, followed only after preparation by
`确认更新`. The prepared view shows both target versions and the remaining
validity time. Refresh, re-login, or browser disconnect restores the active or
prepared operation from the server. The client never invokes apply
automatically.

The rollback panel replaces the old official-binary rollback list. Each of the
latest three complete snapshots shows both versions, short custom commit,
publication time, and current eligibility. Selecting one starts `准备回退`;
only a successfully prepared rollback exposes `确认回退` and its expiry.

Visible progress distinguishes image download, backup, backup validation,
prepared, expired, drift, extension switch, main switch, health check, success,
conflict, failure, automatic rollback, and rollback failure. Settled historical
success or failure is audit data and is not shown merely because the popup was
opened later.

## Error Handling And Retention

Errors use stable codes for invalid target, missing artifacts, corrupt record,
ledger inconsistency, migration mismatch, expiry, current-release drift,
high-water drift, commit/origin drift, Compose/environment drift, digest drift,
backup drift, apply failure with successful automatic rollback, and automatic
rollback failure.

Before production mutation, every failure leaves production unchanged. After
mutation, the operation cannot settle until automatic rollback has either
succeeded or been explicitly recorded as failed with recovery evidence.

Immutable release and operation metadata are retained for audit. Artifact
cleanup must preserve the current release, the three rollback-eligible releases,
and every active/prepared operation's base and target artifacts. Records whose
required artifacts have been deliberately expired remain auditable but are not
offered as rollback targets. Docker cache cleanup is not part of this feature.

## Test Strategy

Implementation begins with failing contracts and covers:

- baseline migration to `Official v0.1.163 / Custom v1.0.0` and refusal on any
  production identity mismatch;
- official-only version retention, custom patch increment, combined increment,
  and docs-only/no-change refusal;
- candidate display without allocation, allocation only after health success,
  no allocation on expiry/failure/drift/unconfirmed prepare, and high-water
  non-reuse after rollback;
- immutable record creation, compatibility projection, atomic write recovery,
  checksum corruption, contradictory partial state, and idempotent migration;
- latest-three complete snapshot ordering, exclusion of current, artifact
  eligibility, and historical version display after rollback;
- distinct idempotent update prepare/apply and rollback prepare/apply actions;
- duplicate clicks, concurrent requests, refresh/re-login recovery, prepared
  expiry, and stale high-water/current-release rejection;
- update prepare's zero-container-change contract;
- update apply's no-network, no-pull, no-Actions, no-backup contract;
- rollback prepare's fresh-current-backup contract and conditional missing-image
  pull behavior;
- rollback apply's no-network, no-pull, no-backup, no-database-restore contract;
- explicit dual Compose rendering, `--pull never`, extension-before-main order,
  and prohibition on PostgreSQL, Redis, and `risk-control-postgres` lifecycle
  commands;
- complete health gates, failed update automatic rollback, failed selected
  rollback restoration of its pre-rollback release, and rollback-failure
  evidence;
- frontend dual-version display, target type and short SHA, prepared countdown,
  explicit confirmation, latest-three rollback list, refresh recovery, and no
  replay of settled historical outcomes;
- fail-closed migration of the old official binary rollback and legacy
  single-stage update paths.

Required verification includes the complete backend Go suite, complete frontend
Vitest suite, frontend typecheck and production build, both `extensions-self`
Go suites, deployment Node contracts, PowerShell script contracts, prepare/apply
shell fixtures, real explicit Compose rendering, and `git diff --check`. The
feature commit and exact merge commit are tested separately before a normal
non-force push. Actions and both GHCR images are then observed separately.

## Delivery Boundary

The implementation task updates code, tests, the release runbook, custom
operations guide, and `deploy/ops/README.md`. It creates a feature commit, merges
locally to `custom-release`, reruns required checks on the exact merge commit,
and pushes without rewriting history.

Code completion, push, Actions, GHCR publication, host-script installation,
baseline migration, production preparation, production apply, production
health, and rollback are separate reportable facts. Neither this design nor its
implementation authorizes installing VPS scripts, creating the production
ledger, clicking update/rollback, or changing production.
