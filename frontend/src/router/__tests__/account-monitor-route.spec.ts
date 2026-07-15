import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('account monitor route contract', () => {
  it('keeps the official frontend integration thin and admin-only', () => {
    const root = resolve(__dirname, '../..')
    const router = readFileSync(resolve(root, 'router/index.ts'), 'utf8')
    const sidebar = readFileSync(resolve(root, 'components/layout/AppSidebar.vue'), 'utf8')

    expect(router).toContain("path: '/admin/account-monitor'")
    expect(router).toContain("component: () => import('@/views/admin/AccountMonitorView.vue')")
    expect(router).toMatch(/AdminAccountMonitor[\s\S]*requiresAdmin:\s*true/)
    expect(sidebar).toContain("path: '/admin/account-monitor'")
    expect(sidebar).toContain("t('nav.accountMonitor')")
  })
})
