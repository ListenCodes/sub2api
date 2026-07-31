# Release Notice, Failure Recovery, And Stable Merge Contract Design

**Date:** 2026-07-31

**Status:** Approved for implementation

## Goal

Correct the custom release experience and the Stable integration pipeline without
changing any official Stable-owned boundary. The implementation must:

- let an administrator acknowledge a documentation-only notice while keeping its
  content available;
- keep the collapsed badge amber for every real runtime update until a successful
  deployment makes `has_update=false`;
- recover a target-matching durable terminal failure after refresh, login, or use
  from another device;
- expose the exact required Actions check, conclusion, check URL, error code, and
  `production_changed=false` evidence instead of a generic Actions failure;
- create and validate the canonical Stable merge subject and parent structure;
- integrate official Stable `v0.1.169` at peeled commit
  `26d894ef4f50645a4bf1030e378ac892f17d0223`; and
- finish at tested and pushed `origin/custom-release` plus verified Actions and
  paired GHCR images, while production remains unchanged.

This design does not authorize an administrator update, a VPS deployment, host
script synchronization, or any `/opt/sub2api-custom` change.

## Existing Evidence And Root Causes

The release UI already separates `has_update` from `notice_unread`, but the
current acknowledgement policy applies to every fingerprint. Opening the menu
calls `markCurrentNoticeRead`, immediately clearing the amber presentation even
for an actionable runtime update.

The durable release operation already lives in the data-volume ledger. The
backend status endpoint accepts an optional job ID and, when omitted, reads the
server-side current operation pointer. This is sufficient to make the server the
cross-device authority. The frontend currently defeats that behavior in two
places: `finishUpdateFailure` deletes its local job accelerator, and
`resumeUpdatePolling` treats every terminal operation as disposable history.

The v0.1.169 integration attempts have the correct custom first side and official
second parent, but `sync-upstream.sh` used `git merge --no-ff --no-edit` against a
raw commit. Git therefore generated a branch-specific subject such as
`Merge commit '<sha>' into integration/...`. The isolation contract searches
first-parent history only for `merge: integrate stable Release vX.Y.Z`, so it
continued to pair the v0.1.169 baseline with the older v0.1.168 merge and failed
the deployment job. Images were correctly skipped after that required check
failed, and production stayed at its previous commit.

The Actions waiter currently collapses every required-check failure to a nonzero
exit and a generic message. `prepare-release.sh` consequently persists only
`ACTIONS_FAILED`; it discards the failing check name, conclusion, and URL even
though the durable job schema already carries workflow and failure metadata.

## Chosen Architecture

Keep the existing custom-owned release feature, durable filesystem ledger, and
two-phase host pipeline. Refine their contracts rather than adding a notification
system, database table, or second job store.

The implementation is divided into four bounded units:

1. Frontend attention policy and terminal recovery in
   `frontend/src/features/custom-release/`.
2. Additive durable job evidence fields in the custom release backend API.
3. Structured Actions evidence and persistence in versioned host scripts.
4. Canonical Stable merge creation and promotion validation in versioned host
   scripts and deploy contracts.

The following remain unchanged:

- `frontend/src/components/common/VersionBadge.vue`;
- `frontend/src/router/index.ts`;
- the official user model and user table;
- Wire providers and generated Wire files;
- every Stable zero-overlap file named by `AGENTS.md`; and
- production containers, data, systemd units, and installed host scripts.

### Alternatives Rejected

1. Keep acknowledgement for every update and periodically re-fetch it. This
   produces visible amber flicker and still permits a real update to appear read.
2. Store terminal failures only in browser storage. It cannot recover across
   devices and creates a second, weaker job ledger.
3. Add a database notification model. The existing release operation file and
   current-operation pointer already provide durable identity and evidence.
4. Loosen the isolation test to recognize Git's generic merge title. A generic
   title is branch-dependent and loses the explicit Stable integration contract.

## Attention Policy

The UI derives collapsed-badge attention from the update class:

```text
docs-only: notice_unread
official/custom/combined runtime update: has_update
none or incomplete detection: false
```

`has_update` remains the functional truth. For a runtime update, opening or
closing the menu, refreshing the page, acknowledging a notice endpoint, logging
out, or changing devices cannot neutralize the amber background, dot, ping, or
accessible update title. Only a successful deployment followed by detection with
`has_update=false` clears runtime attention.

