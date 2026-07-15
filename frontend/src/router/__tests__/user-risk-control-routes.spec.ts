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

  it('keeps one extensions-center sidebar item and removes the legacy custom entries', () => {
    const source = readFileSync(resolve(__dirname, '../../components/layout/AppSidebar.vue'), 'utf8')

    expect(source.match(/path:\s*'\/admin\/extensions'/g)).toHaveLength(1)
    expect(source).toContain("label: '扩展中心'")
    expect(source).not.toContain("path: '/admin/account-monitor'")
    expect(source).not.toContain("path: '/admin/user-risk-control'")
  })

  it('keeps content moderation at the legacy risk-control route', () => {
    expect(router.resolve('/admin/risk-control').matched.at(-1)?.path).toBe('/admin/risk-control')
  })
})
