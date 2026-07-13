import { describe, expect, it } from 'vitest'
import router from '@/router'

describe('user risk control routes', () => {
  it.each([
    ['/admin/risk-control/overview', '/admin/user-risk-control/users'],
    ['/admin/risk-control/cases', '/admin/user-risk-control/users'],
    ['/admin/risk-control/events', '/admin/user-risk-control/users'],
    ['/admin/risk-control/subjects', '/admin/user-risk-control/users'],
    ['/admin/risk-control/scenarios', '/admin/user-risk-control/rules'],
    ['/admin/risk-control/lists', '/admin/user-risk-control/rules'],
    ['/admin/risk-control/audit', '/admin/user-risk-control/audit'],
  ])('redirects %s to %s', (from, to) => {
    const route = router.getRoutes().find((candidate) => candidate.path === from)
    expect(route?.redirect).toBe(to)
  })

  it('keeps content moderation at the legacy risk-control route', () => {
    expect(router.resolve('/admin/risk-control').matched.at(-1)?.path).toBe('/admin/risk-control')
  })
})
