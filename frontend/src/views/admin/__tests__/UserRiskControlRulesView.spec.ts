import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import UserRiskControlRulesView from '@/views/admin/UserRiskControlRulesView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listRules: vi.fn(),
    updateRule: vi.fn(),
    createRule: vi.fn(),
    testRule: vi.fn(),
  },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
afterEach(() => vi.clearAllMocks())

describe('UserRiskControlRulesView', () => {
  it('loads a scenario rule, saves changes, and shows test output', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockResolvedValue({ id: 1, revision: 4 })
    vi.mocked(userRiskControlV2API.testRule).mockResolvedValue({ matched: true, score: 80, riskLevel: 'high', action: 'review', conditions: ['登录失败次数达到 5 次'] })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="rule-threshold"]').setValue('8')
    await wrapper.get('[data-testid="save-rule"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.updateRule).toHaveBeenCalledWith(1, expect.objectContaining({ threshold: 8 }))
    expect(wrapper.text()).toContain('规则已保存')

    await wrapper.get('[data-testid="test-rule"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.testRule).toHaveBeenCalled()
    expect(wrapper.text()).toContain('80')
    expect(wrapper.text()).toContain('高风险')
    expect(wrapper.text()).toContain('人工复核')
    expect(wrapper.text()).toContain('登录失败次数达到 5 次')
  })

  it('offers candidate rejection as a configurable rule action', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'registration_abuse', name: 'Registration abuse', enabled: true, windowSeconds: 600, threshold: 3, score: 80, riskLevel: 'critical', action: 'reject_candidate', revision: 1 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="rule-action"]').text()).toContain('拒绝注册')
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

  it('validates and creates a scenario rule through the admin API', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    vi.mocked(userRiskControlV2API.createRule).mockResolvedValue({ id: 9, code: 'custom_login', name: '自定义登录', description: '短时间内登录失败', eventTypes: ['login_failure'], enabled: true, windowSeconds: 300, threshold: 5, score: 70, riskLevel: 'high', action: 'review', revision: 1 })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await wrapper.get('[data-testid="rule-code-input"]').setValue('custom_login')
    await wrapper.get('[data-testid="rule-name-input"]').setValue('自定义登录')
    await wrapper.get('[data-testid="rule-description-input"]').setValue('短时间内登录失败')
    await wrapper.get('[data-testid="rule-event-type"]').setValue('login_failure')
    await wrapper.get('[data-testid="rule-window"]').setValue('300')
    await wrapper.get('[data-testid="rule-threshold-create"]').setValue('5')
    await wrapper.get('[data-testid="create-rule"]').trigger('click')
    await flushPromises()

    expect(userRiskControlV2API.createRule).toHaveBeenCalledWith(expect.objectContaining({ code: 'custom_login', name: '自定义登录', eventTypes: ['login_failure'], windowSeconds: 300, threshold: 5 }))
    expect(wrapper.text()).toContain('规则已创建')
  })

  it('rejects an unsafe rule code before sending the create request', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await wrapper.get('[data-testid="rule-code-input"]').setValue('bad code')
    await wrapper.get('[data-testid="rule-name-input"]').setValue('错误规则')
    await wrapper.get('[data-testid="create-rule"]').trigger('click')
    expect(userRiskControlV2API.createRule).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('规则编码只能使用小写字母、数字、下划线和短横线')
  })
})
