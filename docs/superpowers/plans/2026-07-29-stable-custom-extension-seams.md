# Stable Custom Extension Seams Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove custom-release edits from the upstream route assembly hotspots, isolate the other confirmed custom integration hotspots, and add CI contracts that prevent those files from becoming custom merge points again.

**Architecture:** A custom `SetupCustomRouter` wrapper becomes the only backend bootstrap seam. It installs a scoped post-request risk observer before calling the upstream `SetupRouter`, then registers custom routes after the upstream router is complete. Custom admin/proxy handlers own their dependencies separately from upstream handlers; frontend routes and local Compose services use additive registries/overlays; unavoidable auth lifecycle hooks are constrained by an explicit shared-file budget.

**Tech Stack:** Go 1.26, Gin, Google Wire, Vue 3, Vue Router 4, TypeScript, Node test runner, Docker Compose, PowerShell/Bash contract tests.

---

## File Map

**Backend route seam**

- Create `backend/internal/server/custom_router.go`: custom bootstrap wrapper and gateway route-scope predicate.
- Create `backend/internal/server/custom_router_test.go`: route-scope and post-request ordering tests.
- Modify `backend/internal/server/http.go`: call `SetupCustomRouter` at the single low-churn bootstrap line.
- Restore `backend/internal/server/router.go`: no custom symbols or arguments.
- Restore `backend/internal/server/routes/gateway.go`: upstream function signature and route registrations.
- Modify `backend/internal/handler/risk_control_gateway.go`: add a filtered middleware constructor while preserving the existing constructor.
- Modify `backend/internal/server/routes/custom_extensions.go`: register routes only; do not return gateway hooks.
- Modify `deploy/tests/backend-extension-route-contract.test.mjs`: enforce the new ownership boundary.

**Custom handler and security isolation**

- Create `backend/internal/handler/custom_extensions.go`: construct the custom handler bundle from the existing upstream handler graph.
- Create `backend/internal/handler/custom_extension_accessors.go`: expose only the existing `AuthService` needed to build custom handlers.
- Create `backend/internal/handler/admin/custom_user_handler.go`: own risk client, admin service, auth service, proxies, risk-status action, and auto-ban callback.
- Move custom methods out of `backend/internal/handler/admin/user_handler.go`, `user_risk_control_proxy.go`, and `user_risk_control_enforcement.go` onto `CustomUserHandler`.
- Restore `backend/internal/handler/admin/user_handler.go`, `backend/internal/handler/wire.go`, and `backend/cmd/server/wire_gen.go` to upstream construction.
- Create `backend/internal/server/middleware/custom_extension_headers.go`: route-specific framing policy.
- Restore `backend/internal/server/middleware/security_headers.go` to upstream behavior.

**Frontend and deployment isolation**

- Create `frontend/src/features/extensions/install.ts`: dynamically install extension routes.
- Modify `frontend/src/main.ts`: one call to install custom frontend features.
- Restore `frontend/src/router/index.ts`: no direct custom route import/spread.
- Keep extension navigation data in `frontend/src/features/extensions/navigation.ts`; reduce `AppSidebar.vue` to one generic extension-navigation seam.
- Create `deploy/docker-compose.custom.local.yml`: local extension services overlay.
- Restore `deploy/docker-compose.local.yml`; remove custom extension setup from `deploy/docker-deploy.sh` while retaining a small upstreamable patch that stops all credential output.
- Create `deploy/ops/bootstrap-custom-local.sh`: generate custom local secrets without printing them and run the explicit Compose pair.

**Conflict guardrails**

- Create `deploy/tests/custom-overlap-budget.test.mjs`: reject forbidden custom edits in upstream hotspots.
- Modify `.github/workflows/custom-release.yml`: run the overlap budget with the recorded Stable baseline available.

## Confirmed Hotspots

The audit compared `v0.1.168..custom-release` and counted upstream changes in `v0.1.162..v0.1.168`.

