import { describe, expect, it, vi } from 'vitest'
import { afterEach } from 'vitest'
import { mainAdminClient, userRiskControlV2API } from '@/api/admin/userRiskControlV2'

describe('userRiskControlV2API', () => {

  it('loads the independent V2 identity rule domains', async () => {
    const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({ data: { items: [{ code: 'v2_registration_ip_accounts', domain: 'ip', configured_enabled: true, enabled: true, state: 'healthy', window_seconds: 600, threshold: 5, score: 60, mode: 'shadow', revision: 1, updated_at: '2026-08-13T04:58:00Z' }] } } as never)
    const rules = await userRiskControlV2API.listIdentityRules()
    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/identity-rules')
    expect(rules).toEqual([expect.objectContaining({ code: 'v2_registration_ip_accounts', domain: 'ip', mode: 'shadow' })])
  })

	it('loads rule effects and versions and keeps replay behind Dry Run APIs', async () => {
		const get = vi.spyOn(mainAdminClient, 'get')
			.mockResolvedValueOnce({ data: { items: [{ rule_code: 'v2_registration_ip_accounts', revision: 2 }] } } as never)
			.mockResolvedValueOnce({ data: { items: [{ revision: 2, enabled: true }] } } as never)
		const post = vi.spyOn(mainAdminClient, 'post')
			.mockResolvedValueOnce({ data: { id: 8, dry_run: true, status: 'completed' } } as never)
			.mockResolvedValueOnce({ data: { id: 9, dry_run: false, status: 'completed', approved_dry_run_id: 8 } } as never)
			.mockResolvedValueOnce({ data: { code: 'v2_registration_ip_accounts', revision: 3, enabled: false } } as never)

		await userRiskControlV2API.listIdentityRuleEffects()
		await userRiskControlV2API.listIdentityRuleVersions('v2_registration_ip_accounts')
		await userRiskControlV2API.dryRunIdentityRebuild()
		await userRiskControlV2API.applyIdentityRebuild()
		await userRiskControlV2API.disableIdentityRule('v2_registration_ip_accounts', '数据质量复核')

		expect(get).toHaveBeenNthCalledWith(1, '/admin/user-risk-control/identity-rule-effects')
		expect(get).toHaveBeenNthCalledWith(2, '/admin/user-risk-control/identity-rules/v2_registration_ip_accounts/versions')
		expect(post).toHaveBeenNthCalledWith(1, '/admin/risk-rebuilds/dry-run', {})
		expect(post).toHaveBeenNthCalledWith(2, '/admin/risk-rebuilds', {})
		expect(post).toHaveBeenNthCalledWith(3, '/admin/user-risk-control/identity-rules/v2_registration_ip_accounts/disable', { reason: '数据质量复核' })
	})
  afterEach(() => vi.restoreAllMocks())

  it('requests real users with risk filters and pagination', async () => {
    const mainGet = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'user@example.com', status: 'active' }], total: 1, page: 2, page_size: 20 },
    } as never)

    await expect(userRiskControlV2API.listUsers({
      page: 2,
      pageSize: 20,
      search: 'user@example.com',
      status: 'active',
      riskType: 'login_failure',
      riskLevel: 'high',
      pendingOnly: true,
    })).resolves.toMatchObject({ total: 1 })

		expect(mainGet).toHaveBeenCalledTimes(1)
		expect(mainGet).toHaveBeenCalledWith('/admin/user-risk/users', expect.objectContaining({
      params: expect.objectContaining({
				view: 'pending',
				page: 2,
				page_size: 20,
				search: 'user@example.com',
				status: 'active',
        risk_type: 'login_failure',
        risk_level: 'high',
      }),
    }))
  })

  it('loads one server-aggregated page without browser-side stitching', async () => {
    const mainGet = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1 },
    } as never)

    await userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })

		expect(mainGet).toHaveBeenCalledTimes(1)
		expect(mainGet).toHaveBeenCalledWith('/admin/user-risk/users', expect.objectContaining({
			params: expect.objectContaining({ view: 'pending', page: 1, page_size: 20 }),
    }))
  })

  it('trusts the server boundary to exclude reliability observations from user risk', async () => {
    vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_type: null, risk_level: null, risk_score: 0, risk_reason: null, event_count: 0 }], total: 1 },
		} as never)

    await expect(userRiskControlV2API.listUsers()).resolves.toMatchObject({
      items: [{ id: 7, risk_type: null, risk_level: null, risk_score: 0, risk_reason: null, event_count: 0 }],
    })
  })

  it('uses the main admin user API for ban and unban', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValue({
      data: { user: { id: 7, status: 'disabled' } },
    } as never)
    await userRiskControlV2API.setUserStatus(7, 'disabled', 'Repeated login failures')

    expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status', {
      status: 'disabled',
      reason: 'Repeated login failures',
    })
  })

  it('surfaces risk service outages instead of treating them as no-risk users', async () => {
    vi.spyOn(mainAdminClient, 'get').mockRejectedValueOnce(new Error('risk service unavailable'))

    await expect(userRiskControlV2API.listUsers()).rejects.toThrow('risk service unavailable')
  })

  it('saves a typed rule and can test it', async () => {
    const put = vi.spyOn(mainAdminClient, 'put').mockResolvedValueOnce({ data: { id: 1, revision: 4 } } as never)
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { matched: true, decision: { score: 80 } } } as never)
    const rule = { code: 'login_failure', enabled: true, windowSeconds: 300, threshold: 5, score: 80, riskLevel: 'high' as const, action: 'review' as const, revision: 3 }

    await userRiskControlV2API.updateRule(1, rule)
    await expect(userRiskControlV2API.testRule(rule, { count: 5, event_type: 'login_failure' })).resolves.toMatchObject({ matched: true, score: 80, riskLevel: 'high', action: 'review' })

    expect(put).toHaveBeenCalledWith('/admin/user-risk-control/rules/login_failure', expect.objectContaining({ window_seconds: 300, threshold: 5, revision: 3 }))
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/rules/test', expect.objectContaining({ count: 5, event_type: 'login_failure' }))
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/rules/test', expect.objectContaining({ rule: expect.objectContaining({ enabled: true }) }))
  })

  it('filters audit records by numeric actor without pulling every admin account', async () => {
		const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({ data: { items: [{ id: 3, actor_id: 11, action: 'ban', target_type: 'user', target_id: '7', result: 'success', reason: 'Repeated failures', metadata: { before_status: 'active', after_status: 'disabled' }, created_at: '2026-07-11T12:00:00Z' }], total: 1 } } as never)

		await expect(userRiskControlV2API.listAudit({ actor: '11', action: 'ban', targetUserId: 7, result: 'success', page: 2, pageSize: 20 })).resolves.toMatchObject({
			items: [{ actor: '11', target_user_id: 7, before_status: 'active', after_status: 'disabled', reason: 'Repeated failures' }],
    })

		expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/audit', {
      params: { category: 'security', action: 'ban', target_user_id: 7, actor_id: 11, result: 'success', page: 2, limit: 20 },
    })
  })

  it('falls back to the real main-site account when no risk event exists', async () => {
    const riskError = Object.assign(new Error('not found'), { response: { status: 404 } })
    vi.spyOn(mainAdminClient, 'get')
      .mockRejectedValueOnce(riskError)
      .mockResolvedValueOnce({
      data: { id: 9, username: 'NoRiskUser', email: 'no-risk@example.com', status: 'active' },
    } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)

    await expect(userRiskControlV2API.getUserDetail(9)).resolves.toMatchObject({
      user: { id: 9, username: 'NoRiskUser', email: 'no-risk@example.com', status: 'active' },
      events: [],
    })
  })

  it('loads operation history for the account detail drawer', async () => {
    vi.spyOn(mainAdminClient, 'get')
      .mockResolvedValueOnce({ data: { id: 7, username: 'Alice', account_status: 'active', risk_type: 'login_failure_burst', risk_level: 'high', score: 80, event_count: 1, ip_count: 2, device_count: 1, timeline: [{ id: 31, event_type: 'login_failure', risk_type: 'login_failure', score: 80, reason: 'rule=login_failure_burst count=9 window=300', ip: '198.51.100.10', device_id: 'chrome-124', occurred_at: '2026-07-11T11:58:00Z' }] } } as never)
      .mockResolvedValueOnce({
      data: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'disabled' },
    } as never)
      .mockResolvedValueOnce({ data: { items: [{ id: 3, actor_id: 11, action: 'ban', target_type: 'user', target_id: '7', result: 'success', reason: 'Repeated failures', metadata: { before_status: 'active', after_status: 'disabled' }, created_at: '2026-07-11T12:00:00Z' }], total: 1 } } as never)

    await expect(userRiskControlV2API.getUserDetail(7)).resolves.toMatchObject({
      audit: [{ target_user_id: 7, action: 'ban', before_status: 'active', after_status: 'disabled' }],
      associations: { ip_count: 2, device_count: 1 },
      events: [{ ip: '198.51.100.10', device_id: 'chrome-124' }],
    })
  })

  it('treats a missing risk timeline as an empty event list', async () => {
    vi.spyOn(mainAdminClient, 'get')
      .mockResolvedValueOnce({ data: { id: 7, username: 'Alice', account_status: 'active', risk_type: 'login_failure', risk_level: 'high', score: 80, event_count: 0 } } as never)
      .mockResolvedValueOnce({ data: { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' } } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)

    await expect(userRiskControlV2API.getUserDetail(7)).resolves.toMatchObject({ events: [] })
  })

  it('does not fabricate an active account when the main user API fails', async () => {
    const riskNotFound = Object.assign(new Error('not found'), { response: { status: 404 } })
    vi.spyOn(mainAdminClient, 'get')
      .mockRejectedValueOnce(riskNotFound)
      .mockRejectedValueOnce(new Error('main service unavailable'))
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)

    await expect(userRiskControlV2API.getUserDetail(9)).rejects.toThrow('main service unavailable')
  })

  it('delegates risk sorting to the server-paginated endpoint', async () => {
		const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
        data: {
          items: [
						{ id: 8, username: 'Bob', email: 'bob@example.com', status: 'active', risk_score: 90 },
						{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', risk_score: 40 },
          ],
          total: 2,
        },
			} as never)

    const result = await userRiskControlV2API.listUsers({ sortBy: 'risk_score', sortOrder: 'desc', page: 1, pageSize: 20 })

    expect(result.items.map((user) => user.id)).toEqual([8, 7])
		expect(get).toHaveBeenCalledTimes(1)
		expect(get).toHaveBeenCalledWith('/admin/user-risk/users', expect.objectContaining({
      params: expect.objectContaining({ sort_by: 'risk_score', sort_order: 'desc' }),
    }))
  })

  it('creates a rule through the authenticated admin proxy', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { id: 12, revision: 1, code: 'login_failure_burst' } } as never)

    await expect(userRiskControlV2API.createRule({
      code: 'login_failure_burst',
      name: '登录失败爆发',
      description: '短时间内连续登录失败',
      eventTypes: ['login_failure'],
			countStrategy: 'user_events',
      enabled: true,
      windowSeconds: 300,
      threshold: 5,
      score: 80,
      riskLevel: 'high',
      action: 'review',
      revision: 1,
    })).resolves.toMatchObject({ id: 12, revision: 1 })

    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/rules', expect.objectContaining({
      code: 'login_failure_burst',
      event_types: ['login_failure'],
			count_strategy: 'user_events',
      window_seconds: 300,
    }))
  })

  it('keeps one result per target for a partial batch status action', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockImplementation(async (url, payload) => {
      const id = Number(String(url).match(/users\/(\d+)/)?.[1])
      if (id === 8) throw Object.assign(new Error('目标账号已被其他管理员处理'), { response: { data: { error: '目标账号已被其他管理员处理' } } })
      return { data: { user: { id, status: 'disabled' }, batch_id: (payload as { batch_id: string }).batch_id } } as never
    })

    const results = await userRiskControlV2API.batchSetUserStatus([7, 8], 'disabled', '批量处置：重复登录失败')

    expect(results).toEqual([
      expect.objectContaining({ id: 7, status: 'success' }),
      expect.objectContaining({ id: 8, status: 'failed', reason: '目标账号已被其他管理员处理' }),
    ])
    expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status', expect.objectContaining({
      status: 'disabled', reason: '批量处置：重复登录失败', batch_id: expect.any(String),
    }))
  })

	it('reports token-revocation failures as partial after the account state changed', async () => {
		vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { user: { id: 7, status: 'disabled' }, result: 'partial', failure_reason: 'Account status changed, but active sessions could not be revoked' } } as never)
		await expect(userRiskControlV2API.batchSetUserStatus([7], 'disabled', '人工处置', 1)).resolves.toEqual([
			expect.objectContaining({ id: 7, status: 'partial', reason: 'Account status changed, but active sessions could not be revoked' }),
		])
	})

  it('marks each processed risk subject through the risk-control proxy', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValue({ data: { id: 7, processed: true } } as never)

    await expect(userRiskControlV2API.markUsersProcessed([7], '人工复核完成', 1)).resolves.toEqual([{ id: 7, status: 'success' }])
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/users/7/processed', expect.objectContaining({ reason: '人工复核完成', batch_id: expect.any(String) }))
  })
})
