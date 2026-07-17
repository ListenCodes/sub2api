import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')
const read = (path: string) => readFileSync(resolve(root, path), 'utf8')

describe('extension style alignment contracts', () => {
  it('lets AppLayout own the extension workspace boundary', () => {
    const source = read('views/admin/ExtensionsCenterView.vue')

    expect(source).not.toContain('max-w-[1600px]')
    expect(source).not.toMatch(/px-4|py-5|sm:px-6|lg:px-8/)
    expect(source).toMatch(/<AppLayout>\s*<RouterView\s*\/?>\s*<\/AppLayout>/)
  })

  it('renders the shared risk tabs inside every table page fixed action area', () => {
    const panel = read('views/admin/extensions/UserRiskControlPanel.vue')
    const tabs = read('views/admin/extensions/UserRiskControlTabs.vue')

    expect(panel).not.toContain('<nav')
    expect(panel).toMatch(/<RouterView\s*\/?>/)
    expect(tabs).toMatch(/class="tabs(?:\s|")/)
    expect(tabs).toContain("t('admin.userRiskControl.tabsLabel')")

    for (const path of [
      'views/admin/UserRiskControlUsersView.vue',
      'views/admin/UserRiskControlRulesView.vue',
      'views/admin/UserRiskControlAuditView.vue',
    ]) {
      const source = read(path)
      expect(source).toMatch(/<template #actions>[\s\S]*?<UserRiskControlTabs/)
    }
  })

  it('uses extension tokens for filters, cards, tabs, details, and loading states', () => {
    const accountFilters = read('views/admin/account-monitor/AccountMonitorFilters.vue')
    const groupFilters = read('views/admin/group-monitor/GroupMonitorFilters.vue')
    const groupCard = read('views/admin/group-monitor/GroupMonitorCard.vue')
    const accountDrawer = read('views/admin/account-monitor/AccountMonitorDrawer.vue')
    const groupDetail = read('views/admin/group-monitor/GroupMonitorDetailDialog.vue')
    const groupPanel = read('views/admin/group-monitor/GroupMonitorPanel.vue')

    expect(accountFilters).toContain('input-label')
    expect(groupFilters).toContain('input-label')
    expect(accountFilters).not.toContain('border-y')
    expect(groupFilters).not.toContain('border-y')
    expect(groupCard).toContain('card card-hover')
    expect(groupCard).toContain('dark:text-emerald-400')
    expect(groupCard).toContain('dark:text-amber-400')
    expect(groupCard).toContain('dark:text-red-400')
    expect(accountDrawer).toContain('class="tabs"')
    expect(accountDrawer).toContain(' px-5 py-4')
    expect(groupDetail).toContain(' px-5 py-4')
    expect(groupPanel).toContain('data-testid="group-monitor-skeleton"')
    expect(accountDrawer).toContain('data-testid="account-detail-skeleton"')
    expect(groupDetail).toContain('data-testid="group-detail-skeleton"')
  })
})
