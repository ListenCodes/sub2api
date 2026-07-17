import { config, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import UserRiskControlAuditView from '@/views/admin/UserRiskControlAuditView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'
import Pagination from '@/components/common/Pagination.vue'
import DataTable from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'

vi.mock('@/api/admin/userRiskControlV2', () => ({ userRiskControlV2API: { listAudit: vi.fn() } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key, locale: ref('zh') }) }))
enableAutoUnmount(afterEach)
beforeAll(() => { config.global.stubs.RouterLink = { props: ['to'], template: '<a :href="String(to)"><slot /></a>' } })
afterAll(() => { delete config.global.stubs.RouterLink })
beforeEach(() => window.localStorage.clear())
afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

function emitFilter(wrapper: ReturnType<typeof mount>, testId: string, value: string) {
  wrapper.findComponent(`[data-testid="${testId}"]`).vm.$emit('update:modelValue', value)
}

describe('UserRiskControlAuditView', () => {
  it('uses the shared responsive workspace and date range picker', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 0 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.findComponent(TablePageLayout).exists()).toBe(true)
    expect(wrapper.findComponent(DataTable).exists()).toBe(true)
    expect(wrapper.findComponent(DateRangePicker).exists()).toBe(true)
  })

  it('filters audit records automatically without an apply button', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 0 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    expect(wrapper.find('[data-testid="apply-audit-filters"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('common.apply')
    emitFilter(wrapper, 'audit-action-filter', 'ban')
    emitFilter(wrapper, 'audit-result-filter', 'failed')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: 'ban', result: 'failed', page: 1, pageSize: 20 })
  })

  it('debounces actor and target text filters for 300 ms', async () => {
    vi.useFakeTimers()
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 0 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    vi.mocked(userRiskControlV2API.listAudit).mockClear()

    emitFilter(wrapper, 'audit-actor-filter', 'admin-11')
    await vi.advanceTimersByTimeAsync(299)
    expect(userRiskControlV2API.listAudit).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ actor: 'admin-11' }))

    vi.mocked(userRiskControlV2API.listAudit).mockClear()
    emitFilter(wrapper, 'audit-target-filter', 'alice@example.com')
    await vi.advanceTimersByTimeAsync(299)
    expect(userRiskControlV2API.listAudit).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ actor: 'admin-11', target: 'alice@example.com' }))
  })

  it('resets to the first page when audit filters change', async () => {
    vi.mocked(userRiskControlV2API.listAudit)
      .mockResolvedValueOnce({ items: [], total: 41, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 41, page: 2, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    emitFilter(wrapper, 'audit-action-filter', 'ban')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: 'ban', result: '', page: 1, pageSize: 20 })
  })

  it('resets active audit filters and supports changing the page size', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 80, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    emitFilter(wrapper, 'audit-action-filter', 'ban')
    await flushPromises()
    await wrapper.get('[data-testid="reset-audit-filters"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: '', result: '', page: 1, pageSize: 20 })

    wrapper.findComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: '', result: '', page: 1, pageSize: 50 })
  })

  it('moves to the next audit page using the server total', async () => {
    vi.mocked(userRiskControlV2API.listAudit)
      .mockResolvedValueOnce({ items: [], total: 21, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 21, page: 2, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: '', result: '', page: 2, pageSize: 20 })
  })

  it('renders Chinese audit labels, failure detail, and batch identifiers', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 3, actor: '管理员 11', action: 'ban', target_type: 'user', target_id: '7', target_user_id: 7, result: 'failed', reason: '重复登录失败', failure_reason: '账号已经封禁', batch_id: 'batch-1', request_id: 'request-1', before_status: 'active', after_status: 'disabled', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('封禁账号')
    expect(wrapper.text()).toContain('失败')
    expect(wrapper.text()).toContain('账号已经封禁')
    expect(wrapper.text()).toContain('batch-1')
    expect(wrapper.text()).toContain('request-1')
  })

  it('does not invent account status changes for rule operations', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 4, actor: 'qa-admin', action: 'update_rule', target_type: 'rule', target_id: 'login_failure', target_user_id: 0, result: 'success', reason: '调整阈值', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="audit-status-change-4"]').text()).toBe('-')
  })

  it('passes audit sorting and extended filters to the API', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 3, actor: '11', action: 'ban', target_type: 'user', target_id: '7', target_user_id: 7, result: 'success', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    const actorSearch = wrapper.findComponent('[data-testid="audit-actor-filter"]')
    actorSearch.vm.$emit('update:modelValue', '11')
    actorSearch.vm.$emit('search', '11')
    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2026-07-01', endDate: '2026-07-14', preset: null })
    const table = wrapper.findComponent(DataTable)
    expect(table.props('serverSideSort')).toBe(true)
    expect(table.props('columns')).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'time', sortable: true })]))
    table.vm.$emit('sort', 'time', 'asc')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'created_at', sortOrder: 'asc' }))
  })
})
