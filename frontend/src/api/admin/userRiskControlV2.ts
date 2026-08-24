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
  identity?: IdentityListSummary
	case_id?: number
	case_status?: RiskCaseStatus
	assignee_id?: number
	evidence_strength?: EvidenceStrength
	decision_id?: string
	historical_max_score?: number
	account_availability?: EvidenceAvailability
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
  identity_version?: 'legacy_v1'
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
	metadata?: Record<string, unknown>
	actor_account?: AuditAccount
	target_account?: AuditAccount
}

export interface AuditAccount {
  id: number
  email: string
  username?: string
  status?: string
  availability: EvidenceAvailability
}

export interface RiskListResponse<T> { items: T[]; total: number; page?: number; page_size?: number }
export type IdentityDomain = 'ip' | 'device' | 'composite'
export type IdentityDomainState = 'healthy' | 'degraded' | 'not_evaluable' | 'paused' | 'disabled'
export type EvidenceAvailability = 'available' | 'unavailable' | 'not_evaluable' | 'deleted'
export type EvidenceStrength = 'observation' | 'weak' | 'medium_high' | 'high'
export type RiskCaseStatus = 'pending' | 'in_review' | 'observing' | 'resolved'
export type RiskCaseView = 'pending' | 'my' | 'observing' | 'resolved' | 'all'
export type RiskFeedback = 'confirmed_abuse' | 'legitimate_shared' | 'insufficient_evidence' | 'data_error' | 'business_violation'
export interface IdentitySignalSummary { rule_code: string; rule_revision?: number; signal_family?: string; status?: 'active' | 'expired' | 'resolved' | 'superseded'; decision_id?: string; score: number; evidence_count: number; evidence_snapshot?: Record<string, unknown>; occurred_at: string; active_until?: string }
export interface IdentityDomainSummary { domain: IdentityDomain; state: IdentityDomainState; score: number; signal_count: number; historical_max_score?: number; historical_signal_count?: number; associated_account_count: number; signals: IdentitySignalSummary[] }
export interface IdentitySummary { user_id: number; identity_version: 'v2'; mode: 'shadow' | 'enforce'; overall_score: number; historical_max_score?: number; historical_signal_count?: number; legacy_notice: string; domains: IdentityDomainSummary[] }
export interface IdentityHealth { enabled: boolean; admin_enabled: boolean; mode: 'shadow' | 'enforce'; shadow_until?: string; schema: string; key_id?: string; geo_source: string; domains: Record<IdentityDomain, IdentityDomainState>; quality_domains?: Record<IdentityDomain, IdentityDomainState>; quality_24h: { events?: number; valid_ip?: number; valid_device?: number; linked_users?: number; max_network_users?: number; minimum_events?: number; minimum_coverage_percent?: number; maximum_ip_share_percent?: number }; delivery?: { enabled?: boolean; sources?: number; gap_sources?: number; stale_sources?: number; queue_depth?: number; dropped?: number; failed?: number }; processing?: { pending?: number; retry?: number; failed?: number }; features?: { current_score?: boolean; cases?: boolean; explain?: boolean; delivery?: boolean; composite_enforcement?: boolean }; configured_rule_count?: number; prospective_rule_count?: number; effective_rule_count?: number; ingest_queue?: { state?: string; queued?: number; capacity?: number; enqueued?: number; succeeded?: number; failed?: number; dropped?: number; average_latency_ms?: number } }
export interface IPIdentity { id: number; ip: string; ip_family: 4 | 6; ip_source: string; is_public: boolean; country_code: string; region: string; city: string; asn: number; geo_source: string; geo_verified: boolean; availability: Exclude<EvidenceAvailability, 'deleted'>; unavailable_reason?: string; unavailable_impact?: string; data_source: string; network_label?: string; first_seen_at: string; last_seen_at: string; registration_success_count: number; login_success_count: number; api_success_count: number; associated_account_count: number }
export interface DeviceIdentity { id: number; identity_kind: 'browser_instance' | 'browser_profile' | 'api_client'; display_code: string; confidence: 'low' | 'medium_high' | 'high'; browser_family: string; os_family: string; device_class: string; language_family: string; cookie_status: string; first_seen_at: string; last_seen_at: string; registration_success_count: number; login_success_count: number; api_success_count: number; network_count: number; associated_account_count: number }
export interface AssociatedRiskUser { user_id: number; relation: 'ip' | 'browser_instance' | 'api_client' | 'multi_domain' | 'composite'; shared_network_count: number; shared_browser_instance_count: number; shared_api_client_count: number; shared_device_count: number; cooccurring_evidence_count: number; evidence_strength: EvidenceStrength; evidence_window_seconds: number; concurrent: boolean; overlap_start?: string; overlap_end?: string; first_seen_at: string; last_seen_at: string; source_event_ids: number[]; limitations: string[]; account?: { id: number; email: string; username: string; status: string; availability: EvidenceAvailability; unavailable_reason?: string; deleted: boolean; created_at: string } }
export interface IdentityListSummary { user_id: number; latest_ip: string; country_code: string; region: string; browser_instance_count: number; api_client_count: number; associated_account_count: number; active_rule_count: number; quality_state: IdentityDomainState }
export interface IdentityRule { code: string; domain: 'account' | IdentityDomain; configured_enabled: boolean; enabled: boolean; state: IdentityDomainState; window_seconds: number; threshold: number; score: number; mode: 'shadow'; revision: number; updated_at: string; active_from?: string; active_until?: string }
export interface IdentityRuleEffect { rule_code: string; revision: number; hit_events: number; unique_subjects: number; sample_user_ids: number[]; confirmed_rate: number; legitimate_shared_rate: number; missing_signal_rate: number }
export interface IdentityRuleVersion { revision: number; signal_family: string; domain: string; enabled: boolean; rule_snapshot: Record<string, unknown>; active_from: string; active_until?: string }
export interface IdentityRebuildResult { id: number; dry_run: boolean; status: string; current_signal_users: number; v2_signal_users: number; current_signals: number; v2_signals: number; changed_subjects: number; rule_hits: Record<string, number>; sample_user_ids: number[]; evidence_high_water: number; rule_watermark: Record<string, number>; approved_dry_run_id?: number; started_at: string; completed_at?: string }
export interface AuditFilters {
	category?: 'security' | 'rules' | 'sensitive'
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
	view?: RiskCaseView
  sortBy?: RiskSortBy
  sortOrder?: SortOrder
}

