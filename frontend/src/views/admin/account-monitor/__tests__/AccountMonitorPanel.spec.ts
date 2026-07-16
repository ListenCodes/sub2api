import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { accountMonitorAPI } from '@/api/admin/accountMonitor'
import AccountMonitorPanel from '@/views/admin/account-monitor/AccountMonitorPanel.vue'
import AccountMonitorDrawer from '@/views/admin/account-monitor/AccountMonitorDrawer.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

vi.mock('@/api/admin/accountMonitor', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/admin/accountMonitor')>()
  return {
    ...original,
    accountMonitorAPI: {
      getOverview: vi.fn(),
      getDataQuality: vi.fn(),
      listAccounts: vi.fn(),
      getAccount: vi.fn(),
      getModels: vi.fn(),
      getUsers: vi.fn(),
      getErrors: vi.fn(),
      getTrends: vi.fn(),
      getAttempts: vi.fn(),
      getThreshold: vi.fn(),
      updateThreshold: vi.fn(),
      startRebuild: vi.fn(),
      getRebuildJob: vi.fn(),
      cancelAccountDetails: vi.fn(),
      dispose: vi.fn(),
    },
  }
})

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key, locale: ref('zh') }),
}))

enableAutoUnmount(afterEach)

const account = {
  account_id: 42,
  account_name: 'Primary OpenAI',
  platform: 'openai',
  status: 'active',
  attempts: 100,
  successes: 91,
  failures: 9,
  tokens: 12345,
  user_cost: 4.2,
  account_cost: 2.1,
  average_duration_ms: 220,
  p95_duration_ms: 480,
  model_count: 3,
  user_count: 8,
  image_count: 2,
  video_count: 1,
  video_duration_seconds: 12,
	groups: [
		{ group_id: 11, name: 'GPT Pro', platform: 'openai', status: 'active', rate_multiplier: 1.5 },
		{ group_id: 12, name: 'Shared Pool', platform: 'openai', status: 'active', rate_multiplier: 2 },
	],
  health: { risk_score: 72, risk_score_available: true, level: 'critical' as const, reasons: ['认证错误增多'] },
}

const idleAccount = {
	...account,
	account_id: 43,
	account_name: 'Idle Grok',
	platform: 'grok',
	attempts: 0,
	successes: 0,
	failures: 0,
	groups: [],
	health: { risk_score: 0, risk_score_available: false, level: 'normal' as const, reasons: [] },
}

function seed() {
  vi.mocked(accountMonitorAPI.getOverview).mockResolvedValue({
    attempts: 100, successes: 91, failures: 9, requests: 80, request_successes: 78,
    active_accounts: 1, abnormal_accounts: 1, average_risk_score: 72, high_risk_accounts: 1,
    users: 8, tokens: 12345, user_cost: 4.2, account_cost: 2.1,
    average_duration_ms: 220, p95_duration_ms: 480, last_sync_at: '2026-07-15T08:00:00Z', sync_lag_seconds: 15,
  })
	vi.mocked(accountMonitorAPI.getDataQuality).mockResolvedValue({
    source_connected: true, error_attribution_rate: 0.98, unattributed_errors: 2,
    recovered_failures: 4, exact_models: 88, estimated_models: 12, fallback_identities: 1,
		missing_group_requests: 3, exact_model_requests: 80, estimated_model_requests: 20,
		data_as_of: '2026-07-15T08:00:00Z', collection_lag_seconds: 900,
		stale_data_warning: '采集已延迟 900 秒', recent_source_error: '',
		usage_cursor: { cursor_time: '2026-07-15T08:00:00Z', cursor_id: 11, last_success_at: '2026-07-15T08:00:00Z' },
		error_cursor: { cursor_time: '2026-07-15T07:59:00Z', cursor_id: 22, last_success_at: '2026-07-15T07:59:00Z' },
		available_from: '2026-07-01T00:00:00Z', available_to: '2026-07-15T08:00:00Z', data_source: '90 天明细',
  })
	vi.mocked(accountMonitorAPI.listAccounts).mockResolvedValue({
		items: [account, idleAccount], total: 895, page: 1, page_size: 20,
		groups: [
			{ group_id: 11, name: 'GPT Pro', platform: 'openai', status: 'active', rate_multiplier: 1.5 },
			{ group_id: 12, name: 'Shared Pool', platform: 'openai', status: 'active', rate_multiplier: 2 },
			{ group_id: 99, name: 'Archive Only', platform: 'gemini', status: 'active', rate_multiplier: 1 },
		],
	})
  vi.mocked(accountMonitorAPI.getModels).mockResolvedValue({ items: [{ actual_model: 'gpt-5', model_attribution: 'exact', attempts: 100, successes: 91, failures: 9, tokens: 12345, user_cost: 4.2, account_cost: 2.1, average_duration_ms: 220, p95_duration_ms: 480 }], total: 1, page: 1, page_size: 20 })
	vi.mocked(accountMonitorAPI.getUsers).mockResolvedValue({ items: [{ user_id: 7, api_key_id: 9, email: 'alice@example.test', api_key_name: 'Prod Key', attempts: 4, successes: 3, failures: 1, success_rate: 0.75, tokens: 120, user_cost: 1.25, last_attempted_at: '2026-07-15T07:59:00Z' }], total: 1, page: 1, page_size: 20 })
  vi.mocked(accountMonitorAPI.getErrors).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  vi.mocked(accountMonitorAPI.getTrends).mockResolvedValue([])
  vi.mocked(accountMonitorAPI.getAttempts).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  vi.mocked(accountMonitorAPI.getAccount).mockResolvedValue(account)
  vi.mocked(accountMonitorAPI.getThreshold).mockResolvedValue({ scope: 'global', scope_id: 0, success_rate: 0.9 })
}

