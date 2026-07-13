import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserRiskControlUsersView from '@/views/admin/UserRiskControlUsersView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listUsers: vi.fn(),
    getUserDetail: vi.fn(),
    setUserStatus: vi.fn(),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('UserRiskControlUsersView', () => {
  it('renders real account risk rows and updates results after filtering', async () => {
    vi.mocked(userRiskControlV2API.listUsers)
      .mockResolvedValueOnce({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_type: 'login_failure', risk_level: 'high', risk_score: 80, pending: true }], total: 1, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('login_failure')

    await wrapper.get('[data-testid="risk-type-filter"]').setValue('login_failure')
    await wrapper.get('[data-testid="apply-filters"]').trigger('click')
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ riskType: 'login_failure' }))
    expect(wrapper.text()).toContain('admin.userRiskControl.empty')
  })

  it('opens detail drawer and requires confirmation before banning', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_level: 'high', risk_score: 80 }], total: 1, page: 1, page_size: 20 })
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({ user: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }, events: [], audit: [] })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="user-row-7"]').trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('admin.userRiskControl.drawer.title')
    await (document.querySelector('[data-testid="ban-user"]') as HTMLElement).click()
    expect(document.body.textContent).toContain('admin.userRiskControl.confirmBan')
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()

    ;(document.querySelector('[data-testid="confirm-status-action"]') as HTMLElement).click()
    await flushPromises()
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('admin.userRiskControl.reasonRequired')

    ;(document.querySelector('textarea') as HTMLTextAreaElement).value = 'Repeated login failures'
    ;(document.querySelector('textarea') as HTMLTextAreaElement).dispatchEvent(new Event('input', { bubbles: true }))
    ;(document.querySelector('[data-testid="confirm-status-action"]') as HTMLElement).click()
    await flushPromises()
    expect(userRiskControlV2API.setUserStatus).toHaveBeenCalledWith(7, 'disabled', expect.any(String))
  })
})
