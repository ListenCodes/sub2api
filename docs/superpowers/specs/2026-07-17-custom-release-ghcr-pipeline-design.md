# Custom Release GHCR Pipeline Design

## Status

Approved implementation direction from the release architecture objective. This
document defines the end-to-end path from an administrator update request to a
verified, digest-pinned release on the active production host.

## Problem

The current `custom-release` branch already resolves and verifies official
stable Releases, but publication still depends on VPS-local image builds,
mutable image tags, a three-state update job, a polling trigger script, and cron.
Health failures preserve rollback tags but do not restore the previous release.

The target architecture must separate source integration, CI image production,
and production publication while keeping one administrator action as the only
production trigger.

## Goals

- Keep `origin/custom-release` as the only production branch.
- Read upstream updates only from `Wei-Shaw/sub2api /releases/latest`.
- Test every candidate in GitHub Actions and build two images from one commit.
- Publish and run immutable GHCR digests with verifiable OCI metadata.
- Return an update `job_id` immediately and persist every job transition.
- Deploy extensions before the main application and automatically restore the
  previous digest pair after a failed deployment health gate.
- Replace the cron trigger consumer with `systemd.path` and `systemd.service`.
- Remove all scheduled update behavior while preserving health monitoring.

## Non-Goals

- The project will not create GitHub Releases in the fork.
- `custom` will not be merged into `custom-release` or used for production.
- The VPS will not build application images in the normal release path.
- The publisher will not recreate `risk-control-postgres`.
- An automatic code rollback will not restore either database.
- Docker build caches will not be cleared.
- Production rollback will not be deliberately exercised during this change.

## Ownership Model

### GitHub Actions

Actions owns deterministic validation and image production. It has only:

```yaml
permissions:
  contents: read
  packages: write
```

It cannot advance a source branch. A workflow runs for pushes to
`custom-release` and `integration/release-*`. Test jobs cover the backend,
golangci-lint, frontend typecheck/Vitest/build, both extension Go modules,
deployment Node tests, shell syntax, and both Docker builds. Image publication
starts only after every validation job succeeds.

The workflow pushes:

```text
ghcr.io/listencodes/sub2api-custom:custom-<full-commit-sha>
ghcr.io/listencodes/sub2api-extensions:custom-<full-commit-sha>
```

Both images carry these labels:

```text
org.opencontainers.image.revision=<full-commit-sha>
org.opencontainers.image.version=<stable-release-version>
org.opencontainers.image.source=https://github.com/ListenCodes/sub2api
```

The two packages are made public once through the GitHub package API using a
local administrative credential. No GitHub credential is installed on the VPS
for GHCR; production pulls anonymously.

### Host Orchestrator

The host owns release integration, candidate promotion, backups, deployment,
and rollback. This is required because Actions has `contents: read` and cannot
push `custom-release`.

The orchestrator uses the existing Git remote authentication only for Git fetch
and guarded push operations. It uses unauthenticated public GitHub API and GHCR
reads for workflow and image checks. It never reads the local GitHub token file
as a production credential.

### Application Container

The backend accepts the administrator request and returns a `job_id`. Its
container-side trigger helper only creates the persistent job request and host
trigger; it does not wait for integration, Actions, image builds, or deployment.
The frontend resumes status polling after a page refresh.

## Branch And Candidate Flow

### New Stable Release

1. Read `Wei-Shaw/sub2api /releases/latest` and reject draft or prerelease data.
2. Read the Git tag ref, require an annotated tag object, fetch only the exact
   tag, and validate API tag-object SHA, local tag-object SHA, peeled commit,
   and Release commit identity.
3. Re-read `origin/custom-release` and the committed stable baseline. Stop if
   the base changed or any identity is inconsistent.
4. Create a temporary `integration/release-*` worktree from the recorded base.
5. Merge only the peeled Release commit. Update
   `deploy/stable-release-baseline.json` in the candidate merge commit.
