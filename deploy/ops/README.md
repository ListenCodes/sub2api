# Release Operations

## Dual-version ledger

Bootstrap is Official v0.1.163 / Custom v1.0.0 at production commit
`aa2d24106cab0a03785330d8e0ff4e02b0474a0e`. Custom and combined runtime
releases allocate the next global custom patch; official-only releases do not.
Rollback is prepare/apply, selects one of the last three complete snapshots
excluding current, restores the historical display pair without lowering or
reusing high-water, and normally restores neither database. Production ledger
migration requires separate administrator authorization.

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
the single read-only host trigger bind, risk-control settings,
`extensions-self`, and its dedicated database/volume. Every production
validation or lifecycle command must include
both files in this order; implicit `docker-compose.override.yml` discovery is
forbidden:

```bash
docker compose --project-name deploy \
  -f /root/sub2api/deploy/docker-compose.yml \
  -f /root/sub2api/deploy/docker-compose.custom.yml \
  --env-file /root/sub2api/deploy/.env config --quiet
```

The rendered Web service has exactly two mounts: `sub2api_data:/app/data` and
`/opt/sub2api-custom/sync-trigger.sh:/app/scripts/sync-upstream.sh:ro`. Source
checkout, `/repo`, `/var/run/docker.sock`, and `/usr/bin/docker` mounts are
forbidden. The Web API reads the ledger and writes trigger/data state only;
host scripts own Git, Docker, image/OCI, Compose, and backup validation.

The packages are public and the VPS pulls anonymously. GitHub credentials are
not production image credentials.

## Local Development Overlay

Local custom development uses a separate explicit Compose pair:

```text
deploy/docker-compose.local.yml          official Stable-owned base
deploy/docker-compose.custom.local.yml   custom services and environment overlay
deploy/.env.local                        untracked mode-0600 secrets
```

Run `deploy/ops/bootstrap-custom-local.sh` from the repository checkout to
create the environment and start the pair. The script refuses to overwrite an
existing environment file and must never print generated secret values. Every
later local `config`, `up`, `logs`, or lifecycle command must pass the base file
first, the custom overlay second, and `--env-file deploy/.env.local` explicitly.
Do not move custom services back into `docker-compose.local.yml`.

## Script Ownership

- `sync-trigger.sh` atomically writes the action and durable job ID to `release-trigger`
  and returns immediately.
- `sub2api-release.path` watches that file and starts the one-shot
  `sub2api-release.service`.
- `sync-and-publish.sh` claims the trigger under `flock` and dispatches the
  matching prepare/apply executor; it never calls a publisher.
- `prepare-release.sh` performs remote gates, digest pulls, explicit Compose
  rendering and complete backup validation, then stops at `prepared` with a
  signed-by-SHA256 60-minute manifest. It must not run Compose lifecycle
  commands or write production state.
- `prepare-identity-rollout.sh` prepares only the six fixed, ordered identity
  transitions. It preserves commit, digest, version, and high-water identity,
  generates three pairwise-independent identity keys only in the root-owned
  mode `0600` host secret file under a root-owned mode `0700` directory, and
  creates the same complete backup under root-only
  `/var/lib/sub2api-release/backups` and a 60-minute immutable manifest.
- `apply-release.sh` consumes only that manifest, rejects expiry and drift,
  performs local `--pull never` extensions-first/main-second switching and
  health checks, then atomically writes production state or rolls back.
- `sync-upstream.sh` verifies `Wei-Shaw/sub2api /releases/latest`, fetches the
  exact annotated tag, and creates `origin/integration/release-*` with the
  canonical Stable merge subject, approved base as first parent, and peeled
  Release commit as second parent.
- `classify-release-scope.sh` compares the production and target commits; a
  target containing only Markdown, `AGENTS.md`, or any `.gitignore` is marked
  `docs_only` and stops before Actions, GHCR verification, or publication.
- `wait-for-actions.sh` requires the complete Custom Release validation suite
  and returns one structured JSON result with concrete failed-check evidence.
- `verify-release-images.sh` checks public pull, `linux/amd64`, digest identity,
  and OCI revision/version/source labels for both images.
- `promote-release.sh` advances only the tested remote candidate after a base,
  canonical merge-parent, subject, and Stable baseline recheck. It does not move
  the local production source.
- `publish-custom.sh` is a deprecated fail-closed compatibility shim and is
  never a final release entry point.
- `bootstrap-custom-local.sh` owns secret-safe local custom setup and the
  explicit local Compose pair; it is not a production publisher.

## New Site And Migration