| Priority | Shared file | Evidence | Target state |
|---|---|---|---|
| P0 | `backend/internal/server/router.go` | 3 upstream changes; current custom registration and gateway argument conflict with v0.1.168 | Byte-equivalent to Stable for custom concerns |
| P0 | `backend/internal/server/routes/gateway.go` | 6 upstream changes; 45 custom diff lines and a custom function signature | No custom middleware parameter or route edits |
| P0 | `backend/internal/handler/openai_gateway_handler.go` | 14 upstream changes for one custom line | Remove direct risk call; infer from shared outcome context |
| P0 | `deploy/docker-deploy.sh` | Custom code extends an upstream script that prints generated credentials | Remove custom setup; retain only a reviewed no-secret-output patch |
| P1 | `backend/internal/handler/admin/user_handler.go` | 133 custom lines in central admin handler plus Wire edits | Separate `CustomUserHandler`; upstream constructor unchanged |
| P1 | `backend/internal/handler/auth_handler.go` and OAuth flow files | Risk preflight/report calls are embedded in core auth paths | Small named lifecycle-hook budget with behavior tests |
| P1 | `backend/internal/server/middleware/security_headers.go` | Homepage exception changes global security middleware | Route-specific custom middleware |
| P1 | `backend/internal/server/routes/admin.go` | Three legacy update routes differ for release safety | Keep a documented three-line safety seam until upstream accepts a route-policy hook |
| P1 | `frontend/src/router/index.ts` and `AppSidebar.vue` | Central frontend registries import custom features | Dynamic route installer plus one generic nav seam |
| P1 | `deploy/docker-compose.local.yml` | 64 custom lines in an upstream Compose file | Additive explicit local overlay |

### Task 1: Characterize the Existing Gateway Risk Scope

**Files:**
- Create: `backend/internal/server/custom_router_test.go`
- Modify: `backend/internal/handler/risk_control_gateway_test.go`

- [ ] **Step 1: Write the failing path-scope test**

```go
func TestCustomGatewayRiskRouteScope(t *testing.T) {
	tests := map[string]bool{
		"/v1/messages": true,
		"/v1/sub2api/billing": false,
		"/v1beta/models/:model": true,
		"/responses": true,
		"/backend-api/codex/responses": true,
		"/videos/generations": true,
		"/videos/:request_id": false,
		"/api/v1/admin/users": false,
	}
	for fullPath, want := range tests {
		t.Run(fullPath, func(t *testing.T) {
			if got := isCustomGatewayRiskRoute(fullPath); got != want {
				t.Fatalf("isCustomGatewayRiskRoute(%q) = %v, want %v", fullPath, got, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and verify the missing seam fails**

Run: `cd backend && go test ./internal/server -run TestCustomGatewayRiskRouteScope -count=1`

Expected: FAIL with `undefined: isCustomGatewayRiskRoute`.

- [ ] **Step 3: Add a filtered post-request middleware test**

The test must install `RiskEventMiddlewareWhen`, set the auth subject and API key group inside a downstream handler, and assert the predicate runs after `c.Next()`. Add a second request with no group and assert no report is sent.

```go
engine.Use(RiskEventMiddlewareWhen(client, func(c *gin.Context) bool {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	return ok && apiKey != nil && apiKey.Group != nil
}))
```

- [ ] **Step 4: Run the focused handler test and verify it fails**

Run: `cd backend && go test ./internal/handler -run TestRiskEventMiddlewareWhen -count=1`

Expected: FAIL with `undefined: RiskEventMiddlewareWhen`.

- [ ] **Step 5: Commit characterization tests**

```bash
git add backend/internal/server/custom_router_test.go backend/internal/handler/risk_control_gateway_test.go
git commit -m "test: characterize custom gateway risk scope"
```

### Task 2: Add the Filtered Risk Observer

**Files:**
- Modify: `backend/internal/handler/risk_control_gateway.go`
- Test: `backend/internal/handler/risk_control_gateway_test.go`

- [ ] **Step 1: Preserve the public constructor and add a filtered constructor**

```go
type RiskEventPredicate func(*gin.Context) bool

func RiskEventMiddleware(client *service.RiskControlClient, banHandlers ...RiskBanHandler) gin.HandlerFunc {
	return RiskEventMiddlewareWhen(client, nil, banHandlers...)
}

