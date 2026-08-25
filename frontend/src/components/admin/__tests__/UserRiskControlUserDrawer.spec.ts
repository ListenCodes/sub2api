import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UserRiskControlUserDrawer from '@/components/admin/UserRiskControlUserDrawer.vue'
import { userRiskControlV2API, type RiskUserRow } from '@/api/admin/userRiskControlV2'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/api/admin/userRiskControlV2', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/userRiskControlV2')>()
  return {
    ...actual,
    userRiskControlV2API: {
      getUserDetail: vi.fn(),
	  getReviewCase: vi.fn(),
      claimReviewCase: vi.fn(),
	  createReviewCase: vi.fn(),
	  observeReviewCase: vi.fn(),
      submitReviewFeedback: vi.fn(),
	  resolveReviewCase: vi.fn(),
      setUserStatus: vi.fn(),
	  retryUserSessionRevocation: vi.fn(),
    },
  }
})

const user = {
  id: 7,
  username: 'Alice',
  email: 'alice@example.com',
  status: 'active' as const,
  case_id: 31,
  case_status: 'pending' as const,
}

const IdentityDetailStub = defineComponent({
  name: 'UserRiskIdentityDetail',
  props: { userId: { type: Number, required: true } },
  emits: ['investigate', 'tab-change'],
  template: '<div data-testid="identity-detail" />',
})

