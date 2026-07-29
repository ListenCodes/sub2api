import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'

import { installExtensionRoutes } from './install'

describe('installExtensionRoutes', () => {
  it('installs extension routes after the catch-all without losing admin metadata', () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/:pathMatch(.*)*',
          name: 'NotFound',
          component: { template: '<div>not found</div>' }
        }
      ]
    })

    installExtensionRoutes(router)

    const current = router.resolve('/admin/extensions/user-risk/users')
    expect(current.name).toBe('AdminExtensionUserRiskUsers')
    expect(current.matched[0]?.path).toBe('/admin/extensions')
    expect(current.meta.requiresAuth).toBe(true)
    expect(current.meta.requiresAdmin).toBe(true)
  })
})
