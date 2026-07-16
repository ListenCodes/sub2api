# Risk Control Admin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the three executable Chinese-language risk-control admin pages with auditable user actions, rule lifecycle management, and sortable/filterable operation history.

**Architecture:** Keep account identity and status in Sub2API, risk events/rules/audits in the independent Go service, and expose the service only through the authenticated same-origin proxy. The existing frontend adapter composes main-user rows with risk signals; risk-aware filters and sorts operate on the complete candidate set before pagination. Batch status actions call the authoritative single-user endpoint with a bounded concurrency runner and batch identifiers.

**Tech Stack:** Go `net/http`, PostgreSQL, Gin admin handlers, Vue 3 `<script setup>`, TypeScript, Tailwind, Vitest, Vue Test Utils, Vite.

---

### Task 1: Shared labels and API contracts

**Files:**
- Create: `frontend/src/utils/userRiskControlLabels.ts`
- Create: `frontend/src/utils/__tests__/userRiskControlLabels.spec.ts`
- Modify: `frontend/src/api/admin/userRiskControlV2.ts`
- Modify: `frontend/src/api/admin/__tests__/userRiskControlV2.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/userRiskControl.ts`
- Modify: `frontend/src/i18n/locales/en/admin/userRiskControl.ts`

- [ ] **Step 1: Write failing label tests**

  Cover all required known values and an unknown value. Assert `formatRiskType('login_failure')` is `登录失败`, `formatRiskLevel('critical')` is `严重风险`, `formatRiskAction('reject_candidate')` is `拒绝注册`, `formatAccountStatus('disabled')` is `已封禁`, `formatAuditResult('partial')` is `部分成功`, and an unknown risk type contains both `未知类型` and the original value. Add a reason test that turns a rule hit with count/window into readable Chinese text.

- [ ] **Step 2: Run the new test and verify it fails for missing exports**

  Run `pnpm --dir frontend exec vitest run src/utils/__tests__/userRiskControlLabels.spec.ts`; expect module/export failures.

- [ ] **Step 3: Implement the shared mapping module**

  Export typed option arrays for risk types, levels, actions, account statuses, processing states, and audit results. Export `formatRiskType`, `formatRiskLevel`, `formatRiskAction`, `formatAccountStatus`, `formatAuditResult`, and `formatRiskReason`. Unknown values must use category-specific Chinese fallback text with the raw value. Keep raw enum values for API requests only.

- [ ] **Step 4: Extend API types and request helpers**

  Add `event_count`, `ip_count`, `device_count`, `last_event_at`, `created_at`, `processing_status`, `sort_by`, `sort_order`, score/date filters, audit actor/date/sort fields, `failure_reason`, `batch_id`, rule description/event types, and the `auto_ban` action. Add `createRule` and a bounded `batchSetUserStatus` helper signature. Preserve existing endpoint paths and internal values.

- [ ] **Step 5: Run label and API tests**

  Run the focused Vitest files and confirm the new mapping and payload assertions pass before changing any page template.

- [ ] **Step 6: Commit**

  `git add frontend/src/utils frontend/src/api/admin/userRiskControlV2.ts frontend/src/i18n/locales && git commit -m "feat: add risk control labels and api contracts"`

### Task 2: Risk service rule creation and repository support

**Files:**
- Modify: `risk-control/model.go`
- Modify: `risk-control/db.go`
- Modify: `risk-control/admin.go`
- Modify: `risk-control/http.go`
- Modify: `risk-control/contract.go`
- Modify: `risk-control/schema.sql`
- Modify: `risk-control/rules.go`
- Create: `risk-control/admin_rules_test.go`
- Modify: `risk-control/http_test.go`
- Modify: `risk-control/rules_test.go`

- [ ] **Step 1: Write failing repository and validator tests**

  Add tests for safe code validation, missing name/event type, invalid window/threshold/score/level/action, successful memory creation with revision 1, duplicate memory creation conflict, and rule test output containing matched rule codes and decision level/action. Add SQL repository tests with `sqlmock` expectations for insert and duplicate constraint mapping if the dependency is not already present.

- [ ] **Step 2: Run the Go risk-control tests and verify the new tests fail**

  Run `(Get-Location); go test ./...` from `risk-control`; with Go unavailable locally, record the exact missing-tool result and still keep the tests as the contract to run in a Go-enabled environment.

- [ ] **Step 3: Add repository and validation primitives**

  Add `ErrRuleCodeConflict`, `CreateRule(context.Context, Rule) (Rule, error)` to `RiskRepository`, implement locked duplicate checking and deterministic IDs in `MemoryRepository`, implement PostgreSQL insert with unique-code conflict mapping in `SQLRepository`, and add a shared validator that accepts only `[a-z0-9][a-z0-9_-]{1,79}` plus known event types, levels, and actions.

- [ ] **Step 4: Add the signed admin POST route**

  Dispatch `POST /api/v1/admin/rules` to a handler that decodes a rule plus optional reason, validates it, creates it, records `create_rule` with actor ID/reason/revision metadata, and returns the created rule. Return 400 for input errors and 409 for duplicate codes. Add reason support to update audit records and preserve revision conflicts.