func RiskEventMiddlewareWhen(
	client *service.RiskControlClient,
	shouldReport RiskEventPredicate,
	banHandlers ...RiskBanHandler,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if shouldReport != nil && !shouldReport(c) {
			return
		}
		reportRiskEventFromContext(c, client, banHandlers...)
	}
}
```

Move the existing post-`c.Next()` body into `reportRiskEventFromContext`; do not change its classification, timeouts, redaction, or enforcement behavior.

- [ ] **Step 2: Run focused tests**

Run: `cd backend && go test ./internal/handler -run 'TestRiskEventMiddleware' -count=1`

Expected: PASS.

- [ ] **Step 3: Commit the observer seam**

```bash
git add backend/internal/handler/risk_control_gateway.go backend/internal/handler/risk_control_gateway_test.go
git commit -m "refactor: add filtered risk event observer"
```

### Task 3: Move Router Integration to the Custom Bootstrap Wrapper

**Files:**
- Create: `backend/internal/server/custom_router.go`
- Modify: `backend/internal/server/http.go`
- Modify: `backend/internal/server/router.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/routes/gateway_test.go`
- Modify: `backend/internal/server/routes/gateway_key_billing_test.go`
- Test: `backend/internal/server/custom_router_test.go`

- [ ] **Step 1: Implement the route classifier**

```go
var customGatewayRiskAliases = map[string]struct{}{
	"/responses": {},
	"/responses/*subpath": {},
	"/alpha/search": {},
	"/models": {},
	"/messages/count_tokens": {},
	"/chat/completions": {},
	"/embeddings": {},
	"/images/generations": {},
	"/images/edits": {},
	"/images/generations/async": {},
	"/images/edits/async": {},
	"/images/tasks/:task_id": {},
	"/videos/generations": {},
	"/videos/edits": {},
	"/videos/extensions": {},
	"/antigravity/models": {},
}

func isCustomGatewayRiskRoute(fullPath string) bool {
	if fullPath == "/v1/sub2api/billing" {
		return false
	}
	if strings.HasPrefix(fullPath, "/v1/") ||
		strings.HasPrefix(fullPath, "/v1beta/") ||
		strings.HasPrefix(fullPath, "/backend-api/codex/") ||
		strings.HasPrefix(fullPath, "/antigravity/v1/") ||
		strings.HasPrefix(fullPath, "/antigravity/v1beta/") {
		return true
	}
	_, ok := customGatewayRiskAliases[fullPath]
	return ok
}
```

- [ ] **Step 2: Implement the custom bootstrap wrapper**

`SetupCustomRouter` must have the same arguments as `SetupRouter`. Its body is:

```go
customHandlers := handler.NewCustomExtensions(handlers)
r.Use(handler.RiskEventMiddlewareWhen(
	customHandlers.RiskControlClient,
	func(c *gin.Context) bool {
		apiKey, ok := middleware2.GetAPIKeyFromContext(c)
		return ok && apiKey != nil && apiKey.Group != nil && isCustomGatewayRiskRoute(c.FullPath())
	},
	customHandlers.AdminUser.ApplyRiskBan,
))

SetupRouter(r, handlers, jwtAuth, optionalJWTAuth, adminAuth, apiKeyAuth, auditLog,
	stepUpAuth, apiKeyService, subscriptionService, opsService, settingService,
	compositeResolver, cfg, redisClient)

