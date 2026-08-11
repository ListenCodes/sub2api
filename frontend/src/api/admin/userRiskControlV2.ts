import apiClient from '@/api/client'
import { formatRiskReason } from '@/utils/userRiskControlLabels'

export const mainAdminClient = apiClient

export type RiskLevel = 'none' | 'low' | 'medium' | 'high' | 'critical'
export type RiskAction = 'observe' | 'review' | 'ban' | 'unban' | 'reject_candidate' | 'auto_ban'
export type AccountStatus = 'active' | 'disabled' | 'pending'
export type RiskSortBy = 'risk_score' | 'risk_level' | 'event_count' | 'last_event_at' | 'created_at'
export type SortOrder = 'asc' | 'desc'

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
  processing_status?: string | null
  event_count?: number
  ip_count?: number
  device_count?: number
  last_event_at?: string | null
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
  ip?: string
  device_id?: string
  decision?: string
  rule_codes?: string[]
  evidence?: Record<string, unknown>
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
  result: 'success' | 'partial' | 'failed' | 'pending'
  created_at: string
  failure_reason?: string
  batch_id?: string
  request_id?: string
}

export interface RiskListResponse<T> { items: T[]; total: number; page?: number; page_size?: number }
export interface AuditFilters {
  action?: string
  targetUserId?: number
  target?: string
  actor?: string
  result?: string
  from?: string
  to?: string
  sortBy?: 'created_at' | 'result' | 'target'
  sortOrder?: SortOrder
  page?: number
  pageSize?: number
}
export interface UserRiskFilters {
  page?: number
  pageSize?: number
  search?: string
  status?: AccountStatus | ''
  riskType?: string
  riskLevel?: RiskLevel | ''
  pendingOnly?: boolean
  processingStatus?: string
  minScore?: number
  maxScore?: number
  from?: string
  to?: string
  riskOnly?: boolean
  sortBy?: RiskSortBy
  sortOrder?: SortOrder
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
  countStrategy?: 'associated_events' | 'subject_device_events' | 'ip_distinct_subjects'
}

export type RuleInput = Omit<Rule, 'id' | 'name'> & { code: string; name?: string }
export type RuleCreateInput = Omit<Rule, 'id' | 'revision' | 'eventTypes'> & { eventTypes: string[]; revision?: number; reason?: string }

function compactParams(params: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== undefined && value !== '' && value !== false))
}

