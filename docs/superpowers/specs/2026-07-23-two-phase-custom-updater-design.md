# Two-Phase Custom Updater Design

## Goal

Replace the administrator's single-stage update action with a durable two-stage
release flow. Preparation may resolve and promote an approved Stable candidate,
verify and pull immutable images, render Compose, and create a complete backup,
but only a second explicit administrator action may change production containers
or `release-state.json`.

## Boundaries

- Development happens from `custom-release` in an isolated feature worktree.
- Detection is read-only: it does not fetch into the production Git tree, pull
  images, write a release trigger, or mutate production.
- Preparation may guard-promote a verified official integration candidate to
  `origin/custom-release`, but does not change `/root/sub2api`, containers, or
  `release-state.json`.
- Apply does not contact GitHub, wait for Actions, pull images, or back up a
  database. It uses only the immutable preparation evidence and locally present
  images.
- Only `extensions-self` and `sub2api` are recreated. PostgreSQL, Redis, and
  `risk-control-postgres` are never lifecycle-managed by this updater.
- No production installation or deployment is part of this development task.

## Unified Detection

`GET /api/v1/admin/system/check-updates` combines three facts:

1. the current `release-state.json` production commit and Stable identity;
2. the latest non-draft, non-prerelease `Wei-Shaw/sub2api` Release;
3. `ListenCodes/sub2api` `custom-release` HEAD and the changed-file scope from
   the production commit.

The response classifies the result as `none`, `official`, `custom`, `combined`,
or `docs-only`. It exposes the target custom commit as a full SHA plus a short
SHA and never derives custom availability from semver. An official runtime
update combined with documentation-only custom commits remains `combined`.
Pure documentation-only changes are visible but cannot create a preparation
job. If a required source cannot be refreshed, the response carries an explicit
warning and does not falsely report `none`.

The official Release response may use the existing short cache. Production
state and custom branch scope are read on every check so a cached Stable result
cannot hide a newly pushed custom commit.

## API And Compatibility

The custom administrator route group adds:

- `POST /api/v1/admin/system/update/prepare`
- `POST /api/v1/admin/system/update/apply`
- `GET /api/v1/admin/system/update/status`

Apply accepts `{ "job_id": "update-..." }`. Both POST actions require their own
Idempotency-Key and retain administrator authentication, audit logging,
compliance checks, the system operation lock, and one-at-a-time durable job
semantics.

The existing `POST /api/v1/admin/system/update` remains as a deprecated alias
for prepare. It can never enter apply. Existing jobs without an `action` field
are interpreted as legacy prepare jobs only; a legacy job already in a deployment
state fails closed and is never resumed into container mutation.

## Durable State Machine

The job retains one job ID across preparation and apply:

```text
checking_updates -> validating_tag -> merging_release
-> waiting_actions -> waiting_images -> downloading_images
-> preparing_compose -> backing_up -> validating_backup -> prepared

prepared --explicit apply--> apply_queued -> deploying_extensions
-> deploying_main -> health_checking -> success
```

`conflict`, `failed`, `expired`, and `drifted` are settled states. A failed
post-mutation apply enters `rolling_back` before `failed`. `prepared` stops UI
polling but is not a completed release; it is the only normal state from which
apply may start.

Repeated prepare requests return the active preparation job. Repeated apply
requests for the same prepared job return its current state and never create a
second trigger. A new prepare is allowed after expiration or drift.

## Host Orchestration

`sync-trigger.sh` atomically writes an action plus job ID. The existing
`sub2api-release.path` and one-shot service remain the only consumer.
`sync-and-publish.sh` acquires the release lock, claims the trigger, validates
the requested action, and dispatches one of two executors:

- `prepare-release.sh`
- `apply-release.sh`

Shared validation, Compose, backup, health, and rollback primitives live in a
focused shell library. `publish-custom.sh` becomes a fail-closed legacy shim and
is not called by the service, dispatcher, prepare executor, or apply executor.

## Preparation Contract

Preparation locks the production commit, current Stable identity, latest Stable
identity, and current `origin/custom-release` HEAD. When an official update is
needed, it uses the existing exact annotated-tag resolver and temporary
integration worktree. After all seven Actions jobs and paired-image identity
checks pass, it guard-promotes the tested commit to `origin/custom-release`.

It then:

- verifies OCI revision, version, source, platform, and immutable digests;
- anonymously pulls both digest references;
- creates a temporary target worktree without moving `/root/sub2api`;
- stages the target base Compose, custom Compose, and `.env` with both digests;
- runs explicit `--project-name deploy`, both `-f` arguments, `config --quiet`,
  and `config --format json`;
- verifies services, project name, networks, volumes, mounts, health checks,
  and the paired digest contract;
- backs up and verifies both databases, the matching current Compose pair,
  `.env`, Nginx, certificates, old digests, container/image metadata, rollback
  tags, pg_restore lists, and SHA256 sums.

No `docker compose up/down/rm`, Git production-worktree switch, or production
state write is permitted during preparation.

## Prepared Manifest

Preparation atomically writes a read-only manifest and a separate SHA256 file.
It contains at least:

- production commit and target commit;
- current and target Stable tag/commit identities;
- main and extensions digests;
- current and staged base/custom Compose hashes;
- current and staged `.env` hashes;
- backup directory and backup checksum identity;
- prepared timestamp and an expiry exactly 15 minutes later;
- workflow URL and reusable immutable image-verification evidence.

The manifest is never edited. Expiration is derived from `expires_at`. A retry
for the same target may reuse verified local image evidence, but creates a new
backup directory and manifest after rechecking the current environment.

## Apply Contract

Apply acquires the same release lock and rejects the request unless the manifest
hash, expiry, backup checksums, production commit, local clean source, remote
tracking target, current Compose and `.env` hashes, old image identities, and
local prepared digests all still match.

After validation it performs only local operations. It fast-forwards the clean
production checkout to the locked commit, installs the staged `.env`, invokes
Compose with the explicit pair and `--pull never`, recreates `extensions-self`
first and waits for health, then recreates `sub2api`. The complete internal,
public, native admin, extension proxy, and signed data-quality checks run before
an atomic `release-state.json` write.

On failure after mutation, rollback restores the previous local source pointer
without `git reset --hard`, restores the backed-up Compose pair and `.env`, and
recreates only `extensions-self` then `sub2api` with the previous image pair.
Database restore is never automatic.

## Frontend

`VersionBadge` keeps the existing visual language but renders the unified update
kind and target Stable/short-SHA identities. Its primary actions are `下载更新`
and, only after `prepared`, `确认更新`.

On mount it asks the server for the current durable job even when localStorage
is empty. localStorage remains a fallback pointer. The component stops polling
at `prepared`, displays a server-derived expiry countdown, never invokes apply
automatically, and resumes polling after explicit confirmation. It renders
expired, drifted, conflict, failure, rollback, and success evidence distinctly.

## Verification

Tests must prove the detection matrix, docs-only refusal, distinct idempotent
actions, legacy migration, durable refresh recovery, expiration, drift,
duplicate clicks, and rollback. Shell fixtures must record every fake Docker,
Git, GitHub, and database command and prove that prepare performs no container
lifecycle action while apply performs no network, pull, Actions, or backup
operation. The explicit Compose pair is also rendered with Docker Compose.

The final feature and exact local merge commit both run the required backend,
frontend, extension, deployment, PowerShell, shell fixture, Compose, and diff
checks. Only the merge commit is pushed. Actions and both GHCR images are
observed afterward; production scripts are not installed and the update action
is not clicked.
