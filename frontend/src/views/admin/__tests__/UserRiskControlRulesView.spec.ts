import { config, enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
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
    getIdentityHealth: vi.fn(),
    listIdentityRules: vi.fn(),
	listIdentityRuleEffects: vi.fn(),
	listIdentityRuleVersions: vi.fn(),
	disableIdentityRule: vi.fn(),
	dryRunIdentityRebuild: vi.fn(),
	applyIdentityRebuild: vi.fn(),
	saveIdentityRuleDraft: vi.fn(),
	simulateIdentityRule: vi.fn(),
	identityRuleLifecycle: vi.fn(),
    updateRule: vi.fn(),
    createRule: vi.fn(),
    testRule: vi.fn(),
  },
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeAll(() => { config.global.stubs.RouterLink = { props: ['to'], template: '<a :href="String(to)"><slot /></a>' } })
beforeEach(() => {
	vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({
		enabled: true,
		admin_enabled: true,
		mode: 'shadow',
		shadow_until: '2026-09-01T00:00:00Z',
		schema: 'identity',
		geo_source: 'cloudflare_verified',
		domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' },
		quality_24h: { events: 48, valid_ip: 46, valid_device: 44 },
		effective_rule_count: 2,
		configured_rule_count: 2,
		prospective_rule_count: 2,
	})
	vi.mocked(userRiskControlV2API.listIdentityRuleEffects).mockResolvedValue([])
	vi.mocked(userRiskControlV2API.listIdentityRuleVersions).mockResolvedValue([])
  vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([
    { code: 'v2_registration_email_retries', domain: 'account', configured_enabled: true, enabled: true, state: 'healthy', window_seconds: 600, threshold: 5, score: 0, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
    { code: 'v2_registration_device_accounts', domain: 'device', configured_enabled: true, enabled: true, state: 'healthy', window_seconds: 600, threshold: 3, score: 70, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
  ])
})
afterAll(() => { delete config.global.stubs.RouterLink })
afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.clearAllMocks()
})

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

async function openEventRules(wrapper: VueWrapper) {
	await wrapper.get('[data-testid="rule-view-event"]').trigger('click')
	await flushPromises()
}

