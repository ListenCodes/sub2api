# Sub2API Agent Rules

This file is the repository-level operating contract for all agents and
development conversations working in this repository.

## Canonical Locations

- Local source of truth: `E:\Code\sub2api`
- Fork remote: `origin` -> `ListenCodes/sub2api`
- Upstream remote: `upstream` -> `Wei-Shaw/sub2api`
- Integration branch: `custom`
- Production main source tree: `/root/sub2api`
- Production main image: `sub2api:custom`
- Versioned operations scripts: `deploy/ops/`
- Custom extensions source: `/root/sub2api/extensions-self`
- Extensions container and network hostname: `extensions-self`
- Extensions image: `deploy-extensions-self`

Risk control, account monitoring, and the custom homepage are versioned under `extensions-self/` and
released in one container from the same approved `origin/custom` commit as the
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
   `custom` as a personal working branch.
3. Use `upstream` only to fetch and integrate upstream changes locally.
4. Resolve conflicts locally, run the relevant tests, then merge or fast-forward
   the validated result into `custom` and push it to `origin/custom`.
5. Never use `git reset --hard`, force-push, or discard another agent's changes
   without explicit user authorization.
6. A dirty worktree is not an acceptable production deployment source. Stop and
   report the files before continuing.

## Normal Change Workflow

1. Inspect `git status`, branch, remotes, and the current production commit.
2. Create or use an isolated feature branch/worktree.
3. Implement the change and add focused tests.
4. Run the required backend, frontend, extensions-self, and deployment checks.
5. Review the diff and `git diff --check`.
6. Commit to the feature branch, merge into `custom`, and push `origin/custom`.
7. Do not publish to production automatically after a code task. Production
   publishing requires explicit user authorization.

## VPS Fallback Workflow

The VPS remains an approved emergency development and publishing channel, but
every emergency change must be traceable and recoverable.

1. Connect and execute remote commands only through `ssh-skill`.
2. Start from the deployed commit in `/root/sub2api`.
3. Create an `emergency/vps-YYYYMMDD` branch before editing.
4. Make the smallest possible change, test it, and build `sub2api:custom`.
5. Run the health check before declaring success.
6. Commit and push the emergency branch or approved commit to `origin`.
7. Reconcile the change into the local `custom` branch at the next opportunity.

Do not edit a running container, edit a generated image, or leave uncommitted
production changes as the only copy of a fix.

## Deployment Safety

Every production deployment must:

- Record the source commit and image tags.
- Back up the PostgreSQL database, Compose/configuration, and Nginx vhost.
- Back up `risk-control-postgres` before publishing extensions schema or account-monitor changes.
- Build and deploy the exact intended image tag.
- Check application, extensions-self, PostgreSQL, Redis, and public HTTP health.
- Keep a previous image and configuration available for rollback.
- Avoid touching PostgreSQL and Redis unless the change explicitly requires it.

The main Compose file must build and deploy the same tag: `sub2api:custom`.
Do not reintroduce date-specific application tags in production Compose.

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

The admin upstream action and the scheduled upstream job use the unified
`sync-and-publish.sh` flow. It fetches `upstream/main`, tests a merge in a
temporary worktree, and pushes `origin/integration/upstream-*`. When the merge
is conflict-free and the recorded `origin/custom` base has not changed, the
flow may fast-forward `custom` to that integration branch, push `origin/custom`,
and invoke `publish-custom.sh` for production.

Any merge conflict, changed custom base, dirty VPS tree, failed push, failed
backup, failed build, or failed health check stops the flow without publishing.
The integration branch and rollback artifacts remain available for manual
resolution. No step may use a rebase, force-push, or an arbitrary commit.

For merge conflicts, `sync-upstream.sh` also records the conflicted files,
both commit IDs, a resolution hint, and a diagnostic snapshot under the sync
data directory. The admin update panel must expose these details and state
that production was not changed. Never hide a conflict behind a generic
failure message or resolve it with a blanket `ours`/`theirs` strategy.

`publish-custom.sh` remains the only production build/deploy entrypoint. It
accepts only the exact approved `origin/custom` commit and must not fetch or
merge `upstream/main` during a release.
