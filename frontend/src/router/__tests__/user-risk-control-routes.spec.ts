import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import router from '@/router'

describe('user risk control routes', () => {
  it.each([
    ['/admin/user-risk-control/users', '/admin/extensions/user-risk/users'],
    ['/admin/user-risk-control/rules', '/admin/extensions/user-risk/rules'],
    ['/admin/user-risk-control/audit', '/admin/extensions/user-risk/audit'],
    ['/admin/risk-control/overview', '/admin/extensions/user-risk/users'],
    ['/admin/risk-control/cases', '/admin/extensions/user-risk/users'],
    ['/admin/risk-control/events', '/admin/extensions/user-risk/users'],
    ['/admin/risk-control/subjects', '/admin/extensions/user-risk/users'],
    ['/admin/risk-control/scenarios', '/admin/extensions/user-risk/rules'],
    ['/admin/risk-control/lists', '/admin/extensions/user-risk/rules'],
    ['/admin/risk-control/audit', '/admin/extensions/user-risk/audit'],
  ])('redirects %s to %s', (from, to) => {
    const route = router.getRoutes().find((candidate) => candidate.path === from)
    expect(typeof route?.redirect).toBe('function')
    expect((route?.redirect as (route: { query: Record<string, string> }) => unknown)({ query: { page: '3', sort: 'risk_score' } }))
      .toEqual({ path: to, query: { page: '3', sort: 'risk_score' } })
  })

  it.each([
    '/admin/extensions/user-risk/users',
    '/admin/extensions/user-risk/rules',
    '/admin/extensions/user-risk/audit',
    '/admin/extensions/account-monitor',
    '/admin/extensions/group-monitor',
  ])('registers native extension route %s', (path) => {
    expect(router.resolve(path).matched.at(-1)?.path).toBe(path)
  })

  it.each([
    ['/admin/extensions/user-risk/users', 'admin.userRiskControl.usersTitle', 'admin.userRiskControl.usersDescription'],
    ['/admin/extensions/user-risk/rules', 'admin.userRiskControl.rulesTitle', 'admin.userRiskControl.rulesDescription'],
    ['/admin/extensions/user-risk/audit', 'admin.userRiskControl.auditPageTitle', 'admin.userRiskControl.auditPageDescription'],
    ['/admin/extensions/account-monitor', 'admin.accountMonitor.title', 'admin.accountMonitor.description'],
    ['/admin/extensions/group-monitor', 'admin.accountMonitor.groupTitle', 'admin.accountMonitor.groupDescription'],
  ])('uses localized title metadata for %s', (path, titleKey, descriptionKey) => {
    const meta = router.resolve(path).matched.at(-1)?.meta
    expect(meta?.titleKey).toBe(titleKey)
    expect(meta?.descriptionKey).toBe(descriptionKey)
    expect(meta?.title).toBeUndefined()
  })

  it('renders extensions center as an expandable sidebar group with three native entries', () => {
    const source = readFileSync(resolve(__dirname, '../../components/layout/AppSidebar.vue'), 'utf8')
    const center = readFileSync(resolve(__dirname, '../../views/admin/ExtensionsCenterView.vue'), 'utf8')

    expect(source.match(/path:\s*'\/admin\/extensions'/g)).toHaveLength(1)
    expect(source).toContain("label: '扩展中心'")
    expect(source).toContain("path: '/admin/extensions/user-risk/users'")
    expect(source).toContain("label: '用户风控'")
    expect(source).toContain("path: '/admin/extensions/account-monitor'")
    expect(source).toContain("label: '账号监控'")
    expect(source).toContain("path: '/admin/extensions/group-monitor'")
    expect(source).toContain("label: '分组监控'")
    expect(source).toMatch(/path:\s*'\/admin\/extensions',[\s\S]*?expandOnly:\s*true,[\s\S]*?children:\s*\[/)
    expect(source).not.toContain("path: '/admin/account-monitor'")
    expect(source).not.toContain("path: '/admin/user-risk-control'")
    expect(center).not.toContain('aria-label="扩展中心"')
    expect(center).not.toContain('const tabs =')
    expect(center).not.toContain('>扩展中心</h1>')
    expect(center).not.toContain('用户安全与运行质量')
  })

  it('keeps content moderation at the legacy risk-control route', () => {
    expect(router.resolve('/admin/risk-control').matched.at(-1)?.path).toBe('/admin/risk-control')
  })
})