A docs-only target keeps the existing per-admin fingerprint acknowledgement.
Opening the menu may best-effort mark that fingerprint read. The collapsed badge
then becomes neutral, but the docs-only target, release link, and explanatory
content remain visible whenever the menu is opened. A new docs-only fingerprint
is unread again.

The frontend calls the read endpoint only for `update_kind=docs-only`. Prepare,
apply, and rollback operations do not acknowledge a runtime target because
runtime attention is not advisory.

## Durable Failure Recovery

Browser local storage remains an accelerator containing only a job ID. It is not
the authority and does not contain a copied failure record.

On administrator mount, recovery performs these steps:

1. Fetch update detection and current release identity.
2. If local storage contains a job ID, query that exact durable operation first.
3. Query the status endpoint without a job ID to obtain the server's current
   durable operation when local storage is empty, stale, missing, or older.
4. Prefer the server operation when it has a later `updated_at`, or when the local
   operation is unavailable.
5. Resume nonterminal and prepared operations through existing polling.
6. Restore a terminal update failure only when its locked target matches the
   currently detected update target.

Target matching uses server-owned identities, not error text or job ID:

- Stable tag and commit must match `target_official_version` and
  `target_official_commit` when the official side changes;
- `target_custom_commit` must match the detected target Custom commit when the
  custom side changes; and
- the job must be an update operation, not a rollback operation.

An old failure cannot cover a new target. When any locked target identity differs,
the frontend treats the old operation as history, removes only the stale browser
accelerator, clears the old failure presentation, and shows the new update.

Restorable failure statuses are `failed`, `conflict`, `drifted`,
`failed_rolled_back`, and `rollback_failed`. `expired` returns to preparation for
the same target. `success` clears the accelerator and refreshes detection/current
release; it is not replayed as a failure card.

Closing the menu only hides it. Opening the menu does not reset a restored failure.
There is no ignore or mark-read action for a runtime failure. The failure is
cleared only by a successful retry, a successful update, or replacement by a new
target identity.

The failure action is labeled `Retry preparation` / `重试准备`. It creates a new
prepare operation and replaces the local accelerator with the returned job ID.
It must not look like a first-time `Download update` action.

## Failure Evidence Contract

The durable `UpdateJob` JSON and API add optional fields:

```json
{
  "failed_check": "deployment",
  "check_url": "https://github.com/.../actions/runs/.../job/...",
  "conclusion": "failure"
}
```

Existing `message`, `error_code`, `workflow_url`, and `production_changed` fields
remain authoritative. For a required Actions failure, the operation records:

- `message`: a specific human-readable statement naming the required check and
  conclusion;
- `error_code`: a stable code distinguishing failed, cancelled, skipped, missing,
  malformed, API, and timeout evidence where applicable;
- `failed_check`: the exact required check name;
- `conclusion`: GitHub's terminal conclusion or `missing`;
- `check_url`: the exact check-run URL when supplied by GitHub;
- `workflow_url`: the best available Actions/check URL; and
- `published=false`, `production_changed=false`.

The failure card renders the message without truncating it, the required check,
error code, conclusion, a safe external Actions link when present, and an explicit
`production_changed=false` statement. Missing optional evidence is omitted rather
than invented.

`wait-for-actions.sh` emits exactly one JSON evidence object on stdout. A success
object contains `ok=true` and a nonempty `workflow_url`. A terminal failure emits
`ok=false` plus the fields above and exits nonzero. Live queued/in-progress checks
continue polling until completion or timeout. Fixture mode fails immediately for
missing or incomplete evidence so tests do not sleep.

`prepare-release.sh` captures and validates the JSON on both success and failure.
It merges validated failure evidence into the ledger settlement instead of
replacing it with generic `ACTIONS_FAILED`. Malformed waiter output fails closed
with `ACTIONS_EVIDENCE_INVALID` and still records `production_changed=false`.

## Canonical Stable Merge Contract

`sync-upstream.sh` creates the Stable merge with the explicit subject:

```text
merge: integrate stable Release <release-tag>
```

The merge must have exactly two parents. Its first parent is the validated custom
candidate lineage; its second parent is the exact peeled annotated Release commit.
The baseline metadata commit may follow the merge on first-parent history.

