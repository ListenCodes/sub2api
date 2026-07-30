# Release Notice And Rollback UI Design

**Date:** 2026-07-30

**Status:** Approved direction, written specification pending review

## Goal

Improve the custom release badge without changing upstream Stable-owned UI or
database models. The result must:

- preserve `has_update` as the real capability and content state;
- show the amber badge treatment only for an unread target, once per admin
  across browsers and devices;
- present the current release and the last three complete rollback snapshots in
  a two-phase UI that follows the official `VersionBadge.vue` visual language;
- list rollback snapshots without Git or Docker access inside the Web container;
- keep all source, paired-image, OCI, Compose, and backup validation on the host;
- remove the source checkout, Docker socket, and Docker binary mounts from the
  `sub2api` container after a staged host-script migration; and
- preserve the current one-hour prepare/apply, `--pull never`, paired rollback,
  and no-automatic-database-restore contracts.

This work changes code, tests, and operating documentation. It does not itself
authorize a production release.

## Existing Evidence

The current implementation already has additive ownership boundaries:

- `frontend/src/features/custom-release/CustomReleaseBadge.vue` owns the badge,
  dropdown, polling, and update/rollback orchestration.
- `frontend/src/features/custom-release/ReleaseRollbackPanel.vue` owns the
  custom rollback presentation.
- `backend/internal/service/custom_release_service.go` owns custom release
  detection and operation creation.
- `backend/internal/handler/admin/custom_release_handler.go` and
  `backend/internal/server/routes/custom_extensions.go` own the custom admin
  HTTP surface.
- `deploy/ops/prepare-rollback.sh` and `apply-rollback.sh` own host validation
  and mutation.

The production ledger contains historical records and complete backups. The
empty Web list is caused by the additional runtime filter in
`ListRollbackReleases`: it executes the host Git and Docker CLIs from the Alpine
Web container. The mounted Ubuntu Docker binary requires glibc's
`/lib64/ld-linux-x86-64.so.2`, while the container provides musl at
`/lib/ld-musl-x86_64.so.1`. The resulting `ENOENT` makes every record fail the
filter without surfacing an API error.

The existing ledger implementation already excludes the current release,
validates the complete backup contract and checksums, sorts newest first, and
applies the limit after validation. The host prepare script already revalidates
the selected snapshot, Git commit, paired images, OCI identity, explicit
Compose pair, and fresh current backup before writing a prepared manifest.

## Chosen Architecture

Use a narrow custom-owned extension of the existing release API. The Web
process reads only the persistent data volume and GitHub metadata. It never
executes Git or Docker for rollback eligibility. The host remains the sole
authority for whether a selected snapshot can actually be prepared and
applied.

The upstream Stable component
`frontend/src/components/common/VersionBadge.vue`, the official user schema,
the central router, Wire graph, and every zero-overlap file listed in
`AGENTS.md` remain unchanged.

Alternatives rejected by the approved direction are:

1. Browser-local acknowledgement. It cannot provide per-admin cross-device
   semantics.
2. A column on the official user table. It creates an upstream schema merge
   point for custom UI state.
3. Docker or Git checks in the Web API. It requires excessive container
   privilege and duplicates the host prepare gate.

## Update Fingerprint And Read State

### Fingerprint

When detection is complete and `has_update` is true, the service computes a
lowercase SHA-256 fingerprint from this canonical UTF-8 payload:

```text
custom-release-notice-v1
<update_kind>
<target_official_version>
<target_official_commit>
<target_custom_commit>
```

Each displayed line is terminated by one LF byte, including the last line.

`CustomReleaseInfo` adds:

```text
target_official_commit
update_fingerprint
notice_unread
notice_warning (optional)
```

`has_update`, `update_kind`, `docs_only`, and `runtime_update` keep their current
meaning. `notice_unread` controls only the amber background, dot, animation, and
accessible title on the collapsed badge. The dropdown content and update
actions continue to use `has_update` and the existing detection fields.

