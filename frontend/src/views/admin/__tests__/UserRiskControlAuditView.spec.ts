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
})
