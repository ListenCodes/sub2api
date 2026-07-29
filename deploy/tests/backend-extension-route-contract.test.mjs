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
  const routesIndex = registrar.indexOf('registerCustomAdminRoutes(admin, h)')
  assert.ok(authIndex >= 0 && authIndex < auditIndex && auditIndex < complianceIndex && complianceIndex < routesIndex)
  assert.match(registrar, /public\.GET\("\/homepage\/\*path"/)
  assert.match(registrar, /public\.HEAD\("\/homepage\/\*path"/)

  for (const file of [
    'backend/internal/server/routes/admin.go',
    'backend/internal/server/routes/auth.go'
  ]) {
    const source = read(file)
    assert.doesNotMatch(source, /ProxyRiskControl|ProxyAccountMonitor|ProxyExtensionsHomepage/)
  }
})

test('gateway keeps the custom middleware as an injected hook', () => {
  const router = read('backend/internal/server/router.go')
  const gateway = read('backend/internal/server/routes/gateway.go')
  assert.match(router, /RegisterCustomExtensionRoutes/)
  assert.match(router, /RegisterGatewayRoutes[\s\S]*customExtensions\.GatewayRiskEvents/)
  assert.doesNotMatch(gateway, /NewRiskControlClientFromEnv\(\)/)
  assert.match(gateway, /extensionMiddleware gin\.HandlerFunc/)
  assert.doesNotMatch(gateway, /extensionMiddleware \.\.\./)
  assert.match(gateway, /riskEvents/)
})

test('stable model plaza and custom extension routes coexist', () => {
  const router = read('backend/internal/server/router.go')
  assert.match(router, /optionalJWTAuth middleware2\.OptionalJWTAuthMiddleware/)
  assert.match(router, /RegisterModelPlazaRoutes\(v1, h, optionalJWTAuth/)
  assert.match(router, /RegisterCustomExtensionRoutes\(v1, h, adminAuth/)
  assert.match(router, /RegisterGatewayRoutes[\s\S]*customExtensions\.GatewayRiskEvents/)
})