An incomplete detection or a `none` result has no fingerprint and is never
reported as unread. A docs-only target has a fingerprint and can be marked read,
but its docs-only content remains visible whenever the dropdown is opened.

### Persistent state

The custom state file is stored in the existing application data volume at:

```text
/app/data/custom-release-notice-state.json
```

Tests can override the path with `SUB2API_RELEASE_NOTICE_STATE_FILE`. Schema 1
is:

```json
{
  "schema_version": 1,
  "admins": {
    "42": {
      "last_read_fingerprint": "<64 lowercase hex characters>",
      "read_at": "2026-07-30T10:00:00Z"
    }
  }
}
```

The store accepts only positive numeric user IDs, a valid fingerprint, and
RFC3339 timestamps. It keeps at most 10,000 admin entries; before adding entry
10,001 it removes the oldest `read_at` record, using numeric user ID as the
deterministic tie breaker. Writes use a process mutex, a `0600` temporary file
in the same directory, file sync, atomic rename, and directory sync. It rejects
symbolic-link targets. No secret value is stored.

Read-state errors are advisory. Update detection and all prepare/apply actions
remain available. On a read error, a real update is treated as unread and
`notice_warning` is returned. A mark-read write failure returns
`persisted: false`; it does not fail or delay a release action.

### HTTP contract

`GET /api/v1/admin/system/custom-release/check` obtains the authenticated admin
user ID from `middleware.AuthSubject`, computes the target fingerprint, and
decorates the response with that admin's unread state.

`POST /api/v1/admin/system/custom-release/read` accepts:

```json
{"fingerprint":"<current fingerprint>"}
```

It returns:

```json
{"fingerprint":"<fingerprint>","persisted":true}
```

The endpoint is registered only under the existing authenticated custom admin
group. It validates fingerprint syntax. Recording a stale fingerprint is safe:
it cannot suppress a newer target, and guessing a future SHA-256 target is not a
meaningful privilege escalation.

The frontend marks the current fingerprint read on all of these transitions:

- the closed dropdown is opened;
- update prepare is requested;
- update apply is confirmed;
- rollback prepare or apply is requested while an update fingerprint exists.

These calls are best effort and never block the requested operation. The store
sets its in-memory `noticeUnread` false immediately and reconciles on the next
server check.

## Rollback Candidate Contract

`UpdateService.ListRollbackReleases` returns:

```text
ledger.ListRollbackReleases(3, nil)
```

This is deliberately a data-volume-only operation. The ledger remains
responsible for:

- excluding `state.current_release_id`;
- rejecting malformed records;
- requiring every complete backup artifact and checksum;
- sorting by `published_at` descending; and
- returning at most three entries.

The removed Web runtime filter and its `/repo`, `git`, and `docker` helpers are
not replaced. Eligibility is not weakened because
`deploy/ops/prepare-rollback.sh` independently fails closed before production
mutation when any of these are absent or inconsistent:

- the selected target is no longer one of the newest three complete snapshots;
- the target Git commit is unavailable;
- either immutable image is unavailable;
- either image has the wrong architecture, source, version, revision, digest,
  or repository identity;
- the target Compose pair or environment does not match the record;
- the target backup or checksum manifest is corrupt; or
- the fresh backup of current production cannot be created and validated.

The Web list therefore means "recorded complete snapshot", while successful
host preparation means "currently executable rollback".

## Frontend State And Visual Design

### Badge

`CustomReleaseBadge.vue` keeps the current dropdown and real update state. Its
collapsed button uses `noticeUnread`, not `hasUpdate`, for the amber background,
ping animation, dot, and unread title. After acknowledgement it returns to the
neutral style even while update details and prepare controls remain available.

The existing `hasUpdate` computed value continues to drive current/target copy,
docs-only copy, changelog links, and update availability. No operation state is
stored in browser acknowledgement storage.

### Current release loading

`store.fetchCurrentRelease` no longer swallows errors. The store exposes
`currentReleaseLoading` and `currentReleaseError` and clears stale identity on
failure. Opening the rollback panel always has one of four explicit states:

