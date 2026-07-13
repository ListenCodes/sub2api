import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserRiskControlUsersView from '@/views/admin/UserRiskControlUsersView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listUsers: vi.fn(),
    getUserDetail: vi.fn(),
    setUserStatus: vi.fn(),
    batchSetUserStatus: vi.fn(),
    markUsersProcessed: vi.fn(),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('UserRiskControlUsersView', () => {
  it('renders real account risk rows and updates results after filtering', async () => {
    vi.mocked(userRiskControlV2API.listUsers)
      .mockResolvedValueOnce({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_type: 'login_failure', risk_level: 'high', risk_score: 80, risk_reason: '命中规则：登录失败爆发（5 分钟内失败 5 次）', event_count: 5, ip_count: 2, device_count: 1, last_event_at: '2026-07-12T12:00:00Z', pending: true }], total: 1, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('登录失败')
    expect(wrapper.text()).toContain('高风险')
    expect(wrapper.text()).toContain('命中规则：登录失败爆发')

    await wrapper.get('[data-testid="risk-type-filter"]').setValue('login_failure')
    await wrapper.get('[data-testid="apply-filters"]').trigger('click')
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ riskType: 'login_failure' }))
    expect(wrapper.text()).toContain('admin.userRiskControl.empty')
  })

  it('opens detail drawer and requires confirmation before banning', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_level: 'high', risk_score: 80 }], total: 1, page: 1, page_size: 20 })
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({ user: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }, events: [{ id: 1, type: 'login_failure', risk_type: 'login_failure', risk_level: 'high', score: 80, reason: '命中规则：登录失败爆发（5 分钟内失败 5 次）', rule_codes: ['login_failure_burst'], occurred_at: '2026-07-12T12:00:00Z' }], audit: [] })

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

  it('shows Chinese event evidence and audit outcomes in the detail drawer', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_level: 'high', risk_score: 80 }], total: 1, page: 1, page_size: 20 })
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({
      user: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' },
      events: [{ id: 1, type: 'login_failure', risk_type: 'login_failure', risk_level: 'high', score: 80, reason: '命中规则：登录失败爆发（5 分钟内失败 5 次）', error_code: 'invalid_credentials', rule_codes: ['login_failure_burst'], occurred_at: '2026-07-12T12:00:00Z' }],
      audit: [{ id: 2, actor: '11', target_type: 'user', target_id: '7', target_user_id: 7, action: 'ban', result: 'failed', reason: '重复操作', failure_reason: '账号已经封禁', created_at: '2026-07-12T12:01:00Z' }],
    })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="user-row-7"]').trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('登录失败')
    expect(document.body.textContent).toContain('高风险')
    expect(document.body.textContent).toContain('命中规则：登录失败爆发')
    expect(document.body.textContent).toContain('invalid_credentials')
    expect(document.body.textContent).toContain('账号已经封禁')
    expect(document.body.textContent).toContain('失败')
  })

  it('selects the current page and reports partial batch results', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({
      items: [
        { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_score: 80 },
        { id: 8, username: 'Bob', email: 'bob@example.com', status: 'active', risk_score: 30 },
      ], total: 2, page: 1, page_size: 20,
    })
    vi.mocked(userRiskControlV2API.batchSetUserStatus).mockResolvedValue([
      { id: 7, status: 'success' },
      { id: 8, status: 'failed', reason: '目标账号已被其他管理员处理' },
    ])

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="select-current-page"]').setValue(true)
    expect(wrapper.get('[data-testid="selected-count"]').text()).toContain('2')

    await wrapper.get('[data-testid="batch-ban"]').trigger('click')
    await (document.querySelector('[data-testid="batch-reason"]') as HTMLTextAreaElement).setAttribute('data-test-ready', 'true')
    const reasonInput = document.querySelector('[data-testid="batch-reason"]') as HTMLTextAreaElement
    reasonInput.value = '批量处置：重复登录失败'
    reasonInput.dispatchEvent(new Event('input', { bubbles: true }))
    ;(document.querySelector('[data-testid="batch-confirm"]') as HTMLElement).click()
    await flushPromises()

    expect(userRiskControlV2API.batchSetUserStatus).toHaveBeenCalledWith([7, 8], 'disabled', '批量处置：重复登录失败')
    expect(wrapper.text()).toContain('部分成功')
    expect(wrapper.text()).toContain('目标账号已被其他管理员处理')
    expect(wrapper.find('[data-testid="batch-action-bar"]').exists()).toBe(false)
  })

  it('changes the query order when risk score sort is clicked', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 1, username: 'Sort me', email: 'sort@example.com', status: 'active' }], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="sort-risk-score"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'risk_score', sortOrder: 'desc' }))
    await wrapper.get('[data-testid="sort-risk-score"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'risk_score', sortOrder: 'asc' }))
  })
})