routes.RegisterCustomExtensionRoutes(
	r.Group("/api/v1"), handlers, customHandlers, adminAuth, auditLog, settingService,
)
return r
```

- [ ] **Step 3: Change the single bootstrap call**

In `backend/internal/server/http.go`, replace only:

```go
return SetupRouter(r, handlers, jwtAuth, optionalJWTAuth, adminAuth, apiKeyAuth, auditLog, stepUpAuth, apiKeyService, subscriptionService, opsService, settingService, compositeResolver, cfg, redisClient)
```

with:

```go
return SetupCustomRouter(r, handlers, jwtAuth, optionalJWTAuth, adminAuth, apiKeyAuth, auditLog, stepUpAuth, apiKeyService, subscriptionService, opsService, settingService, compositeResolver, cfg, redisClient)
```

- [ ] **Step 4: Restore the upstream route assemblers**

Remove `RegisterCustomExtensionRoutes` from `router.go`. Restore `RegisterGatewayRoutes` to its v0.1.168 signature, remove `extensionMiddleware`, `riskEvents`, and every custom risk argument/use from `gateway.go`. Update direct tests to use the upstream signature.

- [ ] **Step 5: Run route and handler tests**

Run: `cd backend && go test ./internal/server ./internal/server/routes ./internal/handler -count=1`

Expected: PASS.

- [ ] **Step 6: Verify the upstream hotspots are custom-clean**

Run: `git diff --exit-code v0.1.168 -- backend/internal/server/router.go backend/internal/server/routes/gateway.go`

Expected: exit 0 and no output.

- [ ] **Step 7: Commit the bootstrap seam**

```bash
git add backend/internal/server backend/internal/handler/risk_control_gateway.go
git commit -m "refactor: move custom routes behind bootstrap seam"
```

### Task 4: Separate Custom Admin Handlers from Upstream UserHandler

**Files:**
- Create: `backend/internal/handler/custom_extensions.go`
- Create: `backend/internal/handler/custom_extension_accessors.go`
- Create: `backend/internal/handler/admin/custom_user_handler.go`
- Modify: `backend/internal/handler/admin/user_risk_control_proxy.go`
- Modify: `backend/internal/handler/admin/user_risk_control_enforcement.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Restore: `backend/internal/handler/admin/user_handler.go`
- Restore: `backend/internal/handler/wire.go`
- Restore: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: Write the failing ownership test**

Extend `backend-extension-route-contract.test.mjs` to assert that `user_handler.go` does not contain `riskControlClient`, `ProvideUserHandler`, `SetRiskStatus`, or `SetRiskControlClient`, and that `custom_user_handler.go` contains those responsibilities.

- [ ] **Step 2: Add the minimal service accessor in an additive file**

```go
func (h *AuthHandler) AuthServiceForCustomExtensions() *service.AuthService {
	if h == nil {
		return nil
	}
	return h.authService
}
```

- [ ] **Step 3: Add `CustomUserHandler`**

```go
type CustomUserHandler struct {
	adminService      service.AdminService
	authService       *service.AuthService
	riskControlClient *service.RiskControlClient
}

func NewCustomUserHandler(base *UserHandler, authService *service.AuthService, client *service.RiskControlClient) *CustomUserHandler {
	var adminService service.AdminService
	if base != nil {
		adminService = base.adminService
	}
	return &CustomUserHandler{
		adminService: adminService,
		authService: authService,
		riskControlClient: client,
	}
}
```

Change the receivers of `ProxyRiskControl`, `ProxyAccountMonitor`, `SetRiskStatus`, audit helpers, and `ApplyRiskBan` from `*UserHandler` to `*CustomUserHandler`.

- [ ] **Step 4: Build one custom handler bundle**

```go
type CustomExtensions struct {
	RiskControlClient *service.RiskControlClient
	AdminUser          *admin.CustomUserHandler
}

func NewCustomExtensions(h *Handlers) *CustomExtensions {
	client := service.NewRiskControlClientFromEnv()
	var base *admin.UserHandler
	var authService *service.AuthService
	if h != nil {
		if h.Admin != nil {
			base = h.Admin.User
		}
		if h.Auth != nil {
			authService = h.Auth.AuthServiceForCustomExtensions()
		}
	}
	return &CustomExtensions{
		RiskControlClient: client,
		AdminUser: admin.NewCustomUserHandler(base, authService, client),
	}
}
```

- [ ] **Step 5: Restore upstream constructors and regenerate Wire**

Restore `NewUserHandler` and `wire.go` to v0.1.168 ownership, then run:

Run: `cd backend && go generate ./cmd/server`

Expected: `wire_gen.go` uses `admin.NewUserHandler`, not `admin.ProvideUserHandler`.

- [ ] **Step 6: Run focused and full backend tests**