export interface RiskWorkOverview { pending: number; mine: number; observing: number; atRisk: number; dataQuality: number }

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
  countStrategy?: 'user_events' | 'email_subject_events' | 'ip_distinct_success_users' | 'browser_instance_distinct_success_users' | 'api_client_distinct_users' | 'ip_browser_cooccurrence'
}

export type RuleInput = Omit<Rule, 'id' | 'name'> & { code: string; name?: string }
export type RuleCreateInput = Omit<Rule, 'id' | 'revision' | 'eventTypes'> & { eventTypes: string[]; revision?: number; reason?: string }

function compactParams(params: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== undefined && value !== '' && value !== false))
}

async function listUsers(filters: UserRiskFilters = {}): Promise<RiskListResponse<RiskUserRow>> {
  const { data } = await mainAdminClient.get<RiskListResponse<RiskUserRow>>('/admin/user-risk/users', {
		params: compactParams({ view: filters.view || 'pending', page: filters.page || 1, page_size: filters.pageSize || 20, search: filters.search, status: filters.status, risk_type: filters.riskType, risk_level: filters.riskLevel, processing_status: filters.processingStatus, risk_only: filters.riskOnly, min_score: filters.minScore, max_score: filters.maxScore, sort_by: filters.sortBy, sort_order: filters.sortOrder }),
	})
	return data
}

async function getWorkOverview(): Promise<RiskWorkOverview> {
  const { data } = await mainAdminClient.get<{ pending?: number; mine?: number; observing?: number; at_risk?: number; data_quality?: number }>('/admin/user-risk/work-overview')
  return { pending: Number(data.pending || 0), mine: Number(data.mine || 0), observing: Number(data.observing || 0), atRisk: Number(data.at_risk || 0), dataQuality: Number(data.data_quality || 0) }
}

