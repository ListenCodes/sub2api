import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UserRiskIdentityDetail from './UserRiskIdentityDetail.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/api/admin/userRiskControlV2', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/userRiskControlV2')>()
  return { ...actual, userRiskControlV2API: { getUserIdentitySummary: vi.fn(), getIdentityHealth: vi.fn(), listUserIPIdentities: vi.fn(), listUserDeviceIdentities: vi.fn(), listAssociatedUsers: vi.fn() } }
})

describe('UserRiskIdentityDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(userRiskControlV2API.getUserIdentitySummary).mockResolvedValue({ user_id: 7, identity_version: 'v2', mode: 'shadow', overall_score: 60, legacy_notice: 'legacy', domains: [{ domain: 'ip', state: 'healthy', score: 60, signal_count: 1, associated_account_count: 2, signals: [{ rule_code: 'v2_registration_ip_accounts', score: 60, evidence_count: 2, occurred_at: '2026-08-12T00:00:00Z' }] }] })
    vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', schema: 'v2', geo_source: 'cloudflare_or_local', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_24h: {} })
    vi.mocked(userRiskControlV2API.listUserIPIdentities).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(userRiskControlV2API.listUserDeviceIdentities).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(userRiskControlV2API.listAssociatedUsers).mockResolvedValue({ items: [], total: 0 })
  })

  it('loads only summary and health until a detail tab is activated', async () => {
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    expect(userRiskControlV2API.getUserIdentitySummary).toHaveBeenCalledWith(7)
    expect(userRiskControlV2API.listUserIPIdentities).not.toHaveBeenCalled()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenCalledWith(7, 1, 20, '')
    expect(userRiskControlV2API.listUserDeviceIdentities).not.toHaveBeenCalled()
  })

  it('applies and clears an exact IP query from the IP evidence tab', async () => {
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()

    const input = wrapper.get('input[type="search"]')
    await input.setValue(' 8.8.8.8 ')
    await wrapper.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenLastCalledWith(7, 1, 20, '8.8.8.8')

    await wrapper.get('button[title="common.close"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenLastCalledWith(7, 1, 20, '')
  })

  it('renders signal reasons and data quality without loading full IP details', async () => {
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('v2_registration_ip_accounts')
    expect(wrapper.text()).toContain('admin.userRiskControl.identityDataQuality')
    expect(userRiskControlV2API.listUserIPIdentities).not.toHaveBeenCalled()
  })

  it('renders the Stage 0 disabled state without requesting identity evidence', async () => {
    vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: false, admin_enabled: false, mode: 'shadow', schema: 'v2', geo_source: 'cloudflare_or_local', domains: { ip: 'disabled', device: 'disabled', composite: 'disabled' }, quality_24h: {} })

    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('admin.userRiskControl.identityDisabled')
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(userRiskControlV2API.getUserIdentitySummary).not.toHaveBeenCalled()
    expect(userRiskControlV2API.listUserIPIdentities).not.toHaveBeenCalled()
  })

  it('keeps a real health failure as a retryable service error', async () => {
    vi.mocked(userRiskControlV2API.getIdentityHealth).mockRejectedValue(new Error('risk identity service unavailable'))

    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('risk identity service unavailable')
    expect(wrapper.text()).toContain('admin.userRiskControl.retry')
  })
})
