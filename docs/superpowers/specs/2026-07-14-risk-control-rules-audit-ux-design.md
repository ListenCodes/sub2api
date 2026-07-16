# Risk Control Rules And Audit UX Design

## Goal

Bring the scenario-rules and operation-audit pages in line with the compact administrative style already used by the risk-user and account-management pages.

## Audit Page

- Replace the filter card and explicit `common.apply` button with a flat, bordered toolbar.
- Use `SearchInput` for administrator and target searches, project `Select` controls for action and result, a compact date-range control, and an icon-only reset action.
- Debounced text search, select changes, and date changes trigger a real server request and reset pagination to page one.
- Replace the custom previous/next footer with the shared `Pagination` component, including persisted page-size selection.
- Keep server-side sorting and the existing audit table contract unchanged.

## Rules Page

- Replace one-card-per-rule rendering with a single full-width, horizontally scrollable table.
- Show rule identity, event type, enabled state, threshold/window, risk score/level, action, and an edit command in each summary row.
- Expand one row at a time for editing, saving, revision-conflict recovery, and rule testing. Existing API calls and validation remain unchanged.
- Keep rule creation in the page, but restyle it as a compact full-width work panel rather than a decorative card.
- Preserve responsive behavior through a stable minimum table width and internal horizontal scrolling.

## Verification

- Add focused component tests proving the audit page has no Apply button, filters automatically query, page size is selectable, and reset clears filters.
- Add focused component tests proving rules render in a table and editing expands only after the edit command.
- Run only the two related Vitest files, frontend type checking, and desktop/mobile browser smoke checks.