- [ ] **Step 5: Add schema/audit fields only when needed by the implementation**

  Keep the existing unique `risk_rules.code` constraint. If failure detail or batch identifiers require a first-class column, add an idempotent migration statement; otherwise expose those values through stable metadata keys (`failure_reason`, `batch_id`, `request_id`) and scan them into the API model.

- [ ] **Step 6: Run the focused Go tests and inspect the HTTP payloads**

  Run `go test ./...` in `risk-control` and confirm POST create, duplicate, invalid input, revision conflict, and audit behavior. No success claim is made if the Go toolchain remains unavailable.

- [ ] **Step 7: Commit**

  `git add risk-control && git commit -m "feat: support risk control rule creation"`

### Task 3: Proxy and authoritative account audit behavior

**Files:**
- Modify: `backend/internal/handler/admin/user_risk_control_proxy.go`
- Modify: `backend/internal/handler/admin/user_risk_control_proxy_test.go`
- Modify: `backend/internal/handler/admin/user_handler.go`
- Create or modify: `backend/internal/handler/admin/user_handler_risk_status_test.go`
- Modify: `backend/internal/service/risk_control_client.go`

- [ ] **Step 1: Write failing proxy and status-audit tests**

  Assert POST `/admin/user-risk-control/rules` is forwarded, an unallowlisted POST is rejected, whitespace-only status reasons are rejected, and a status failure emits an audit report with action, target, reason, result `failed`, failure detail, and the supplied batch ID. Assert successful status changes retain before/after status and reason.

- [ ] **Step 2: Run the focused backend tests and verify the new assertions fail**

  Run `go test ./backend/internal/handler/admin ./backend/internal/service`; expected failures are the missing POST allowlist and missing failure-audit behavior.

- [ ] **Step 3: Implement proxy allowlisting and request validation**

  Allow only `POST /rules` in the risk proxy in addition to existing read/update/test paths. Trim and validate status reasons before account mutation; add optional `batch_id` and `request_id` to the status request and risk audit payload.

- [ ] **Step 4: Implement per-target success/failure audit reporting**

  Centralize `SetRiskStatus` failure exits through a bounded audit reporter. Keep the main user update authoritative, revoke tokens on a successful ban, and record `success` or `failed` with the actual error message, before/after state, actor ID, target ID, reason, and batch ID. Preserve the existing short timeout and do not log secrets.

- [ ] **Step 5: Run focused backend tests**

  Run `go test ./backend/internal/handler/admin ./backend/internal/service`; inspect response codes and captured audit payloads.

- [ ] **Step 6: Commit**

  `git add backend/internal/handler/admin backend/internal/service && git commit -m "feat: audit risk status outcomes and rule creation proxy"`

### Task 4: User risk adapter and page

**Files:**
- Modify: `frontend/src/api/admin/userRiskControlV2.ts`
- Modify: `frontend/src/api/admin/__tests__/userRiskControlV2.spec.ts`
- Modify: `frontend/src/views/admin/UserRiskControlUsersView.vue`
- Modify: `frontend/src/components/admin/UserRiskControlUserDrawer.vue`
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts`

- [ ] **Step 1: Write failing adapter tests**

  Assert search/status/risk filters remain internal API values, risk sorting requests all candidates and returns descending/ascending stable order, account creation sorting forwards `sort_by`/`sort_order`, readable reason fallback is present, and `batchSetUserStatus` returns one success or failure result per target while limiting concurrency.

- [ ] **Step 2: Run the focused API/view tests and verify the new behavior fails**

  Run `pnpm --dir frontend exec vitest run src/api/admin/__tests__/userRiskControlV2.spec.ts src/views/admin/__tests__/UserRiskControlUsersView.spec.ts`; expect missing helper/UI selectors and raw enum assertions.

- [ ] **Step 3: Implement adapter composition and batch runner**

  Normalize both main-user and risk-service response shapes into `RiskUserRow`. Fetch all main users only when risk filters or risk-field sorting require it, fetch matching signals, apply full-result stable sorting, paginate once, and return all required counts/timestamps. Implement a fixed concurrency runner (maximum 4) with a unique batch ID and per-target error extraction.

- [ ] **Step 4: Implement the compact user table workflow**

  Add Chinese filters for account status, risk type, risk level, processing status, score range, date range, and risk-only; add clickable sort headers for score, level, event count, latest event, and account creation; add current-page selection and a visible batch toolbar. Keep row click separate from checkbox/action clicks. Add a confirmation modal with trimmed reason validation and result summary listing each target outcome. Refresh and clear selection after completion.

- [ ] **Step 5: Expand the drawer evidence**

  Render Chinese identity/status/risk labels, readable reasons, counts, event type/result/time, error code, endpoint/model, matched rules, and audit action/result/failure detail. Preserve single-user ban/unban with required trimmed reason.

- [ ] **Step 6: Update view tests to assert user-facing Chinese text and interactions**

  Replace raw enum assertions with Chinese labels; add tests for select-all, deselect, batch confirmation, whitespace rejection, partial failures, refresh/selection clearing, sort query changes, and drawer event evidence.

- [ ] **Step 7: Run focused frontend tests**

  Run the adapter and users view tests until all new interactions pass.

- [ ] **Step 8: Commit**

  `git add frontend/src/api/admin/userRiskControlV2.ts frontend/src/views/admin/UserRiskControlUsersView.vue frontend/src/components/admin/UserRiskControlUserDrawer.vue frontend/src/views/admin/__tests__/UserRiskControlUsersView.spec.ts frontend/src/api/admin/__tests__/userRiskControlV2.spec.ts && git commit -m "feat: complete user risk operations"`

### Task 5: Scenario rules page

**Files:**
- Modify: `frontend/src/api/admin/userRiskControlV2.ts`
- Modify: `frontend/src/views/admin/UserRiskControlRulesView.vue`
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts`

