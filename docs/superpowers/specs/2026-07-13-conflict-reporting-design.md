# Upstream Conflict Reporting Design

**Goal:** Make upstream sync failures actionable by attempting normal Git
auto-merges, preserving conflict evidence, and showing the exact conflict
files in the admin update panel without changing production.

## Design

The existing normal three-way merge remains the automatic resolution mechanism
for non-overlapping changes. The system must not use `-X ours` or `-X theirs`,
because those strategies can silently remove risk middleware or upstream
features.

When Git reports unresolved files, `sync-upstream.sh` will:

1. Collect the unmerged file list, current `origin/custom` base, and
   `upstream/main` commit.
2. Save a conflict snapshot under the configured data directory, including
   the merge status, unmerged index stages, combined diff, and a metadata file.
3. Abort the temporary merge and leave `custom`, production, and
   `origin/custom` unchanged.
4. Write structured conflict fields to `sync-status` and a concise terminal
   result for the admin trigger.

The update API will expose the conflict fields unchanged. The admin panel will
render a dedicated conflict failure state with the file list, a clear
production-unchanged message, and the diagnostic artifact path. Generic
publish failures continue to use the existing failure state.

## Status Contract

```json
{
  "conflict_files": ["path/to/file"],
  "conflict_base": "custom commit",
  "conflict_upstream": "upstream commit",
  "conflict_log": "/app/data/sync-conflicts/<job>/metadata.json",
  "resolution_hint": "Resolve the listed files, merge upstream/main into custom, test, and retry."
}
```

All conflict fields are optional and empty for normal running, success, and
non-conflict failures.

## Verification

- Shell contract tests assert conflict metadata and artifact capture.
- Go tests verify the status decoder and API response metadata.
- Frontend tests verify the conflict failure state renders the file list and
  does not present a restart or publish-success action.
- Existing frontend, backend, risk-control, and build checks remain required.
