import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { accountMonitorAPI } from '@/api/admin/accountMonitor'
import GroupMonitorDetailDialog from '@/views/admin/group-monitor/GroupMonitorDetailDialog.vue'

vi.mock('@/api/admin/accountMonitor', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/admin/accountMonitor')>()
  return { ...original, accountMonitorAPI: { getGroup: vi.fn(), cancelGroupDetail: vi.fn() } }
})
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key, locale: ref('zh') }) }))
afterEach(() => { document.body.innerHTML = ''; vi.clearAllMocks() })

const bucket = { bucket_at: '2026-07-15T06:00:00Z', total: 5, successes: 4, failures: 1, status: 'partial_failure' as const }
const timeline = [bucket, ...Array.from({ length: 23 }, (_, index) => ({ bucket_at: new Date(Date.UTC(2026, 6, 15, 7 + index)).toISOString(), total: 0, successes: 0, failures: 0, status: 'no_data' as const }))]
const group = { group_id: 7, name: 'OpenAI Primary', platform: 'openai', group_status: 'active', call_status: 'partial_failure' as const, total_requests: 15, successes: 12, failures: 3, success_rate: 0.8, timeline }

describe('GroupMonitorDetailDialog', () => {
  it('renders totals, alphabetical actual models, success/total cells, and bucket details', async () => {
    vi.mocked(accountMonitorAPI.getGroup).mockResolvedValue({ group, data_as_of: '2026-07-15T08:00:00Z', models: [
      { actual_model: 'z-model', total_requests: 5, successes: 4, failures: 1, exact_model_requests: 4, estimated_model_requests: 1, success_rate: 0.8, timeline },
      { actual_model: 'a-model', total_requests: 10, successes: 8, failures: 2, exact_model_requests: 10, estimated_model_requests: 0, success_rate: 0.8, timeline },
    ] })
    mount(GroupMonitorDetailDialog, { props: { show: true, groupID: 7, range: '24h', originalStatus: 'active' }, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(document.body.textContent).toContain('15')
    expect(document.body.textContent).toContain('2 个实际模型')
    expect(document.body.textContent!.indexOf('a-model')).toBeLessThan(document.body.textContent!.indexOf('z-model'))
    const cell = document.querySelector('[data-testid="model-bucket-cell"]') as HTMLElement
    expect(cell.textContent).toContain('4 / 5')
		expect(document.querySelectorAll('[data-testid="model-bucket-cell"]')).toHaveLength(48)
		const scroll = document.querySelector('[data-testid="group-model-timeline-scroll"]') as HTMLElement
		expect(scroll.classList.contains('overflow-x-scroll')).toBe(true)
		expect(scroll.style.scrollbarGutter).toBe('stable')
    cell.click()
    await flushPromises()
    expect(document.body.textContent).toContain('失败 1')
  })

  it('marks an inactive state change and emits removed for a soft-deleted group', async () => {
    vi.mocked(accountMonitorAPI.getGroup).mockResolvedValueOnce({ group: { ...group, group_status: 'inactive' }, data_as_of: '2026-07-15T08:00:00Z', models: [] })
    const wrapper = mount(GroupMonitorDetailDialog, { props: { show: true, groupID: 7, range: '6h', originalStatus: 'active' }, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(document.body.textContent).toContain('状态已变化')

    vi.mocked(accountMonitorAPI.getGroup).mockRejectedValue({ status: 404, message: 'not found' })
    await wrapper.setProps({ groupID: 8 })
    await flushPromises()
    expect(wrapper.emitted('removed')).toBeTruthy()
  })
})