- [ ] **Step 1: Write failing rules tests**

  Add create-form validation tests for empty/unsafe code, missing name/event type, invalid numeric ranges, duplicate-code error, successful create, template prefill, enable/disable save, test result level/action/conditions, and revision conflict reload behavior.

- [ ] **Step 2: Run the rules tests and verify they fail**

  Run `pnpm --dir frontend exec vitest run src/views/admin/__tests__/UserRiskControlRulesView.spec.ts`; expect missing create form, labels, and API method failures.

- [ ] **Step 3: Implement create API and editable rule form**

  Add `createRule` payload conversion to `/admin/user-risk-control/rules`, use the shared label utilities for all displayed enum values, expose seven templates, validate before request, and append the returned rule. Make enable/disable an explicit saved action, not an inert checkbox.

- [ ] **Step 4: Implement test and revision conflict state**

  Show matched/not matched, score, Chinese level/action, and matched conditions. On 409, keep local edits untouched, show the conflict text, and provide a reload action that fetches the latest rule.

- [ ] **Step 5: Run rules tests and commit**

  Run the focused rules test file, then `git add frontend/src/api/admin/userRiskControlV2.ts frontend/src/views/admin/UserRiskControlRulesView.vue frontend/src/views/admin/__tests__/UserRiskControlRulesView.spec.ts && git commit -m "feat: add scenario rule lifecycle ui"`.

### Task 6: Audit adapter and page

**Files:**
- Modify: `frontend/src/api/admin/userRiskControlV2.ts`
- Modify: `frontend/src/views/admin/UserRiskControlAuditView.vue`
- Modify: `frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts`

- [ ] **Step 1: Write failing audit tests**

  Assert filters for actor, target user/account, action, result, and time range map to the API; sort headers change request parameters; page navigation uses server totals; Chinese labels render for action/result/status; and failure reason plus batch/request IDs are visible.

- [ ] **Step 2: Run the audit tests and verify they fail**

  Run `pnpm --dir frontend exec vitest run src/views/admin/__tests__/UserRiskControlAuditView.spec.ts`; expect missing fields and inert headers.

- [ ] **Step 3: Implement audit field mapping and controls**

  Map metadata keys into explicit UI fields, preserve raw IDs as secondary text, add date inputs and sorting controls, and render non-empty failure detail separately from operator reason. Keep pagination and loading/error/empty states stable on mobile.

- [ ] **Step 4: Run audit tests and commit**

  Run the focused audit test file, then `git add frontend/src/api/admin/userRiskControlV2.ts frontend/src/views/admin/UserRiskControlAuditView.vue frontend/src/views/admin/__tests__/UserRiskControlAuditView.spec.ts && git commit -m "feat: complete risk operation audit view"`.

### Task 7: Integrated verification and browser smoke test

**Files:**
- Modify only files required by verification failures.
- Keep temporary screenshots/scripts outside the repository.

- [ ] **Step 1: Run all required automated checks**

  From `E:\Code\worktrees\sub2api-risk-control-admin`, run `go test ./...`, `pnpm --dir frontend exec vitest run src/views/admin/__tests__ src/api/admin/__tests__ src/utils/__tests__`, `pnpm --dir frontend typecheck`, `pnpm --dir frontend build`, `pnpm --dir frontend lint:check`, and `git diff --check`. Record missing Go tooling separately if still unavailable.

- [ ] **Step 2: Start the local frontend/backend services**

  Inspect repository scripts and available local fixtures. Start the minimum local services needed for the admin route without production credentials; use fixture data only for browser validation and do not invent production records.

- [ ] **Step 3: Browser-check the three flows**

  The flow under test is: admin app loads -> user risk page filters/selects/sorts -> batch confirmation and partial result state -> rules create/test/toggle -> audit filters/sorts/pagination. Check 1440x900 and 390x844 for overflow, overlap, console errors, 4xx/5xx, and text wrapping.

- [ ] **Step 4: Review diff and status**

  Inspect `git diff --stat`, `git diff --check`, all commits, and `git status --short --branch`. Confirm no production deployment, secret, generated artifact, or unrelated change is present.

- [ ] **Step 5: Final report**

  Report implementation commit(s), each automated command result, browser URL/viewport and interaction evidence, Go-toolchain limitation if any, and deployment status separately. Mark the goal complete only after every required check and flow has authoritative evidence.
