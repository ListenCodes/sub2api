import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('account monitor route contract', () => {
  it('redirects the legacy route into the native extensions center and preserves query state', () => {
    const root = resolve(__dirname, '../..')
    const routes = readFileSync(resolve(root, 'features/extensions/routes.ts'), 'utf8')
    const navigation = readFileSync(resolve(root, 'features/extensions/navigation.ts'), 'utf8')

    expect(routes).toContain("path: '/admin/account-monitor'")
    expect(routes).toContain("path: '/admin/extensions'")
    expect(routes).toContain("path: 'account-monitor'")
    expect(routes).toContain("query: to.query")
    expect(navigation).not.toContain("path: '/admin/account-monitor'")
    expect(navigation).toContain("path: '/admin/extensions/account-monitor'")
    expect(navigation).toContain("activePrefix: '/admin/extensions/user-risk'")
  })
})