async function getUserDetail(id: number): Promise<RiskUserDetail> {
  const riskPromise = mainAdminClient
    .get<{ id: number; username: string; account_status: AccountStatus; risk_type: string; risk_level: RiskLevel; score: number; reason?: string; event_count: number; ip_count?: number; device_count?: number; last_event_at?: string; case_id?: number; case_status?: RiskCaseStatus; timeline?: Array<Record<string, unknown>> }>(`/admin/user-risk-control/users/${id}`)
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
    id: Number(event.id), type: String(event.event_type || ''), identity_version: event.identity_version === 'legacy_v1' ? 'legacy_v1' : undefined, risk_type: event.risk_type as RiskEvent['risk_type'], risk_level: event.risk_level as RiskLevel | undefined,
    score: Number(event.score || 0), reason: formatRiskReason(event.reason, { eventType: String(event.risk_type || event.event_type || ''), identityVersion: String(event.identity_version || ''), ruleCode: Array.isArray(event.rule_codes) ? String(event.rule_codes[0] || '') : '' }), error_code: String(event.error_code || ''), endpoint: String(event.endpoint || ''), model: String(event.model || ''), ip: String(event.ip || ''), device_id: String(event.device_id || ''), decision: String(event.decision || ''), rule_codes: Array.isArray(event.rule_codes) ? event.rule_codes.map(String) : [], evidence: (event.evidence || {}) as Record<string, unknown>, occurred_at: String(event.occurred_at || event.created_at || ''),
  }))
  const subjectReason = data.reason ? formatRiskReason(data.reason, { eventType: data.risk_type }) : null
  return {
    user: { id: data.id, username: main.data.username || data.username, email: main.data.email, status: main.data.status, risk_type: data.risk_type || null, risk_level: data.risk_level === 'none' ? null : data.risk_level, risk_score: data.score, risk_reason: subjectReason, event_count: data.event_count, ip_count: data.ip_count, device_count: data.device_count, last_event_at: data.last_event_at || null, case_id: data.case_id || undefined, case_status: data.case_status || undefined },
    summary: { score: data.score, level: data.risk_level, reason: subjectReason || '', event_count: data.event_count },
    events, audit: audit.items, associations: { ip_count: Number(data.ip_count || 0), device_count: Number(data.device_count || 0) },
  }
}

async function getUserIdentitySummary(id: number): Promise<IdentitySummary> {
  const { data } = await mainAdminClient.get<IdentitySummary>(`/admin/users/${id}/identity-summary`)
  return data
}

async function listUserIPIdentities(id: number, page = 1, pageSize = 20, exactIP = ''): Promise<RiskListResponse<IPIdentity>> {
  const query = exactIP.trim()
  if (!query) {
    const { data } = await mainAdminClient.get<RiskListResponse<IPIdentity>>(`/admin/users/${id}/ip-identities`, { params: { page, limit: pageSize } })
    return data
  }
  const { data } = await mainAdminClient.post<RiskListResponse<IPIdentity>>(`/admin/users/${id}/ip-identities/search`, { page, limit: pageSize, query })
  return data
}

async function listUserDeviceIdentities(id: number, page = 1, pageSize = 20): Promise<RiskListResponse<DeviceIdentity>> {
  const { data } = await mainAdminClient.get<RiskListResponse<DeviceIdentity>>(`/admin/users/${id}/device-identities`, { params: { page, limit: pageSize } })
  return data
}

async function listAssociatedUsers(id: number, page = 1, pageSize = 20): Promise<RiskListResponse<AssociatedRiskUser>> {
  const { data } = await mainAdminClient.get<RiskListResponse<AssociatedRiskUser>>(`/admin/users/${id}/associated-users`, { params: { page, limit: pageSize } })
  return data
}