Run: `cd backend && go test ./internal/handler/admin ./internal/handler ./internal/server/... -count=1`

Expected: PASS.

- [ ] **Step 7: Commit custom handler ownership**

```bash
git add backend/cmd/server/wire_gen.go backend/internal/handler backend/internal/server/routes/custom_extensions.go deploy/tests/backend-extension-route-contract.test.mjs
git commit -m "refactor: isolate custom admin handler dependencies"
```

### Task 5: Make Homepage Framing Route-Specific

**Files:**
- Create: `backend/internal/server/middleware/custom_extension_headers.go`
- Create: `backend/internal/server/middleware/custom_extension_headers_test.go`
- Modify: `backend/internal/server/routes/custom_extensions.go`
- Restore: `backend/internal/server/middleware/security_headers.go`
- Restore: `backend/internal/server/middleware/security_headers_test.go`

- [ ] **Step 1: Write failing exact-scope tests**

Cover `GET` and `HEAD` under `/api/v1/extensions-self/homepage`, and reject `POST`, `/homepage-archive`, `/account-monitor`, and all non-extension routes.

- [ ] **Step 2: Move the policy into a route middleware**

```go
func ExtensionsHomepageFrameHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		if policy := c.Writer.Header().Get("Content-Security-Policy"); policy != "" {
			c.Header("Content-Security-Policy", replaceDirectiveValues(policy, "frame-ancestors", "'self'"))
		}
		c.Next()
	}
}
```

Attach it only to the custom homepage group before `GET` and `HEAD` registration.

- [ ] **Step 3: Restore the global security middleware**

`security_headers.go` must again always set `X-Frame-Options: DENY`; move `replaceDirectiveValues` to the additive custom header file.

- [ ] **Step 4: Run middleware and proxy tests**

Run: `cd backend && go test ./internal/server/middleware ./internal/handler -run 'Homepage|SecurityHeaders|ExtensionsProxy' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit route-specific framing**

```bash
git add backend/internal/server/middleware backend/internal/server/routes/custom_extensions.go
git commit -m "refactor: scope extension framing headers to homepage routes"
```

### Task 6: Remove Direct Risk Calls from High-Churn Gateway Handlers

**Files:**
- Restore: `backend/internal/handler/gateway_handler.go`
- Restore: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/risk_control_gateway.go`
- Test: `backend/internal/handler/risk_control_gateway_test.go`

- [ ] **Step 1: Add classification tests from existing shared context**

Test these inputs without calling `SetRiskEventContext`:

```go
[]struct {
	status int
	streamError *service.OpsStreamError
	upstreamStatus int
	want string
}{
	{status: http.StatusOK, want: "api_request"},
	{status: http.StatusTooManyRequests, want: "quota_exceeded"},
	{status: http.StatusBadGateway, upstreamStatus: http.StatusGatewayTimeout, want: "upstream_error"},
	{status: http.StatusBadRequest, want: "api_error"},
}
```

- [ ] **Step 2: Restore both upstream error helpers**

Remove the one-line `SetRiskEventContext` calls from `GatewayHandler.errorResponse` and `OpenAIGatewayHandler.errorResponse`. Preserve `SetRiskEventContext` for the custom content-moderation marker because it carries information not present in status/upstream context.

- [ ] **Step 3: Run gateway and risk tests**

Run: `cd backend && go test ./internal/handler ./internal/service -run 'Risk|OpsStream|UpstreamError' -count=1`

Expected: PASS.

- [ ] **Step 4: Commit high-churn cleanup**

```bash
git add backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/risk_control_gateway.go backend/internal/handler/risk_control_gateway_test.go
git commit -m "refactor: infer gateway risk outcomes from shared context"
```

### Task 7: Install Frontend Extensions Outside the Central Router

**Files:**
- Create: `frontend/src/features/extensions/install.ts`
- Modify: `frontend/src/main.ts`
- Restore: `frontend/src/router/index.ts`
- Modify: `frontend/src/router/__tests__/user-risk-control-routes.spec.ts`

- [ ] **Step 1: Write a failing installer test**

