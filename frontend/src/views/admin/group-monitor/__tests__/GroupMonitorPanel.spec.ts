import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { accountMonitorAPI } from '@/api/admin/accountMonitor'
import GroupMonitorPanel from '@/views/admin/group-monitor/GroupMonitorPanel.vue'
import { parseGroupMonitorQuery, serializeGroupMonitorQuery } from '@/views/admin/group-monitor/useGroupMonitorFilters'

vi.mock('@/api/admin/accountMonitor', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/admin/accountMonitor')>()
  return { ...original, accountMonitorAPI: { listGroups: vi.fn(), getGroup: vi.fn(), dispose: vi.fn() } }
})
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key, locale: ref('zh') }) }))
enableAutoUnmount(afterEach)

const group = { group_id: 7, name: 'OpenAI Primary', platform: 'openai', group_status: 'active', call_status: 'normal' as const, total_requests: 10, successes: 10, failures: 0, success_rate: 1, timeline: [{ bucket_at: '2026-07-15T06:00:00Z', total: 10, successes: 10, failures: 0, status: 'normal' as const }] }

describe('group monitor URL state', () => {
  it('restores and serializes filters and paging exactly', () => {
		window.__APP_CONFIG__ = { table_page_size_options: [20, 100, 1000] } as typeof window.__APP_CONFIG__
		const state = parseGroupMonitorQuery({ range: '30d', query: 'openai', platform: 'openai', group_status: 'all', call_status: 'partial_failure', page: '3', page_size: '1000', group: '7' })
		expect(state).toEqual({ range: '30d', query: 'openai', platform: 'openai', groupStatus: 'all', callStatus: 'partial_failure', page: 3, pageSize: 1000, selectedGroupID: 7 })
		expect(serializeGroupMonitorQuery(state)).toEqual({ range: '30d', query: 'openai', platform: 'openai', group_status: 'all', call_status: 'partial_failure', page: '3', page_size: '1000', group: '7' })
  })

	it('normalizes removed one-hour and twelve-hour ranges to six hours', () => {
		expect(parseGroupMonitorQuery({ range: '1h' }).range).toBe('6h')
		expect(parseGroupMonitorQuery({ range: '12h' }).range).toBe('6h')
	})

	it('restores the has-calls filter', () => {
		expect(parseGroupMonitorQuery({ call_status: 'has_calls' }).callStatus).toBe('has_calls')
	})
})

describe('GroupMonitorPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
		window.__APP_CONFIG__ = { table_page_size_options: [20, 100, 1000] } as typeof window.__APP_CONFIG__
		vi.mocked(accountMonitorAPI.listGroups).mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 12, platforms: ['openai'], data_as_of: '2026-07-15T08:00:00Z', data_quality: { missing_group_requests: 2, exact_model_requests: 9, estimated_model_requests: 1, data_as_of: '2026-07-15T08:00:00Z', collection_lag_seconds: 900, stale_data_warning: '采集已延迟 900 秒', recent_source_error: '', usage_cursor: { cursor_time: '2026-07-15T08:00:00Z', cursor_id: 11, last_success_at: '2026-07-15T08:00:00Z' }, error_cursor: { cursor_time: '2026-07-15T07:59:00Z', cursor_id: 22, last_success_at: '2026-07-15T07:59:00Z' }, available_from: '2026-07-01T00:00:00Z', available_to: '2026-07-15T08:00:00Z' } })
  })

  it('renders deterministic cards in a responsive one-to-four column grid and refreshes without discarding the last success', async () => {
    const wrapper = mount(GroupMonitorPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    const grid = wrapper.get('[data-testid="group-monitor-grid"]')
    expect(grid.classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'md:grid-cols-2', 'xl:grid-cols-3', '2xl:grid-cols-4']))
		expect(wrapper.text()).toContain('OpenAI Primary')
		expect(wrapper.text()).toContain('采集已延迟 900 秒')
		expect(wrapper.text()).not.toContain('自动刷新')
		expect(wrapper.find('[data-testid="group-filter-apply"]').exists()).toBe(false)
		for (const [testID, label] of [
			['group-filter-query-label', '分组名称'],
			['group-filter-platform-label', '平台'],
			['group-filter-status-label', '分组状态'],
			['group-filter-call-status-label', '调用状态'],
			['group-filter-range-label', '时间范围'],
		] as const) {
			expect(wrapper.get(`[data-testid="${testID}"]`).text()).toBe(label)
		}

    vi.mocked(accountMonitorAPI.listGroups).mockRejectedValueOnce(new Error('分组聚合暂不可用'))
    await wrapper.get('[data-testid="group-monitor-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('分组聚合暂不可用')
    expect(wrapper.text()).toContain('OpenAI Primary')
  })

  it('passes platform, name, status, range, and paging to the server', async () => {
    const wrapper = mount(GroupMonitorPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    wrapper.getComponent('[data-testid="group-query"]').vm.$emit('update:modelValue', 'primary')
    wrapper.getComponent('[data-testid="group-platform"]').vm.$emit('update:modelValue', 'openai')
    wrapper.getComponent('[data-testid="group-status"]').vm.$emit('update:modelValue', 'all')
    wrapper.getComponent('[data-testid="group-range"]').vm.$emit('update:modelValue', '24h')
    await flushPromises()
		expect(accountMonitorAPI.listGroups).toHaveBeenLastCalledWith(expect.objectContaining({ query: 'primary', platform: 'openai', groupStatus: 'all', range: '24h', page: 1, pageSize: 20 }))
  })
})
