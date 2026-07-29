import type { Router } from 'vue-router'

import { extensionRoutes } from './routes'

export function installExtensionRoutes(router: Router): void {
  for (const route of extensionRoutes) {
    router.addRoute(route)
  }
}