Create a memory-history router with a catch-all, call `installExtensionRoutes(router)`, navigate to `/admin/extensions/user-risk/users`, and assert the named extension route wins over the catch-all and retains `requiresAuth`/`requiresAdmin` through parent metadata.

- [ ] **Step 2: Implement dynamic registration**

```ts
import type { Router } from 'vue-router'
import { extensionRoutes } from './routes'

export function installExtensionRoutes(router: Router): void {
  for (const route of extensionRoutes) router.addRoute(route)
}
```

- [ ] **Step 3: Install before initial navigation**

In `main.ts`, call `installExtensionRoutes(router)` after `initI18n()` and before `app.use(router)`.

- [ ] **Step 4: Restore the central router**

Remove the `extensionRoutes` import and spread from `frontend/src/router/index.ts`.

- [ ] **Step 5: Run frontend route tests and typecheck**

Run: `cd frontend && pnpm vitest run src/router/__tests__/user-risk-control-routes.spec.ts src/router/__tests__/feature-access.spec.ts`

Run: `cd frontend && pnpm typecheck`

Expected: PASS.

- [ ] **Step 6: Commit frontend route installation**

```bash
git add frontend/src/main.ts frontend/src/router frontend/src/features/extensions
git commit -m "refactor: install extension routes at app bootstrap"
```

### Task 7A: Cap the Shared Sidebar Seam

**Files:**
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Keep additive: `frontend/src/features/extensions/navigation.ts`

- [ ] **Step 1: Write a source ownership test**

```ts
it('keeps extension navigation behind one sidebar provider', () => {
  expect(componentSource.match(/createExtensionAdminNavItems/g)).toHaveLength(2)
  expect(componentSource).toContain('...createExtensionAdminNavItems({ ShieldIcon, ChartIcon, FolderIcon })')
  expect(componentSource).not.toContain('/admin/extensions/user-risk')
  expect(componentSource).not.toContain('/admin/extensions/account-monitor')
  expect(componentSource).not.toContain('/admin/extensions/group-monitor')
})
```

- [ ] **Step 2: Remove unrelated custom navigation edits**

Restore the upstream computed home destination:

```ts
const homePath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
```

Keep extension paths and labels only in `features/extensions/navigation.ts`. Keep `activePrefix` as a generic optional `NavItem` field so future extension children do not require more sidebar conditions.

- [ ] **Step 3: Keep custom release UI behind one import seam**

The sidebar may import the custom release badge at the existing `VersionBadge` local name, but no release API, store, state machine, or dialog logic may move into `AppSidebar.vue`:

```ts
import VersionBadge from '@/features/custom-release/CustomReleaseBadge.vue'
```

- [ ] **Step 4: Run sidebar tests**

Run: `cd frontend && pnpm vitest run src/components/layout/__tests__/AppSidebar.spec.ts`

Expected: PASS.

- [ ] **Step 5: Commit the bounded sidebar seam**

```bash
git add frontend/src/components/layout/AppSidebar.vue frontend/src/components/layout/__tests__/AppSidebar.spec.ts frontend/src/features/extensions/navigation.ts
git commit -m "refactor: bound sidebar extension integration"
```

### Task 8: Isolate Local Compose and Remove Credential Output

**Files:**
- Create: `deploy/docker-compose.custom.local.yml`
- Create: `deploy/ops/bootstrap-custom-local.sh`
- Restore: `deploy/docker-compose.local.yml`
- Modify: `deploy/docker-deploy.sh`
- Modify: `deploy/tests/compose-overlay-contract.test.mjs`
- Modify: `deploy/tests/account-monitor-contract.test.mjs`
- Modify: `deploy/tests/extensions-self-layout.test.mjs`

- [ ] **Step 1: Write failing ownership and secret-output contracts**

Assert the upstream local Compose file contains no `risk-control-postgres`, `extensions-self`, or `RISK_CONTROL_`; assert the new overlay owns them. Execute the bootstrap wrapper with fixture binaries and fixed secret values, then assert stdout/stderr contains none of those values.

- [ ] **Step 2: Move local services to the additive overlay**

