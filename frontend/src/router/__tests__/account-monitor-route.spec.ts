import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('account monitor route contract', () => {
  it('redirects the legacy route into the native extensions center and preserves query state', () => {
    const root = resolve(__dirname, '../..')
    const router = readFileSync(resolve(root, 'router/index.ts'), 'utf8')
    const sidebar = readFileSync(resolve(root, 'components/layout/AppSidebar.vue'), 'utf8')

    expect(router).toContain("path: '/admin/account-monitor'")
    expect(router).toContain("path: '/admin/extensions/account-monitor'")
    expect(router).toContain("query: to.query")
    expect(sidebar).not.toContain("path: '/admin/account-monitor'")
    expect(sidebar).toContain("path: '/admin/extensions/account-monitor'")
  })
})