describe('AccountMonitorPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    seed()
  })

  it('renders overview, data quality, native accounts, and an explainable risk score', async () => {
    const wrapper = mount(AccountMonitorPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()

    expect(wrapper.text()).toContain('账号尝试')
    expect(wrapper.text()).toContain('数据质量')
    expect(wrapper.text()).toContain('Primary OpenAI')
		expect(wrapper.text()).toContain('Idle Grok')
		expect(wrapper.text()).toContain('GPT Pro')
		expect(wrapper.text()).toContain('Shared Pool')
		expect(wrapper.get('[data-testid="account-group-11"]').text()).toBe('GPT Pro · 1.5x')
		expect(wrapper.get('[data-testid="account-group-12"]').text()).toBe('Shared Pool · 2x')
		expect(wrapper.get('[data-testid="account-group-11"]').find('[data-platform]').exists()).toBe(false)
		expect(wrapper.text()).toContain('未分组')
		expect(wrapper.find('[data-platform="openai"]').exists()).toBe(true)
		expect(wrapper.find('[data-platform="grok"]').exists()).toBe(true)
		expect(wrapper.text()).not.toContain('自动刷新')
    expect(wrapper.text()).toContain('72')
    expect(wrapper.text()).toContain('严重')
		expect(wrapper.text()).toContain('认证错误增多')
		expect(wrapper.text()).toContain('采集已延迟 900 秒')
		expect(wrapper.text()).toContain(new Date('2026-07-01T00:00:00Z').toLocaleString())
		expect(wrapper.text()).toContain('用量游标 11')
		expect(wrapper.text()).toContain('错误游标 22')
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

	it('offers groups from the complete inventory rather than only the current account page', async () => {
		const wrapper = mount(AccountMonitorPanel, { global: { stubs: { Icon: true } } })
		await flushPromises()

		await wrapper.findAll('[data-testid="account-monitor-filters"] button[aria-label="Select option"]')[2].trigger('click')
		expect(document.body.textContent).toContain('Archive Only')
	})

  it('uses server-side risk sorting and preserves an open account on manual refresh', async () => {
    const wrapper = mount(AccountMonitorPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()

    await wrapper.get('[data-testid="account-row-42"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Primary OpenAI')

    await wrapper.get('[data-testid="sort-risk-score"]').trigger('click')
    await flushPromises()
    expect(accountMonitorAPI.listAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'risk_score', sortOrder: 'desc' }))

    await wrapper.get('[data-testid="account-monitor-refresh"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Primary OpenAI')
  })

	it('loads five detail tabs without requesting attempts and keeps a tab-local error retryable', async () => {
    vi.mocked(accountMonitorAPI.getErrors).mockRejectedValue(new Error('错误明细暂不可用'))
    const wrapper = mount(AccountMonitorPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="account-row-42"]').trigger('click')
    await flushPromises()

    ;(document.querySelector('[data-testid="account-detail-tab-errors"]') as HTMLElement).click()
    await flushPromises()
    expect(document.body.textContent).toContain('错误明细暂不可用')
    vi.mocked(accountMonitorAPI.getErrors).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    ;(document.querySelector('[data-testid="account-detail-retry"]') as HTMLElement).click()
    await flushPromises()
    expect(accountMonitorAPI.getErrors).toHaveBeenCalledTimes(2)

		for (const tab of ['models', 'users', 'trends', 'media']) {
      const button = document.querySelector(`[data-testid="account-detail-tab-${tab}"]`) as HTMLElement
      button.click()
      await flushPromises()
    }
		expect(document.querySelector('[data-testid="account-detail-tab-attempts"]')).toBeNull()
		expect(accountMonitorAPI.getAttempts).not.toHaveBeenCalled()
  })

	it('keeps a fixed scrollable detail shell with sticky approved user columns', async () => {
		const wrapper = mount(AccountMonitorPanel, { global: { stubs: { Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="account-row-42"]').trigger('click')
		await flushPromises()
		;(document.querySelector('[data-testid="account-detail-tab-users"]') as HTMLElement).click()
		await flushPromises()

		const shell = document.querySelector('[data-testid="account-monitor-drawer"]') as HTMLElement
		const content = document.querySelector('[data-testid="account-detail-content"]') as HTMLElement
		expect(shell.className).toContain('h-[80vh]')
		expect(shell.className).toContain('max-h-[80vh]')
		expect(content.className).toContain('overflow-auto')
		expect(document.querySelector('thead th')?.className).toContain('sticky')
		expect(document.body.textContent).toContain('成功率')
		expect(document.body.textContent).toContain('最近调用')
		expect(document.body.textContent).not.toContain('用户名')
		expect(document.body.textContent).not.toContain('脱敏前缀')

		shell.click()
		await flushPromises()
		expect(document.querySelector('[data-testid="account-monitor-drawer"]')).not.toBeNull()
		const drawer = wrapper.findComponent(AccountMonitorDrawer)
		expect(drawer.findComponent(BaseDialog).props('closeOnClickOutside')).toBe(true)
	})
})
