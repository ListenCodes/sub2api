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
    const columns = wrapper.findComponent(DataTable).props('columns') as Array<{ key: string; class?: string }>
    expect(columns.find((column) => column.key === 'reason')?.class).toContain('whitespace-normal')
    expect(columns.find((column) => column.key === 'target')?.class).toContain('break-all')
	expect(wrapper.text()).toContain('暂无处置记录')
	expect(wrapper.text()).not.toContain('暂无匹配账号')
  })

	it('clears stale audit rows when the next category fails to load', async () => {
		vi.mocked(userRiskControlV2API.listAudit).mockResolvedValueOnce({ items: [{ id: 9, actor: '11', action: 'ban', target_type: 'user', target_id: '7', target_user_id: 7, result: 'success', reason: '旧处置记录', created_at: '2026-07-11T12:00:00Z' }], total: 1 }).mockRejectedValueOnce(new Error('审计服务暂时不可用'))
		const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		expect(wrapper.text()).toContain('旧处置记录')
		await wrapper.get('[data-testid="audit-category-configuration"]').trigger('click')
		await flushPromises()
		expect(wrapper.text()).toContain('审计服务暂时不可用')
		expect(wrapper.text()).not.toContain('旧处置记录')
		expect(wrapper.findComponent(Pagination).exists()).toBe(false)
	})

	it('does not render free-text identifiers from sensitive query failures', async () => {
		vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 12, actor: '11', action: 'view_identity_detail', target_type: 'user', target_id: '7', target_user_id: 7, result: 'failed', reason: '', failure_reason: '查询 198.51.100.17 / session secret-session 失败', metadata: { sections: ['ip_identity'] }, created_at: '2026-07-11T12:00:00Z' }], total: 1 })
		const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		expect(wrapper.text()).toContain('敏感详情查询未完成')
		expect(wrapper.text()).not.toContain('198.51.100.17')
		expect(wrapper.text()).not.toContain('secret-session')
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
		expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ category: 'disposition', action: 'ban', result: 'failed', page: 1, pageSize: 20 })
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
		expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ category: 'disposition', action: 'ban', result: '', page: 1, pageSize: 20 })
  })

  it('resets active audit filters and supports changing the page size', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [], total: 80, page: 1, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    emitFilter(wrapper, 'audit-action-filter', 'ban')
    await flushPromises()
    await wrapper.get('[data-testid="reset-audit-filters"]').trigger('click')
    await flushPromises()
		expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ category: 'disposition', action: '', result: '', page: 1, pageSize: 20 })

    wrapper.findComponent(Pagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
		expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ category: 'disposition', action: '', result: '', page: 1, pageSize: 50 })
  })

  it('moves to the next audit page using the server total', async () => {
    vi.mocked(userRiskControlV2API.listAudit)
      .mockResolvedValueOnce({ items: [], total: 21, page: 1, page_size: 20 })
      .mockResolvedValueOnce({ items: [], total: 21, page: 2, page_size: 20 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
		expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith({ category: 'disposition', action: '', result: '', page: 2, pageSize: 20 })
  })

  it('prioritizes administrator and target accounts while folding batch identifiers', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{
      id: 3,
      actor: '11',
      actor_account: { id: 11, email: 'admin@example.com', username: 'Admin', status: 'active', availability: 'available' },
      action: 'ban',
      target_type: 'user',
      target_id: '7',
      target_user_id: 7,
      target_account: { id: 7, email: 'alice@example.com', username: 'Alice', status: 'disabled', availability: 'available' },
      result: 'failed',
      reason: '重复登录失败',
      failure_reason: '账号已经封禁',
      batch_id: 'batch-1',
      request_id: 'request-1',
      before_status: 'active',
      after_status: 'disabled',
      created_at: '2026-07-11T12:00:00Z',
    }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('封禁账号')
    expect(wrapper.text()).toContain('失败')
    expect(wrapper.text()).toContain('账号已经封禁')
    expect(wrapper.text()).toContain('admin@example.com')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('账号 #7')
    expect(wrapper.text()).not.toContain('batch-1')
    expect(wrapper.text()).not.toContain('request-1')
    await wrapper.get('[data-testid="audit-technical-3"]').trigger('click')
    expect(wrapper.text()).toContain('batch-1')
    expect(wrapper.text()).toContain('request-1')
  })

  it('does not invent account status changes for rule operations', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 4, actor: 'qa-admin', action: 'update_rule', target_type: 'rule', target_id: 'login_failure', target_user_id: 0, result: 'success', reason: '调整阈值', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.get('[data-testid="audit-status-change-4"]').text()).toBe('-')
  })

  it('uses a readable identity-rule target and folds its internal code by default', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 6, actor: '11', action: 'disable_identity_rule', target_type: 'identity_rule', target_id: 'v2_registration_ip_accounts', result: 'success', reason: '人工停用', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()

    expect(wrapper.text()).toContain('同真实 IP 多账号注册')
    expect(wrapper.text()).not.toContain('v2_registration_ip_accounts')
    await wrapper.get('[data-testid="audit-technical-6"]').trigger('click')
    expect(wrapper.text()).toContain('v2_registration_ip_accounts')
  })

  it('separates audit categories and keeps identity detail metadata folded by default', async () => {
    vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 8, actor: '11', action: 'view_identity_detail', target_type: 'user', target_id: '7', target_user_id: 7, result: 'success', metadata: { section: 'associated-users' }, batch_id: 'sensitive-batch', request_id: 'sensitive-request', created_at: '2026-07-11T12:00:00Z' }], total: 1 })
    const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()

    expect(wrapper.text()).not.toContain('关联账号')
		expect(wrapper.text()).not.toContain('sensitive-request')
		expect(wrapper.find('[data-testid="audit-technical-8"]').exists()).toBe(true)
		await wrapper.get('[data-testid="audit-technical-8"]').trigger('click')
		expect(wrapper.text()).toContain('sensitive-batch')
		expect(wrapper.text()).toContain('sensitive-request')
    await wrapper.get('[data-testid="toggle-sensitive-audit-8"]').trigger('click')
    expect(wrapper.text()).toContain('关联账号')

	await wrapper.get('[data-testid="audit-category-configuration"]').trigger('click')
    await flushPromises()
	expect(userRiskControlV2API.listAudit).toHaveBeenLastCalledWith(expect.objectContaining({ category: 'configuration', action: '' }))
  })

	it('shows only allowlisted sensitive sections and never echoes free-text sensitive values', async () => {
		vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 10, actor: '11', action: 'view_identity_detail', target_type: 'user', target_id: '7', target_user_id: 7, result: 'success', reason: '查看 198.51.100.17', metadata: { sections: ['ip-identities', 'raw-session-secret'] }, created_at: '2026-07-11T12:00:00Z' }], total: 1 })
		const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
		await flushPromises()
		expect(wrapper.text()).toContain('查看身份详情 · 2 个分区')
		expect(wrapper.text()).not.toContain('198.51.100.17')
		expect(wrapper.text()).not.toContain('raw-session-secret')
		await wrapper.get('[data-testid="toggle-sensitive-audit-10"]').trigger('click')
		expect(wrapper.text()).toContain('IP 身份、其他身份分区')
		expect(wrapper.text()).not.toContain('raw-session-secret')
	})

	it('formats rule diffs with readable values', async () => {
		vi.mocked(userRiskControlV2API.listAudit).mockResolvedValue({ items: [{ id: 11, actor: '11', action: 'update_rule', target_type: 'rule', target_id: 'login_failure', target_user_id: 0, result: 'success', reason: '调整规则', metadata: { diff: { enabled: { before: true, after: false }, action: { before: 'review', after: 'reject_candidate' }, count_strategy: { before: 'user_events', after: 'ip_distinct_success_users' } } }, created_at: '2026-07-11T12:00:00Z' }], total: 1 })
		const wrapper = mount(UserRiskControlAuditView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		expect(wrapper.text()).toContain('已启用 → 已停用')
		expect(wrapper.text()).toContain('人工复核 → 拒绝注册')
		expect(wrapper.text()).toContain('按用户事件计数 → IP 去重成功用户')
		expect(wrapper.text()).not.toContain('reject_candidate')
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