async function getIdentityHealth(): Promise<IdentityHealth> {
  const { data } = await mainAdminClient.get<IdentityHealth>('/admin/identity-health')
  return data
}

async function listIdentityRules(): Promise<IdentityRule[]> {
  const { data } = await mainAdminClient.get<{ items: IdentityRule[] }>('/admin/user-risk-control/identity-rules')
  return data.items
}

async function listIdentityRuleEffects(): Promise<IdentityRuleEffect[]> {
	const { data } = await mainAdminClient.get<{ items: IdentityRuleEffect[] }>('/admin/user-risk-control/identity-rule-effects')
	return data.items
}

async function listIdentityRuleVersions(code: string): Promise<IdentityRuleVersion[]> {
	const { data } = await mainAdminClient.get<{ items: IdentityRuleVersion[] }>(`/admin/user-risk-control/identity-rules/${encodeURIComponent(code)}/versions`)
	return data.items
}

async function disableIdentityRule(code: string, reason: string): Promise<{ code: string; revision: number; enabled: false }> {
	const { data } = await mainAdminClient.post<{ code: string; revision: number; enabled: false }>(`/admin/user-risk-control/identity-rules/${encodeURIComponent(code)}/disable`, { reason: reason.trim() })
	return data
}

async function dryRunIdentityRebuild(): Promise<IdentityRebuildResult> {
	const { data } = await mainAdminClient.post<IdentityRebuildResult>('/admin/risk-rebuilds/dry-run', {})
	return data
}

async function applyIdentityRebuild(approvedDryRunID: number): Promise<IdentityRebuildResult> {
	const { data } = await mainAdminClient.post<IdentityRebuildResult>('/admin/risk-rebuilds', { approved_dry_run_id: approvedDryRunID })
	return data
}

export type RiskStatusResult = { user: RiskUserRow; result: 'success' | 'partial'; failureReason?: string }

async function setUserStatusResult(id: number, status: AccountStatus, reason: string, batchId?: string): Promise<RiskStatusResult> {
  // Account status is authoritative in the main Sub2API users table.
	const { data } = await mainAdminClient.post<{ user: RiskUserRow; result?: 'success' | 'partial'; failure_reason?: string }>(`/admin/users/${id}/risk-status`, { status, reason: reason.trim(), batch_id: batchId })
	return { user: data.user, result: data.result === 'partial' ? 'partial' : 'success', failureReason: data.failure_reason }
}

async function setUserStatus(id: number, status: AccountStatus, reason: string, batchId?: string): Promise<RiskStatusResult> {
	return setUserStatusResult(id, status, reason, batchId)
}

type BatchStatusResult = { id: number; status: 'success' | 'partial' | 'failed'; user?: RiskUserRow; reason?: string }

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
		const outcome = await setUserStatusResult(id, status, trimmedReason, batchId)
		results[index] = { id, status: outcome.result, user: outcome.user, reason: outcome.failureReason }
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
  return data.items.map((rule) => ({ id: Number(rule.id), code: String(rule.code), name: String(rule.name || rule.code), description: String(rule.description || ''), eventTypes: Array.isArray(rule.event_types) ? rule.event_types.map(String) : [String(rule.code)], countStrategy: String(rule.count_strategy || 'user_events') as Rule['countStrategy'], enabled: Boolean(rule.enabled), windowSeconds: Number(rule.window_seconds || 0), threshold: Number(rule.threshold || 1), score: Number(rule.score || 0), riskLevel: String(rule.risk_level || 'low') as RiskLevel, action: String(rule.action || 'observe') as RiskAction, revision: Number(rule.revision || 1) }))
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
  return { id: Number(data.id), code: String(data.code || rule.code), name: String(data.name || rule.name || rule.code), description: String(data.description || rule.description || ''), eventTypes: Array.isArray(data.event_types) ? data.event_types.map(String) : rule.eventTypes, countStrategy: String(data.count_strategy || rule.countStrategy || 'user_events') as Rule['countStrategy'], enabled: Boolean(data.enabled ?? rule.enabled), windowSeconds: Number(data.window_seconds ?? rule.windowSeconds), threshold: Number(data.threshold ?? rule.threshold), score: Number(data.score ?? rule.score), riskLevel: String(data.risk_level || rule.riskLevel) as RiskLevel, action: String(data.action || rule.action) as RiskAction, revision: Number(data.revision || 1) }
}

