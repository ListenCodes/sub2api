# Risk Control Admin UI Consistency Design

## Scope

Align the three risk-control admin pages with the existing account-management
workspace without changing their API routes or product boundaries. The result
must remain compact on desktop and become readable without horizontal table
scrolling on a 390x844 mobile viewport.

## Design Direction

Use the repository's existing operational UI system. `AppHeader` owns the page
title and description; page content begins with actions and filters. Filters,
tables, pagination, dialogs, text fields, selects, toggles, and empty states use
the shared components already used by account management. No new palette,
typography, decoration, or marketing-style content is introduced.

## Page Structure

- User risk and audit pages use `TablePageLayout`: actions, filters, a
  `DataTable`, and shared pagination form one predictable work surface.
- Scenario rules use the same layout. Rule creation opens a wide
  `BaseDialog`; rule editing stays attached to the selected rule but uses the
  same field controls and spacing as creation.
- Desktop tables remain dense. `DataTable` renders labeled mobile cards below
  768px, preserving selection, sorting, row opening, and actions.

## Form Controls

- Text fields use `Input`; enumerations use `Select`; boolean settings use
  `Toggle`; reasons use `TextArea`; date filters use `DateRangePicker`.
- Rule templates are a compact segmented choice with an explicit selected
  state. Selecting one only pre-fills the form.
- Number fields keep native range semantics through the project's `.input`
  class because the shared `Input` does not expose `min`, `max`, or `step`.
- Ban confirmation uses the destructive button style. Unban and mark-processed
  confirmations use the primary style.

## Data Presentation Fixes

- Explain legacy reasons such as `rule=login_failure_burst count=9 window=300`
  in Chinese and map known rule codes to readable rule names.
- Preserve event IP and device identifiers in the API adapter and show distinct
  association values in the user drawer, in addition to counts.
- Prefer an audit actor account/name when supplied, with actor ID as fallback.
  Status transitions render only for account-status actions; rule operations
  show a neutral dash rather than `unknown -> unknown`.

## Accessibility And Responsive Behavior

- Shared dialogs provide dialog semantics, title association, Escape handling,
  focus entry/restoration, and body scroll locking.
- Sort state uses `DataTable`'s icon and `aria-sort` behavior.
- Mobile cards expose the same important data as desktop rows. Interactive
  checkboxes and buttons stop row-click propagation.
- No browser-default select, textarea, toggle, or rule form input remains in
  the risk-control surfaces.

## Verification

- Add focused unit tests for shared components, reason parsing, association
  preservation, actor-name preference, dialog semantics, and responsive table
  use before implementation.
- Run the risk-control view/API/utility tests, typecheck, build, lint check, and
  `git diff --check`.
- Verify all three pages at 1440x900 and 390x844 with mocked read-only fixtures,
  including create/edit, filters, selection, confirmations, drawer evidence,
  pagination, and console/network errors.
