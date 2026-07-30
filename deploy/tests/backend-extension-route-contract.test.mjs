import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

test('custom backend routes have one dedicated registration boundary', () => {
  const registrarPath = resolve(repoRoot, 'backend/internal/server/routes/custom_extensions.go')
  assert.equal(existsSync(registrarPath), true, 'custom route registrar is missing')
  const registrar = read('backend/internal/server/routes/custom_extensions.go')
  assert.match(registrar, /RegisterCustomExtensionRoutes/)
  assert.match(registrar, /ProxyRiskControl/)
  assert.match(registrar, /ProxyAccountMonitor/)
  assert.match(registrar, /ProxyExtensionsHomepage/)
  const authIndex = registrar.indexOf('admin.Use(gin.HandlerFunc(adminAuth))')
  const auditIndex = registrar.indexOf('admin.Use(gin.HandlerFunc(auditLog))')
  const complianceIndex = registrar.indexOf('admin.Use(middleware.AdminComplianceGuard(settingService))')
  const routesIndex = registrar.indexOf('registerCustomAdminRoutes(admin, h, custom)')
  assert.ok(authIndex >= 0 && authIndex < auditIndex && auditIndex < complianceIndex && complianceIndex < routesIndex)
  assert.match(registrar, /public\.GET\("\/homepage\/\*path"/)
  assert.match(registrar, /public\.HEAD\("\/homepage\/\*path"/)
  assert.match(registrar, /ExtensionsHomepageFrameHeaders/)
  assert.match(registrar, /admin\.POST\("\/system\/custom-release\/read", h\.Admin\.System\.MarkCustomReleaseRead\)/)

  for (const file of [
    'backend/internal/server/routes/admin.go',
    'backend/internal/server/routes/auth.go'
  ]) {
    const source = read(file)
    assert.doesNotMatch(source, /ProxyRiskControl|ProxyAccountMonitor|ProxyExtensionsHomepage/)
    assert.doesNotMatch(source, /system\/custom-release\/read|MarkCustomReleaseRead/)
  }
})

test('custom backend integration is owned by the bootstrap wrapper', () => {
  const router = read('backend/internal/server/router.go')
  const gateway = read('backend/internal/server/routes/gateway.go')
  const http = read('backend/internal/server/http.go')
  const customRouter = read('backend/internal/server/custom_router.go')
  assert.doesNotMatch(router, /RegisterCustomExtensionRoutes|GatewayRiskEvents|CustomExtensions/)
  assert.doesNotMatch(gateway, /NewRiskControlClientFromEnv\(\)|extensionMiddleware|riskEvents/)
  assert.match(http, /return SetupCustomRouter\(/)
  assert.match(customRouter, /RiskEventMiddlewareWhen/)
  assert.match(customRouter, /RegisterCustomExtensionRoutes/)
})

test('custom admin dependencies do not extend the upstream user handler', () => {
  const upstreamHandler = read('backend/internal/handler/admin/user_handler.go')
  const customHandlerPath = resolve(repoRoot, 'backend/internal/handler/admin/custom_user_handler.go')
  assert.equal(existsSync(customHandlerPath), true, 'custom user handler is missing')
  const customHandler = [
    read('backend/internal/handler/admin/custom_user_handler.go'),
    read('backend/internal/handler/admin/user_risk_control_proxy.go'),
    read('backend/internal/handler/admin/user_risk_control_enforcement.go')
  ].join('\n')
  assert.doesNotMatch(upstreamHandler, /riskControlClient|ProvideUserHandler|SetRiskStatus|SetRiskControlClient/)
  assert.match(customHandler, /type CustomUserHandler struct/)
  assert.match(customHandler, /func \(h \*CustomUserHandler\) SetRiskStatus/)
  assert.match(customHandler, /func \(h \*CustomUserHandler\) ProxyRiskControl/)
  assert.match(customHandler, /func \(h \*CustomUserHandler\) ProxyAccountMonitor/)
  assert.match(customHandler, /func \(h \*CustomUserHandler\) ApplyRiskBan/)
})

test('stable model plaza and custom extension routes coexist', () => {
  const router = read('backend/internal/server/router.go')
  const customRouter = read('backend/internal/server/custom_router.go')
  assert.match(router, /optionalJWTAuth middleware2\.OptionalJWTAuthMiddleware/)
  assert.match(router, /RegisterModelPlazaRoutes\(v1, h, optionalJWTAuth/)
  assert.match(customRouter, /SetupRouter\(/)
  assert.match(customRouter, /RegisterCustomExtensionRoutes\(/)
})
