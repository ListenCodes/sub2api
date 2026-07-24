# One-Hour Release Confirmation And Site Bootstrap Design

## Goal

Extend prepared update and rollback confirmation from 15 minutes to 1 hour, and
provide a safe Docker-based entry point for deploying a fresh custom site or
restoring a complete existing-site backup.

## Confirmation lifetime

Both update and rollback preparation manifests expire exactly 60 minutes after
`prepared_at`. The server-provided `expires_at` remains authoritative; apply
continues to fail closed on expiry or drift. Tests must prove both prepare
scripts create a 60-minute window and that expired manifests are still rejected.
All operator-facing release documents must say one hour.

## Bootstrap command

Add `deploy/ops/bootstrap-custom-site.sh` with two explicit modes:

- `fresh`: deploy a new empty site from a clean, exact `origin/custom-release`
  commit and two immutable GHCR digest references. The initial healthy snapshot
  is recorded using the verified tag and commit from
  `deploy/stable-release-baseline.json` as Official, and Custom `v1.0.0` with
  custom high-water `0` for that independent site.
- `migrate`: restore a complete, checksum-verified release bundle containing
  both database dumps, the matching Compose pair, `.env`, release ledger,
  release-state projection, Nginx/certificate material, image metadata, and
  rollback evidence. It preserves the migrated site's dual-version history and
  high-water.

Add `deploy/ops/export-custom-site.sh` to create the migration bundle consumed
by `migrate`. It takes fresh dumps of both databases and copies the current
ledger, release-state projection, rollback artifacts, Compose pair, `.env`,
Nginx/certificates, and image metadata into one checksummed directory.

The operator runs the versioned script from an already-cloned
`origin/custom-release` checkout. A secrets file with mode `0600` is supplied by
path; secrets are never accepted as command-line values or written to Git.

## Safety boundary

The installer performs preflight validation before changing runtime state:

- Linux root, Docker with Compose, Git, `jq`, `curl`, `sha256sum`, PostgreSQL
  restore tools, clean checkout, exact branch and commit identity;
- anonymous validation of both public digest-pinned images, `linux/amd64`, and
  matching OCI revision/version/source labels;
- explicit base-plus-custom Compose rendering using project name `deploy`;
- required `.env` values and file permissions;
- empty target databases for `fresh`, or an explicit destructive-restore
  confirmation plus complete checksummed bundle for `migrate`;
- no active release operation and no existing site unless the selected mode
  permits it.

Fresh initialization is a distinct ledger bootstrap path and does not weaken
the existing fixed-identity production migration script. Migration copies
release artifacts under the standard named-volume artifact root so historical
backup paths remain canonical and verifiable on the new host.

After validation it starts dependencies, then `extensions-self`, then
`sub2api`, performs the same internal and public health gates used by releases,
creates or restores the ledger, installs `/opt/sub2api-custom` scripts and the
systemd path watcher, and emits a deployment report. Both modes require an empty
target host. On failure, only containers, files, and named volumes created by
that exact bootstrap attempt may be removed; existing resources are never
deleted or replaced. It never recreates an existing
`risk-control-postgres`, restores a database in ordinary release rollback,
uses mutable application tags, or edits a running container.

DNS records, external CDN configuration, and certificate issuance remain
operator-controlled because they depend on credentials outside the repository.
The script can install supplied Nginx/certificate files but cannot change DNS.

## Testing

Node contract tests inspect the bootstrap and export scripts for fail-closed mode parsing,
digest-only images, explicit Compose pairing, secret-file checks, restore
confirmation, ledger initialization/restoration, safe service order, systemd
installation, and prohibition of `docker compose down`, volume deletion,
mutable tags, broad prune/delete operations, and replacement of pre-existing
databases. Shell syntax and fixture-driven
dry runs cover fresh and migrate preflight without touching Docker or production.
The existing deployment contract suite remains green.

## Documentation

Update `deploy/README.md`, `deploy/ops/README.md`,
`deploy/RELEASE-RUNBOOK.md`, and `docs/SUB2API-CUSTOM-OPERATIONS.md` with:

- the one-hour confirmation window;
- the fresh and migrate commands and required input bundle;
- the boundary between Docker bootstrap and external DNS/TLS work;
- the normal later update flow through prepare and explicit apply.