async function testRule(rule: Rule, input: Record<string, unknown>) {
  const { data } = await mainAdminClient.post<{ matched: boolean; score?: number; decision?: { score?: number; risk_level?: string; action?: string; rule_codes?: string[]; reason?: string } }>('/admin/user-risk-control/rules/test', { ...input, rule: { code: rule.code, enabled: rule.enabled, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, event_types: [String(input.event_type || rule.code)], count_strategy: rule.countStrategy } })
  return { matched: data.matched, score: data.score ?? data.decision?.score ?? 0, riskLevel: data.decision?.risk_level || rule.riskLevel, action: data.decision?.action || rule.action, conditions: data.decision?.rule_codes || [], reason: data.decision?.reason || '' }
}

async function listAudit(filters: AuditFilters = {}): Promise<RiskListResponse<RiskAuditRecord>> {
  const action = filters.action === 'rule_update' ? 'update_rule' : filters.action
  const actorQuery = filters.actor?.trim()
	const actorID = actorQuery && /^\d+$/.test(actorQuery) ? Number(actorQuery) : undefined
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>>; total: number; page?: number; page_size?: number }>('/admin/user-risk-control/audit', {
    params: compactParams({ category: filters.category || 'security', action, target_user_id: filters.targetUserId, target: filters.target, actor_id: actorID, actor: actorID ? undefined : actorQuery, result: filters.result, from: filters.from, to: filters.to, sort_by: filters.sortBy, sort_order: filters.sortOrder, page: filters.page || 1, limit: filters.pageSize || 20 }),
  })
  return { total: data.total, page: data.page || filters.page || 1, page_size: data.page_size || filters.pageSize || 20, items: data.items.map((record) => {
    const metadata = (record.metadata || {}) as Record<string, unknown>
    const recordActorID = Number(record.actor_id || 0)
    return {
		id: Number(record.id), actor: recordActorID ? String(recordActorID) : undefined,
      target_type: String(record.target_type || ''), target_id: String(record.target_id || ''),
      target_user_id: record.target_type === 'user' ? Number(record.target_id || 0) : 0,
      action: String(record.action), before_status: metadata.before_status as AccountStatus | null || null,
      after_status: metadata.after_status as AccountStatus | null || null, reason: String(record.reason || ''), failure_reason: String(record.failure_reason || metadata.failure_reason || ''), batch_id: String(record.batch_id || metadata.batch_id || ''), request_id: String(record.request_id || metadata.request_id || ''),
		metadata,
		actor_account: record.actor_account as RiskAuditRecord['actor_account'],
		target_account: record.target_account as RiskAuditRecord['target_account'],
		result: String(record.result || 'success') as RiskAuditRecord['result'], created_at: String(record.created_at || ''),
    }
  }) }
}

async function claimReviewCase(id: number): Promise<void> {
	await mainAdminClient.post(`/admin/user-risk-control/review-cases/${id}/claim`, {})
}

async function submitReviewFeedback(id: number, feedback: RiskFeedback, reason: string): Promise<void> {
	await mainAdminClient.post(`/admin/user-risk-control/review-cases/${id}/feedback`, { feedback, reason: reason.trim() })
}

export const userRiskControlV2API = { listUsers, getWorkOverview, getUserDetail, getUserIdentitySummary, listUserIPIdentities, listUserDeviceIdentities, listAssociatedUsers, getIdentityHealth, listIdentityRules, listIdentityRuleEffects, listIdentityRuleVersions, disableIdentityRule, dryRunIdentityRebuild, applyIdentityRebuild, claimReviewCase, submitReviewFeedback, setUserStatus, batchSetUserStatus, markUsersProcessed, listRules, updateRule, createRule, testRule, listAudit }
export default userRiskControlV2API