const ConfirmDialogStub = defineComponent({
  props: { show: Boolean },
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show"><slot /><button data-testid="confirm-status-stub" @click="$emit(\'confirm\')">confirm</button></div>',
})

function mountDrawer(overrides: Partial<RiskUserRow> = {}) {
  return mount(UserRiskControlUserDrawer, {
    props: { user: { ...user, ...overrides } },
    global: {
      stubs: {
        Teleport: true,
        Icon: true,
        UserRiskIdentityDetail: IdentityDetailStub,
        ConfirmDialog: ConfirmDialogStub,
      },
    },
  })
}

describe('UserRiskControlUserDrawer review case workflow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
	window.sessionStorage.clear()
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({ user, events: [], audit: [] })
	vi.mocked(userRiskControlV2API.claimReviewCase).mockResolvedValue({ status: 'in_review', revision: 2 })
	vi.mocked(userRiskControlV2API.createReviewCase).mockResolvedValue({ id: 32, status: 'pending', revision: 1 })
	vi.mocked(userRiskControlV2API.observeReviewCase).mockResolvedValue({ status: 'observing', review_due_at: '2026-08-27T00:00:00Z', observation_goal: '等待补充证据', revision: 2 })
	vi.mocked(userRiskControlV2API.submitReviewFeedback).mockResolvedValue()
	vi.mocked(userRiskControlV2API.getReviewCase).mockResolvedValue({ id: 31, user_id: 7, status: 'in_review', revision: 3 })
	vi.mocked(userRiskControlV2API.resolveReviewCase).mockResolvedValue({ result: 'success', request_id: 'request-1', retryable: false, account: { user_id: 7, action: 'none', result: 'skipped' }, case: { id: 31, result: 'resolved' } })
	vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValue({ user, result: 'success', requestId: 'risk-request-7', retryable: false })
	vi.mocked(userRiskControlV2API.retryUserSessionRevocation).mockResolvedValue({ user: { ...user, status: 'disabled' }, result: 'success', requestId: 'risk-request-7', retryable: false })
  })

  it('claims a pending case without changing the account status', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    await wrapper.get('[data-testid="claim-review-case"]').trigger('click')
    await flushPromises()

    expect(userRiskControlV2API.claimReviewCase).toHaveBeenCalledWith(31)
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
    expect(wrapper.emitted('case-claimed')?.[0]?.[0]).toMatchObject({ id: 7, status: 'active', case_status: 'in_review' })
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.find('[data-testid="review-feedback-type"]').exists()).toBe(true)
  })

	it('creates a manual review case with a required operator reason', async () => {
		const wrapper = mountDrawer({ case_id: undefined, case_status: undefined })
		await flushPromises()
		await wrapper.get('[data-testid="create-review-case"]').trigger('click')
		expect(userRiskControlV2API.createReviewCase).not.toHaveBeenCalled()
		expect(wrapper.text()).toContain('建案原因不能为空')

		await wrapper.get('[data-testid="manual-case-reason"] textarea').setValue('客服升级的异常注册线索')
		await wrapper.get('[data-testid="create-review-case"]').trigger('click')
		await flushPromises()
		expect(userRiskControlV2API.createReviewCase).toHaveBeenCalledWith(7, '客服升级的异常注册线索', 'pending', 'manual_review', undefined)
		expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ id: 7, case_id: 32, case_status: 'pending' })
	})

	it('keeps unclaimed pending cases in the claim-only workflow', async () => {
		const wrapper = mountDrawer()
		await flushPromises()

		expect(wrapper.find('[data-testid="observe-review-case"]').exists()).toBe(false)
		expect(wrapper.find('[data-testid="observation-goal"]').exists()).toBe(false)
		expect(wrapper.get('[data-testid="claim-review-case"]').exists()).toBe(true)
		expect(userRiskControlV2API.observeReviewCase).not.toHaveBeenCalled()
		expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
	})

  it('requires claim and a reason before recording feedback without enforcing an account action', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    expect(wrapper.find('[data-testid="submit-review-feedback"]').exists()).toBe(false)
    await wrapper.get('[data-testid="claim-review-case"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="submit-review-feedback"]').trigger('click')
    expect(userRiskControlV2API.submitReviewFeedback).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.userRiskControl.feedbackReasonRequired')

    await wrapper.get('[data-testid="review-feedback-reason"] textarea').setValue('Evidence does not establish abuse')
    await wrapper.get('[data-testid="submit-review-feedback"]').trigger('click')
    await flushPromises()

		expect(userRiskControlV2API.resolveReviewCase).toHaveBeenCalledWith(31, 7, 'insufficient_evidence', 'Evidence does not establish abuse', 'none', 2, expect.any(String))
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ id: 7, status: 'active', case_status: 'resolved', pending: false })
  })

  it('does not offer ban or unban for a pending account', async () => {
    const wrapper = mountDrawer({ status: 'pending' })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="unban-user"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.userRiskControl.pendingAccountNoStatusAction')
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
  })

	it('locks the original resolution intent after a partial completion', async () => {
		vi.mocked(userRiskControlV2API.resolveReviewCase).mockResolvedValue({ result: 'partial', request_id: 'request-partial', retryable: true, account: { user_id: 7, action: 'none', result: 'skipped' }, case: { id: 31, result: 'failed', failure_reason: 'extension unavailable' } })
		const wrapper = mountDrawer()
		await flushPromises()
		await wrapper.get('[data-testid="claim-review-case"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="review-feedback-reason"] textarea').setValue('Evidence confirms abuse')
		await wrapper.get('[data-testid="submit-review-feedback"]').trigger('click')
		await flushPromises()

		expect(wrapper.get('[data-testid="review-feedback-type"] button').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="review-feedback-reason"] textarea').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="review-account-action"] button').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="submit-review-feedback"]').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="observe-review-case"]').attributes('disabled')).toBeDefined()
		await wrapper.get('[data-testid="observe-review-case"]').trigger('click')
		expect(userRiskControlV2API.observeReviewCase).not.toHaveBeenCalled()
		expect(wrapper.get('[data-testid="retry-resolve-case"]').exists()).toBe(true)
	})

	it('keeps account recovery available when the case is resolved first', async () => {
		vi.mocked(userRiskControlV2API.resolveReviewCase).mockResolvedValue({ result: 'partial', request_id: 'request-account-retry', retryable: true, account: { user_id: 7, action: 'disable', result: 'failed', retryable: true, pending_step: 'status_confirmation', failure_reason: 'database unavailable' }, case: { id: 31, result: 'resolved' } })
		const wrapper = mountDrawer()
		await flushPromises()
		await wrapper.get('[data-testid="claim-review-case"]').trigger('click')
		await flushPromises()
		wrapper.findAllComponents({ name: 'Select' })[1].vm.$emit('update:modelValue', 'disable')
		await wrapper.get('[data-testid="review-feedback-reason"] textarea').setValue('Evidence confirms abuse')
		await wrapper.get('[data-testid="submit-review-feedback"]').trigger('click')
		await flushPromises()
		expect(wrapper.get('[data-testid="status-action-warning"]').text()).toContain('database unavailable')
		expect(wrapper.get('[data-testid="retry-session-revocation"]').text()).toContain('重试状态确认')
		expect(JSON.parse(window.sessionStorage.getItem('sub2api:risk-account-recovery:7') || '{}')).toMatchObject({ requestId: 'request-account-retry', status: 'disabled', pendingStep: 'status_confirmation' })
		expect(wrapper.emitted('updated')?.at(-1)?.[0]).toMatchObject({ case_status: 'resolved', pending: false })
	})

  it('keeps an exact in-drawer investigation stack and returns to the source account', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    wrapper.findComponent(IdentityDetailStub).vm.$emit('investigate', {
      user_id: 9,
      relation: 'composite',
      evidence_strength: 'high',
      account: { id: 9, email: 'related@example.com', username: 'Related', status: 'active', availability: 'available', deleted: false, created_at: '' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('related@example.com')
    expect(wrapper.findComponent(IdentityDetailStub).props('userId')).toBe(9)
    expect(wrapper.get('[data-testid="investigation-back"]').text()).toContain('Alice')
    await wrapper.get('[data-testid="investigation-back"]').trigger('click')
    expect(wrapper.findComponent(IdentityDetailStub).props('userId')).toBe(7)
    expect(wrapper.text()).toContain('alice@example.com')
  })

  it('waits for exact risk completion before choosing the associated account action level', async () => {
    let resolveAssociated!: (detail: Awaited<ReturnType<typeof userRiskControlV2API.getUserDetail>>) => void
    const associatedDetail = new Promise<Awaited<ReturnType<typeof userRiskControlV2API.getUserDetail>>>((resolve) => { resolveAssociated = resolve })
    vi.mocked(userRiskControlV2API.getUserDetail)
      .mockResolvedValueOnce({ user, events: [], audit: [] })
      .mockReturnValueOnce(associatedDetail)
    const wrapper = mountDrawer()
    await flushPromises()

    wrapper.findComponent(IdentityDetailStub).vm.$emit('investigate', {
      user_id: 9,
      relation: 'composite',
      evidence_strength: 'high',
      account: { id: 9, email: 'related@example.com', username: 'Related', status: 'active', availability: 'available', deleted: false, created_at: '' },
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="secondary-account-actions"]').exists()).toBe(false)

    resolveAssociated({
        user: { id: 9, email: 'related@example.com', username: 'Related', status: 'active', risk_score: 88, risk_level: 'high', risk_type: 'v2_registration_composite_accounts', case_id: 41, case_status: 'pending' },
        events: [],
        audit: [],
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="secondary-account-actions"]').exists()).toBe(false)
  })

  it('keeps an associated account non-actionable when exact completion fails', async () => {
    vi.mocked(userRiskControlV2API.getUserDetail)
      .mockResolvedValueOnce({ user, events: [], audit: [] })
      .mockRejectedValueOnce(new Error('account completion unavailable'))
    const wrapper = mountDrawer()
    await flushPromises()

    wrapper.findComponent(IdentityDetailStub).vm.$emit('investigate', {
      user_id: 9,
      relation: 'ip',
      evidence_strength: 'weak',
      account: { id: 9, email: 'related@example.com', username: 'Related', status: '', availability: 'available', deleted: false, created_at: '' },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="unban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="secondary-account-actions"]').exists()).toBe(false)
  })

  it('does not expose account status actions when an associated account cannot be completed', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    wrapper.findComponent(IdentityDetailStub).vm.$emit('investigate', {
      user_id: 9,
      relation: 'ip',
      evidence_strength: 'weak',
      account: { id: 9, email: '', username: '', status: '', availability: 'deleted', deleted: true, created_at: '' },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="unban-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="secondary-account-actions"]').exists()).toBe(false)
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
  })

  it('moves ban into secondary actions for a normal zero-risk account', async () => {
    const wrapper = mountDrawer({ case_id: undefined, case_status: undefined, risk_score: 0, risk_level: null, risk_type: null })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="secondary-account-actions"]').exists()).toBe(true)
    await wrapper.get('[data-testid="secondary-account-actions"]').trigger('click')
    expect(wrapper.get('[data-testid="ban-user-secondary"]').text()).toContain('admin.userRiskControl.ban')
    expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
  })

  it.each(['v2_api_client_accounts', 'api_request'])('keeps a zero-point %s observation in secondary actions', async (riskType) => {
    const wrapper = mountDrawer({ case_id: undefined, case_status: undefined, risk_score: 0, risk_level: null, risk_type: riskType })
    await flushPromises()

    expect(wrapper.find('[data-testid="ban-user"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="secondary-account-actions"]').exists()).toBe(true)
  })

  it('keeps a partial status result visible without closing the investigation', async () => {
    vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValue({
      user: { ...user, status: 'disabled' },
      result: 'partial',
      failureReason: '账号状态已更新，但活动会话撤销失败',
	  requestId: 'risk-request-7',
	  retryable: true,
	  pendingStep: 'session_revocation',
    })
    const wrapper = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
    await flushPromises()

    await wrapper.get('[data-testid="ban-user"]').trigger('click')
    await wrapper.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
    await wrapper.get('[data-testid="confirm-status-stub"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="status-action-warning"]').text()).toContain('账号状态已更新，但活动会话撤销失败')
    expect(wrapper.emitted('status-partial')?.[0]?.[0]).toMatchObject({ id: 7, status: 'disabled' })
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

	it('retries only the unfinished session cleanup without reopening the status dialog', async () => {
		vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValue({ user: { ...user, status: 'disabled' }, result: 'partial', failureReason: '会话撤销失败', requestId: 'risk-request-7', retryable: true, pendingStep: 'session_revocation' })
		const wrapper = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		await wrapper.get('[data-testid="ban-user"]').trigger('click')
		await wrapper.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
		await wrapper.get('[data-testid="confirm-status-stub"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="retry-session-revocation"]').trigger('click')
		await flushPromises()
		expect(userRiskControlV2API.retryUserSessionRevocation).toHaveBeenCalledWith(7, '人工确认风险', 'risk-request-7', undefined)
		expect(wrapper.find('[data-testid="status-action-warning"]').exists()).toBe(false)
		expect(wrapper.find('[data-testid="status-confirmation"]').exists()).toBe(false)
	})

	it('restores unfinished account cleanup after the drawer is reopened', async () => {
		vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValue({ user: { ...user, status: 'disabled' }, result: 'partial', failureReason: '会话撤销失败', requestId: 'risk-request-persisted', retryable: true, pendingStep: 'session_revocation' })
		const first = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		await first.get('[data-testid="ban-user"]').trigger('click')
		await first.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
		await first.get('[data-testid="confirm-status-stub"]').trigger('click')
		await flushPromises()
		first.unmount()

		const reopened = mountDrawer({ status: 'disabled', risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		expect(reopened.get('[data-testid="status-action-warning"]').text()).toContain('仍有步骤需要恢复')
		expect(reopened.find('[data-testid="unban-user"]').exists()).toBe(false)
		expect(reopened.get('[data-testid="retry-session-revocation"]').text()).toContain('重试会话清理')
	})

	it('keeps the original request available when a status response is lost', async () => {
		vi.mocked(userRiskControlV2API.setUserStatus).mockRejectedValueOnce({ status: 504, message: 'gateway timeout' })
		const first = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		await first.get('[data-testid="ban-user"]').trigger('click')
		await first.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
		await first.get('[data-testid="confirm-status-stub"]').trigger('click')
		await flushPromises()
		const requestId = vi.mocked(userRiskControlV2API.setUserStatus).mock.calls[0][4]
		expect(first.get('[data-testid="status-action-warning"]').text()).toContain('结果未知')
		expect(first.get('[data-testid="retry-session-revocation"]').text()).toContain('重试状态确认')
		first.unmount()

		vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValueOnce({ user: { ...user, status: 'disabled' }, result: 'success', requestId: requestId!, retryable: false })
		const reopened = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		expect(reopened.get('[data-testid="status-action-warning"]').text()).toContain('结果未知')
		await reopened.get('[data-testid="retry-session-revocation"]').trigger('click')
		await flushPromises()
		expect(userRiskControlV2API.setUserStatus).toHaveBeenLastCalledWith(7, 'disabled', '人工确认风险', undefined, requestId)
		expect(reopened.find('[data-testid="status-action-warning"]').exists()).toBe(false)
	})

	it('restores a lost case-resolution response and retries the exact original intent', async () => {
		vi.mocked(userRiskControlV2API.resolveReviewCase).mockRejectedValueOnce({ status: 504, message: 'gateway timeout' })
		const first = mountDrawer()
		await flushPromises()
		await first.get('[data-testid="claim-review-case"]').trigger('click')
		await flushPromises()
		first.findAllComponents({ name: 'Select' })[1].vm.$emit('update:modelValue', 'disable')
		await first.get('[data-testid="review-feedback-reason"] textarea').setValue('确认滥用并禁用账号')
		await first.get('[data-testid="submit-review-feedback"]').trigger('click')
		await flushPromises()
		const originalCall = vi.mocked(userRiskControlV2API.resolveReviewCase).mock.calls[0]
		const requestID = originalCall[6]
		expect(first.text()).toContain('结案请求结果未知')
		expect(JSON.parse(window.sessionStorage.getItem('sub2api:risk-case-resolution-recovery:31') || '{}')).toMatchObject({ requestId: requestID, reason: '确认滥用并禁用账号', accountAction: 'disable' })
		first.unmount()

		vi.mocked(userRiskControlV2API.resolveReviewCase).mockResolvedValueOnce({ result: 'success', request_id: requestID!, retryable: false, account: { user_id: 7, action: 'disable', result: 'success', after_status: 'disabled' }, case: { id: 31, result: 'resolved' } })
		const reopened = mountDrawer({ case_status: 'resolved', case_revision: 2 })
		await flushPromises()
		expect(reopened.get('[data-testid="retry-resolve-case"]').exists()).toBe(true)
		await reopened.get('[data-testid="retry-resolve-case"]').trigger('click')
		await flushPromises()
		expect(userRiskControlV2API.resolveReviewCase).toHaveBeenLastCalledWith(31, 7, 'insufficient_evidence', '确认滥用并禁用账号', 'disable', 2, requestID)
		expect(window.sessionStorage.getItem('sub2api:risk-case-resolution-recovery:31')).toBeNull()
	})

	it('does not send an account mutation when recovery state cannot be stored', async () => {
		const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('blocked', 'SecurityError') })
		const wrapper = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		await wrapper.get('[data-testid="ban-user"]').trigger('click')
		await wrapper.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
		await wrapper.get('[data-testid="confirm-status-stub"]').trigger('click')
		await flushPromises()
		expect(userRiskControlV2API.setUserStatus).not.toHaveBeenCalled()
		expect(wrapper.get('[data-testid="status-confirmation-error"]').text()).toContain('无法保存恢复信息')
		storage.mockRestore()
	})

	it('clears a recovery that receives a definitive normalized 4xx', async () => {
		window.sessionStorage.setItem('sub2api:risk-account-recovery:7', JSON.stringify({ reason: '人工确认风险', requestId: 'request-7', status: 'disabled', pendingStep: 'status_confirmation' }))
		vi.mocked(userRiskControlV2API.setUserStatus).mockRejectedValueOnce({ status: 409, message: '目标账号已被其他管理员处理' })
		const wrapper = mountDrawer({ status: 'disabled', risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
		await flushPromises()
		await wrapper.get('[data-testid="retry-session-revocation"]').trigger('click')
		await flushPromises()
		expect(window.sessionStorage.getItem('sub2api:risk-account-recovery:7')).toBeNull()
		expect(wrapper.find('[data-testid="status-action-warning"]').exists()).toBe(false)
		expect(wrapper.find('[data-testid="unban-user"]').exists()).toBe(true)
	})

	it('reloads a non-retryable partial case and unlocks the refreshed revision', async () => {
		vi.mocked(userRiskControlV2API.resolveReviewCase).mockResolvedValue({ result: 'partial', request_id: 'request-stale', retryable: false, account: { user_id: 7, action: 'none', result: 'not_executed' }, case: { id: 31, result: 'failed', failure_reason: 'revision changed' } })
		vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({ user: { ...user, case_status: 'in_review', case_revision: 4 }, events: [], audit: [] })
		vi.mocked(userRiskControlV2API.getReviewCase).mockResolvedValue({ id: 31, user_id: 7, status: 'in_review', revision: 4 })
		const wrapper = mountDrawer()
		await flushPromises()
		await wrapper.get('[data-testid="claim-review-case"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="review-feedback-reason"] textarea').setValue('证据需要复核')
		await wrapper.get('[data-testid="submit-review-feedback"]').trigger('click')
		await flushPromises()
		expect(wrapper.find('[data-testid="retry-resolve-case"]').exists()).toBe(false)
		await wrapper.get('[data-testid="reload-review-case"]').trigger('click')
		await flushPromises()
		expect(wrapper.find('[data-testid="resolution-step-results"]').exists()).toBe(false)
		expect(wrapper.get('[data-testid="submit-review-feedback"]').attributes('disabled')).toBeUndefined()
	})

	it('shows a retry action when legacy detail loading fails', async () => {
		vi.mocked(userRiskControlV2API.getUserDetail).mockRejectedValueOnce(new Error('旧详情服务暂时不可用')).mockResolvedValueOnce({ user, events: [], audit: [] })
		const wrapper = mountDrawer()
		await flushPromises()
		expect(wrapper.get('[data-testid="legacy-detail-error"]').text()).toContain('旧详情服务暂时不可用')
		await wrapper.get('[data-testid="retry-legacy-detail"]').trigger('click')
		await flushPromises()
		expect(wrapper.find('[data-testid="legacy-detail-error"]').exists()).toBe(false)
		expect(wrapper.find('[data-testid="legacy-risk-timeline"]').exists()).toBe(true)
	})

  it('shows rejected status actions and keeps the confirmation context', async () => {
	vi.mocked(userRiskControlV2API.setUserStatus).mockRejectedValue({ status: 409, message: '目标账号已被其他管理员处理' })
    const wrapper = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
    await flushPromises()

    await wrapper.get('[data-testid="ban-user"]').trigger('click')
    await wrapper.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
    await wrapper.get('[data-testid="confirm-status-stub"]').trigger('click')
    await flushPromises()

		expect(wrapper.get('[data-testid="status-confirmation-error"]').text()).toContain('目标账号已被其他管理员处理')
    expect(wrapper.find('[data-testid="confirm-status-stub"]').exists()).toBe(true)
    expect(wrapper.emitted('updated')).toBeUndefined()
  })
})
