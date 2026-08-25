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
		await userRiskControlV2API.applyIdentityRebuild(8)
		await userRiskControlV2API.disableIdentityRule('v2_registration_ip_accounts', '数据质量复核')

		expect(get).toHaveBeenNthCalledWith(1, '/admin/user-risk-control/identity-rule-effects')
		expect(get).toHaveBeenNthCalledWith(2, '/admin/user-risk-control/identity-rules/v2_registration_ip_accounts/versions')
		expect(post).toHaveBeenNthCalledWith(1, '/admin/risk-rebuilds/dry-run', {})
		expect(post).toHaveBeenNthCalledWith(2, '/admin/risk-rebuilds', { approved_dry_run_id: 8 })
		expect(post).toHaveBeenNthCalledWith(3, '/admin/user-risk-control/identity-rules/v2_registration_ip_accounts/disable', { reason: '数据质量复核' })
	})

	it('publishes a complete identity rule change in one request', async () => {
		const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { code: 'v2_registration_composite_accounts', revision: 3, operation: 'publish' } } as never)

		await userRiskControlV2API.identityRuleLifecycle('v2_registration_composite_accounts', 'publish', {
			baseRevision: 2,
			windowSeconds: 900,
			threshold: 4,
			score: 90,
			configuredAction: 'reject_candidate',
			enabled: true,
		})

		expect(post).toHaveBeenCalledWith('/admin/user-risk-control/identity-rules/v2_registration_composite_accounts/publish', {
			reason: '',
			base_revision: 2,
			window_seconds: 900,
			threshold: 4,
			score: 90,
			configured_action: 'reject_candidate',
			enabled: true,
			target_revision: undefined,
			simulation_id: undefined,
			confirmed: undefined,
			confirmation: undefined,
		})
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
				view: 'users',
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
			params: expect.objectContaining({ view: 'users', page: 1, page_size: 20 }),
    }))
  })

  it('loads the server-aggregated work overview in one request', async () => {
    const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { pending: 3, mine: 2, observing: 4, at_risk: 7, data_quality: 1 },
    } as never)

		await expect(userRiskControlV2API.getWorkOverview()).resolves.toEqual({ unassignedPending: 3, myInReview: 2, reviewDue: 4, allOpen: 9 })
    expect(get).toHaveBeenCalledWith('/admin/user-risk/work-overview')
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
    await expect(userRiskControlV2API.setUserStatus(7, 'disabled', 'Repeated login failures')).resolves.toMatchObject({
      user: { id: 7, status: 'disabled' },
      result: 'success',
	  retryable: false,
	  requestId: expect.any(String),
    })

    expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status', {
      status: 'disabled',
	  reason: 'Repeated login failures',
	  request_id: expect.any(String),
	}, { headers: { 'Idempotency-Key': expect.any(String) } })
  })

  it('preserves partial status results for a single account action', async () => {
    vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({
      data: { user: { id: 7, status: 'disabled' }, result: 'partial', failure_reason: 'Active sessions could not be revoked', request_id: 'risk-request-7', retryable: true, pending_step: 'session_revocation' },
    } as never)

    await expect(userRiskControlV2API.setUserStatus(7, 'disabled', 'Manual review')).resolves.toMatchObject({
      user: { id: 7, status: 'disabled' },
      result: 'partial',
      failureReason: 'Active sessions could not be revoked',
	  requestId: 'risk-request-7',
	  retryable: true,
	  pendingStep: 'session_revocation',
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
    await expect(userRiskControlV2API.testRule(rule, { sample: { observed_count: 5, event_type: 'login_failure', user_id: 7 } })).resolves.toMatchObject({ matched: true, score: 80, riskLevel: 'high', action: 'review' })

    expect(put).toHaveBeenCalledWith('/admin/user-risk-control/rules/login_failure', expect.objectContaining({ window_seconds: 300, threshold: 5, revision: 3 }))
	expect(post).toHaveBeenCalledWith('/admin/user-risk-control/rules/test', expect.objectContaining({ sample: expect.objectContaining({ observed_count: 5, event_type: 'login_failure', user_id: 7 }) }))
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/rules/test', expect.objectContaining({ rule: expect.objectContaining({ enabled: true }) }))
  })

  it('filters audit records by numeric actor without pulling every admin account', async () => {
		const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({ data: { items: [{ id: 3, actor_id: 11, actor_account: { id: 11, email: 'admin@example.com', username: 'Admin', availability: 'available' }, action: 'ban', target_type: 'user', target_id: '7', target_account: { id: 7, email: 'alice@example.com', username: 'Alice', availability: 'available' }, result: 'success', reason: 'Repeated failures', metadata: { before_status: 'active', after_status: 'disabled' }, created_at: '2026-07-11T12:00:00Z' }], total: 1 } } as never)

		await expect(userRiskControlV2API.listAudit({ actor: '11', action: 'ban', targetUserId: 7, result: 'success', page: 2, pageSize: 20 })).resolves.toMatchObject({
			items: [{ actor: '11', actor_account: expect.objectContaining({ email: 'admin@example.com' }), target_account: expect.objectContaining({ email: 'alice@example.com' }), target_user_id: 7, before_status: 'active', after_status: 'disabled', reason: 'Repeated failures' }],
    })

		expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/audit', {
		params: { category: 'disposition', action: 'ban', target_user_id: 7, actor_id: 11, result: 'success', page: 2, limit: 20 },
    })
  })

  it('passes administrator account text to the server instead of silently dropping it', async () => {
    const get = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)

    await userRiskControlV2API.listAudit({ actor: 'admin@example.com', target: 'alice@example.com' })

    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/audit', {
      params: expect.objectContaining({ actor: 'admin@example.com', target: 'alice@example.com' }),
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

  it('preserves review-case context when loading one exact account', async () => {
    vi.spyOn(mainAdminClient, 'get')
      .mockResolvedValueOnce({ data: { id: 9, username: 'Related', account_status: 'active', risk_type: 'registration_identity_abuse', risk_level: 'high', score: 80, event_count: 1, case_id: 41, case_status: 'pending' } } as never)
      .mockResolvedValueOnce({ data: { id: 9, username: 'Related', email: 'related@example.com', status: 'active' } } as never)
      .mockResolvedValueOnce({ data: { items: [], total: 0 } } as never)

    await expect(userRiskControlV2API.getUserDetail(9)).resolves.toMatchObject({
      user: { id: 9, case_id: 41, case_status: 'pending' },
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
		if (id === 8) throw { status: 409, message: '目标账号已被其他管理员处理' }
      return { data: { user: { id, status: 'disabled' }, batch_id: (payload as { batch_id: string }).batch_id } } as never
    })

    const results = await userRiskControlV2API.batchSetUserStatus([7, 8], 'disabled', '批量处置：重复登录失败')

    expect(results).toEqual([
		expect.objectContaining({ id: 7, status: 'success', requestedStatus: 'disabled' }),
		expect.objectContaining({ id: 8, status: 'failed', reason: '目标账号已被其他管理员处理', requestedStatus: 'disabled', retryable: false }),
    ])
    expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status', expect.objectContaining({
      status: 'disabled', reason: '批量处置：重复登录失败', batch_id: expect.any(String),
	}), { headers: { 'Idempotency-Key': expect.any(String) } })
  })

	it('reports token-revocation failures as partial after the account state changed', async () => {
		vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { user: { id: 7, status: 'disabled' }, result: 'partial', failure_reason: 'Account status changed, but active sessions could not be revoked', request_id: 'batch-request-7', retryable: true, pending_step: 'session_revocation' } } as never)
		await expect(userRiskControlV2API.batchSetUserStatus([7], 'disabled', '人工处置', 1)).resolves.toEqual([
			expect.objectContaining({ id: 7, status: 'partial', reason: 'Account status changed, but active sessions could not be revoked', operationReason: '人工处置', requestId: 'batch-request-7', retryable: true, pendingStep: 'session_revocation' }),
		])
	})

	it('persists prepared batch request IDs before waiting for responses and keeps gateway failures retryable', async () => {
		vi.spyOn(mainAdminClient, 'post').mockRejectedValue({ status: 504, message: 'gateway timeout' })
		let prepared: Parameters<NonNullable<Parameters<typeof userRiskControlV2API.batchSetUserStatus>[4]>>[0] = []
		const pending = userRiskControlV2API.batchSetUserStatus([7], 'disabled', '人工处置', 1, (items) => { prepared = items })
		expect(prepared).toEqual([expect.objectContaining({ id: 7, status: 'partial', requestedStatus: 'disabled', requestId: expect.any(String), retryable: true, pendingStep: 'status_confirmation' })])
		await expect(pending).resolves.toEqual([expect.objectContaining({ id: 7, status: 'partial', reason: '请求结果未知：gateway timeout', requestId: prepared[0].requestId, retryable: true, pendingStep: 'status_confirmation' })])
	})

	it('does not send a batch mutation when the caller cannot persist prepared recovery IDs', async () => {
		const post = vi.spyOn(mainAdminClient, 'post')
		await expect(userRiskControlV2API.batchSetUserStatus([7], 'disabled', '人工处置', 1, () => {
			throw new Error('session storage unavailable')
		})).rejects.toThrow('session storage unavailable')
		expect(post).not.toHaveBeenCalled()
	})

	it('retries only retryable session-revocation results with their original request IDs', async () => {
		const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { user: { id: 7, status: 'disabled' }, result: 'success', request_id: 'request-7', retryable: false } } as never)
		const results = await userRiskControlV2API.retryBatchSessionRevocations([
			{ id: 7, status: 'partial', operationReason: '人工处置', requestId: 'request-7', batchId: 'batch-1', retryable: true, pendingStep: 'session_revocation' },
			{ id: 8, status: 'failed', reason: '账号已变化' },
		])
		expect(results).toEqual([expect.objectContaining({ id: 7, status: 'success', requestId: 'request-7' }), expect.objectContaining({ id: 8, status: 'failed' })])
		expect(post).toHaveBeenCalledTimes(1)
		expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status/revoke-sessions', { reason: '人工处置', request_id: 'request-7', batch_id: 'batch-1' }, { headers: { 'Idempotency-Key': expect.stringMatching(/^risk-session-7-/) } })
	})

	it('retries an unknown batch status result with the original target and request ID', async () => {
		const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValueOnce({ data: { user: { id: 7, status: 'active' }, result: 'success', request_id: 'request-7', retryable: false } } as never)
		const results = await userRiskControlV2API.retryBatchSessionRevocations([
			{ id: 7, status: 'partial', operationReason: '人工恢复', requestedStatus: 'active', requestId: 'request-7', batchId: 'batch-1', retryable: true, pendingStep: 'status_confirmation' },
		])
		expect(results[0]).toMatchObject({ id: 7, status: 'success', requestedStatus: 'active', requestId: 'request-7' })
		expect(post).toHaveBeenCalledWith('/admin/users/7/risk-status', expect.objectContaining({ status: 'active', reason: '人工恢复', batch_id: 'batch-1', request_id: 'request-7' }), { headers: { 'Idempotency-Key': 'request-7' } })
	})

	it('terminates a recovery when the normalized client returns a definitive 4xx', async () => {
		vi.spyOn(mainAdminClient, 'post').mockRejectedValue({ status: 422, message: '账号状态不允许该操作' })
		const results = await userRiskControlV2API.retryBatchSessionRevocations([
			{ id: 7, status: 'partial', operationReason: '人工恢复', requestedStatus: 'active', requestId: 'request-7', batchId: 'batch-1', retryable: true, pendingStep: 'status_confirmation' },
		])
		expect(results[0]).toMatchObject({ id: 7, status: 'failed', reason: '账号状态不允许该操作', retryable: false })
		expect(results[0].pendingStep).toBeUndefined()
	})

  it('marks each processed risk subject through the risk-control proxy', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValue({ data: { id: 7, processed: true } } as never)

    await expect(userRiskControlV2API.markUsersProcessed([7], '人工复核完成', 1)).resolves.toEqual([{ id: 7, status: 'success' }])
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/users/7/processed', expect.objectContaining({ reason: '人工复核完成', batch_id: expect.any(String) }))
  })
})