`docker-compose.custom.local.yml` must contain the current local `sub2api` environment additions, `risk-control-postgres`, `extensions-self`, health checks, dependency conditions, and the custom data volume/bind mount. It must be invoked explicitly:

```bash
docker compose \
  -f deploy/docker-compose.local.yml \
  -f deploy/docker-compose.custom.local.yml \
  --env-file deploy/.env.local up -d
```

- [ ] **Step 3: Implement secret generation without output**

The wrapper writes `0600` env data and prints only:

```text
Custom local environment created at deploy/.env.local
Generated 5 secret values; values were not printed
```

It must never print raw `POSTGRES_PASSWORD`, `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, `RISK_CONTROL_INTERNAL_SECRET`, or `RISK_CONTROL_POSTGRES_PASSWORD` values.

- [ ] **Step 4: Restore the upstream local Compose and reduce the deploy script to a security patch**

Run: `git restore --source=v0.1.168 -- deploy/docker-compose.local.yml deploy/docker-deploy.sh`

This restore is permitted only in the isolated implementation worktree after confirming those diffs are entirely part of this refactor. Then replace the credential display block in `docker-deploy.sh` with:

```bash
echo "Generated credentials were saved to .env and were not printed."
```

The resulting diff against v0.1.168 may remove secret-output lines, but may not add any `RISK_CONTROL_`, `extensions-self`, or custom Compose behavior.

- [ ] **Step 5: Run deployment contracts**

Run: `node --test deploy/tests/compose-overlay-contract.test.mjs deploy/tests/account-monitor-contract.test.mjs deploy/tests/extensions-self-layout.test.mjs`

Expected: PASS.

- [ ] **Step 6: Commit deployment isolation**

```bash
git add deploy/docker-compose.local.yml deploy/docker-compose.custom.local.yml deploy/docker-deploy.sh deploy/ops/bootstrap-custom-local.sh deploy/tests
git commit -m "refactor: isolate custom local deployment overlay"
```

### Task 9: Enforce a Shared-File Conflict Budget

**Files:**
- Create: `deploy/tests/custom-overlap-budget.test.mjs`
- Modify: `.github/workflows/custom-release.yml`
- Modify: `deploy/tests/backend-extension-route-contract.test.mjs`

- [ ] **Step 1: Define forbidden and budgeted files**

The contract must fail if custom symbols occur in these forbidden files:

```js
const forbidden = [
  'backend/internal/server/router.go',
  'backend/internal/server/routes/gateway.go',
  'backend/internal/handler/admin/user_handler.go',
  'backend/internal/server/middleware/security_headers.go',
  'frontend/src/router/index.ts',
  'deploy/docker-compose.local.yml'
]
```

Keep `backend/internal/server/routes/admin.go` as an explicit three-line safety exception for legacy update/rollback routes. Keep auth lifecycle hook files in a separate budget and fail when their custom marker count grows beyond the reviewed count. Budget `deploy/docker-deploy.sh` separately: added lines may contain only the no-secret-output message, and the test must reject any `echo`/`printf` line containing a secret variable.

- [ ] **Step 2: Compare against the recorded Stable commit**

Read `deploy/stable-release-baseline.json`, obtain `stable_release_commit`, and run:

```js
execFileSync('git', ['diff', '--unified=0', stableCommit, '--', file], { encoding: 'utf8' })
```

The test output must name the file and the unexpected added lines.

- [ ] **Step 3: Run the overlap budget locally**

Run: `node --test deploy/tests/custom-overlap-budget.test.mjs deploy/tests/backend-extension-route-contract.test.mjs`

Expected: PASS.

- [ ] **Step 4: Add the test to Custom Release CI**

Place it in the existing deployment-contract job after the Stable baseline fetch, so the tag commit exists before the test executes.

- [ ] **Step 5: Commit the guardrail**

```bash
git add deploy/tests .github/workflows/custom-release.yml
git commit -m "test: enforce custom upstream overlap budget"
```

### Task 10: Full Verification and Merge Simulation

**Files:**
- No new files.

- [ ] **Step 1: Format and validate diffs**

Run: `cd backend && gofmt -w internal/server/custom_router.go internal/server/custom_router_test.go internal/handler/custom_extensions.go internal/handler/custom_extension_accessors.go internal/handler/admin/custom_user_handler.go internal/server/middleware/custom_extension_headers.go internal/server/middleware/custom_extension_headers_test.go`

Run: `git diff --check`

Expected: no output.

- [ ] **Step 2: Run backend tests and lint**

Run: `cd backend && go test ./... -count=1`

Run: `cd backend && golangci-lint run ./...`

Expected: PASS.

- [ ] **Step 3: Run frontend tests and build**

Run: `cd frontend && pnpm typecheck`

Run: `cd frontend && pnpm vitest run`

Run: `cd frontend && pnpm build`

Expected: PASS.

- [ ] **Step 4: Run extension and deployment contracts**

Run: `cd extensions-self/account-monitor && go test ./... -count=1`

Run: `cd extensions-self/risk-control && go test ./... -count=1`

Run: `node --test deploy/tests/*.test.mjs`

Run: `pwsh -NoProfile -File deploy/ops/tests/test-script-contract.ps1`

Run: `bash deploy/tests/site-bootstrap-test.sh`

Run: `bash deploy/ops/tests/test-release-pipeline.sh`

Expected: PASS.

- [ ] **Step 5: Verify hotspot reduction**

Run:

```bash
git diff --exit-code v0.1.168 -- \
  backend/internal/server/router.go \
  backend/internal/server/routes/gateway.go \
  backend/internal/handler/admin/user_handler.go \
  backend/internal/server/middleware/security_headers.go \
  frontend/src/router/index.ts \
  deploy/docker-compose.local.yml
```

Expected: exit 0 and no output.

- [ ] **Step 6: Simulate integration with the current Stable tag**

Run from the repository root after the implementation branch is named `refactor/stable-custom-extension-seams`:

```bash
git worktree add ../sub2api-custom-seam-merge-sim -b verify/custom-extension-seam-merge origin/custom-release
git -C ../sub2api-custom-seam-merge-sim merge --no-ff refactor/stable-custom-extension-seams
git -C ../sub2api-custom-seam-merge-sim merge --no-ff v0.1.168
git -C ../sub2api-custom-seam-merge-sim status --short
```

Expected: both merges succeed and the final status output is empty. Do not push the verification branch; remove the verification worktree and branch only after recording the result.

- [ ] **Step 7: Commit final verification adjustments**

```bash
git add -A
git commit -m "test: verify stable custom extension seams"
```

## Acceptance Criteria

- `router.go` and `routes/gateway.go` contain no custom registration, middleware parameter, risk symbol, or extension symbol.
- `http.go` has exactly one custom bootstrap call and no custom route details.
- Gateway risk reporting preserves the current included/excluded route set, runs only after authenticated group assignment, and keeps fail-open/timeouts/redaction unchanged.
- Upstream `UserHandler`, Wire provider declarations, and generated Wire constructor are custom-clean.
- Homepage framing is relaxed only for `GET`/`HEAD` custom homepage proxy routes.
- Frontend extension routes are installed before initial navigation without modifying the central route array.
- Local extension services live only in the explicit custom local Compose overlay.
- No bootstrap or deployment command prints generated secret values.
- The CI overlap budget rejects future custom edits in the forbidden upstream hotspots.
- The three legacy update/rollback route changes remain the only documented route-assembly exception until a generic upstream route-policy hook is available.

## Deliberate Residual Seams

Two behaviors cannot be made zero-touch without changing upstream contracts:

1. Auth registration/login preflight and outcome reporting require lifecycle points inside core flows. Consolidate them behind named `AuthRiskHooks` calls, cap the reviewed added-line count in CI, and do not add ad hoc risk calls to new OAuth files.
2. Legacy `/admin/system/update` and rollback endpoints must fail closed or map to prepare-only behavior. Keep the current three route lines as an explicit safety patch until upstream exposes a replaceable update-route policy. Do not move this safety boundary to unauthenticated global middleware.

These seams are compile/test-visible and budgeted; they are not claimed as fully isolated.
