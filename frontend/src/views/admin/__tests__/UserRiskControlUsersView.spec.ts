import { config, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import UserRiskControlUsersView from '@/views/admin/UserRiskControlUsersView.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Toggle from '@/components/common/Toggle.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'

vi.mock('@/api/admin/userRiskControlV2', () => ({
  userRiskControlV2API: {
    listUsers: vi.fn(),
    getWorkOverview: vi.fn(),
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
enableAutoUnmount(afterEach)
beforeAll(() => { config.global.stubs.RouterLink = { props: ['to'], template: '<a :href="String(to)"><slot /></a>' } })
beforeEach(() => {
  vi.mocked(userRiskControlV2API.getWorkOverview).mockResolvedValue({ pending: 3, mine: 2, observing: 4, atRisk: 7, dataQuality: 1 })
})
afterAll(() => { delete config.global.stubs.RouterLink })
afterEach(() => {
  vi.useRealTimers()
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 })
  window.localStorage.removeItem('table-page-size')
  vi.clearAllMocks()
})

describe('UserRiskControlUsersView', () => {
  it('loads all users by default', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })

    mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenCalledWith(expect.objectContaining({ view: 'all', sortBy: 'risk_score', sortOrder: 'desc' }))
  })

  it('shows actionable work counts while keeping all users as the active default', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="risk-work-overview"]').text()).toContain('待复核')
    expect(wrapper.get('[data-testid="work-count-pending"]').text()).toContain('3')
    expect(wrapper.get('[data-testid="work-count-at-risk"]').text()).toContain('7')
    expect(wrapper.get('[data-testid="risk-case-views"]').text()).toContain('全部用户')
    await wrapper.get('[data-testid="work-count-my"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ view: 'my' }))
  })

  it('shows an unavailable work overview instead of fabricating zero counts, then retries', async () => {
    vi.mocked(userRiskControlV2API.getWorkOverview).mockRejectedValueOnce(new Error('overview unavailable'))
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="work-overview-error"]').text()).toContain('overview unavailable')
    expect(wrapper.get('[data-testid="work-count-pending"]').text()).toContain('—')
    expect(wrapper.get('[data-testid="work-count-pending"]').text()).not.toContain('0')

    vi.mocked(userRiskControlV2API.getWorkOverview).mockResolvedValueOnce({ pending: 5, mine: 1, observing: 2, atRisk: 6, dataQuality: 3 })
    await wrapper.get('[data-testid="retry-work-overview"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="work-overview-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="work-count-pending"]').text()).toContain('5')
  })

  it('keeps low-frequency conditions behind advanced filters', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    const advanced = wrapper.get('[data-testid="advanced-risk-filters"]')
    expect(advanced.attributes('open')).toBeUndefined()
    expect(wrapper.get('[data-testid="primary-risk-filters"]').find('[data-testid="risk-user-search"]').exists()).toBe(true)
    expect(advanced.find('[data-testid="risk-type-filter"]').exists()).toBe(true)
    expect(advanced.find('[data-testid="processing-status-filter"]').exists()).toBe(true)
    expect(advanced.find('[data-testid="min-score-filter"]').exists()).toBe(true)
		const riskOptions = wrapper.getComponent('[data-testid="risk-type-filter"]').props('options') as Array<{ value: string; label: string }>
		expect(riskOptions).toEqual(expect.arrayContaining([
			expect.objectContaining({ value: 'login_failure', label: '登录失败' }),
			expect.objectContaining({ value: 'v2_registration_ip_accounts', label: '同 IP 多成功注册账号' }),
		]))
		expect(wrapper.text()).not.toContain('v2_registration_ip_accounts')
  })

  it('uses the shared responsive workspace and filter controls', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.findComponent(TablePageLayout).exists()).toBe(true)
    expect(wrapper.findComponent(DataTable).exists()).toBe(true)
    expect(wrapper.findComponent(Toggle).exists()).toBe(true)

    await wrapper.get('[data-testid="user-select-7"]').setValue(true)
    await wrapper.get('[data-testid="batch-mark-processed"]').trigger('click')
    const dialog = wrapper.findComponent(BaseDialog)
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('closeOnClickOutside')).toBe(true)
    expect(document.querySelector('[data-testid="batch-confirm"]')?.classList.contains('btn-primary')).toBe(true)
  })

  it('debounces text and score filters for 300 ms', async () => {
    vi.useFakeTimers()
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    vi.mocked(userRiskControlV2API.listUsers).mockClear()

    wrapper.findComponent('[data-testid="risk-user-search"]').vm.$emit('update:modelValue', 'alice')
    await vi.advanceTimersByTimeAsync(299)
    expect(userRiskControlV2API.listUsers).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'alice' }))

    vi.mocked(userRiskControlV2API.listUsers).mockClear()
    const minimum = wrapper.get('[data-testid="min-score-filter"]')
    ;(minimum.element as HTMLInputElement).value = '60'
    await minimum.trigger('input')
    await vi.advanceTimersByTimeAsync(299)
    expect(userRiskControlV2API.listUsers).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'alice', minScore: 60 }))
  })

  it('renders real account risk rows and updates results automatically after filtering', async () => {
    vi.mocked(userRiskControlV2API.listUsers)
      .mockResolvedValueOnce({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_type: 'login_failure', risk_level: 'high', risk_score: 80, risk_reason: '命中规则：登录失败爆发（5 分钟内失败 5 次）', event_count: 5, ip_count: 2, device_count: 1, last_event_at: '2026-07-12T12:00:00Z', pending: true }], total: 1, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('登录失败')
    expect(wrapper.text()).toContain('高风险')
    expect(wrapper.text()).toContain('命中规则：登录失败爆发')

    const riskTypeFilter = wrapper.getComponent('[data-testid="risk-type-filter"]')
    riskTypeFilter.vm.$emit('update:modelValue', 'login_failure')
    riskTypeFilter.vm.$emit('change', 'login_failure')
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ riskType: 'login_failure' }))
    expect(wrapper.text()).toContain('admin.userRiskControl.empty')
    expect(wrapper.find('[data-testid="apply-filters"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('common.apply')
  })

  it('prioritizes the account identifier and fills the available table width', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', identity: { user_id: 7, latest_ip: '203.0.113.0/24', country_code: 'US', region: 'CA', browser_instance_count: 2, api_client_count: 1, associated_account_count: 3, active_rule_count: 1, quality_state: 'healthy' } }], total: 1, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

		expect(wrapper.get('[data-testid="account-primary-7"]').text()).toBe('alice@example.com')
		expect(wrapper.get('[data-testid="account-secondary-7"]').text()).toContain('Alice · #7')
    expect(wrapper.get('[data-testid="user-row-7"]').classes()).toEqual(expect.arrayContaining(['max-w-[50vw]', 'sm:max-w-none']))
    expect(wrapper.get('[data-testid="risk-users-table"]').classes()).toContain('w-full')
    expect(wrapper.findComponent(DataTable).props('stickyFirstColumn')).toBe(true)
		expect(wrapper.text()).toContain('当前风险')
		expect(wrapper.text()).toContain('主信号')
		expect(wrapper.text()).toContain('案件状态')
		expect(wrapper.findComponent(DataTable).props('columns').map((column: { key: string }) => column.key)).toEqual(['select', 'account', 'accountStatus', 'evaluation', 'riskScore', 'riskType', 'lastEvent', 'processing'])
		expect(wrapper.text()).toContain('评估完整')
		expect(wrapper.text()).not.toContain('203.0.113.0/24')
  })

  it('resets to the first page when the page size changes', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 80, page: 1, page_size: 20 })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, pageSize: 50 }))
  })

  it('caps mobile pages at 20 accounts so the first screen stays compact', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 80, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ pageSize: 20 }))
    expect(wrapper.get('[data-testid="primary-risk-filters"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="account-status-filter"]').classes()).toContain('w-[calc(50%-0.375rem)]')
    expect(wrapper.get('[data-testid="risk-level-filter"]').classes()).toContain('w-[calc(50%-0.375rem)]')
    expect(wrapper.get('[data-testid="advanced-risk-filters"]').find('[data-testid="mobile-select-current-page"]').exists()).toBe(true)
  })

  it('caps a persisted 50-row preference before the initial mobile request', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    window.localStorage.setItem('table-page-size', '50')
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 80, page: 1, page_size: 20 })

    mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(userRiskControlV2API.listUsers).toHaveBeenCalledWith(expect.objectContaining({ pageSize: 20 }))
  })

	it('keeps account status, evaluation coverage, and risk conclusion distinct at desktop width', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1280 })
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [], total: 80, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

		expect(wrapper.findComponent(DataTable).props('columns').map((column: { key: string }) => column.key)).toEqual(['select', 'account', 'accountStatus', 'evaluation', 'riskScore', 'riskType', 'lastEvent', 'processing'])
    wrapper.getComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ pageSize: 50 }))
  })

  it('keeps the latest filter result when an older request finishes later', async () => {
    let resolveInitial!: (value: Awaited<ReturnType<typeof userRiskControlV2API.listUsers>>) => void
    let resolveLatest!: (value: Awaited<ReturnType<typeof userRiskControlV2API.listUsers>>) => void
    const initialRequest = new Promise<Awaited<ReturnType<typeof userRiskControlV2API.listUsers>>>((resolve) => { resolveInitial = resolve })
    const latestRequest = new Promise<Awaited<ReturnType<typeof userRiskControlV2API.listUsers>>>((resolve) => { resolveLatest = resolve })
    vi.mocked(userRiskControlV2API.listUsers)
      .mockReturnValueOnce(initialRequest)
      .mockReturnValueOnce(latestRequest)

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    wrapper.getComponent('[data-testid="risk-type-filter"]').vm.$emit('update:modelValue', 'login_failure')

    resolveLatest({ items: [{ id: 8, username: 'Bob', email: 'bob@example.com', status: 'active', risk_type: 'login_failure' }], total: 1, page: 1, page_size: 20 })
    await flushPromises()
    expect(wrapper.text()).toContain('bob@example.com')

    resolveInitial({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_type: 'api_request' }], total: 1, page: 1, page_size: 20 })
    await flushPromises()

    expect(wrapper.text()).toContain('bob@example.com')
    expect(wrapper.text()).not.toContain('alice@example.com')
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
    expect(document.body.textContent).toContain('admin.userRiskControl.statusChangeMessage')
    expect(wrapper.findComponent(ConfirmDialog).props('show')).toBe(true)
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()

    wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('admin.userRiskControl.reasonRequired')

    const statusReason = document.querySelector('[data-testid="status-reason"] textarea') as HTMLTextAreaElement
    statusReason.value = 'Repeated login failures'
    statusReason.dispatchEvent(new Event('input', { bubbles: true }))
    wrapper.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(userRiskControlV2API.setUserStatus).toHaveBeenCalledWith(7, 'disabled', expect.any(String))
  })

  it('shows Chinese event evidence and audit outcomes in the detail drawer', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_level: 'high', risk_score: 80 }], total: 1, page: 1, page_size: 20 })
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({
      user: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' },
      events: [{ id: 1, type: 'login_failure', risk_type: 'login_failure', risk_level: 'high', score: 80, reason: '命中规则：登录失败爆发（5 分钟内失败 5 次）', error_code: 'invalid_credentials', ip: '198.51.100.10', device_id: 'chrome-124', rule_codes: ['login_failure_burst'], occurred_at: '2026-07-12T12:00:00Z' }],
      audit: [{ id: 2, actor: '11', target_type: 'user', target_id: '7', target_user_id: 7, action: 'ban', result: 'failed', reason: '重复操作', failure_reason: '账号已经封禁', created_at: '2026-07-12T12:01:00Z' }],
    })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="user-row-7"]').trigger('click')
    await flushPromises()

    const timeline = document.querySelector('[data-testid="legacy-risk-timeline"]') as HTMLDetailsElement
    const history = document.querySelector('[data-testid="legacy-action-history"]') as HTMLDetailsElement
    expect(timeline.open).toBe(false)
    expect(history.open).toBe(false)
    timeline.open = true
    history.open = true
    expect(document.body.textContent).toContain('登录失败')
    expect(document.body.textContent).toContain('高风险')
    expect(document.body.textContent).toContain('命中规则：登录失败爆发')
    expect(document.body.textContent).toContain('invalid_credentials')
    expect(document.body.textContent).toContain('198.51.100.10')
    expect(document.body.textContent).toContain('chrome-124')
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
    const reasonInput = document.querySelector('[data-testid="batch-reason"] textarea') as HTMLTextAreaElement
    reasonInput.value = '批量处置：重复登录失败'
    reasonInput.dispatchEvent(new Event('input', { bubbles: true }))
    ;(document.querySelector('[data-testid="batch-confirm"]') as HTMLElement).click()
    await flushPromises()

    expect(userRiskControlV2API.batchSetUserStatus).toHaveBeenCalledWith([7, 8], 'disabled', '批量处置：重复登录失败')
    expect(wrapper.text()).toContain('部分成功')
    expect(wrapper.text()).toContain('目标账号已被其他管理员处理')
    expect(wrapper.find('[data-testid="batch-action-bar"]').exists()).toBe(false)
  })

  it('does not batch ban or unban a mixed selection containing pending accounts', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({
      items: [
        { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_score: 80 },
        { id: 8, username: 'Pending', email: 'pending@example.com', status: 'pending', risk_score: 30 },
      ], total: 2, page: 1, page_size: 20,
    })

    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="select-current-page"]').setValue(true)

    expect(wrapper.get('[data-testid="batch-ban"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="batch-unban"]').attributes('disabled')).toBeDefined()
    expect(userRiskControlV2API.batchSetUserStatus).not.toHaveBeenCalled()
  })

  it('changes the query order when risk score sort is clicked', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 1, username: 'Sort me', email: 'sort@example.com', status: 'active' }], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    const table = wrapper.findComponent(DataTable)
    expect(table.props('serverSideSort')).toBe(true)
    expect(table.props('columns')).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'riskScore', sortable: true })]))
    table.vm.$emit('sort', 'riskScore', 'asc')
    await flushPromises()
    expect(userRiskControlV2API.listUsers).toHaveBeenLastCalledWith(expect.objectContaining({ sortBy: 'risk_score', sortOrder: 'asc' }))
  })

  it('keeps pagination fixed and places the batch bar inside the table area', async () => {
    vi.mocked(userRiskControlV2API.listUsers).mockResolvedValue({ items: [{ id: 7, username: 'Alice', status: 'active' }], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlUsersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.findComponent(TablePageLayout).vm.$slots.pagination).toBeTruthy()
    await wrapper.get('[data-testid="user-select-7"]').setValue(true)
    expect(wrapper.get('.layout-section-scrollable [data-testid="batch-action-bar"]').exists()).toBe(true)
  })
})