1. loading spinner;
2. current-release or history error with a retry button;
3. empty history message; or
4. selectable history.

There is no `isReleaseBuild && currentRelease == null` blank branch.

### Rollback panel

`ReleaseRollbackPanel.vue` retains all logic and copies the visual language,
not code ownership, from the official Stable `VersionBadge.vue`:

- the existing clock/chevron entry remains in the custom badge;
- loading uses the primary spinner;
- errors use the red bordered message and full-width retry button;
- empty history uses centered muted text;
- each candidate is a full-width bordered button with an amber radio marker;
- selected candidates use amber border/background/text;
- commands use full-width icon-and-text buttons; and
- prepared state uses an emerald confirmation block and an amber confirmation
  action consistent with the destructive rollback meaning.

Each current/target identity displays:

```text
Official <version> / Custom <version>
commit <first 8 characters> / localized published time
```

The current identity is informational and is never selectable. Selecting a
target enables `Prepare rollback`. Once preparation reaches `prepared`, the
target is locked to the job's `target_release_id`; list clicks and selection
changes are disabled. The prepared view displays the immutable target pair,
short commit, expiry countdown, and `Confirm rollback`.

Preparation and apply progress use the existing durable polling path. A
transient poll error does not discard the operation. On terminal failure or
drift the target remains visible, the error is shown, and retry reloads current
release plus candidates before allowing a new prepare. At countdown zero the
frontend invokes the existing expired-apply refusal path to settle the host
operation, resumes polling until the `expired` terminal state, then unlocks the
target and reloads candidates. Production is never changed by this expiry
settlement.

The panel emits `retry`, `prepare`, `apply`, and `expired` events. It does not
call APIs directly. `CustomReleaseBadge.vue` remains the orchestration owner.

## Container Privilege Reduction

The final `deploy/docker-compose.custom.yml` adds only the custom mount required
by the running Web process:

```yaml
volumes:
  - /opt/sub2api-custom/sync-trigger.sh:/app/scripts/sync-upstream.sh:ro
```

The unmodified Stable base Compose supplies `sub2api_data:/app/data`. Therefore
the final rendered service contains exactly the data mount plus the read-only
trigger mount, while the overlay contains only the trigger addition. The final
rendered `sub2api` service must not contain:

```text
/root/sub2api -> /repo
/var/run/docker.sock -> /var/run/docker.sock
/usr/bin/docker -> /usr/bin/docker
```

No official Dockerfile change is needed. Removing the Web runtime eligibility
filter also removes the reason to put `git` in the custom runtime image for this
feature; changing the Dockerfile solely to remove an already-installed package
is outside this change.

`deploy/tests/compose-overlay-contract.test.mjs` checks both source text and the
rendered Compose JSON. It positively requires the data and read-only trigger
mounts and rejects the three privileged mounts.

## Required Staged Migration

The currently installed `/opt/sub2api-custom/release-common.sh` requires `/repo`
and `/var/run/docker.sock` in every rendered target. It therefore rejects a
no-mount target before production changes. A single production jump to the
final commit is not safe.

The migration has two separately deployable runtime commits:

### Stage A: transition validator

1. Change the host Compose validator to accept both the legacy privileged
   mount set and the final reduced mount set, while always requiring data,
   trigger, services, networks, volumes, health checks, and exact paired images.
2. Keep `deploy/docker-compose.custom.yml` unchanged in this commit.
3. Add contract fixtures for both accepted shapes and for partially removed or
   unexpected privileged shapes that must fail.
4. Merge and push Stage A to `origin/custom-release`, wait for Actions/GHCR,
   obtain explicit production-release authorization, deploy it through the
   administrator two-phase path, and then separately install/verify the Stage A
   `deploy/ops` files under `/opt/sub2api-custom/`.

### Stage B: final behavior and strict validator

1. Land the notice state, rollback API/UI, host fail-closed tests, documentation,
   and removal of the three privileged mounts.
