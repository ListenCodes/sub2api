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
  afterEach(() => vi.restoreAllMocks())

  it('requests real users with risk filters and pagination', async () => {
    const mainGet = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'user@example.com', status: 'active' }], total: 1, page: 2, page_size: 20 },
    } as never).mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', account_status: 'active', risk_type: 'login_failure', risk_level: 'high', score: 80, reason: 'Repeated failures', last_action: 'review', pending: true }], total: 1 },
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

    expect(mainGet).toHaveBeenCalledWith('/admin/user-risk-control/users', expect.objectContaining({
      params: expect.objectContaining({
        risk_type: 'login_failure',
        risk_level: 'high',
      }),
    }))
    expect(mainGet).toHaveBeenCalledWith('/admin/user-risk-control/users', expect.objectContaining({
      params: expect.not.objectContaining({ search: 'user@example.com', status: 'active' }),
    }))
    expect(mainGet).toHaveBeenCalledWith('/admin/users', expect.objectContaining({
      params: expect.objectContaining({ page: 1, page_size: 1000, search: 'user@example.com', status: 'active' }),
    }))
  })

  it('loads risk signals only for the current page when no risk filter is active', async () => {
    const mainGet = vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1 },
    } as never).mockResolvedValueOnce({ data: { items: [{ id: 7, risk_type: 'login_failure', risk_level: 'high', score: 80 }], total: 1 } } as never)
      .mockResolvedValueOnce({ data: { items: [] } } as never)

    await userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })

    expect(mainGet).toHaveBeenCalledWith('/admin/user-risk-control/users', expect.objectContaining({
      params: expect.objectContaining({ user_ids: '7' }),
    }))
  })

  it('does not expose a legacy API observation as the account risk summary', async () => {
    vi.spyOn(mainAdminClient, 'get').mockResolvedValueOnce({
      data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1 },
    } as never).mockResolvedValueOnce({
      data: { items: [{ id: 7, risk_type: 'api_request', risk_level: 'low', score: 0, reason: '命中规则：API 请求观察（24 小时内1 次事件）', event_count: 206 }], total: 1 },
    } as never).mockResolvedValueOnce({ data: { items: [] } } as never)

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

  it('resolves an administrator account to the audit actor id and displays the account', async () => {
    const get = vi.spyOn(mainAdminClient, 'get')
      .mockResolvedValueOnce({ data: { items: [{ id: 11, username: 'qa-admin', email: 'qa@example.com', status: 'active', role: 'admin' }], total: 1 } } as never)
      .mockResolvedValueOnce({ data: { items: [{ id: 3, actor_id: 11, action: 'ban', target_type: 'user', target_id: '7', result: 'success', reason: 'Repeated failures', metadata: { before_status: 'active', after_status: 'disabled' }, created_at: '2026-07-11T12:00:00Z' }], total: 1 } } as never)

    await expect(userRiskControlV2API.listAudit({ actor: 'qa@example.com', action: 'ban', targetUserId: 7, result: 'success', page: 2, pageSize: 20 })).resolves.toMatchObject({
      items: [{ actor: 'qa@example.com', target_user_id: 7, before_status: 'active', after_status: 'disabled', reason: 'Repeated failures' }],
    })

    expect(get).toHaveBeenCalledWith('/admin/users', { params: expect.objectContaining({ role: 'admin', page: 1, page_size: 1000 }) })
    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/audit', {
      params: { action: 'ban', target_user_id: 7, actor_id: 11, result: 'success', page: 2, limit: 20 },
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
      .mockResolvedValueOnce({ data: { items: [{ id: 11, username: 'qa-admin', email: 'qa@example.com', status: 'active', role: 'admin' }], total: 1 } } as never)
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

  it('sorts risk-aware users after loading the complete candidate set', async () => {
    const get = vi.spyOn(mainAdminClient, 'get')
      .mockResolvedValueOnce({
        data: {
          items: [
            { id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' },
            { id: 8, username: 'Bob', email: 'bob@example.com', status: 'active' },
          ],
          total: 2,
        },
      } as never)
      .mockResolvedValueOnce({
        data: {
          items: [
            { id: 7, risk_type: 'login_failure', risk_level: 'high', score: 40, event_count: 2 },
            { id: 8, risk_type: 'api_error', risk_level: 'medium', score: 90, event_count: 4 },
          ],
          total: 2,
        },
      } as never)
      .mockResolvedValueOnce({ data: { items: [] } } as never)

    const result = await userRiskControlV2API.listUsers({ sortBy: 'risk_score', sortOrder: 'desc', page: 1, pageSize: 20 })

    expect(result.items.map((user) => user.id)).toEqual([8, 7])
    expect(get).toHaveBeenCalledWith('/admin/user-risk-control/users', expect.objectContaining({
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
      countStrategy: 'associated_events',
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
      count_strategy: 'associated_events',
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

  it('marks each processed risk subject through the risk-control proxy', async () => {
    const post = vi.spyOn(mainAdminClient, 'post').mockResolvedValue({ data: { id: 7, processed: true } } as never)

    await expect(userRiskControlV2API.markUsersProcessed([7], '人工复核完成', 1)).resolves.toEqual([{ id: 7, status: 'success' }])
    expect(post).toHaveBeenCalledWith('/admin/user-risk-control/users/7/processed', expect.objectContaining({ reason: '人工复核完成', batch_id: expect.any(String) }))
  })
})
