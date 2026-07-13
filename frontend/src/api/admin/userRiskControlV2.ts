import apiClient from '@/api/client'

export const mainAdminClient = apiClient

export type RiskLevel = 'none' | 'low' | 'medium' | 'high' | 'critical'
export type RiskAction = 'observe' | 'review' | 'ban' | 'reject_candidate'
export type AccountStatus = 'active' | 'disabled'

export interface RiskUserRow {
  id: number
  username?: string | null
  email: string
  status: AccountStatus
  risk_type?: string | null
  risk_level?: RiskLevel | null
  risk_score?: number | null
  risk_reason?: string | null
  last_action?: RiskAction | null
  pending?: boolean
  last_risk_at?: string | null
  created_at?: string
}

export interface MainAdminUser {
  id: number
  username: string
  email: string
  status: AccountStatus
  role?: 'admin' | 'user'
  created_at?: string
  last_active_at?: string | null
}

export interface RiskUserDetail {
  user: RiskUserRow
  summary?: { score: number; level: RiskLevel; reason?: string; event_count?: number }
  events: RiskEvent[]
  audit: RiskAuditRecord[]
  usage?: { requests: number; tokens: number; cost: number }
  associations?: { ip_count: number; device_count: number }
}

export interface RiskEvent {
  id: number
  type: string
  risk_type?: string
  risk_level?: RiskLevel
  score?: number
  reason?: string
  error_code?: string
  endpoint?: string
  model?: string
  occurred_at: string
}

export interface RiskAuditRecord {
  id: number
  actor?: string
  target_type?: string
  target_id?: string
  target_user_id: number
  action: string
  before_status?: AccountStatus | null
  after_status?: AccountStatus | null
  reason?: string
  result: 'success' | 'failed'
  created_at: string
}

export interface RiskListResponse<T> { items: T[]; total: number; page?: number; page_size?: number }
export interface AuditFilters { action?: string; targetUserId?: number; result?: string; page?: number; pageSize?: number }
export interface UserRiskFilters {
  page?: number
  pageSize?: number
  search?: string
  status?: AccountStatus | ''
  riskType?: string
  riskLevel?: RiskLevel | ''
  pendingOnly?: boolean
}

export interface Rule {
  id: number
  code: string
  name: string
  enabled: boolean
  windowSeconds: number
  threshold: number
  score: number
  riskLevel: RiskLevel
  action: RiskAction
  revision: number
  description?: string
  eventTypes?: string[]
}

export type RuleInput = Omit<Rule, 'id' | 'name'> & { code: string; name?: string }

function compactParams(params: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== undefined && value !== '' && value !== false))
}

async function fetchAllRiskSignals(filters: UserRiskFilters, userIDs?: number[]) {
  const items: Array<{ id: number; username?: string; account_status?: AccountStatus; risk_type?: string; risk_level?: RiskLevel; score?: number; reason?: string; last_action?: RiskAction; pending?: boolean; last_event_at?: string }> = []
  let page = 1
  while (true) {
    const { data } = await mainAdminClient.get<{ items: typeof items; total: number }>('/admin/user-risk-control/users', {
      params: compactParams({ risk_type: filters.riskType, risk_level: filters.riskLevel, user_ids: userIDs?.join(','), limit: 1000, page }),
    })
    items.push(...data.items)
    if (items.length >= data.total || data.items.length === 0) return items
    page += 1
  }
}

async function fetchAllMainUsers(params: Record<string, unknown>) {
  const items: MainAdminUser[] = []
  let page = 1
  let total = 0
  do {
    const { data } = await mainAdminClient.get<{ items: MainAdminUser[]; total: number }>('/admin/users', { params: { ...params, page, page_size: 1000 } })
    items.push(...data.items)
    total = data.total
    page += 1
    if (data.items.length === 0) break
  } while (items.length < total)
  return { items, total }
}

async function listUsers(filters: UserRiskFilters = {}): Promise<RiskListResponse<RiskUserRow>> {
  const params = compactParams({ search: filters.search, status: filters.status })
  const filteringRisk = Boolean(filters.pendingOnly || filters.riskType || filters.riskLevel)
  const main = filteringRisk
    ? await fetchAllMainUsers(params)
    : await mainAdminClient.get<{ items: MainAdminUser[]; total: number }>('/admin/users', { params: { ...params, page: filters.page || 1, page_size: filters.pageSize || 20 } }).then(({ data }) => data)
  const riskItems = await fetchAllRiskSignals(filters, filteringRisk ? undefined : main.items.map((user) => user.id))
  const riskByID = new Map(riskItems.map((item) => [item.id, item]))
  const items = main.items.map((user) => {
    const signal = riskByID.get(user.id)
    return { ...user, risk_type: signal?.risk_type || null, risk_level: signal?.risk_level || null, risk_score: signal?.score || 0, risk_reason: signal?.reason || null, last_action: signal?.last_action || null, pending: Boolean(signal?.pending), last_risk_at: signal?.last_event_at || null } satisfies RiskUserRow
  }).filter((user) => (!filters.pendingOnly || Boolean(user.pending)) && (!filters.riskType || user.risk_type === filters.riskType) && (!filters.riskLevel || user.risk_level === filters.riskLevel))
  const currentPage = filters.page || 1
  const currentPageSize = filters.pageSize || 20
  const visibleItems = filteringRisk ? items.slice((currentPage - 1) * currentPageSize, currentPage * currentPageSize) : items
  return { items: visibleItems, total: filteringRisk ? items.length : main.total, page: currentPage, page_size: currentPageSize }
}