6. Push the integration branch and wait for the candidate workflow checks.
7. Wait for both commit-addressed GHCR images and verify their metadata.
8. Re-fetch `origin/custom-release`, require that the recorded base is unchanged,
   then fast-forward it to the already-tested candidate and push it.
9. Back up and publish the candidate image digests.

Conflicts preserve the integration branch and diagnostic snapshot and terminate
in `conflict`. Validation failures terminate in `failed`. Neither changes
`origin/custom-release` or production.

### Undeployed Custom Commit

If there is no new stable Release but `origin/custom-release` differs from the
recorded production commit, the orchestrator does not create a merge. It waits
for the existing commit's Actions checks and two images, validates them, and
publishes their digests.

### No Change

If the latest stable Release is already integrated and the branch commit equals
the recorded production commit, the job ends successfully with an
already-current result. It does not pull images or recreate containers.

## Persistent Job State

Job files live in the existing persistent application data volume under:

```text
release-jobs/<job-id>.json
release-current-job-id
release-trigger
release-state.json
```

Writes use a temporary file plus atomic rename. The current job identifier lets
the status endpoint recover after backend restart and lets the frontend resume
after refresh. A job record contains:

```json
{
  "job_id": "update-...",
  "status": "waiting_actions",
  "message": "Waiting for custom-release validation",
  "created_at": "RFC3339",
  "updated_at": "RFC3339",
  "finished_at": "RFC3339 or empty",
  "base_commit": "full SHA",
  "target_commit": "full SHA",
  "release_tag": "vX.Y.Z",
  "release_commit": "full SHA",
  "workflow_url": "URL or empty",
  "main_digest": "sha256:... or empty",
  "extensions_digest": "sha256:... or empty",
  "production_changed": false,
  "error_code": "stable machine-readable code or empty",
  "conflict_files": [],
  "artifact_path": "path or empty",
  "rollback": {
    "attempted": false,
    "succeeded": false,
    "message": ""
  }
}
```

The state machine includes these states:

```text
checking_release -> validating_tag -> merging_release -> waiting_actions
-> waiting_images -> promoting_release -> backing_up
-> deploying_extensions -> deploying_main -> health_checking -> success

health_checking/deployment failure -> rolling_back -> failed
merge conflict -> conflict
pre-deployment validation failure -> failed
```

`promoting_release` is an additional explicit state. The required states remain
stable API values. Terminal states are `success`, `failed`, and `conflict`.

The backend status endpoint accepts a `job_id`; if omitted it returns the
current or latest persisted job. The frontend stores the returned ID locally,
queries current status on mount, treats all non-terminal states as progress,
and uses a deadline long enough for Actions and image publication rather than
the current 15-minute assumption.

## Production Release State

`release-state.json` records the last healthy production release:

```json
{
  "production_commit": "full SHA",
  "stable_release_tag": "vX.Y.Z",
  "stable_release_commit": "full SHA",
  "main_digest": "sha256:...",
  "extensions_digest": "sha256:...",
  "published_at": "RFC3339",
  "backup_dir": "/root/backups/sub2api/<release-id>"
}
```

It is updated only after every health check succeeds. A failed release followed
by a successful automatic rollback leaves it describing the restored release.

## Digest Validation

Before a backup or production change, the publisher verifies both target images:

- the tag is `custom-<target full SHA>`;
- the registry resolves a digest and the pull is anonymous;
- the manifest includes `linux/amd64`;
- `org.opencontainers.image.revision` equals the target SHA;
- `org.opencontainers.image.version` equals the stable version;
- `org.opencontainers.image.source` equals the fork URL;
- the locally pulled image `RepoDigest` equals the registry result.

The publisher receives the expected commit and digests explicitly and accepts
only the exact `origin/custom-release` commit. Mutable tags are never written to
production Compose state.

## Backup, Deployment, And Rollback