describe('UserRiskControlRulesView', () => {
	it('opens on the daily identity-rule status instead of a secondary management tool', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()

		expect(wrapper.get('[data-testid="rule-view-identity"]').attributes('aria-selected')).toBe('true')
		expect(wrapper.get('[data-testid="identity-v2-rules"]').exists()).toBe(true)
		expect(wrapper.get('[data-testid="edit-identity-rule-mobile-v2_registration_email_retries"]').classes()).toContain('md:hidden')
		expect(wrapper.get('[data-testid="edit-identity-rule-v2_registration_email_retries"]').classes()).toEqual(expect.arrayContaining(['hidden', 'md:inline-flex']))
	})

	it('publishes an identity rule directly with one save action', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const rule = { code: 'v2_registration_composite_accounts', domain: 'composite', configured_enabled: true, enabled: true, state: 'healthy', detection_state: 'healthy', decision_mode: 'enforce', configured_action: 'reject_candidate', effective_action: 'reject_candidate', data_quality: 'healthy', enforcement_eligible: true, reason_codes: [], config_source: 'database', window_seconds: 600, threshold: 3, score: 90, mode: 'enforce', revision: 2, updated_at: '2026-08-24T00:00:00Z' } as const
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([rule])
		vi.mocked(userRiskControlV2API.identityRuleLifecycle).mockResolvedValue({ code: rule.code, revision: 3, operation: 'publish' })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get(`[data-testid="edit-identity-rule-${rule.code}"]`).trigger('click')
		expect(bodyElement<HTMLButtonElement>('[data-testid="publish-identity-rule"]').disabled).toBe(true)
		await setBodyValue('[data-testid="identity-rule-threshold"]', '4')
		expect(document.body.querySelector('[data-testid="save-identity-draft"]')).toBeNull()
		expect(document.body.querySelector('[data-testid="simulate-identity-rule"]')).toBeNull()
		expect(document.body.querySelector('[data-testid="identity-rule-confirmation"]')).toBeNull()
		expect(bodyElement('[data-testid="identity-rule-change-summary"]').textContent).toContain('阈值 3 → 4')
		await clickBody('[data-testid="publish-identity-rule"]')

		expect(userRiskControlV2API.saveIdentityRuleDraft).not.toHaveBeenCalled()
		expect(userRiskControlV2API.simulateIdentityRule).not.toHaveBeenCalled()
		expect(userRiskControlV2API.identityRuleLifecycle).toHaveBeenCalledWith(rule.code, 'publish', { reason: '', baseRevision: 2, windowSeconds: 600, threshold: 4, score: 90, configuredAction: 'reject_candidate', enabled: true })
	})

	it('enables a disabled identity rule through the same direct save action', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const rule = { code: 'v2_registration_device_accounts', domain: 'device', configured_enabled: false, enabled: false, state: 'disabled', detection_state: 'disabled', decision_mode: 'shadow', configured_action: 'review', effective_action: 'none', data_quality: 'healthy', enforcement_eligible: false, reason_codes: [], config_source: 'database', window_seconds: 600, threshold: 3, score: 70, mode: 'shadow', revision: 2, updated_at: '2026-08-24T00:00:00Z' } as const
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([rule])
		vi.mocked(userRiskControlV2API.identityRuleLifecycle).mockResolvedValue({ code: rule.code, revision: 3, operation: 'publish' })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get(`[data-testid="edit-identity-rule-${rule.code}"]`).trigger('click')
		bodyElement('[data-testid="identity-rule-enabled"]').dispatchEvent(new MouseEvent('click', { bubbles: true }))
		await flushPromises()
		await clickBody('[data-testid="publish-identity-rule"]')
		expect(userRiskControlV2API.identityRuleLifecycle).toHaveBeenCalledWith(rule.code, 'publish', expect.objectContaining({ enabled: true }))
	})

	it('restores the target version enabled state through a read-only rollback preview', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const rule = { code: 'v2_registration_device_accounts', domain: 'device', configured_enabled: true, enabled: true, state: 'healthy', detection_state: 'healthy', decision_mode: 'shadow', configured_action: 'review', effective_action: 'review', data_quality: 'healthy', enforcement_eligible: false, reason_codes: [], config_source: 'database', window_seconds: 600, threshold: 3, score: 70, mode: 'shadow', revision: 3, updated_at: '2026-08-24T00:00:00Z' } as const
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([rule])
		vi.mocked(userRiskControlV2API.listIdentityRuleVersions).mockResolvedValue([
			{ revision: 3, signal_family: 'registration_identity', domain: 'device', enabled: true, rule_snapshot: { window_seconds: 600, threshold: 3, score: 70, configured_action: 'review' }, active_from: '2026-08-24T00:00:00Z' },
			{ revision: 1, signal_family: 'registration_identity', domain: 'device', enabled: false, rule_snapshot: { window_seconds: 900, threshold: 2, score: 60, configured_action: 'observe' }, active_from: '2026-08-20T00:00:00Z', active_until: '2026-08-21T00:00:00Z' },
		])
		vi.mocked(userRiskControlV2API.identityRuleLifecycle).mockResolvedValue({ code: rule.code, revision: 4, operation: 'rollback' })
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-versions"]').trigger('click')
		await flushPromises()
		const rollback = Array.from(wrapper.findAll('button')).find((button) => button.text().includes('回滚到此版'))
		expect(rollback).toBeTruthy()
		await rollback!.trigger('click')
		expect(bodyElement<HTMLInputElement>('[data-testid="identity-rule-window"]').disabled).toBe(true)
		expect(bodyElement('[data-testid="identity-rule-change-summary"]').textContent).toContain('停用规则')
		await clickBody('[data-testid="publish-identity-rule"]')
		expect(userRiskControlV2API.identityRuleLifecycle).toHaveBeenCalledWith(rule.code, 'rollback', { reason: '', targetRevision: 1 })
	})

	it('reloads the latest identity rule after a revision conflict', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const rule = { code: 'v2_registration_composite_accounts', domain: 'composite', configured_enabled: true, enabled: true, state: 'healthy', configured_action: 'reject_candidate', effective_action: 'reject_candidate', window_seconds: 600, threshold: 3, score: 90, mode: 'enforce', revision: 2, updated_at: '2026-08-24T00:00:00Z' } as const
		const latest = { ...rule, threshold: 5, revision: 3, updated_at: '2026-08-25T00:00:00Z' } as const
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValueOnce([rule]).mockResolvedValue([latest])
		vi.mocked(userRiskControlV2API.identityRuleLifecycle)
			.mockRejectedValueOnce({ status: 409, message: 'rule revision conflict' })
			.mockResolvedValue({ code: rule.code, revision: 4, operation: 'publish' })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get(`[data-testid="edit-identity-rule-${rule.code}"]`).trigger('click')
		await setBodyValue('[data-testid="identity-rule-threshold"]', '4')
		await clickBody('[data-testid="publish-identity-rule"]')

		expect(bodyElement('[data-testid="identity-rule-editor"]').textContent).toContain('规则已由其他管理员更新，请重新加载后再修改。')
		expect(bodyElement<HTMLButtonElement>('[data-testid="publish-identity-rule"]').disabled).toBe(true)
		await clickBody('[data-testid="reload-identity-rule"]')
		expect(bodyElement<HTMLInputElement>('[data-testid="identity-rule-threshold"]').value).toBe('5')

		await setBodyValue('[data-testid="identity-rule-threshold"]', '6')
		await clickBody('[data-testid="publish-identity-rule"]')
		expect(userRiskControlV2API.identityRuleLifecycle).toHaveBeenLastCalledWith(rule.code, 'publish', expect.objectContaining({ baseRevision: 3, threshold: 6 }))
	})

	it('keeps identity, event, replay, effect, and version workflows inside one page', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.listIdentityRuleEffects).mockResolvedValue([{ rule_code: 'v2_registration_ip_accounts', revision: 2, hit_events: 8, unique_subjects: 3, sample_user_ids: [7], confirmed_rate: 0.5, legitimate_shared_rate: 0.25, missing_signal_rate: 0 }])
		vi.mocked(userRiskControlV2API.listIdentityRuleVersions).mockResolvedValue([{ revision: 2, signal_family: 'registration_identity', domain: 'ip', enabled: true, rule_snapshot: {}, active_from: '2026-08-17T00:00:00Z' }])
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 2, current_signals: 1, v2_signals: 2, changed_subjects: 1, rule_hits: { v2_registration_ip_accounts: 2 }, sample_user_ids: [7], evidence_high_water: 42, rule_watermark: { v2_registration_ip_accounts: 2 }, started_at: '2026-08-17T00:00:00Z' })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		for (const view of ['identity', 'event', 'replay', 'shadow', 'versions']) expect(wrapper.find(`[data-testid="rule-view-${view}"]`).exists()).toBe(true)
		await wrapper.get('[data-testid="rule-view-shadow"]').trigger('click')
		await flushPromises()
		expect(wrapper.get('[data-testid="identity-rule-effects"]').text()).toContain('50.0%')
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await flushPromises()
		expect(wrapper.get('[data-testid="rebuild-summary"]').text()).not.toContain('42')
		expect(wrapper.get('[data-testid="rebuild-technical-details"]').attributes('open')).toBeUndefined()
		expect(wrapper.get('[data-testid="rebuild-technical-details"]').text()).toContain('42')
		await wrapper.get('[data-testid="rule-view-versions"]').trigger('click')
		await flushPromises()
		const versions = wrapper.get('[data-testid="identity-rule-versions"]').text()
		expect(versions).toContain('第 2 版')
		expect(versions).not.toContain('registration_identity')
		expect(versions).not.toContain('r2')
	})

	it('shows detection state, effective action, operating window, and data quality', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-identity"]').trigger('click')

		const status = wrapper.get('[data-testid="identity-shadow-status"]').text()
		expect(status).toContain('身份检测已启用；当前实际动作以规则行展示为准')
		expect(status).toContain('生效检测 2 条')
		expect(status).toContain('2026-09-01')
		expect(status).toContain('数据质量正常')
	})

	it('shows composite registration enforcement without claiming other identity rules enforce', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([
			{ code: 'v2_registration_device_accounts', domain: 'device', configured_enabled: true, enabled: true, state: 'healthy', window_seconds: 600, threshold: 3, score: 70, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
			{ code: 'v2_registration_composite_accounts', domain: 'composite', configured_enabled: true, enabled: true, state: 'healthy', window_seconds: 600, threshold: 3, score: 90, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
		])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({
			enabled: true, admin_enabled: true, mode: 'enforce', shadow_until: '2026-09-01T00:00:00Z', schema: 'v2',
			geo_source: 'cloudflare_verified', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' },
			quality_24h: {}, effective_rule_count: 2, features: { composite_enforcement: true },
		})
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()

		const panel = wrapper.get('[data-testid="identity-v2-rules"]')
		expect(panel.text()).toContain('综合注册候选处置已开启')
		expect(panel.text()).toContain('10 分钟内 3 个成功账号')
		expect(panel.text()).toContain('不会自动封禁现有账号')
		expect(panel.get('[data-testid="identity-rule-v2_registration_composite_accounts"]').text()).toContain('自动拒绝候选')
		expect(panel.get('[data-testid="identity-rule-v2_registration_device_accounts"]').text()).toContain('观察（不执行）')
	})

	it('does not claim detection is running when rollout or effective rules are disabled', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: false, admin_enabled: true, mode: 'shadow', schema: 'identity', geo_source: 'none', domains: { ip: 'disabled', device: 'disabled', composite: 'disabled' }, quality_24h: {}, effective_rule_count: 0 })
		vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([{ code: 'v2_registration_ip_accounts', domain: 'ip', configured_enabled: true, enabled: false, state: 'disabled', window_seconds: 600, threshold: 5, score: 60, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' }])

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		const status = wrapper.get('[data-testid="identity-shadow-status"]').text()
		expect(status).toContain('当前未启用')
		expect(status).not.toContain('规则已启用并计算')
		expect(status).not.toContain('长期有效')
	})

	it('uses quality domains for status and replay gating', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', shadow_until: '2026-01-01T00:00:00Z', schema: 'identity', geo_source: 'cloudflare_verified', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_domains: { ip: 'paused', device: 'healthy', composite: 'healthy' }, quality_24h: {}, effective_rule_count: 2 })
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 1, current_signals: 1, v2_signals: 1, changed_subjects: 0, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, started_at: new Date().toISOString(), completed_at: new Date().toISOString() })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		expect(wrapper.get('[data-testid="identity-shadow-status"]').text()).toContain('数据质量需关注')
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await flushPromises()
		expect(wrapper.get('[data-testid="rebuild-apply"]').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="identity-rebuild"]').text()).toContain('数据质量不满足写入条件')
	})

	it('shows an explained empty sample state instead of five zero-percent metrics', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.listIdentityRuleEffects).mockResolvedValue([
			{ rule_code: 'v2_registration_ip_accounts', revision: 2, hit_events: 0, unique_subjects: 0, sample_user_ids: [], confirmed_rate: 0, legitimate_shared_rate: 0, missing_signal_rate: 0 },
		])

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-shadow"]').trigger('click')

		const effects = wrapper.get('[data-testid="identity-rule-effects"]').text()
		expect(effects).toContain('尚无有效样本')
		expect(effects).toContain('管理员反馈后形成确认率')
		expect(effects).toContain('样本期')
		expect(effects).not.toContain('0.0%')
		expect(effects).not.toContain('v2_')
	})

	it('keeps replay writes disabled while the recorded observation period is still active', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 2, current_signals: 1, v2_signals: 2, changed_subjects: 1, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, started_at: new Date().toISOString(), completed_at: new Date().toISOString() })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await flushPromises()

		expect(wrapper.get('[data-testid="rebuild-apply"]').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="identity-rebuild"]').text()).toContain('观察期尚未截止')
	})

	it('requires a fresh completed preflight and an explicit second confirmation before replay writes', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', shadow_until: '2026-01-01T00:00:00Z', schema: 'identity', geo_source: 'cloudflare_verified', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_24h: { events: 48, valid_ip: 46, valid_device: 44 }, effective_rule_count: 2, configured_rule_count: 2, prospective_rule_count: 2 })
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 2, current_signals: 1, v2_signals: 2, changed_subjects: 1, rule_hits: {}, sample_user_ids: [7], evidence_high_water: 42, rule_watermark: {}, started_at: new Date().toISOString() })
		vi.mocked(userRiskControlV2API.applyIdentityRebuild).mockResolvedValue({ id: 10, dry_run: false, status: 'completed', current_signal_users: 2, v2_signal_users: 2, current_signals: 2, v2_signals: 2, changed_subjects: 0, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, approved_dry_run_id: 9, started_at: new Date().toISOString() })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		expect(wrapper.get('[data-testid="rebuild-apply"]').attributes('disabled')).toBeDefined()
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await flushPromises()
		await wrapper.get('[data-testid="rebuild-apply"]').trigger('click')
		expect(userRiskControlV2API.applyIdentityRebuild).not.toHaveBeenCalled()
		expect(bodyElement('[data-testid="rebuild-confirm-dialog"]')).toBeTruthy()
		await clickBody('[data-testid="rebuild-confirm-ack"]')
		await clickBody('[data-testid="rebuild-confirm-apply"]')
		expect(userRiskControlV2API.applyIdentityRebuild).toHaveBeenCalledWith(9)
	})

	it('rejects a replay response that is incomplete or bound to another preflight', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', shadow_until: '2026-01-01T00:00:00Z', schema: 'identity', geo_source: 'cloudflare_verified', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_24h: {}, effective_rule_count: 2 })
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 1, current_signals: 1, v2_signals: 1, changed_subjects: 0, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, started_at: new Date().toISOString(), completed_at: new Date().toISOString() })
		vi.mocked(userRiskControlV2API.applyIdentityRebuild).mockResolvedValue({ id: 10, dry_run: false, status: 'processing', current_signal_users: 1, v2_signal_users: 1, current_signals: 1, v2_signals: 1, changed_subjects: 0, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, approved_dry_run_id: 12, started_at: new Date().toISOString() })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-apply"]').trigger('click')
		await clickBody('[data-testid="rebuild-confirm-ack"]')
		await clickBody('[data-testid="rebuild-confirm-apply"]')

		expect(wrapper.get('[data-testid="identity-rebuild"]').text()).toContain('写入结果与本次预检不一致')
		expect(wrapper.text()).not.toContain('历史回放已完成')
	})

	it('expires a preflight in the open page after thirty minutes', async () => {
		vi.useFakeTimers()
		vi.setSystemTime(new Date('2026-08-18T00:00:00Z'))
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
		vi.mocked(userRiskControlV2API.getIdentityHealth).mockResolvedValue({ enabled: true, admin_enabled: true, mode: 'shadow', shadow_until: '2026-01-01T00:00:00Z', schema: 'identity', geo_source: 'cloudflare_verified', domains: { ip: 'healthy', device: 'healthy', composite: 'healthy' }, quality_24h: {}, effective_rule_count: 2 })
		vi.mocked(userRiskControlV2API.dryRunIdentityRebuild).mockResolvedValue({ id: 9, dry_run: true, status: 'completed', current_signal_users: 1, v2_signal_users: 1, current_signals: 1, v2_signals: 1, changed_subjects: 0, rule_hits: {}, sample_user_ids: [], evidence_high_water: 42, rule_watermark: {}, started_at: '2026-08-18T00:00:00Z', completed_at: '2026-08-18T00:00:00Z' })

		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await wrapper.get('[data-testid="rule-view-replay"]').trigger('click')
		await wrapper.get('[data-testid="rebuild-dry-run"]').trigger('click')
		await flushPromises()
		expect(wrapper.get('[data-testid="rebuild-apply"]').attributes('disabled')).toBeUndefined()

		await vi.advanceTimersByTimeAsync(31 * 60 * 1000)
		await wrapper.vm.$nextTick()
		expect(wrapper.get('[data-testid="rebuild-apply"]').attributes('disabled')).toBeDefined()
		expect(wrapper.get('[data-testid="identity-rebuild"]').text()).toContain('预检已过期')
	})

	it('keeps active API error rules in the daily table and only folds explicit retired codes', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([
			{ id: 1, code: 'custom_api_errors', name: '业务 API 异常', eventTypes: ['api_error'], enabled: true, windowSeconds: 300, threshold: 5, score: 50, riskLevel: 'medium', action: 'review', revision: 1 },
			{ id: 2, code: 'upstream_error_burst', name: '业务上游异常', eventTypes: ['upstream_error'], enabled: true, windowSeconds: 300, threshold: 5, score: 50, riskLevel: 'medium', action: 'review', revision: 1 },
			{ id: 3, code: 'api_error_burst', name: '已迁移的 API 可靠性规则', eventTypes: ['api_error'], enabled: false, windowSeconds: 300, threshold: 10, score: 35, riskLevel: 'medium', action: 'observe', revision: 1 },
			{ id: 4, code: 'upstream_error', name: '仍启用的上游可靠性规则', eventTypes: ['upstream_error'], enabled: true, windowSeconds: 300, threshold: 10, score: 35, riskLevel: 'medium', action: 'observe', revision: 1 },
		])
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await openEventRules(wrapper)

		const daily = wrapper.findComponent(DataTable).props('data') as Array<{ code: string }>
		expect(daily.map((rule) => rule.code)).toEqual(expect.arrayContaining(['custom_api_errors', 'upstream_error_burst', 'upstream_error']))
		expect(daily.map((rule) => rule.code)).not.toContain('api_error_burst')
		expect(wrapper.get('[data-testid="retired-event-rules"]').text()).toContain('已迁移的 API 可靠性规则')
		expect(wrapper.find('[data-testid="edit-retired-rule-3"]').exists()).toBe(true)
	})
  it('uses shared responsive table and rule form controls', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: '登录失败爆发', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)

    expect(wrapper.findComponent(DataTable).exists()).toBe(true)
	await wrapper.get('[data-testid="rule-view-identity"]').trigger('click')
    const identityRulesPanel = wrapper.get('[data-testid="identity-v2-rules"]')
    expect(identityRulesPanel.text()).toContain('身份规则')
    expect(identityRulesPanel.text()).toContain('同邮箱重复注册尝试')
    expect(identityRulesPanel.text()).toContain('同浏览器实例多账号注册')
    expect(identityRulesPanel.text()).toContain('已启用 · 人工复核')
    expect(identityRulesPanel.text()).not.toContain('V2')
	await wrapper.get('[data-testid="rule-view-event"]').trigger('click')
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
	await openEventRules(wrapper)

    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('登录失败爆发')
    expect(wrapper.find('[data-testid="rule-editor-1"]').exists()).toBe(false)
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    expect(bodyElement('[data-testid="rule-editor-1"]')).toBeTruthy()
  })

  it('keeps generic rules available when the V2 rule endpoint is unavailable', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: '登录失败爆发', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.listIdentityRules).mockRejectedValue(new Error('identity rules unavailable'))

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)

    expect(wrapper.get('[data-testid="risk-rules-table"]').text()).toContain('登录失败爆发')
	await wrapper.get('[data-testid="rule-view-identity"]').trigger('click')
    expect(wrapper.get('[data-testid="identity-rules-error"]').text()).toContain('identity rules unavailable')
  })

  it('does not report configured V2 rules as active before the rollout switch is effective', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([
      { code: 'v2_registration_ip_accounts', domain: 'ip', configured_enabled: true, enabled: false, state: 'disabled', window_seconds: 600, threshold: 5, score: 60, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
    ])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await wrapper.get('[data-testid="rule-view-identity"]').trigger('click')

    expect(wrapper.get('[data-testid="identity-v2-rules"]').text()).toContain('尚未启用')
    expect(wrapper.get('[data-testid="identity-v2-rules"]').text()).toContain('配置已开 · 尚未生效')
  })

  it('shows the data-quality circuit breaker instead of claiming the rule is active', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    vi.mocked(userRiskControlV2API.listIdentityRules).mockResolvedValue([
      { code: 'v2_registration_ip_accounts', domain: 'ip', configured_enabled: true, enabled: false, state: 'paused', window_seconds: 600, threshold: 5, score: 60, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' },
    ])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await wrapper.get('[data-testid="rule-view-identity"]').trigger('click')

    expect(wrapper.get('[data-testid="identity-v2-rules"]').text()).toContain('数据质量保护中')
    expect(wrapper.get('[data-testid="identity-v2-rules"]').text()).toContain('数据质量异常 · 已暂停')
  })

  it('loads a scenario rule, saves changes, and shows test output', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockResolvedValue({ id: 1, revision: 4 })
    vi.mocked(userRiskControlV2API.testRule).mockResolvedValue({ matched: true, score: 80, riskLevel: 'high', action: 'review', configuredAction: 'review', conditions: ['登录失败次数达到 5 次'], excludedReasons: [], evaluation: [{ step: 'count_strategy', passed: true, detail: { strategy: 'user_events', count: 5 } }] })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    await setBodyValue('[data-testid="rule-threshold"]', '8')
	await setBodyValue('[data-testid="edit-rule-reason"]', '调整登录失败阈值')
    await clickBody('[data-testid="save-rule"]')
    expect(userRiskControlV2API.updateRule).toHaveBeenCalledWith(1, expect.objectContaining({ threshold: 8 }))
    expect(wrapper.text()).toContain('规则已保存')

    await clickBody('[data-testid="close-rule-editor"]')
    await wrapper.get('[data-testid="test-rule-1"]').trigger('click')
    await clickBody('[data-testid="run-rule-test"]')
    expect(userRiskControlV2API.testRule).toHaveBeenCalled()
    expect(document.body.textContent).toContain('80')
    expect(document.body.textContent).toContain('高风险')
    expect(document.body.textContent).toContain('人工复核')
    expect(document.body.textContent).toContain('登录失败次数达到 5 次')
    expect(document.body.textContent).toContain('{"strategy":"user_events","count":5}')
    expect(document.body.textContent).not.toContain('[object Object]')
  })

  it('offers candidate rejection as a configurable rule action', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'custom_registration_policy', name: 'Registration policy', enabled: true, eventTypes: ['registration_attempt'], windowSeconds: 600, threshold: 3, score: 80, riskLevel: 'critical', action: 'reject_candidate', revision: 1 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)

    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
    expect(bodyElement('[data-testid="rule-action"]').textContent).toContain('拒绝注册')
  })

  it('shows the upstream revision conflict instead of hiding it behind a generic fallback', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
    vi.mocked(userRiskControlV2API.updateRule).mockRejectedValue({ status: 409, message: 'rule revision conflict' })

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)
    await wrapper.get('[data-testid="edit-rule-1"]').trigger('click')
	await setBodyValue('[data-testid="edit-rule-reason"]', '验证并发版本冲突')
    await clickBody('[data-testid="save-rule"]')

		expect(bodyElement('[data-testid="edit-rule-error"]').textContent).toContain('规则已被其他管理员修改')
		expect(bodyElement('[data-testid="rule-editor-1"]')).toBeTruthy()
  })

	it('keeps rule test failures inside the open test dialog', async () => {
		vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])
		vi.mocked(userRiskControlV2API.testRule).mockRejectedValue(new Error('测试服务暂时不可用'))
		const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
		await flushPromises()
		await openEventRules(wrapper)
		await wrapper.get('[data-testid="test-rule-1"]').trigger('click')
		await clickBody('[data-testid="run-rule-test"]')
		expect(bodyElement('[data-testid="rule-test-error"]').textContent).toContain('测试服务暂时不可用')
		expect(document.body.querySelector('[data-testid="run-rule-test"]')).not.toBeNull()
	})

  it('rejects invalid edited rule values before sending the update request', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([{ id: 1, code: 'login_failure', name: 'Login failures', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 }])

    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)
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
	await openEventRules(wrapper)
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await clickBody('[data-testid="template-login_failure_burst"]')
    await setBodyValue('[data-testid="rule-code-input"]', 'custom_login')
    await setBodyValue('[data-testid="rule-name-input"]', '自定义登录')
    await setBodyValue('[data-testid="rule-description-input"]', '短时间内登录失败')
    await setBodyValue('[data-testid="rule-window"]', '300')
    await setBodyValue('[data-testid="rule-threshold-create"]', '5')
	await setBodyValue('[data-testid="create-rule-reason"]', '新增登录失败场景')
    await clickBody('[data-testid="create-rule"]')

    expect(userRiskControlV2API.createRule).toHaveBeenCalledWith(expect.objectContaining({ code: 'custom_login', name: '自定义登录', eventTypes: ['login_failure'], windowSeconds: 300, threshold: 5 }))
    expect(wrapper.text()).toContain('规则已创建')
  })

  it('rejects an unsafe rule code before sending the create request', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)
    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await setBodyValue('[data-testid="rule-code-input"]', 'bad code')
    await setBodyValue('[data-testid="rule-name-input"]', '错误规则')
    vi.mocked(userRiskControlV2API.createRule).mockClear()
    await clickBody('[data-testid="create-rule"]')
    expect(userRiskControlV2API.createRule).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('规则编码只能使用小写字母、数字、下划线和短横线')
  })

  it('resets the new rule draft whenever the dialog is reopened', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)

    await wrapper.get('[data-testid="new-rule"]').trigger('click')
    await setBodyValue('[data-testid="rule-code-input"]', 'unfinished_rule')
    await wrapper.findComponent(BaseDialog).vm.$emit('close')
    await wrapper.get('[data-testid="new-rule"]').trigger('click')

    const codeTarget = bodyElement('[data-testid="rule-code-input"]')
    const codeInput = codeTarget.matches('input') ? codeTarget as HTMLInputElement : codeTarget.querySelector<HTMLInputElement>('input')
    expect(codeInput?.value).toBe('login_failure_burst')
    expect(bodyElement('[data-testid="template-login_failure_burst"]').getAttribute('aria-pressed')).toBe('true')
  })

  it('filters the complete rule list locally by text, enabled state, and risk level', async () => {
    vi.mocked(userRiskControlV2API.listRules).mockResolvedValue([
      { id: 1, code: 'login_failure', name: 'Login burst', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high', action: 'review', revision: 3 },
      { id: 2, code: 'quota_watch', name: 'Quota watch', enabled: false, windowSeconds: 600, threshold: 8, score: 30, riskLevel: 'low', action: 'observe', revision: 1 },
    ])
    const wrapper = mount(UserRiskControlRulesView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } } })
    await flushPromises()
	await openEventRules(wrapper)

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
	await openEventRules(wrapper)

    const table = wrapper.findComponent(DataTable)
    expect(table.props('serverSideSort')).toBe(false)
    expect(table.props('columns')).toEqual(expect.arrayContaining([expect.objectContaining({ key: 'risk', sortable: true })]))
    expect(table.props('data')).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: 1, risk: 80 }),
      expect.objectContaining({ id: 2, risk: 20 }),
    ]))
  })
})
