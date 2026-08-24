import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserRiskIdentityDetail from './UserRiskIdentityDetail.vue'
import { userRiskControlV2API } from '@/api/admin/userRiskControlV2'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/api/admin/userRiskControlV2', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/userRiskControlV2')>()
  return { ...actual, userRiskControlV2API: { getUserIdentitySummary: vi.fn(), getIdentityHealth: vi.fn(), listUserIPIdentities: vi.fn(), listUserDeviceIdentities: vi.fn(), listAssociatedUsers: vi.fn(), previewNetworkLabel: vi.fn(), applyNetworkLabel: vi.fn(), revokeNetworkLabel: vi.fn() } }
})

describe('UserRiskIdentityDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(userRiskControlV2API.getUserIdentitySummary).mockResolvedValue({ user_id: 7, identity_version: 'v2', mode: 'shadow', overall_score: 60, legacy_notice: 'legacy', domains: [{ domain: 'ip', state: 'healthy', score: 60, signal_count: 1, associated_account_count: 2, signals: [{ rule_code: 'v2_registration_ip_accounts', score: 60, evidence_count: 2, occurred_at: '2026-08-12T00:00:00Z' }] }] })
    vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', schema: 'v2', geo_source: 'cloudflare_or_local', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_24h: {} })
    vi.mocked(userRiskControlV2API.listUserIPIdentities).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(userRiskControlV2API.listUserDeviceIdentities).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(userRiskControlV2API.listAssociatedUsers).mockResolvedValue({ items: [], total: 0 })
	vi.mocked(userRiskControlV2API.previewNetworkLabel).mockResolvedValue({ network_id: 11, proposed_label: 'unknown', affected_signal_count: 2, affected_account_count: 2, affected_decision_count: 1, resolved_domains: [], requires_rebuild: false })
	vi.mocked(userRiskControlV2API.applyNetworkLabel).mockResolvedValue({ updated: true, impact: { network_id: 11, proposed_label: 'unknown', affected_signal_count: 2, affected_account_count: 2, affected_decision_count: 1, resolved_domains: [], requires_rebuild: false } })
	vi.mocked(userRiskControlV2API.revokeNetworkLabel).mockResolvedValue({ network_id: 11, current_label: 'company', affected_signal_count: 0, affected_account_count: 0, affected_decision_count: 0, resolved_domains: [], requires_rebuild: true })
  })
	afterEach(() => { document.body.innerHTML = '' })

  it('loads only summary and health until a detail tab is activated', async () => {
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    expect(userRiskControlV2API.getUserIdentitySummary).toHaveBeenCalledWith(7, expect.any(String))
    const viewSession = vi.mocked(userRiskControlV2API.getUserIdentitySummary).mock.calls[0][1]
    expect(viewSession).toBeTruthy()
    expect(userRiskControlV2API.listUserIPIdentities).not.toHaveBeenCalled()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenCalledWith(7, 1, 20, '', viewSession)
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
    const viewSession = vi.mocked(userRiskControlV2API.getUserIdentitySummary).mock.calls[0][1]
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenLastCalledWith(7, 1, 20, '8.8.8.8', viewSession)

    await wrapper.get('button[title="common.close"]').trigger('click')
    await flushPromises()
    expect(userRiskControlV2API.listUserIPIdentities).toHaveBeenLastCalledWith(7, 1, 20, '', viewSession)
  })

	it('previews impact and requires a reason before applying a shared-network label', async () => {
		vi.mocked(userRiskControlV2API.listUserIPIdentities).mockResolvedValue({ items: [{ id: 11, ip: '8.8.8.8', ip_family: 4, ip_source: 'remote_addr', is_public: true, country_code: 'US', region: 'CA', city: '', asn: 15169, geo_source: 'cloudflare', geo_verified: true, availability: 'available', data_source: 'identity_v2', first_seen_at: '2026-08-24T00:00:00Z', last_seen_at: '2026-08-24T00:00:00Z', registration_success_count: 2, login_success_count: 0, api_success_count: 0, associated_account_count: 2 }], total: 1 })
		const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
		await flushPromises()
		await wrapper.findAll('[role="tab"]')[1].trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="label-network-11"]').trigger('click')
		await flushPromises()
		expect(document.body.querySelector<HTMLElement>('[role="dialog"]')?.style.zIndex).toBe('80')
		const reason = document.body.querySelector<HTMLTextAreaElement>('[data-testid="network-label-reason"] textarea')!
		reason.value = '无法确认网络性质，先标记未知'
		reason.dispatchEvent(new Event('input', { bubbles: true }))
		document.body.querySelector<HTMLButtonElement>('[data-testid="preview-network-label"]')!.click()
		await flushPromises()

		expect(userRiskControlV2API.previewNetworkLabel).toHaveBeenCalledWith(11, 'unknown')
		expect(document.body.querySelector('[data-testid="network-label-impact"]')?.textContent).toContain('2 个账号')
		document.body.querySelector<HTMLButtonElement>('[data-testid="apply-network-label"]')!.click()
		await flushPromises()
		expect(userRiskControlV2API.applyNetworkLabel).toHaveBeenCalledWith(11, 'unknown', '无法确认网络性质，先标记未知')
	})

	it('requires a revoke preview before removing a shared-network label', async () => {
		vi.mocked(userRiskControlV2API.listUserIPIdentities).mockResolvedValue({ items: [{ id: 11, ip: '8.8.8.8', ip_family: 4, ip_source: 'remote_addr', is_public: true, country_code: 'US', region: 'CA', city: '', asn: 15169, geo_source: 'cloudflare', geo_verified: true, availability: 'available', data_source: 'identity_v2', network_label: 'company', first_seen_at: '2026-08-24T00:00:00Z', last_seen_at: '2026-08-24T00:00:00Z', registration_success_count: 2, login_success_count: 0, api_success_count: 0, associated_account_count: 2 }], total: 1 })
		vi.mocked(userRiskControlV2API.previewNetworkLabel).mockResolvedValue({ network_id: 11, current_label: 'company', affected_signal_count: 0, affected_account_count: 0, affected_decision_count: 0, resolved_domains: [], requires_rebuild: true })
		const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
		await flushPromises()
		await wrapper.findAll('[role="tab"]')[1].trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="label-network-11"]').trigger('click')
		await flushPromises()
		const reason = document.body.querySelector<HTMLTextAreaElement>('[data-testid="network-label-reason"] textarea')!
		reason.value = '网络归类已失效'
		reason.dispatchEvent(new Event('input', { bubbles: true }))
		document.body.querySelector<HTMLButtonElement>('[data-testid="preview-revoke-network-label"]')!.click()
		await flushPromises()

		expect(userRiskControlV2API.revokeNetworkLabel).not.toHaveBeenCalled()
		expect(userRiskControlV2API.previewNetworkLabel).toHaveBeenCalledWith(11, '')
		expect(document.body.textContent).toContain('需完成历史回放')
		document.body.querySelector<HTMLButtonElement>('[data-testid="confirm-revoke-network-label"]')!.click()
		await flushPromises()
		expect(userRiskControlV2API.revokeNetworkLabel).toHaveBeenCalledWith(11, '网络归类已失效')
	})

  it('renders signal reasons and keeps global data diagnostics folded', async () => {
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.text()).toContain('同一公网 IP 出现多个成功注册账号')
    expect(wrapper.text()).not.toContain('v2_registration_ip_accounts')
    expect(wrapper.get('[data-testid="identity-data-diagnostics"]').attributes('open')).toBeUndefined()
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

  it('opens an exact associated account inside the investigation flow and folds technical IDs', async () => {
    vi.mocked(userRiskControlV2API.listAssociatedUsers).mockResolvedValue({
      items: [{
        user_id: 9, relation: 'composite', shared_network_count: 1, shared_browser_instance_count: 1,
        shared_api_client_count: 0, shared_device_count: 1, cooccurring_evidence_count: 2,
        evidence_strength: 'high', evidence_window_seconds: 600, concurrent: true,
        overlap_start: '2026-08-12T00:00:00Z', overlap_end: '2026-08-12T00:01:00Z',
        first_seen_at: '2026-08-12T00:00:00Z', last_seen_at: '2026-08-12T00:01:00Z',
        source_event_ids: [101, 102], limitations: ['shared_context_requires_manual_review'],
        account: { id: 9, email: 'related@example.com', username: 'Related', status: '', availability: 'deleted', deleted: true, created_at: '' },
      }],
      total: 1,
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[3].trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.userRiskControl.drawer.deletedAccount')
    expect(wrapper.text()).not.toContain('来源事件：101, 102')
    expect(wrapper.html()).not.toContain('search=9')
    await wrapper.get('[data-testid="associated-user-9"]').trigger('click')
    expect(wrapper.emitted('investigate')?.[0]?.[0]).toMatchObject({ user_id: 9, evidence_strength: 'high' })
    await wrapper.get('[data-testid="associated-technical-9"]').trigger('click')
    expect(wrapper.text()).toContain('来源事件：101, 102')
    expect(wrapper.text()).not.toContain('shared_context_requires_manual_review')
    expect(wrapper.text()).toContain('共享环境仍需结合业务行为人工复核')
  })

  it('shows the actual evidence range and readable limitations even for a historical association', async () => {
    vi.mocked(userRiskControlV2API.listAssociatedUsers).mockResolvedValue({
      items: [{
        user_id: 9, relation: 'ip', shared_network_count: 1, shared_browser_instance_count: 0,
        shared_api_client_count: 0, shared_device_count: 0, cooccurring_evidence_count: 2,
        evidence_strength: 'weak', evidence_window_seconds: 86400, concurrent: false,
        overlap_start: '2026-08-17T00:00:00Z', overlap_end: '2026-08-18T00:00:00Z',
        first_seen_at: '2026-08-17T00:00:00Z', last_seen_at: '2026-08-18T00:00:00Z',
        source_event_ids: [501, 502], limitations: ['ip_only', 'shared_network_possible'],
        account: { id: 9, email: 'related@example.com', username: 'Related', status: 'active', availability: 'available', deleted: false, created_at: '' },
      }],
      total: 1,
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[3].trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('实际证据范围：')
    expect(wrapper.text()).toContain('判定窗口 1 天')
    expect(wrapper.text()).toContain('仅共享 IP 属于弱证据')
    expect(wrapper.text()).toContain('共享网络可能由多个无关账号共同使用')
    expect(wrapper.text()).not.toContain('存在需人工解释的技术限制；存在需人工解释的技术限制')
  })

  it.each([
    ['available', '正常'],
    ['unavailable', '账号补全暂不可用'],
    ['not_evaluable', '账号状态不可评估'],
    ['deleted', 'admin.userRiskControl.drawer.deletedAccount'],
  ] as const)('keeps the %s account completion state distinct', async (availability, label) => {
    vi.mocked(userRiskControlV2API.listAssociatedUsers).mockResolvedValue({
      items: [{
        user_id: 9, relation: 'ip', shared_network_count: 1, shared_browser_instance_count: 0,
        shared_api_client_count: 0, shared_device_count: 0, cooccurring_evidence_count: 0,
        evidence_strength: 'weak', evidence_window_seconds: 600, concurrent: false,
        first_seen_at: '2026-08-12T00:00:00Z', last_seen_at: '2026-08-12T00:01:00Z',
        source_event_ids: [], limitations: [],
        account: { id: 9, email: 'related@example.com', username: 'Related', status: 'active', availability, deleted: availability === 'deleted', created_at: '' },
      }], total: 1,
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[3].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(label)
  })

  it('states the no-risk conclusion before diagnostics for a normal account', async () => {
    vi.mocked(userRiskControlV2API.getUserIdentitySummary).mockResolvedValue({
      user_id: 7, identity_version: 'v2', mode: 'shadow', overall_score: 0, legacy_notice: '',
      domains: [{ domain: 'ip', state: 'healthy', score: 0, signal_count: 0, associated_account_count: 0, signals: [] }],
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()
    expect(wrapper.get('[data-testid="risk-conclusion"]').text()).toContain('当前未发现需要处理的风险')
    expect(wrapper.find('[data-testid="primary-risk-signal"]').exists()).toBe(false)
  })

  it('never promotes zero-point API observations to the primary risk conclusion', async () => {
    vi.mocked(userRiskControlV2API.getUserIdentitySummary).mockResolvedValue({
      user_id: 7, identity_version: 'v2', mode: 'shadow', overall_score: 0, legacy_notice: '',
      domains: [{ domain: 'device', state: 'healthy', score: 0, signal_count: 1, associated_account_count: 2, signals: [{ rule_code: 'v2_api_client_accounts', score: 0, evidence_count: 3, occurred_at: '2026-08-18T00:00:00Z' }] }],
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.get('[data-testid="risk-conclusion"]').text()).toContain('当前未发现需要处理的风险')
    expect(wrapper.find('[data-testid="primary-risk-signal"]').exists()).toBe(false)
  })

  it('treats an anomalous positive IP-only signal as a data inconsistency instead of a primary risk', async () => {
    vi.mocked(userRiskControlV2API.getUserIdentitySummary).mockResolvedValue({
      user_id: 7, identity_version: 'v2', mode: 'shadow', overall_score: 60, legacy_notice: '',
      domains: [{ domain: 'ip', state: 'healthy', score: 60, signal_count: 1, associated_account_count: 2, signals: [{ rule_code: 'v2_registration_ip_accounts', signal_family: 'registration_ip', score: 60, evidence_count: 2, occurred_at: '2026-08-18T00:00:00Z' }] }],
    })
    const wrapper = mount(UserRiskIdentityDetail, { props: { userId: 7 } })
    await flushPromises()

    expect(wrapper.find('[data-testid="primary-risk-signal"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="risk-conclusion"]').text()).toContain('数据不一致')
    expect(wrapper.get('[data-testid="risk-conclusion"]').text()).toContain('不会直接评分或封禁')
  })
})
