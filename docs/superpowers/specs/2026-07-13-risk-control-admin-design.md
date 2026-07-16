# Risk Control Admin Design

## Goal

Complete the existing three-page Sub2API risk-control admin surface: user risk, scenario rules, and operation audit. The pages must use understandable Chinese labels while preserving stable English API values, and all account actions and rule changes must remain auditable.

## Architecture

Sub2API remains authoritative for administrator authentication and account status. Its same-origin admin proxy forwards an allowlisted risk-control API to the independent Go service. The risk service owns risk subjects, events, rules, and audit records, but never writes the main user table.

The user page combines the main admin user list with risk-service signals through the existing API adapter. Main-user search/status/created-time sorting is forwarded to `/admin/users`; risk filters and risk-field sorting fetch the required candidate set and apply deterministic full-result sorting before pagination. Account actions call the existing main-app status endpoint, with a bounded client-side batch runner for multi-user actions.

## User Risk Flow

- A shared label utility maps risk types, levels, actions, account statuses, audit results, and reason text. Unknown values render as `未知类型（原始值）` or the equivalent category-specific fallback.
- The table shows account identity, user ID, Chinese status, Chinese risk type/level, score, readable reason, event/IP/device counts, latest event, last action, and processing status.
- Search and filters retain internal values in API requests. Sort headers toggle ascending/descending and either pass supported main-user sort parameters or trigger full candidate retrieval for risk fields.
- Current-page checkboxes support individual selection, select-all, clear-all, and a batch toolbar. Ban/unban actions require a confirmation dialog and trimmed non-empty reason. The bounded runner returns one result per target, preserves failure details, refreshes data, and clears selection.
- The drawer shows identity, status, risk summary, readable reason, evidence counts, event timeline, error/rule evidence, and audit history, plus the existing single-user status action.

## Rule Flow

The Go service adds `POST /api/v1/admin/rules`. A shared validator checks safe rule codes, required name/description/event type, positive window and threshold, score range, known risk level, and known action. `MemoryRepository`, `SQLRepository`, and the `RiskRepository` interface expose `CreateRule`; SQL relies on the existing unique code constraint and maps duplicate violations to a conflict response. Create and update operations record administrator audit entries, with optional operator reason and revision metadata. Updates retain optimistic revision checks.

The rules page has a create form with the seven required templates, editable rule fields, enable/disable controls, revision-aware save behavior, and a test form that displays match state, score, level, action, and matched conditions. No delete operation is added.

## Audit Flow

Audit records expose actor ID, time, action, target, before/after status, reason, result, failure detail, request/batch identifier, and metadata. The list endpoint accepts administrator, target, action, result, date range, sort field/order, and pagination parameters. Batch account actions send a stable batch identifier and the main application records an independent audit request for each target, including failure records.

## Error Handling and Security

The proxy allowlist includes only the three-page read paths, rule create/update/test paths, and existing admin user status path. The risk service continues to require signed internal requests and an actor ID for admin paths. Validation errors return 400, duplicate rule codes return 409, revision conflicts return 409, and upstream error bodies are preserved by the proxy. No secrets or raw sensitive hashes are displayed as primary admin text.

## Testing and Verification

Tests are added before implementation for label fallbacks, API query/sort/create payloads, user selection and batch result handling, rule validation/create/revision behavior, proxy allowlisting, and audit field mapping. The full Go suite and focused frontend tests are followed by frontend typecheck, build, lint, diff checks, and authenticated local browser smoke tests at 1440x900 and 390x844. No production deployment is performed.
