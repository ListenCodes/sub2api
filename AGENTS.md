# Sub2API Agent Rules

This file is the repository-level operating contract for all agents and
development conversations working in this repository.

For the Chinese project overview and the repeatable developer/operator release
workflow, read `docs/SUB2API-CUSTOM-OPERATIONS.md`. This file remains the
authoritative repository rule set when the overview and implementation details
differ.

## Canonical Locations

- Local source of truth: `E:\Code\sub2api`
- Fork remote: `origin` -> `ListenCodes/sub2api`
- Upstream remote: `upstream` -> `Wei-Shaw/sub2api`
- Production-approved branch: `custom-release`
- Legacy compatibility branch: `custom` (history and `upstream/main` testing only)
- Production main source tree: `/root/sub2api`
- Production main image: `ghcr.io/listencodes/sub2api-custom@sha256:<digest>`
- Versioned operations scripts: `deploy/ops/`
- Custom extensions source: `/root/sub2api/extensions-self`
- Extensions container and network hostname: `extensions-self`
- Extensions image: `ghcr.io/listencodes/sub2api-extensions@sha256:<digest>`

Risk control, account monitoring, and the custom homepage are versioned under `extensions-self/` and
released in one container from the same approved `origin/custom-release` commit as the
main application. The dedicated `risk-control-postgres` service and volume stay
independent. Production secrets remain in `deploy/.env` and must never be
committed.

## Account-Monitor Contract

Account-monitor collection, aggregation, anomaly rules, and APIs belong to
`extensions-self/account-monitor`; native page components belong to the main frontend. Official Sub2API code is
limited to exact failed-attempt model attribution, authenticated/signed proxy
routes, and the native `/admin/extensions/account-monitor` and group-monitor pages.

The monitor may read the main database only through `extensions_self_ro` views
using the dedicated `extensions_self_monitor` login, which inherits the
`extensions_self_monitor_ro` NOLOGIN role. Never reuse the main database owner
or expose account credentials, full API keys, request bodies, or headers. Facts,
aggregates, cursors, thresholds, and rebuild jobs live in
`risk-control-postgres`; publishing must back up that database without
recreating or deleting its container/volume.

Account and group monitoring share `account_monitor_request_facts`. The final result owns
`group_id` and the actual model; ungrouped requests remain visible only in data quality.
Group cards read only the mirrored group dimension and complete 10-minute aggregates.
Historical rebuilds use `deploy/ops/backfill-account-monitor.sh` in non-overlapping segments
of at most 31 days and record every job in the matching release backup directory.

Read `extensions-self/account-monitor/README.md`,
`docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`, and
`docs/ACCOUNT-MONITOR-CHECKLIST.md` before changing monitor semantics or
deployment. Code completion is not a production release; report implementation,
merge/push, backup, deployment, reconciliation, and rollback separately.

## Risk-Control Admin Product Contract

The risk-control admin surface is intentionally limited to three pages:

- user risk: all users, risk events, evidence, filtering, sorting, and account actions;
- scenario rules: create, edit, enable/disable, and test rules;
- operation audit: administrator, target, reason, result, failure detail, filtering, and sorting.

The implementation contract and current gaps are recorded in
`docs/RISK-CONTROL-ADMIN-SPEC.md`. Every agent working on this feature must
read that document before editing. Internal enum values such as
`login_failure`, `critical`, and `reject_candidate` are API values only; the
admin UI must render understandable Chinese labels and reasons. A code change
is not complete when it only adds page skeletons or static buttons: the user,
rule, and audit flows must be executable and covered by tests.

The first release must support current-page selection and batch account
actions with required reasons, rule creation with duplicate/validation checks,
sortable tables, and per-target audit results. Automatic enforcement remains
off or in review/shadow mode until real events, identities, reasons, and audit
records have been verified.

## Source And Branch Rules

1. Read this file and `deploy/RELEASE-RUNBOOK.md` before changing code or
   deployment files.
2. Use a feature branch or an isolated worktree for feature work. Do not use
   `custom-release` as a personal working branch.
3. Use `upstream` only to fetch and integrate upstream changes locally.
4. Resolve conflicts locally, run the relevant tests, then merge or fast-forward
   the validated result into `custom-release` and push it to `origin/custom-release`.
   The legacy `custom` branch cannot be auto-published.
5. Never use `git reset --hard`, force-push, or discard another agent's changes
   without explicit user authorization.
6. A dirty worktree is not an acceptable production deployment source. Stop and
   report the files before continuing.
7. After a custom feature is stable in production, it may be selectively
   `cherry-pick -x` into `custom` for `upstream/main` compatibility testing. The
   entire `custom` branch is never merged back into `custom-release`.

## Normal Change Workflow