Before pushing an integration candidate, the script validates:

- the exact canonical subject;
- two-parent shape;
- first parent is on the approved custom base lineage;
- second parent equals the resolver's peeled Release commit; and
- `stable-release-baseline.json` contains the same tag, tag object, peeled commit,
  and publication timestamp.

`promote-release.sh` repeats fail-closed validation against the remote integration
target before changing `origin/custom-release`. It finds the canonical merge in
the target's first-parent history, verifies the baseline tag against its subject,
verifies the second parent against the baseline commit, and verifies the merge's
first parent descends from the approved base. The remote update must be a normal
fast-forward; force push and history rewriting remain prohibited.

For this task, the accepted final history contains a first-parent canonical
`v0.1.169` merge whose second parent is
`26d894ef4f50645a4bf1030e378ac892f17d0223`, followed by baseline metadata for
that same Release. The two existing generic-title integration candidates are not
promotion sources.

## Testing Strategy

### Frontend

Vitest covers:

- docs-only opens, is acknowledged per fingerprint, loses amber attention, and
  keeps content visible;
- official, custom, and combined runtime updates stay amber after open, close,
  refresh, remount, and acknowledgement attempts;
- a failure keeps its local accelerator and restores from the exact job;
- recovery without local storage uses the server current durable job;
- a newer server operation supersedes an older local accelerator;
- target-matching failure survives remount/login/device simulation;
- an old target failure cannot obscure a new fingerprint/identity;
- close/reopen does not clear failure evidence;
- retry creates a new prepare job and replaces the accelerator;
- success clears the accelerator and runtime attention after fresh detection; and
- the card renders failed check, conclusion, error code, Actions URL, and
  `production_changed=false`.

### Backend

Focused Go tests cover JSON/API preservation of `failed_check`, `check_url`, and
`conclusion`, plus existing validation of durable operation kind, target identity,
and server current-job lookup. No database migration or second recovery endpoint
is added.

### Deploy

Fixture tests cover all seven required checks and these outcomes:

- all checks complete successfully, with a valid images workflow URL;
- `failure`, `cancelled`, and `skipped` on each representative required check;
- missing required check;
- incomplete fixture check;
- malformed API response;
- live timeout evidence;
- images check failure or missing URL; and
- prepare settlement preserves the exact structured evidence and
  `production_changed=false`.

Git contract tests cover canonical subject, first-parent lineage, exact second
parent, baseline identity, candidate rejection for a generic subject, and guarded
fast-forward promotion.

### Full Verification

Run focused and full frontend tests, typecheck, production build, focused and full
backend tests, deploy Node contracts, release pipeline fixtures, shell syntax,
Stable overlap budget, `git diff --check`, and an independent code review. The
final pushed commit must pass every required Custom Release job and publish both
public full-SHA GHCR images.

## Documentation

Update repository operating documentation to state:

- only docs-only targets use per-admin read acknowledgement;
- runtime attention remains until `has_update=false`;
- target-matching durable failures are restored from the server across devices;
- Actions failures expose exact check evidence and confirm production unchanged;
- Stable integration uses the canonical first-parent merge contract; and
- code, push, Actions/GHCR, and production deployment are separate facts.

No operating document records a secret, a mutable image tag as production truth,
or an authorization to deploy this change.

## Acceptance Criteria

- Docs-only acknowledgement changes only its presentation state and never hides
  update content.
- Every real runtime update remains visibly amber until successful deployment
  causes a fresh `has_update=false` result.
- A matching failed durable job and all specific evidence survive refresh, login,
  and device changes; menu close/open does not clear them.
- A new target replaces old failure state, and retry success clears it.
- The UI exposes the exact failed required check, conclusion, error code, Actions
  URL, and `production_changed=false` when available.
- Stable v0.1.169 is represented by the canonical first-parent merge and exact
  baseline identity, and `origin/custom-release` advances only by fast-forward.
- Stable-owned zero-overlap files remain byte-equivalent to v0.1.169 baseline.
- Local tests, independent review, pushed Actions, and paired GHCR verification
  pass.
- Production commit, containers, installed host scripts, and `/opt` remain
  unchanged because no administrator release was authorized.
