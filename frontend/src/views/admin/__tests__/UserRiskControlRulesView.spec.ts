import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserRiskControlRulesView from '@/views/admin/UserRiskControlRulesView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listRules: vi.fn(),
    updateRule: vi.fn(),
    testRule: vi.fn(),
  },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))

describe('UserRiskControlRulesView', () => {
  it('loads a scenario rule, saves changes, and shows test output', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockResolvedValue({ id: 1, revision: 4 })
    vi.mocked(userRiskControlV2API.testRule).mockResolvedValue({ matched: true, score: 80 })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="rule-threshold"]').setValue('8')
    await wrapper.get('[data-testid="save-rule"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.updateRule).toHaveBeenCalledWith(1, expect.objectContaining({ threshold: 8 }))
    expect(wrapper.text()).toContain('admin.userRiskControl.saved')

    await wrapper.get('[data-testid="test-rule"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.testRule).toHaveBeenCalled()
    expect(wrapper.text()).toContain('80')
  })

  it('offers candidate rejection as a configurable rule action', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'registration_abuse', name: 'Registration abuse', enabled: true, windowSeconds: 600, threshold: 3, score: 80, riskLevel: 'critical', action: 'reject_candidate', revision: 1 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="rule-action"]').text()).toContain('reject_candidate')
  })

  it('shows the upstream revision conflict instead of hiding it behind a generic fallback', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockRejectedValue({ message: 'rule revision conflict' })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="save-rule"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('rule revision conflict')
  })
})