The supported custom-release bootstrap targets an empty Linux amd64 Docker
host. The checkout must be clean and exactly equal to `origin/custom-release`;
the input `.env` must be a regular mode-0600 file. Always run the non-mutating
preflight first:

```bash
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE --check-only
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE
```

`fresh` creates the full paired stack and initializes Official from
`stable-release-baseline.json`, Custom v1.0.0, and high-water zero. To preserve
an existing site's data, secrets, release history, and rollback evidence,
export the healthy source and restore only to an empty target:

```bash
deploy/ops/export-custom-site.sh \
  --output /root/sub2api-site-export --confirm EXPORT-SITE
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION --check-only
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION
```

`migrate` verifies the complete SHA256 manifest, both database archives,
ledger artifacts, Compose pair, secrets, public paired image digests, and OCI
identity before restoring. It preserves the exported dual-version display and
high-water. Both modes reject pre-existing target containers or named volumes,
start dependencies before application writers, and install the release watcher.
They do not configure DNS/CDN or external TLS routing. Subsequent update and
rollback prepares require explicit apply within one hour.

There is no daily updater, polling cron consumer, scheduled mode, or VPS-local
image build. The independent health-monitor schedule remains.

## Release Scenarios

For a new official stable Release, prepare validates the annotated tag, merges
only its peeled commit in a temporary worktree, waits for Actions and images,
rechecks the base, promotes `origin/custom-release`, and stops at `prepared`.
Only the separate apply action publishes.

When no new official Release exists but `origin/custom-release` has an
undeployed custom commit, prepare waits for that commit's Actions/images and
creates a manifest without a Release merge. Apply is still explicit. When
neither changed, the job returns `success` without pulling or recreating
services.

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

Before local source or Compose state changes, prepare verifies both
custom-format database dumps and backs up the official and custom Compose files,
`.env`, Nginx, certificate
and key files, old digests, rollback tags, container/image metadata, and
checksums. It never recreates or replaces `risk-control-postgres`.

The Web rollback list uses only complete ledger snapshots, excludes current,
and returns the newest three without Git or Docker probes. The selected target
does not become actionable until `prepare-rollback.sh` verifies its commit,
paired images, OCI revisions, exact Compose artifacts, backup directory, and
checksums. The prepared manifest expires after 60 minutes and requires a
separate administrator apply action.

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

Any failed deployment or full health gate enters `rolling_back` and performs an
automatic paired rollback: it restores the previous source, matching Compose/
`.env`, `SUB2API_IMAGE`, and `EXTENSIONS_SELF_IMAGE`, then recreates
`extensions-self` then `sub2api`, and reruns health checks. Database restore is
not automatic; `risk_control_db.dump` is used only for separately authorized
schema or data corruption recovery.

`release-state.json` is updated only after a healthy apply and records the
production commit, stable Release tag/commit, both digests, publication time,
and backup directory. Backfill starts only after health and only across the
`data-quality` `available_from/to` range.

## Host Retention And Maintenance

The full cleanup contract is defined in `deploy/RELEASE-RUNBOOK.md` under
"Host Artifact Retention And Cleanup". Maintenance is manually authorized and
must hold `/var/lock/sub2api-release.lock`; it stops when the ledger has an
active/prepared operation, the release service is running, or the production
worktree is dirty.

Keep the complete ledger and legacy compatibility audit. Retain the three
backup directories referenced by the newest successful releases, validate
their checksums, and protect the current image pair plus every main/extensions
pair and rollback tag recorded by those three snapshots. Retain the prepared
directories associated with those releases, the newest three conflict
diagnostics, and newest three host-script backups. Manual
backups may be reduced to the newest three only after unique or explicitly
pinned migration/emergency material has been excluded from deletion.

Never use `docker image prune -a`, `docker system prune`, or a volume prune.
Construct the protected image-ID set first, leave other applications out of
scope, validate every recursive target with `realpath`, and write a UTC
maintenance log with exact deletion targets and before/after storage. After
cleanup, rerun backup, digest, Compose, container, HTTP, and systemd checks.

## Installation

For the Web mount reduction, first deploy the transition release whose
`release-common.sh` accepts both exact legacy and reduced shapes, then back up
and synchronize that version into `/opt/sub2api-custom/`. Verify the installed
copy and a terminal ledger state before advancing the strict Stage B commit to
`origin/custom-release`. Stage B removes the three privileged mounts and
installs a validator that accepts only the reduced shape; reversing this order
causes the old host validator to reject the target before preparation.

Install scripts with mode `0755`, units with mode `0644`, then enable the path:

```bash
install -m 0755 deploy/ops/*.sh /opt/sub2api-custom/
install -m 0644 deploy/ops/actions-check-result.jq /opt/sub2api-custom/
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
