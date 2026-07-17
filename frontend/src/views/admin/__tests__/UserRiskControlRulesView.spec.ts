import { config, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import UserRiskControlRulesView from '@/views/admin/UserRiskControlRulesView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import SearchInput from '@/components/common/SearchInput.vue'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listRules: vi.fn(),
    updateRule: vi.fn(),
    createRule: vi.fn(),
    testRule: vi.fn(),
  },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeAll(() => { config.global.stubs.RouterLink = { props: ['to'], template: '<a :href="String(to)"><slot /></a>' } })
afterAll(() => { delete config.global.stubs.RouterLink })
afterEach(() => vi.clearAllMocks())

function bodyElement<T extends HTMLElement = HTMLElement>(selector: string): T {
  const matches = document.body.querySelectorAll<T>(selector)
  const element = matches[matches.length - 1]
  if (!element) throw new Error(`Missing ${selector} in document body`)
  return element
}

async function setBodyValue(selector: string, value: string) {
  const target = bodyElement(selector)
  const input = target.matches('input, textarea')
    ? target as HTMLInputElement | HTMLTextAreaElement
    : target.querySelector<HTMLInputElement | HTMLTextAreaElement>('input, textarea')
  if (!input) throw new Error(`Missing input control inside ${selector}`)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

async function clickBody(selector: string) {
  bodyElement<HTMLButtonElement>(selector).click()
  await flushPromises()
}

describe('UserRiskControlRulesView', () => {
  it('uses shared responsive table and rule form controls', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: '登录失败爆发', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.findComponent(DataTable).exists()).toBe(true)
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    expect(wrapper.findComponent(BaseDialog).props('show')).toBe(true)
    expect(wrapper.findComponent(BaseDialog).props('closeOnClickOutside')).toBe(true)
    expect(wrapper.findAllComponents(Select).length).toBeGreaterThanOrEqual(3)
    await clickBody('[data-testid="template-login_failure_burst"]')
    expect(bodyElement('[data-testid="template-login_failure_burst"]').getAttribute('aria-pressed')).toBe('true')

    await wrapper.findComponent(BaseDialog).vm.$emit('close')
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    expect(wrapper.findAllComponents(BaseDialog).some((dialog) => dialog.props('show') && dialog.props('closeOnClickOutside'))).toBe(true)
    expect(wrapper.findComponent(Toggle).exists()).toBe(true)
  })

  it('renders rules in a compact table and expands editing on demand', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: '登录失败爆发', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('登录失败爆发')
    expect(wrapper.find('[data-testid="rule-editor-1"]').exists()).toBe(false)
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    expect(bodyElement('[data-testid="rule-editor-1"]')).toBeTruthy()
  })

  it('loads a scenario rule, saves changes, and shows test output', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockResolvedValue({ id: 1, revision: 4 })
    vi.mocked(userRiskControlV2API.testRule).mockResolvedValue({ matched: true, score: 80, riskLevel: 'high', action: 'review', conditions: ['登录失败次数达到 5 次'] })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    await setBodyValue('[data-testid="rule-threshold"]', '8')
    await clickBody('[data-testid="save-rule"]')
    expect(userRiskControlV2API.updateRule).toHaveBeenCalledWith(1, expect.objectContaining({ threshold: 8 }))
    expect(wrapper.text()).toContain('规则已保存')

    await clickBody('[data-testid="test-rule"]')
    expect(userRiskControlV2API.testRule).toHaveBeenCalled()
    expect(document.body.textContent).toContain('80')
    expect(document.body.textContent).toContain('高风险')
    expect(document.body.textContent).toContain('人工复核')
    expect(document.body.textContent).toContain('登录失败次数达到 5 次')
  })

  it('offers candidate rejection as a configurable rule action', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'registration_abuse', name: 'Registration abuse', enabled: true, windowSeconds: 600, threshold: 3, score: 80, riskLevel: 'critical', action: 'reject_candidate', revision: 1 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    expect(bodyElement('[data-testid="rule-action"]').textContent).toContain('拒绝注册')
  })

  it('shows the upstream revision conflict instead of hiding it behind a generic fallback', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockRejectedValue({ status: 409, message: 'rule revision conflict' })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    await clickBody('[data-testid="save-rule"]')

    expect(wrapper.text()).toContain('rule revision conflict')
  })

  it('rejects invalid edited rule values before sending the update request', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    await setBodyValue('[data-testid="rule-threshold"]', '')
    await clickBody('[data-testid="save-rule"]')

    expect(userRiskControlV2API.updateRule).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('阈值必须大于 0')
    expect(document.body.querySelector('[data-testid="reload-rule"]')).toBeNull()
  })

  it('validates and creates a scenario rule through the admin API', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    vi.mocked(userRiskControlV2API.createRule).mockResolvedValue({ id: 9, code: 'custom_login', name: '自定义登录', description: '短时间内登录失败', eventTypes: ['login_failure'], enabled: true, windowSeconds: 300, threshold: 5, score: 70, riskLevel: 'high', action: 'review', revision: 1 })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await clickBody('[data-testid="template-login_failure_burst"]')
    await setBodyValue('[data-testid="rule-code-input"]', 'custom_login')
    await setBodyValue('[data-testid="rule-name-input"]', '自定义登录')
    await setBodyValue('[data-testid="rule-description-input"]', '短时间内登录失败')
    await setBodyValue('[data-testid="rule-window"]', '300')
    await setBodyValue('[data-testid="rule-threshold-create"]', '5')
    await clickBody('[data-testid="create-rule"]')

    expect(userRiskControlV2API.createRule).toHaveBeenCalledWith(expect.objectContaining({ code: 'custom_login', name: '自定义登录', eventTypes: ['login_failure'], windowSeconds: 300, threshold: 5 }))
    expect(wrapper.text()).toContain('规则已创建')
  })

  it('rejects an unsafe rule code before sending the create request', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await setBodyValue('[data-testid="rule-code-input"]', 'bad code')
    await setBodyValue('[data-testid="rule-name-input"]', '错误规则')
    await clickBody('[data-testid="create-rule"]')
    expect(userRiskControlV2API.createRule).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('规则编码只能使用小写字母、数字、下划线和短横线')
  })

  it('resets the new rule draft whenever the dialog is reopened', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await setBodyValue('[data-testid="rule-code-input"]', 'unfinished_rule')
    await wrapper.findComponent(BaseDialog).vm.$emit('close')
    await wrapper.get('[data-testid="new-rule"]').trigger('click')

    const codeTarget = bodyElement('[data-testid="rule-code-input"]')
    const codeInput = codeTarget.matches('input') ? codeTarget as HTMLInputElement : codeTarget.querySelector<HTMLInputElement>('input')
    expect(codeInput?.value).toBe('registration_abuse')
    expect(bodyElement('[data-testid="template-registration_abuse"]').getAttribute('aria-pressed')).toBe('true')
  })

  it('filters the complete rule list locally by text, enabled state, and risk level', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([
      { id: 1, code: 'login_failure', name: 'Login burst', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 },
      { id: 2, code: 'quota_watch', name: 'Quota watch', enabled: false, windowSeconds: 600, threshold: 8, score: 30, riskLevel: 'low', action: 'observe', revision: 1 },
    ])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.findComponent(SearchInput).exists()).toBe(true)
    wrapper.getComponent('[data-testid="rule-search"]').vm.$emit('update:modelValue', 'quota_watch')
    await flushPromises()
    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('Quota watch')
    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).not.toContain('Login burst')

    wrapper.getComponent('[data-testid="rule-search"]').vm.$emit('update:modelValue', '')
    wrapper.getComponent('[data-testid="rule-enabled-filter"]').vm.$emit('update:modelValue', 'enabled')
    await flushPromises()
    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('Login burst')
    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).not.toContain('Quota watch')

    wrapper.getComponent('[data-testid="rule-enabled-filter"]').vm.$emit('update:modelValue', '')
    wrapper.getComponent('[data-testid="rule-level-filter"]').vm.$emit('update:modelValue', 'low')
    await flushPromises()
    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('Quota watch')
    expect(userRiskControlV2API.listRules).toHaveBeenCalledTimes(1)
  })

  it('uses DataTable client sorting with real derived values', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([
      { id: 1, code: 'high_rule', name: 'High rule', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 1 },
      { id: 2, code: 'low_rule', name: 'Low rule', enabled: true, windowSeconds: 300, threshold: 1, score: 20, riskLevel: 'low', action: 'observe', revision: 1 },
    ])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    const table = wrapper.findComponent(DataTable)
    expect(table.props('serverSideSort')).toBe(false)
    expect(table.props('columns')).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'risk', sortable: true })]))
    expect(table.props('data')).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: 1, risk: 80 }),
      expect.objectContaining({ id: 2, risk: 20 }),
    ]))
  })
})
