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
      claimReviewCase: vi.fn(),
      submitReviewFeedback: vi.fn(),
      setUserStatus: vi.fn(),
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
    vi.mocked(userRiskControlV2API.getUserDetail).mockResolvedValue({ user, events: [], audit: [] })
    vi.mocked(userRiskControlV2API.claimReviewCase).mockResolvedValue()
    vi.mocked(userRiskControlV2API.submitReviewFeedback).mockResolvedValue()
    vi.mocked(userRiskControlV2API.setUserStatus).mockResolvedValue({ user, result: 'success' })
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

    expect(userRiskControlV2API.submitReviewFeedback).toHaveBeenCalledWith(31, 'insufficient_evidence', 'Evidence does not establish abuse')
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

  it('shows rejected status actions and keeps the confirmation context', async () => {
    vi.mocked(userRiskControlV2API.setUserStatus).mockRejectedValue(new Error('目标账号已被其他管理员处理'))
    const wrapper = mountDrawer({ risk_score: 80, risk_level: 'high', risk_type: 'login_failure' })
    await flushPromises()

    await wrapper.get('[data-testid="ban-user"]').trigger('click')
    await wrapper.get('[data-testid="status-reason"] textarea').setValue('人工确认风险')
    await wrapper.get('[data-testid="confirm-status-stub"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="status-action-error"]').text()).toContain('目标账号已被其他管理员处理')
    expect(wrapper.find('[data-testid="confirm-status-stub"]').exists()).toBe(true)
    expect(wrapper.emitted('updated')).toBeUndefined()
  })
})