async function getUserDetail(id: number): Promise<RiskUserDetail> {
  const riskPromise = mainAdminClient
    .get<{ id: number; username: string; account_status: AccountStatus; risk_type: string; risk_level: RiskLevel; score: number; event_count: number; ip_count?: number; device_count?: number; timeline: Array<Record<string, unknown>> }>(`/admin/user-risk-control/users/${id}`)
    .then(({ data }) => data)
    .catch((error) => {
      if (error?.response?.status === 404 || error?.status === 404) return null
      throw error
    })
  const mainPromise = mainAdminClient.get<MainAdminUser>(`/admin/users/${id}`)
  const auditPromise = listAudit({ targetUserId: id })
  const [risk, main, audit] = await Promise.all([riskPromise, mainPromise, auditPromise])
  if (!risk) {
    return {
      user: { id: main.data.id, username: main.data.username, email: main.data.email, status: main.data.status, risk_type: null, risk_level: null, risk_score: 0, risk_reason: null },
      events: [],
      audit: audit.items,
    }
  }
  const data = risk
  const events: RiskEvent[] = data.timeline.map((event) => ({
    id: Number(event.id), type: String(event.event_type || ''), risk_type: event.risk_type as RiskEvent['risk_type'], risk_level: event.risk_level as RiskLevel | undefined,
    score: Number(event.score || 0), reason: String(event.reason || ''), error_code: String(event.error_code || ''), endpoint: String(event.endpoint || ''), model: String(event.model || ''), occurred_at: String(event.occurred_at || event.created_at || ''),
  }))
  return {
    user: { id: data.id, username: main.data.username || data.username, email: main.data.email, status: main.data.status, risk_type: data.risk_type, risk_level: data.risk_level, risk_score: data.score, risk_reason: events[0]?.reason || null },
    summary: { score: data.score, level: data.risk_level, reason: events[0]?.reason || '', event_count: data.event_count },
    events, audit: audit.items, associations: { ip_count: Number(data.ip_count || 0), device_count: Number(data.device_count || 0) },
  }
}

async function setUserStatus(id: number, status: AccountStatus, reason: string): Promise<RiskUserRow> {
  // Account status is authoritative in the main Sub2API users table.
  const { data } = await mainAdminClient.post<{ user: RiskUserRow }>(`/admin/users/${id}/risk-status`, { status, reason })
  return data.user
}

async function listRules(): Promise<Rule[]> {
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>> }>('/admin/user-risk-control/rules')
  return data.items.map((rule) => ({ id: Number(rule.id), code: String(rule.code), name: String(rule.name || rule.code), description: String(rule.description || ''), eventTypes: Array.isArray(rule.event_types) ? rule.event_types.map(String) : [String(rule.code)], enabled: Boolean(rule.enabled), windowSeconds: Number(rule.window_seconds || 0), threshold: Number(rule.threshold || 1), score: Number(rule.score || 0), riskLevel: String(rule.risk_level || 'low') as RiskLevel, action: String(rule.action || 'observe') as RiskAction, revision: Number(rule.revision || 1) }))
}

async function updateRule(_id: number, rule: RuleInput): Promise<Pick<Rule, 'id' | 'revision'>> {
  const { data } = await mainAdminClient.put<Pick<Rule, 'id' | 'revision'>>(`/admin/user-risk-control/rules/${rule.code}`, {
    code: rule.code, name: rule.name, description: rule.description, event_types: rule.eventTypes?.length ? rule.eventTypes : [rule.code], enabled: rule.enabled, window_seconds: rule.windowSeconds, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, revision: rule.revision,
  })
  return data
}

async function testRule(rule: Rule, input: Record<string, unknown>) {
  const { data } = await mainAdminClient.post<{ matched: boolean; score?: number; decision?: { score?: number } }>('/admin/user-risk-control/rules/test', { ...input, rule: { code: rule.code, enabled: rule.enabled, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, event_types: [String(input.event_type || rule.code)] } })
  return { matched: data.matched, score: data.score ?? data.decision?.score ?? 0 }
}

async function listAudit(filters: AuditFilters = {}): Promise<RiskListResponse<RiskAuditRecord>> {
  const action = filters.action === 'rule_update' ? 'update_rule' : filters.action
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>>; total: number; page?: number; page_size?: number }>('/admin/user-risk-control/audit', {
    params: compactParams({ action, target_user_id: filters.targetUserId, result: filters.result, page: filters.page || 1, limit: filters.pageSize || 20 }),
  })
  return { total: data.total, page: data.page || filters.page || 1, page_size: data.page_size || filters.pageSize || 20, items: data.items.map((record) => {
    const metadata = (record.metadata || {}) as Record<string, unknown>
    return {
      id: Number(record.id), actor: record.actor_id ? String(record.actor_id) : undefined,
      target_type: String(record.target_type || ''), target_id: String(record.target_id || ''),
      target_user_id: record.target_type === 'user' ? Number(record.target_id || 0) : 0,
      action: String(record.action), before_status: metadata.before_status as AccountStatus | null || null,
      after_status: metadata.after_status as AccountStatus | null || null, reason: String(record.reason || ''),
      result: String(record.result || 'success') as 'success' | 'failed', created_at: String(record.created_at || ''),
    }
  }) }
}

export const userRiskControlV2API = { listUsers, getUserDetail, setUserStatus, listRules, updateRule, testRule, listAudit }
export default userRiskControlV2API
