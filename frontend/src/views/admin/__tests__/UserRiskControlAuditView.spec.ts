import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserRiskControlAuditView from '@/views/admin/UserRiskControlAuditView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('@/api/admin/userRiskControlV2', () => ({ userRiskControlV2API: { listAudit: vi.fn() } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))

describe('UserRiskControlAuditView', () => {
  it('filters audit records by action and result', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 0 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="audit-action-filter"]').setValue('ban')
    await wrapper.get('[data-testid="audit-result-filter"]').setValue('failed')
    await wrapper.get('[data-testid="apply-audit-filters"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: 'ban', result: 'failed', page: 1, pageSize: 20 })
  })

  it('resets to the first page when audit filters change', async () => {
    vi.mocked(userRiskControlV2API.listAudit)
      .mockResolvedValueOnce({ items: [], total: 41, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 41, page: 2, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="audit-next-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="audit-action-filter"]').setValue('ban')
    await wrapper.get('[data-testid="apply-audit-filters"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ action: 'ban', result: '', page: 1, pageSize: 20 })
  })

  it('moves to the next audit page using the server total', async () => {
    vi.mocked(userRiskControlV2API.listAudit)
      .mockResolvedValueOnce({ items: [], total: 21, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 21, page: 2, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="audit-next-page"]').trigger('click')
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

  it('passes audit sorting and extended filters to the API', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 3, actor: '11', action: 'ban', target_type: 'user', target_id: '7', target_user_id: 7, result: 'success', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="audit-actor-filter"]').setValue('11')
    await wrapper.get('[data-testid="audit-from-filter"]').setValue('2026-07-01')
    await wrapper.get('[data-testid="audit-sort-time"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ actor: '11', from: '2026-07-01', sortBy: 'created_at', sortOrder: 'desc' }))
    await wrapper.get('[data-testid="audit-sort-time"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'created_at', sortOrder: 'asc' }))
  })
})
