import { flushPromises, mount } from '@vue/test-utils'
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

function mountDrawer(overrides: Partial<RiskUserRow> = {}) {
  return mount(UserRiskControlUserDrawer, {
    props: { user: { ...user, ...overrides } },
    global: {
      stubs: {
        Teleport: true,
        Icon: true,
        UserRiskIdentityDetail: { template: '<div data-testid="identity-detail" />' },
        ConfirmDialog: { template: '<div><slot /></div>' },
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
})