Before changing Compose image variables, the publisher creates one release
backup directory and verifies:

- main PostgreSQL custom-format dump via `pg_restore --list`;
- risk-control PostgreSQL custom-format dump via `pg_restore --list`;
- Compose, `.env`, Nginx vhost, origin certificate and private key;
- old image digests, running container metadata, image metadata, and rollback
  references;
- checksums for the backup payload.

`deploy/docker-compose.yml` reads:

```text
SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@sha256:...
EXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@sha256:...
```

The publisher updates the production environment atomically, then pulls and
recreates only `extensions-self`, verifies its internal health, recreates only
`sub2api`, and runs the complete internal and public health suite. It never
recreates or replaces `risk-control-postgres`.

If a deployment or health gate fails after production mutation begins, the job
enters `rolling_back`, restores the previous image variables and matching
configuration, recreates `extensions-self` then `sub2api`, and repeats health
checks. Facts, cursors, schemas, and both databases are retained. Database
restore remains a separately authorized corruption recovery action.

## Triggering And Concurrency

The container trigger helper atomically writes `release-trigger` and exits.
`sub2api-release.path` watches that host-visible file and starts
`sub2api-release.service`, which claims the trigger and calls the unified host
orchestrator. The service uses `flock` and processes at most one release job.

The old per-minute cron consumer and daily `auto-update.sh` entry are removed.
No `--scheduled` mode or scheduled publication state remains. The independent
health-monitor timer remains unchanged.

## Error Handling

- Release API, tag, ancestry, base, and image mismatches fail closed before a
  production backup.
- Candidate conflicts record exact files, both commits, Release identity,
  diagnostics, and a human resolution hint.
- An Actions failure records the workflow URL and does not promote the branch.
- A branch base change during waiting causes a terminal failure and requires a
  new administrator job.
- Missing previous digests block deployment before mutation; they cannot be
  discovered after a failed health check.
- A rollback failure is recorded distinctly and preserves all backup evidence.

## Testing Strategy

Implementation follows test-first changes. Coverage includes:

- fixture tests for draft/prerelease rejection and annotated tag identity;
- state transition, atomic persistence, restart, current-job, and terminal-state
  backend tests;
- frontend tests for every state family, refresh recovery, long Actions waits,
  conflict evidence, success, and rollback failure;
- executable fake Git/GitHub/GHCR/Docker publisher tests for no-change, custom
  commit, new Release, base changes, image mismatch, deployment failure, and
  successful automatic rollback;
- Compose contract tests for image variables and forbidden database recreation;
- workflow structure tests for triggers, minimum permissions, complete test
  gates, two image tags, and OCI labels;
- shell syntax checks and both Docker image build checks;
- the complete backend, frontend, extension, and deployment suites in Actions.

Rendered UI validation covers the administrator version button, progress across
non-terminal states, page refresh recovery, conflict details, already-current
completion, and final success/failure messaging without layout regressions.

## Documentation And Reporting

`AGENTS.md`, the release runbook, deploy READMEs, and the VPS operations contract
will describe only this production flow:

```text
feature branch -> custom-release -> Actions and GHCR
-> administrator button -> digest publication
```

Stable custom changes may later be cherry-picked with `-x` into `custom` for
forward-compatibility testing. The entire `custom` branch is never merged back.

Implementation, local test results, branch push, Actions/GHCR publication,
production backup, production deployment, health, scheduled update removal, and
rollback evidence are reported as separate facts.

## Acceptance Criteria

- A single administrator request can complete all three update scenarios.
- The HTTP request returns immediately with a durable `job_id`.
- `origin/custom-release` advances only after candidate Actions and image checks.
- Both production services run the same source commit by immutable digest.
- Failed production health automatically restores the previous digest pair.
- No scheduled code or production cron update consumer remains.
- The current production databases and risk-control database container identity
  are preserved.
- Production status and rollback material remain sufficient to audit or recover
  every attempted release.