1. Inspect `git status`, branch, remotes, and the current production commit.
2. Create or use an isolated feature branch/worktree.
3. Implement the change and add focused tests.
4. Run the required backend, frontend, extensions-self, and deployment checks.
5. Review the diff and `git diff --check`.
6. Commit to the feature branch, merge into `custom-release`, and push
   `origin/custom-release`.
7. Wait for the Custom Release Actions workflow and both public GHCR images:
   `ghcr.io/listencodes/sub2api-custom:custom-<full-sha>` and
   `ghcr.io/listencodes/sub2api-extensions:custom-<full-sha>`.
8. Production changes only after an administrator explicitly uses the update
   button. The host `sub2api-release.path` unit starts the durable release job.

## VPS Fallback Workflow

The VPS remains an approved emergency development and publishing channel, but
every emergency change must be traceable and recoverable.

1. Connect and execute remote commands only through `ssh-skill`.
2. Start from the deployed commit in `/root/sub2api`.
3. Create an `emergency/vps-YYYYMMDD` branch before editing.
4. Make the smallest possible change and run focused tests.
5. Commit and push the emergency branch; reconcile it into `custom-release`
   without rewriting history and wait for Actions plus both GHCR images.
6. Use the same administrator-triggered digest release path and health gates.
7. Reconcile the change into the local development worktree at the next opportunity.

Do not edit a running container, edit a generated image, or leave uncommitted
production changes as the only copy of a fix.

## Deployment Safety

Every production deployment must:

- Record the source commit and both immutable image digests.
- Back up the PostgreSQL database, Compose/configuration, and Nginx vhost.
- Back up `risk-control-postgres` before publishing extensions schema or account-monitor changes.
- Verify OCI revision/version/source labels and `linux/amd64`, then deploy the
  exact `SUB2API_IMAGE` and `EXTENSIONS_SELF_IMAGE` digest references.
- Check application, extensions-self, PostgreSQL, Redis, and public HTTP health.
- Keep the previous digest pair, rollback tags, and matching configuration.
- Automatically roll back both application services after a failed deployment
  or health gate; database restore is never automatic.
- Avoid touching PostgreSQL and Redis unless the change explicitly requires it.

The production Compose file requires `SUB2API_IMAGE` and
`EXTENSIONS_SELF_IMAGE`; both values are `ghcr.io/...@sha256:...`. Do not add a
production build context or a mutable application tag.

## Agent Coordination

Conversations do not share memory. Shared files, Git history, and these rules
are the coordination mechanism.

- Read `AGENTS.md` and `deploy/RELEASE-RUNBOOK.md` at the start of every task.
- Inspect the worktree before editing and preserve unrelated changes.
- Do not work concurrently in the same worktree unless the user explicitly
  coordinates it.
- A code change is not a deployment. Report the commit, tests, and deployment
  status separately.
- If the task says "暂不修改" or does not authorize a release, do not deploy.
- For remote work, also follow `E:\BaiduSyncdisk\Private\VPS\AGENTS.md`.

## Release Boundary

The administrator update action is the only release trigger. It atomically
writes `release-trigger`; `sub2api-release.path` starts
`sub2api-release.service`, which invokes `sync-and-publish.sh`. The flow resolves only the latest non-draft,
non-prerelease GitHub Release, verifies the tag object, fetches that exact tag,
peels its commit, and tests the merge in a temporary worktree. It pushes only
`origin/integration/release-*`. It waits for Actions and the two commit-addressed
GHCR images before guarded promotion of `origin/custom-release`. If there is no
new official Release, an undeployed custom-release commit is still publishable;
if neither source nor production changed, the job ends without recreating containers.

Any merge conflict, changed custom-release base, dirty VPS tree, failed push,
failed Actions/image validation, failed backup, or failed health check stops the flow.
The integration branch and rollback artifacts remain available for manual
resolution. No step may use a rebase, force-push, or an arbitrary commit.

For merge conflicts, `sync-upstream.sh` also records the conflicted files,
the approved branch base, verified Release identity, a resolution hint, and a diagnostic snapshot under the sync
data directory. The admin update panel must expose these details and state
that production was not changed. Never hide a conflict behind a generic
failure message or resolve it with a blanket `ours`/`theirs` strategy.

`publish-custom.sh` is the internal digest deployment entrypoint. It accepts
only the exact approved `origin/custom-release` commit and verified digests,
backs up before the local source fast-forward, deploys extensions before main,
and automatically restores the previous pair after a failed health gate. It
must not build images, fetch or merge `upstream/main`, recreate
`risk-control-postgres`, or automatically restore either database.

Implementation, local tests, branch push, Actions/GHCR results, production
backup, deployment, health, scheduled-update removal, and rollback evidence are
separate facts and must be reported separately.
