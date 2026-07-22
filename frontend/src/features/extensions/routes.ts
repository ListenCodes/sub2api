import type { RouteLocationGeneric, RouteRecordRaw } from 'vue-router'

const redirectWithQuery = (path: string) => (to: RouteLocationGeneric) => ({
  path,
  query: to.query
})

export const extensionRoutes: RouteRecordRaw[] = [
  {
    path: '/admin/account-monitor',
    redirect: redirectWithQuery('/admin/extensions/account-monitor')
  },
  {
    path: '/admin/user-risk-control/users',
    redirect: redirectWithQuery('/admin/extensions/user-risk/users')
  },
  {
    path: '/admin/user-risk-control/rules',
    redirect: redirectWithQuery('/admin/extensions/user-risk/rules')
  },
  {
    path: '/admin/user-risk-control/audit',
    redirect: redirectWithQuery('/admin/extensions/user-risk/audit')
  },
  {
    path: '/admin/extensions',
    component: () => import('@/views/admin/ExtensionsCenterView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      titleKey: 'admin.accountMonitor.extensionsTitle',
      descriptionKey: 'admin.accountMonitor.extensionsDescription'
    },
    children: [
      { path: '', redirect: '/admin/extensions/user-risk/users' },
      {
        path: 'user-risk',
        component: () => import('@/views/admin/extensions/UserRiskControlPanel.vue'),
        children: [
          { path: '', redirect: '/admin/extensions/user-risk/users' },
          {
            path: 'users',
            name: 'AdminExtensionUserRiskUsers',
            component: () => import('@/views/admin/UserRiskControlUsersView.vue'),
            meta: {
              titleKey: 'admin.userRiskControl.usersTitle',
              descriptionKey: 'admin.userRiskControl.usersDescription'
            }
          },
          {
            path: 'rules',
            name: 'AdminExtensionUserRiskRules',
            component: () => import('@/views/admin/UserRiskControlRulesView.vue'),
            meta: {
              titleKey: 'admin.userRiskControl.rulesTitle',
              descriptionKey: 'admin.userRiskControl.rulesDescription'
            }
          },
          {
            path: 'audit',
            name: 'AdminExtensionUserRiskAudit',
            component: () => import('@/views/admin/UserRiskControlAuditView.vue'),
            meta: {
              titleKey: 'admin.userRiskControl.auditPageTitle',
              descriptionKey: 'admin.userRiskControl.auditPageDescription'
            }
          }
        ]
      },
      {
        path: 'account-monitor',
        name: 'AdminExtensionAccountMonitor',
        component: () => import('@/views/admin/AccountMonitorView.vue'),
        meta: {
          titleKey: 'admin.accountMonitor.title',
          descriptionKey: 'admin.accountMonitor.description'
        }
      },
      {
        path: 'group-monitor',
        name: 'AdminExtensionGroupMonitor',
        component: () => import('@/views/admin/group-monitor/GroupMonitorPanel.vue'),
        meta: {
          titleKey: 'admin.accountMonitor.groupTitle',
          descriptionKey: 'admin.accountMonitor.groupDescription'
        }
      }
    ]
  },
  {
    path: '/admin/risk-control/cases',
    redirect: redirectWithQuery('/admin/extensions/user-risk/users')
  },
  {
    path: '/admin/risk-control/events',
    redirect: redirectWithQuery('/admin/extensions/user-risk/users')
  },
  {
    path: '/admin/risk-control/scenarios',
    redirect: redirectWithQuery('/admin/extensions/user-risk/rules')
  },
  {
    path: '/admin/risk-control/subjects',
    redirect: redirectWithQuery('/admin/extensions/user-risk/users')
  },
  {
    path: '/admin/risk-control/lists',
    redirect: redirectWithQuery('/admin/extensions/user-risk/rules')
  },
  {
    path: '/admin/risk-control/audit',
    redirect: redirectWithQuery('/admin/extensions/user-risk/audit')
  },
  {
    path: '/admin/risk-control/overview',
    redirect: redirectWithQuery('/admin/extensions/user-risk/users')
  }
]