async function fetchAllRiskSignals(filters: UserRiskFilters, userIDs?: number[]) {
  const items: Array<{ id: number; username?: string; account_status?: AccountStatus; risk_type?: string; risk_level?: RiskLevel; score?: number; reason?: string; last_action?: RiskAction; pending?: boolean; event_count?: number; ip_count?: number; device_count?: number; last_event_at?: string }> = []
  let page = 1
  while (true) {
    const { data } = await mainAdminClient.get<{ items: typeof items; total: number }>('/admin/user-risk-control/users', {
      params: compactParams({ risk_type: filters.riskType, risk_level: filters.riskLevel, user_ids: userIDs?.join(','), sort_by: filters.sortBy, sort_order: filters.sortOrder, limit: 1000, page }),
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
  const params = compactParams({ search: filters.search, status: filters.status, sort_by: filters.sortBy === 'created_at' ? 'created_at' : undefined, sort_order: filters.sortBy === 'created_at' ? filters.sortOrder : undefined })
  const riskSort = filters.sortBy && filters.sortBy !== 'created_at'
  const filteringRisk = Boolean(filters.pendingOnly || filters.riskType || filters.riskLevel || filters.processingStatus || filters.minScore !== undefined || filters.maxScore !== undefined || filters.from || filters.to || filters.riskOnly || riskSort)
  const main = filteringRisk
    ? await fetchAllMainUsers(params)
    : await mainAdminClient.get<{ items: MainAdminUser[]; total: number }>('/admin/users', { params: { ...params, page: filters.page || 1, page_size: filters.pageSize || 20 } }).then(({ data }) => data)
  const riskItems = await fetchAllRiskSignals(filters, filteringRisk ? undefined : main.items.map((user) => user.id))
  const riskByID = new Map(riskItems.map((item) => [item.id, item]))
  const items = main.items.map((user) => {
    const signal = riskByID.get(user.id)
    const riskReason = formatRiskReason(signal?.reason, { eventType: signal?.risk_type, count: signal?.event_count })
    return { ...user, risk_type: signal?.risk_type || null, risk_level: signal?.risk_level || null, risk_score: signal?.score || 0, risk_reason: signal?.reason ? riskReason : null, last_action: signal?.last_action || null, pending: Boolean(signal?.pending), processing_status: signal?.pending ? 'pending' : signal?.last_action === 'review' ? 'reviewed' : signal?.last_action === 'ban' || signal?.last_action === 'auto_ban' ? 'banned' : signal?.last_action === 'unban' ? 'unbanned' : signal ? 'observed' : null, event_count: signal?.event_count || 0, ip_count: signal?.ip_count || 0, device_count: signal?.device_count || 0, last_event_at: signal?.last_event_at || null, last_risk_at: signal?.last_event_at || null } satisfies RiskUserRow
  }).filter((user) => (!filters.pendingOnly || Boolean(user.pending)) && (!filters.riskOnly || Boolean(user.risk_type)) && (!filters.riskType || user.risk_type === filters.riskType) && (!filters.riskLevel || user.risk_level === filters.riskLevel) && (!filters.processingStatus || user.processing_status === filters.processingStatus) && (filters.minScore === undefined || (user.risk_score || 0) >= filters.minScore) && (filters.maxScore === undefined || (user.risk_score || 0) <= filters.maxScore))
  if (filters.sortBy && filters.sortBy !== 'created_at') {
    const direction = filters.sortOrder === 'asc' ? 1 : -1
    const levelRank: Record<string, number> = { none: 0, low: 1, medium: 2, high: 3, critical: 4 }
    items.sort((left, right) => {
      const leftValue = filters.sortBy === 'risk_level' ? levelRank[left.risk_level || 'none'] || 0 : filters.sortBy === 'event_count' ? left.event_count || 0 : filters.sortBy === 'last_event_at' ? Date.parse(left.last_event_at || '') || 0 : left.risk_score || 0
      const rightValue = filters.sortBy === 'risk_level' ? levelRank[right.risk_level || 'none'] || 0 : filters.sortBy === 'event_count' ? right.event_count || 0 : filters.sortBy === 'last_event_at' ? Date.parse(right.last_event_at || '') || 0 : right.risk_score || 0
      if (leftValue === rightValue) return left.id - right.id
      return (leftValue < rightValue ? -1 : 1) * direction
    })
  }
  const currentPage = filters.page || 1
  const currentPageSize = filters.pageSize || 20
  const visibleItems = filteringRisk ? items.slice((currentPage - 1) * currentPageSize, currentPage * currentPageSize) : items
  return { items: visibleItems, total: filteringRisk ? items.length : main.total, page: currentPage, page_size: currentPageSize }
}

async function getUserDetail(id: number): Promise<RiskUserDetail> {
  const riskPromise = mainAdminClient
    .get<{ id: number; username: string; account_status: AccountStatus; risk_type: string; risk_level: RiskLevel; score: number; event_count: number; ip_count?: number; device_count?: number; last_event_at?: string; timeline?: Array<Record<string, unknown>> }>(`/admin/user-risk-control/users/${id}`)
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
  const timeline = Array.isArray(data.timeline) ? data.timeline : []
  const events: RiskEvent[] = timeline.map((event) => ({
    id: Number(event.id), type: String(event.event_type || ''), risk_type: event.risk_type as RiskEvent['risk_type'], risk_level: event.risk_level as RiskLevel | undefined,
    score: Number(event.score || 0), reason: formatRiskReason(event.reason, { eventType: String(event.risk_type || event.event_type || ''), ruleCode: Array.isArray(event.rule_codes) ? String(event.rule_codes[0] || '') : '' }), error_code: String(event.error_code || ''), endpoint: String(event.endpoint || ''), model: String(event.model || ''), ip: String(event.ip || ''), device_id: String(event.device_id || ''), decision: String(event.decision || ''), rule_codes: Array.isArray(event.rule_codes) ? event.rule_codes.map(String) : [], evidence: (event.evidence || {}) as Record<string, unknown>, occurred_at: String(event.occurred_at || event.created_at || ''),
  }))
  return {
    user: { id: data.id, username: main.data.username || data.username, email: main.data.email, status: main.data.status, risk_type: data.risk_type, risk_level: data.risk_level, risk_score: data.score, risk_reason: events[0]?.reason || null, event_count: data.event_count, ip_count: data.ip_count, device_count: data.device_count, last_event_at: data.last_event_at || null },
    summary: { score: data.score, level: data.risk_level, reason: events[0]?.reason || '', event_count: data.event_count },
    events, audit: audit.items, associations: { ip_count: Number(data.ip_count || 0), device_count: Number(data.device_count || 0) },
  }
}

async function setUserStatus(id: number, status: AccountStatus, reason: string, batchId?: string): Promise<RiskUserRow> {
  // Account status is authoritative in the main Sub2API users table.
  const { data } = await mainAdminClient.post<{ user: RiskUserRow }>(`/admin/users/${id}/risk-status`, { status, reason: reason.trim(), batch_id: batchId })
  return data.user
}

type BatchStatusResult = { id: number; status: 'success' | 'failed'; user?: RiskUserRow; reason?: string }

function errorMessage(error: unknown): string {
  if (typeof error === 'object' && error !== null) {
    const response = (error as { response?: { data?: { error?: string } } }).response
    if (response?.data?.error) return response.data.error
  }
  if (error instanceof Error && error.message.trim()) return error.message
  return '操作失败，未返回具体原因'
}

async function batchSetUserStatus(ids: number[], status: AccountStatus, reason: string, concurrency = 4): Promise<BatchStatusResult[]> {
  const trimmedReason = reason.trim()
  if (!trimmedReason) throw new Error('操作原因不能为空')
  const batchId = `risk-batch-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const results: BatchStatusResult[] = new Array(ids.length)
  let cursor = 0
  async function worker() {
    while (cursor < ids.length) {
      const index = cursor++
      const id = ids[index]
      try {
        const user = await setUserStatus(id, status, trimmedReason, batchId)
        results[index] = { id, status: 'success', user }
      } catch (error) {
        results[index] = { id, status: 'failed', reason: errorMessage(error) }
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(Math.max(concurrency, 1), 4, ids.length) }, () => worker()))
  return results
}

async function markUserProcessed(id: number, reason: string, batchId: string): Promise<void> {
  await mainAdminClient.post(`/admin/user-risk-control/users/${id}/processed`, { reason: reason.trim(), batch_id: batchId })
}

async function markUsersProcessed(ids: number[], reason: string, concurrency = 4): Promise<BatchStatusResult[]> {
  const trimmedReason = reason.trim()
  if (!trimmedReason) throw new Error('操作原因不能为空')
  const batchId = `risk-processed-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const results: BatchStatusResult[] = new Array(ids.length)
  let cursor = 0
  async function worker() {
    while (cursor < ids.length) {
      const index = cursor++
      const id = ids[index]
      try {
        await markUserProcessed(id, trimmedReason, batchId)
        results[index] = { id, status: 'success' }
      } catch (error) {
        results[index] = { id, status: 'failed', reason: errorMessage(error) }
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(Math.max(concurrency, 1), 4, ids.length) }, () => worker()))
  return results
}

async function listRules(): Promise<Rule[]> {
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>> }>('/admin/user-risk-control/rules')
  return data.items.map((rule) => ({ id: Number(rule.id), code: String(rule.code), name: String(rule.name || rule.code), description: String(rule.description || ''), eventTypes: Array.isArray(rule.event_types) ? rule.event_types.map(String) : [String(rule.code)], countStrategy: String(rule.count_strategy || 'associated_events') as Rule['countStrategy'], enabled: Boolean(rule.enabled), windowSeconds: Number(rule.window_seconds || 0), threshold: Number(rule.threshold || 1), score: Number(rule.score || 0), riskLevel: String(rule.risk_level || 'low') as RiskLevel, action: String(rule.action || 'observe') as RiskAction, revision: Number(rule.revision || 1) }))
}

async function updateRule(_id: number, rule: RuleInput): Promise<Pick<Rule, 'id' | 'revision'>> {
  const { data } = await mainAdminClient.put<Pick<Rule, 'id' | 'revision'>>(`/admin/user-risk-control/rules/${rule.code}`, {
    code: rule.code, name: rule.name, description: rule.description, event_types: rule.eventTypes?.length ? rule.eventTypes : [rule.code], count_strategy: rule.countStrategy, enabled: rule.enabled, window_seconds: rule.windowSeconds, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, revision: rule.revision,
  })
  return data
}

async function createRule(rule: RuleCreateInput): Promise<Rule> {
  const { data } = await mainAdminClient.post<Record<string, unknown>>('/admin/user-risk-control/rules', {
    code: rule.code,
    name: rule.name,
    description: rule.description,
    event_types: rule.eventTypes?.length ? rule.eventTypes : [rule.code],
    count_strategy: rule.countStrategy,
    enabled: rule.enabled,
    window_seconds: rule.windowSeconds,
    threshold: rule.threshold,
    score: rule.score,
    risk_level: rule.riskLevel,
    action: rule.action,
    revision: rule.revision || 1,
    reason: rule.reason,
  })
  return { id: Number(data.id), code: String(data.code || rule.code), name: String(data.name || rule.name || rule.code), description: String(data.description || rule.description || ''), eventTypes: Array.isArray(data.event_types) ? data.event_types.map(String) : rule.eventTypes, countStrategy: String(data.count_strategy || rule.countStrategy || 'associated_events') as Rule['countStrategy'], enabled: Boolean(data.enabled ?? rule.enabled), windowSeconds: Number(data.window_seconds ?? rule.windowSeconds), threshold: Number(data.threshold ?? rule.threshold), score: Number(data.score ?? rule.score), riskLevel: String(data.risk_level || rule.riskLevel) as RiskLevel, action: String(data.action || rule.action) as RiskAction, revision: Number(data.revision || 1) }
}

async function testRule(rule: Rule, input: Record<string, unknown>) {
  const { data } = await mainAdminClient.post<{ matched: boolean; score?: number; decision?: { score?: number; risk_level?: string; action?: string; rule_codes?: string[]; reason?: string } }>('/admin/user-risk-control/rules/test', { ...input, rule: { code: rule.code, enabled: rule.enabled, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, event_types: [String(input.event_type || rule.code)], count_strategy: rule.countStrategy } })
  return { matched: data.matched, score: data.score ?? data.decision?.score ?? 0, riskLevel: data.decision?.risk_level || rule.riskLevel, action: data.decision?.action || rule.action, conditions: data.decision?.rule_codes || [], reason: data.decision?.reason || '' }
}

async function listAudit(filters: AuditFilters = {}): Promise<RiskListResponse<RiskAuditRecord>> {
  const action = filters.action === 'rule_update' ? 'update_rule' : filters.action
  const admins = await fetchAllMainUsers({ role: 'admin' })
  const adminByID = new Map(admins.items.map((admin) => [admin.id, admin]))
  const actorQuery = filters.actor?.trim()
  let actorID: number | undefined
  if (actorQuery) {
    if (/^\d+$/.test(actorQuery)) actorID = Number(actorQuery)
    else {
      const normalized = actorQuery.toLocaleLowerCase()
      const matched = admins.items.find((admin) => admin.email.toLocaleLowerCase() === normalized || admin.username.toLocaleLowerCase() === normalized)
      if (!matched) return { items: [], total: 0, page: filters.page || 1, page_size: filters.pageSize || 20 }
      actorID = matched.id
    }
  }
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>>; total: number; page?: number; page_size?: number }>('/admin/user-risk-control/audit', {
    params: compactParams({ action, target_user_id: filters.targetUserId, target: filters.target, actor_id: actorID, result: filters.result, from: filters.from, to: filters.to, sort_by: filters.sortBy, sort_order: filters.sortOrder, page: filters.page || 1, limit: filters.pageSize || 20 }),
  })
  return { total: data.total, page: data.page || filters.page || 1, page_size: data.page_size || filters.pageSize || 20, items: data.items.map((record) => {
    const metadata = (record.metadata || {}) as Record<string, unknown>
    const recordActorID = Number(record.actor_id || 0)
    const actor = adminByID.get(recordActorID)
    return {
      id: Number(record.id), actor: actor?.email || actor?.username || (recordActorID ? String(recordActorID) : undefined),
      target_type: String(record.target_type || ''), target_id: String(record.target_id || ''),
      target_user_id: record.target_type === 'user' ? Number(record.target_id || 0) : 0,
      action: String(record.action), before_status: metadata.before_status as AccountStatus | null || null,
      after_status: metadata.after_status as AccountStatus | null || null, reason: String(record.reason || ''), failure_reason: String(record.failure_reason || metadata.failure_reason || ''), batch_id: String(record.batch_id || metadata.batch_id || ''), request_id: String(record.request_id || metadata.request_id || ''),
      result: String(record.result || 'success') as RiskAuditRecord['result'], created_at: String(record.created_at || ''),
    }
  }) }
}

export const userRiskControlV2API = { listUsers, getUserDetail, setUserStatus, batchSetUserStatus, markUsersProcessed, listRules, updateRule, createRule, testRule, listAudit }
export default userRiskControlV2API
