# Release Operations

These files are the versioned source for `/opt/sub2api-custom/` and the host
systemd units that publish Sub2API in the active production environment.

The operator-facing project and custom-release workflow is documented in
`docs/SUB2API-CUSTOM-OPERATIONS.md`. This file defines the narrower ownership
and safety contract of the host scripts.

## Production Path

The only normal production path is:

```text
feature -> custom-release -> Custom Release Actions
-> public paired GHCR images -> administrator update button
-> sub2api-release.path -> durable host state machine -> digest deployment
```

The paired images are built from one full commit SHA:

```text
ghcr.io/listencodes/sub2api-custom:custom-<full-sha>
ghcr.io/listencodes/sub2api-extensions:custom-<full-sha>
```

Production Compose requires immutable values:

```dotenv
SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@sha256:<digest>
EXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@sha256:<digest>
```

Production Compose is an explicit two-file contract. `deploy/docker-compose.yml`
stays byte-for-byte aligned with the current official Stable Release, while
`deploy/docker-compose.custom.yml` owns the immutable image substitutions,
host update mounts, risk-control settings, `extensions-self`, and its dedicated
database/volume. Every production validation or lifecycle command must include
both files in this order; implicit `docker-compose.override.yml` discovery is
forbidden:

```bash
docker compose --project-name deploy \
  -f /root/sub2api/deploy/docker-compose.yml \
  -f /root/sub2api/deploy/docker-compose.custom.yml \
  --env-file /root/sub2api/deploy/.env config --quiet
```

The packages are public and the VPS pulls anonymously. GitHub credentials are
not production image credentials.

## Script Ownership

- `sync-trigger.sh` atomically writes the durable job ID to `release-trigger`
  and returns immediately.
- `sub2api-release.path` watches that file and starts the one-shot
  `sub2api-release.service`.
- `sync-and-publish.sh` claims the trigger under `flock` and owns all release
  state transitions.
- `sync-upstream.sh` verifies `Wei-Shaw/sub2api /releases/latest`, fetches the
  exact annotated tag, and creates `origin/integration/release-*` when needed.
- `classify-release-scope.sh` compares the production and target commits; a
  target containing only Markdown, `AGENTS.md`, or any `.gitignore` is marked
  `docs_only` and stops before Actions, GHCR verification, or publication.
- `wait-for-actions.sh` requires the complete Custom Release validation suite.
- `verify-release-images.sh` checks public pull, `linux/amd64`, digest identity,
  and OCI revision/version/source labels for both images.
- `promote-release.sh` advances only the tested remote candidate after a base
  recheck. It does not move the local production source.
- `publish-custom.sh` backs up, fast-forwards local source after the backup,
  stages extensions then main by digest, runs health checks, and automatically
  rolls back the previous pair on failure.

There is no daily updater, polling cron consumer, scheduled mode, or VPS-local
image build. The independent health-monitor schedule remains.

## Release Scenarios

For a new official stable Release, the orchestrator validates the annotated tag,
merges only its peeled commit in a temporary worktree, waits for Actions and
images, rechecks the base, promotes `origin/custom-release`, and publishes.

When no new official Release exists but `origin/custom-release` has an
undeployed custom commit, the same administrator action waits for that commit's
Actions/images and publishes it without a Release merge. When neither changed,
the job returns `success` without pulling or recreating services.

Documentation-only commits are not runtime releases. The GitHub workflow ignores
pushes containing only Markdown, `AGENTS.md`, or any `.gitignore`; if a durable job
still targets such a commit, the classifier records `docs_only=true`, leaves
`release-state.json` and production unchanged, and returns `success` without
waiting for checks or images. A mixed or later runtime diff follows the normal
seven-check and paired-image path.

Conflicts are terminal `conflict` jobs with the exact files, both source
identities, a resolution hint, and an artifact under
`release-jobs/` / `sync-conflicts/`. All other pre-deployment validation errors
fail closed before production mutation.

## Backup And Rollback

Before local source or Compose state changes, the publisher verifies both
custom-format database dumps and backs up the official and custom Compose files,
`.env`, Nginx, certificate
and key files, old digests, rollback tags, container/image metadata, and
checksums. It never recreates or replaces `risk-control-postgres`.

The backup names are `main-docker-compose.yml` and
`custom-docker-compose.yml`. Rollback and its Compose health gate load that
matching pair from the same backup directory; they never combine a backed-up
file with the current checkout.

If the legacy deployment has no `release-state.json`, the first administrator
job uses a one-time bootstrap: it records the clean local commit and current
image IDs, tags both local images for rollback, and keeps the old Compose. It
writes the first formal digest state only after the new pair is healthy; a
failed bootstrap restores the old deployment and leaves formal state absent.

The first release containing the overlay also supports a clean pre-overlay
production commit that already has `release-state.json`: the old single-file
configuration is rendered and backed up with a temporary empty custom file,
then the approved target must provide the real tracked overlay. This migration
exception applies only to the current pre-update configuration and never makes
the target overlay optional.

A failed deployment or full health gate enters `rolling_back`, restores the
previous `SUB2API_IMAGE` and `EXTENSIONS_SELF_IMAGE`, recreates
`extensions-self` then `sub2api`, and reruns health checks. Database restore is
not automatic; `risk_control_db.dump` is used only for separately authorized
schema or data corruption recovery.

`release-state.json` is updated only after a healthy release and records the
production commit, stable Release tag/commit, both digests, publication time,
and backup directory. Backfill starts only after health and only across the
`data-quality` `available_from/to` range.

## Installation

Install scripts with mode `0755`, units with mode `0644`, then enable the path:

```bash
install -m 0755 deploy/ops/*.sh /opt/sub2api-custom/
install -m 0644 deploy/ops/sub2api-release.path /etc/systemd/system/
install -m 0644 deploy/ops/sub2api-release.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now sub2api-release.path
```

Do not call `sync-upstream.sh` or `publish-custom.sh` directly for final
acceptance. Trigger and monitor the same durable job path used by the
administrator button.

## Branch Contract

`custom-release` is the only production branch. `custom` is only for
`upstream/main` forward-compatibility testing. A stable custom feature may be
selectively `cherry-pick -x` into `custom`; never merge the entire `custom`
branch into `custom-release`.

Report implementation, tests, `origin/custom-release` push, Actions/GHCR,
backup, deployment, health, trigger migration, and rollback material as
separate results.