2. Change the validator from transition-compatible to strict reduced-mount
   enforcement.
3. Only after Stage A is deployed and its host scripts are verified may Stage B
   advance `origin/custom-release`.
4. After separately authorized Stage B production deployment, install and
   verify the strict final host scripts under `/opt/sub2api-custom/` before
   accepting another administrator trigger.

Both deployments use the normal one-hour prepare/apply path. Neither is
authorized by this design document. Development may prepare both commits on the
feature branch, but must not advance Stage B on `origin/custom-release` before
the Stage A production and host-script gate is recorded.

## Testing

### Backend

Focused Go tests cover:

- canonical fingerprint stability across identical checks;
- a new Official version/commit or Custom commit produces a new fingerprint;
- the same fingerprint is unread once per admin and remains read across service
  instances;
- one admin's acknowledgement does not affect another admin;
- docs-only acknowledgement hides only the badge indicator;
- malformed, unreadable, or unwritable notice state does not block detection or
  update/rollback preparation;
- mark-read authentication, validation, and best-effort persistence;
- rollback history excludes current, returns at most three complete snapshots,
  and performs no Git or Docker execution; and
- current-release API failures remain visible to the frontend contract.

### Frontend

Vitest covers:

- `hasUpdate=true` with `noticeUnread=false` keeps update content but removes
  amber/dot/ping styling;
- opening, preparing, and confirming mark the current fingerprint read without
  blocking on API failure;
- a newly returned fingerprint restores the unread indicator;
- current release and history loading, error, retry, empty, and success states;
- amber target selection, preparation, immutable prepared target, countdown,
  apply, expiry settlement, terminal failure recovery, and successful apply;
- docs-only content after acknowledgement; and
- no blank rollback body when current release loading fails.

### Host and deployment

Shell fixture tests prove `prepare-rollback.sh` leaves the production ledger,
projection, Compose, and containers unchanged when Git is missing, either image
cannot be obtained, OCI revision is wrong, or any required backup/checksum is
corrupt. Existing `--pull never`, extension-before-main, health, paired restore,
and no-database-restore tests remain required.

Node Compose tests cover the Stage A compatibility shapes and the Stage B
strict absence of `/repo`, Docker socket, and Docker binary mounts.

Verification includes focused and full backend tests, focused and full frontend
tests, typecheck/build, all deploy contract tests, shell release fixtures,
`git diff --check`, the Stable overlap budget, and a final diff review.

## Documentation

The implementation updates:

- `AGENTS.md` with the Web/host responsibility split, unread-state ownership,
  reduced-mount invariant, and staged migration rule;
- `docs/SUB2API-CUSTOM-OPERATIONS.md` with administrator behavior and the
  deployment sequence;
- `deploy/RELEASE-RUNBOOK.md` with Stage A/Stage B host-script gates, rollback
  candidate semantics, and verification evidence;
- `deploy/ops/README.md` where the installed host script and Compose contract is
  described; and
- the Sub2API knowledge-base project/VPS documents only where their current
  operational summary must change.

No document records a fixed dynamic production version, digest, or secret.

## Acceptance Criteria

- The collapsed badge alerts each admin once for one exact target fingerprint
  across devices; a new target alerts again.
- Acknowledgement never changes `has_update`, docs-only content, or release
  capability.
- The rollback panel has explicit loading, error/retry, empty, selection,
  prepared/countdown, expiry, apply, failure, and success behavior.
- The Web rollback list is non-empty for complete ledger snapshots and performs
  no Git or Docker check.
- Host preparation remains the fail-closed authority and production remains
  unchanged on every validation failure.
- The final rendered Web service has data/trigger access and no source checkout,
  Docker socket, or Docker binary mount.
- All Stable zero-overlap files remain byte-equivalent to the pinned Stable
  commit.
- Stage A and Stage B are separately committed, tested, pushed, built, deployed,
  and host-synchronized in that order; production deployment requires explicit
  authorization and is reported separately from code completion.
