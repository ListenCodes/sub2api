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
	created_by?: number
	review_due_at?: string
	observation_goal?: string
	resolution_reason?: string
	case_revision?: number
	last_activity_at?: string
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
export type RiskCaseView = 'unassigned' | 'mine' | 'due' | 'open' | 'users'
export type RiskFeedback = 'confirmed_abuse' | 'legitimate_shared' | 'insufficient_evidence' | 'data_error' | 'business_violation'
export interface IdentitySignalSummary { rule_code: string; rule_revision?: number; signal_family?: string; status?: 'active' | 'expired' | 'resolved' | 'superseded'; decision_id?: string; score: number; evidence_count: number; evidence_snapshot?: Record<string, unknown>; occurred_at: string; active_until?: string }
export interface IdentityDomainSummary { domain: IdentityDomain; state: IdentityDomainState; score: number; signal_count: number; historical_max_score?: number; historical_signal_count?: number; associated_account_count: number; signals: IdentitySignalSummary[] }
export interface IdentitySummary { user_id: number; identity_version: 'v2'; mode: 'shadow' | 'enforce'; overall_score: number; historical_max_score?: number; historical_signal_count?: number; legacy_notice: string; domains: IdentityDomainSummary[] }
export interface IdentityHealth { enabled: boolean; admin_enabled: boolean; mode: 'shadow' | 'enforce'; shadow_until?: string; schema: string; key_id?: string; geo_source: string; domains: Record<IdentityDomain, IdentityDomainState>; quality_domains?: Record<IdentityDomain, IdentityDomainState>; quality_24h: { events?: number; valid_ip?: number; valid_device?: number; linked_users?: number; max_network_users?: number; minimum_events?: number; minimum_coverage_percent?: number; maximum_ip_share_percent?: number }; delivery?: { enabled?: boolean; sources?: number; gap_sources?: number; stale_sources?: number; queue_depth?: number; dropped?: number; failed?: number }; processing?: { pending?: number; retry?: number; failed?: number }; features?: { current_score?: boolean; cases?: boolean; explain?: boolean; delivery?: boolean; composite_enforcement?: boolean }; configured_rule_count?: number; prospective_rule_count?: number; effective_rule_count?: number; ingest_queue?: { state?: string; queued?: number; capacity?: number; enqueued?: number; succeeded?: number; failed?: number; dropped?: number; average_latency_ms?: number } }
export type SharedNetworkLabel = 'home' | 'company' | 'school' | 'public_proxy' | 'trusted_egress' | 'mobile_cgnat' | 'unknown'
export interface IPIdentity { id: number; ip: string; ip_family: 4 | 6; ip_source: string; is_public: boolean; country_code: string; region: string; city: string; asn: number; geo_source: string; geo_verified: boolean; availability: Exclude<EvidenceAvailability, 'deleted'>; unavailable_reason?: string; unavailable_impact?: string; data_source: string; network_label?: SharedNetworkLabel; network_label_reason?: string; first_seen_at: string; last_seen_at: string; registration_success_count: number; login_success_count: number; api_success_count: number; associated_account_count: number }
export interface DeviceIdentity { id: number; identity_kind: 'browser_instance' | 'browser_profile' | 'api_client'; display_code: string; confidence: 'low' | 'medium_high' | 'high'; browser_family: string; os_family: string; device_class: string; language_family: string; cookie_status: string; first_seen_at: string; last_seen_at: string; registration_success_count: number; login_success_count: number; api_success_count: number; network_count: number; associated_account_count: number }
export interface AssociatedRiskUser { user_id: number; relation: 'ip' | 'browser_instance' | 'api_client' | 'multi_domain' | 'composite'; shared_network_count: number; shared_browser_instance_count: number; shared_api_client_count: number; shared_device_count: number; cooccurring_evidence_count: number; evidence_strength: EvidenceStrength; evidence_window_seconds: number; concurrent: boolean; overlap_start?: string; overlap_end?: string; first_seen_at: string; last_seen_at: string; source_event_ids: number[]; limitations: string[]; account?: { id: number; email: string; username: string; status: string; availability: EvidenceAvailability; unavailable_reason?: string; deleted: boolean; created_at: string } }
export interface IdentityListSummary { user_id: number; latest_ip: string; country_code: string; region: string; browser_instance_count: number; api_client_count: number; associated_account_count: number; active_signal_count?: number; active_rule_count?: number; quality_state: IdentityDomainState }
export type IdentityConfiguredAction = 'observe' | 'review' | 'reject_candidate' | 'auto_ban'
export interface IdentityRule { code: string; domain: 'account' | IdentityDomain; configured_enabled: boolean; enabled: boolean; state: IdentityDomainState; detection_state: IdentityDomainState; decision_mode: 'shadow' | 'enforce'; configured_action: IdentityConfiguredAction; effective_action: 'none' | IdentityConfiguredAction; data_quality: IdentityDomainState; enforcement_eligible: boolean; reason_codes: string[]; config_source: string; window_seconds: number; threshold: number; score: number; mode: 'shadow' | 'enforce'; revision: number; updated_at: string; active_from?: string; active_until?: string }
export interface IdentityRuleDraft { rule_code: string; base_revision: number; window_seconds: number; threshold: number; score: number; configured_action: IdentityConfiguredAction; reason: string; updated_by?: number; updated_at?: string }
export interface IdentityRuleSimulation { id: number; rule_code: string; base_revision: number; draft: IdentityRuleDraft; affected_signal_count: number; affected_account_count: number; open_case_count: number; configured_action: IdentityConfiguredAction; projected_effective_action: string; existing_accounts_changed: boolean; candidate_account_effect: string; warnings: string[]; expires_at: string; created_at: string }
export interface NetworkLabelImpact { network_id: number; current_label?: SharedNetworkLabel; proposed_label?: SharedNetworkLabel; affected_signal_count: number; affected_account_count: number; affected_decision_count: number; resolved_domains: string[]; requires_rebuild: boolean }
export interface IdentityRuleEffect { rule_code: string; revision: number; hit_events: number; unique_subjects: number; sample_user_ids: number[]; confirmed_rate: number; legitimate_shared_rate: number; missing_signal_rate: number }
export interface IdentityRuleVersion { revision: number; signal_family: string; domain: string; enabled: boolean; rule_snapshot: Record<string, unknown>; active_from: string; active_until?: string }
export interface IdentityRebuildResult { id: number; dry_run: boolean; status: string; current_signal_users: number; v2_signal_users: number; current_signals: number; v2_signals: number; changed_subjects: number; rule_hits: Record<string, number>; sample_user_ids: number[]; evidence_high_water: number; rule_watermark: Record<string, number>; approved_dry_run_id?: number; started_at: string; completed_at?: string }
export interface AuditFilters {
	category?: 'disposition' | 'configuration' | 'testing' | 'sensitive'
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

export interface RiskWorkOverview { unassignedPending: number; myInReview: number; reviewDue: number; allOpen: number }

export interface ResolveRiskCaseResult {
	result: 'success' | 'partial'
	request_id: string
	retryable: boolean
	account: { user_id: number; action?: string; result: string; retryable?: boolean; pending_step?: string; before_status?: string; after_status?: string; failure_reason?: string }
	case: { id?: number; result?: string; failure_reason?: string; case?: Record<string, unknown>; idempotent_replay?: boolean }
}

export interface RiskReviewCaseDetail {
	id: number
	user_id: number
	status: RiskCaseStatus
	revision: number
	review_due_at?: string
	observation_goal?: string
	resolution_reason?: string
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
  countStrategy?: 'user_events' | 'email_subject_events' | 'ip_distinct_success_users' | 'browser_instance_distinct_success_users' | 'api_client_distinct_users' | 'ip_browser_cooccurrence'
}

export type RuleInput = Omit<Rule, 'id' | 'name'> & { code: string; name?: string; reason?: string }
export type RuleCreateInput = Omit<Rule, 'id' | 'revision' | 'eventTypes'> & { eventTypes: string[]; revision?: number; reason?: string }

function compactParams(params: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== undefined && value !== '' && value !== false))
}

async function listUsers(filters: UserRiskFilters = {}): Promise<RiskListResponse<RiskUserRow>> {
  const { data } = await mainAdminClient.get<RiskListResponse<RiskUserRow>>('/admin/user-risk/users', {
		params: compactParams({ view: filters.view || 'users', page: filters.page || 1, page_size: filters.pageSize || 20, search: filters.search, status: filters.status, risk_type: filters.riskType, risk_level: filters.riskLevel, processing_status: (filters.view || 'users') === 'users' ? filters.processingStatus : undefined, risk_only: filters.riskOnly, min_score: filters.minScore, max_score: filters.maxScore, sort_by: filters.sortBy, sort_order: filters.sortOrder }),
	})
	return data
}

async function getWorkOverview(): Promise<RiskWorkOverview> {
  const { data } = await mainAdminClient.get<{ unassigned_pending?: number; my_in_review?: number; review_due?: number; all_open?: number; pending?: number; mine?: number; observing?: number }>('/admin/user-risk/work-overview')
	const unassignedPending = Number(data.unassigned_pending ?? data.pending ?? 0)
	const myInReview = Number(data.my_in_review ?? data.mine ?? 0)
	const reviewDue = Number(data.review_due ?? data.observing ?? 0)
	return { unassignedPending, myInReview, reviewDue, allOpen: Number(data.all_open ?? (unassignedPending + myInReview + reviewDue)) }
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

function identitySessionConfig(viewSession = ''): { headers?: Record<string, string> } { return viewSession ? { headers: { 'X-Risk-View-Session': viewSession } } : {} }

async function getUserIdentitySummary(id: number, viewSession = ''): Promise<IdentitySummary> {
  const path = `/admin/users/${id}/identity-summary`
  const { data } = viewSession
    ? await mainAdminClient.get<IdentitySummary>(path, identitySessionConfig(viewSession))
    : await mainAdminClient.get<IdentitySummary>(path)
  return data
}

async function listUserIPIdentities(id: number, page = 1, pageSize = 20, exactIP = '', viewSession = ''): Promise<RiskListResponse<IPIdentity>> {
  const query = exactIP.trim()
  if (!query) {
    const { data } = await mainAdminClient.get<RiskListResponse<IPIdentity>>(`/admin/users/${id}/ip-identities`, { params: { page, limit: pageSize }, ...identitySessionConfig(viewSession) })
    return data
  }
  const path = `/admin/users/${id}/ip-identities/search`
  const payload = { page, limit: pageSize, query }
  const { data } = viewSession
    ? await mainAdminClient.post<RiskListResponse<IPIdentity>>(path, payload, identitySessionConfig(viewSession))
    : await mainAdminClient.post<RiskListResponse<IPIdentity>>(path, payload)
  return data
}

async function listUserDeviceIdentities(id: number, page = 1, pageSize = 20, viewSession = ''): Promise<RiskListResponse<DeviceIdentity>> {
  const { data } = await mainAdminClient.get<RiskListResponse<DeviceIdentity>>(`/admin/users/${id}/device-identities`, { params: { page, limit: pageSize }, ...identitySessionConfig(viewSession) })
  return data
}

async function listAssociatedUsers(id: number, page = 1, pageSize = 20, viewSession = ''): Promise<RiskListResponse<AssociatedRiskUser>> {
  const { data } = await mainAdminClient.get<RiskListResponse<AssociatedRiskUser>>(`/admin/users/${id}/associated-users`, { params: { page, limit: pageSize }, ...identitySessionConfig(viewSession) })
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

async function saveIdentityRuleDraft(code: string, draft: Omit<IdentityRuleDraft, 'rule_code'>): Promise<IdentityRuleDraft> {
	const { data } = await mainAdminClient.post<IdentityRuleDraft>(`/admin/user-risk-control/identity-rules/${encodeURIComponent(code)}/draft`, draft)
	return data
}

async function simulateIdentityRule(code: string, targetRevision?: number): Promise<IdentityRuleSimulation> {
	const { data } = await mainAdminClient.post<IdentityRuleSimulation>(`/admin/user-risk-control/identity-rules/${encodeURIComponent(code)}/simulations`, targetRevision ? { target_revision: targetRevision } : {})
	return data
}

export type IdentityRuleApproval = {
	reason?: string
	baseRevision?: number
	windowSeconds?: number
	threshold?: number
	score?: number
	configuredAction?: IdentityConfiguredAction
	enabled?: boolean
	targetRevision?: number
	simulationId?: number
	confirmed?: boolean
	confirmation?: string
}

async function identityRuleLifecycle(code: string, operation: 'publish' | 'enable' | 'rollback', approval: IdentityRuleApproval): Promise<{ code: string; revision: number; operation: string }> {
	const { data } = await mainAdminClient.post<{ code: string; revision: number; operation: string }>(`/admin/user-risk-control/identity-rules/${encodeURIComponent(code)}/${operation}`, {
		reason: approval.reason?.trim() || '',
		base_revision: approval.baseRevision,
		window_seconds: approval.windowSeconds,
		threshold: approval.threshold,
		score: approval.score,
		configured_action: approval.configuredAction,
		enabled: approval.enabled,
		target_revision: approval.targetRevision,
		simulation_id: approval.simulationId,
		confirmed: approval.confirmed,
		confirmation: approval.confirmation,
	})
	return data
}

async function previewNetworkLabel(id: number, label: SharedNetworkLabel | ''): Promise<NetworkLabelImpact> {
	const { data } = await mainAdminClient.post<NetworkLabelImpact>(`/admin/user-risk-control/network-identities/${id}/label-preview`, { label })
	return data
}

async function applyNetworkLabel(id: number, label: SharedNetworkLabel, reason: string): Promise<{ updated: boolean; impact: NetworkLabelImpact }> {
	const { data } = await mainAdminClient.post<{ updated: boolean; impact: NetworkLabelImpact }>(`/admin/user-risk-control/network-identities/${id}/label`, { label, reason: reason.trim() })
	return data
}

async function revokeNetworkLabel(id: number, reason: string): Promise<NetworkLabelImpact> {
	const { data } = await mainAdminClient.post<NetworkLabelImpact>(`/admin/user-risk-control/network-identities/${id}/label-revoke`, { reason: reason.trim() })
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

function riskRequestID(prefix: string): string {
	return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

export type RiskStatusResult = { user: RiskUserRow; result: 'success' | 'partial'; failureReason?: string; requestId: string; batchId?: string; retryable: boolean; pendingStep?: string }

async function setUserStatusResult(id: number, status: AccountStatus, reason: string, batchId?: string, requestId?: string): Promise<RiskStatusResult> {
  // Account status is authoritative in the main Sub2API users table.
	const operationID = requestId || riskRequestID(`risk-status-${id}`)
	const { data } = await mainAdminClient.post<{ user: RiskUserRow; result?: 'success' | 'partial'; failure_reason?: string; request_id?: string; batch_id?: string; retryable?: boolean; pending_step?: string }>(`/admin/users/${id}/risk-status`, compactParams({ status, reason: reason.trim(), batch_id: batchId, request_id: operationID }), { headers: { 'Idempotency-Key': operationID } })
	return { user: data.user, result: data.result === 'partial' ? 'partial' : 'success', failureReason: data.failure_reason, requestId: data.request_id || operationID, batchId: data.batch_id || batchId, retryable: data.retryable === true, pendingStep: data.pending_step }
}

async function setUserStatus(id: number, status: AccountStatus, reason: string, batchId?: string, requestId?: string): Promise<RiskStatusResult> {
	return setUserStatusResult(id, status, reason, batchId, requestId)
}

export type BatchStatusResult = { id: number; status: 'success' | 'partial' | 'failed'; user?: RiskUserRow; reason?: string; operationReason?: string; requestedStatus?: AccountStatus; requestId?: string; batchId?: string; retryable?: boolean; pendingStep?: string }

function errorMessage(error: unknown): string {
  if (typeof error === 'object' && error !== null) {
    const message = (error as { message?: unknown }).message
    if (typeof message === 'string' && message.trim()) return message
    const response = (error as { response?: { data?: { error?: string } } }).response
    if (response?.data?.error) return response.data.error
  }
  if (error instanceof Error && error.message.trim()) return error.message
  return '操作失败，未返回具体原因'
}

function statusFromError(error: unknown): number {
	if (typeof error !== 'object' || error === null) return 0
	const direct = Number((error as { status?: unknown }).status)
	if (Number.isFinite(direct) && direct > 0) return direct
	const nested = Number((error as { response?: { status?: unknown } }).response?.status)
	return Number.isFinite(nested) && nested > 0 ? nested : 0
}

function isDefinitiveStatusFailure(error: unknown): boolean {
	const status = statusFromError(error)
	return status >= 400 && status < 500 && status !== 408 && status !== 425 && status !== 429
}

async function batchSetUserStatus(ids: number[], status: AccountStatus, reason: string, concurrency = 4, onPrepared?: (items: BatchStatusResult[]) => void): Promise<BatchStatusResult[]> {
  const trimmedReason = reason.trim()
  if (!trimmedReason) throw new Error('操作原因不能为空')
  const batchId = `risk-batch-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
	const results: BatchStatusResult[] = ids.map((id) => ({ id, status: 'partial', operationReason: trimmedReason, requestedStatus: status, requestId: `${batchId}:${id}`, batchId, retryable: true, pendingStep: 'status_confirmation', reason: '请求已发送，等待确认结果' }))
	onPrepared?.(results.map((item) => ({ ...item })))
  let cursor = 0
  async function worker() {
    while (cursor < ids.length) {
		const index = cursor++
		const id = ids[index]
		const requestId = results[index].requestId!
		try {
		const outcome = await setUserStatusResult(id, status, trimmedReason, batchId, requestId)
		results[index] = { id, status: outcome.result, user: outcome.user, reason: outcome.failureReason, operationReason: trimmedReason, requestedStatus: status, requestId: outcome.requestId, batchId: outcome.batchId || batchId, retryable: outcome.retryable, pendingStep: outcome.pendingStep }
      } catch (error) {
		results[index] = isDefinitiveStatusFailure(error)
			? { id, status: 'failed', reason: errorMessage(error), operationReason: trimmedReason, requestedStatus: status, requestId, batchId, retryable: false }
			: { id, status: 'partial', reason: `请求结果未知：${errorMessage(error)}`, operationReason: trimmedReason, requestedStatus: status, requestId, batchId, retryable: true, pendingStep: 'status_confirmation' }
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(Math.max(concurrency, 1), 4, ids.length) }, () => worker()))
  return results
}

async function retryUserSessionRevocation(id: number, reason: string, requestId: string, batchId?: string): Promise<RiskStatusResult> {
	const retryKey = riskRequestID(`risk-session-${id}`)
	const { data } = await mainAdminClient.post<{ user?: RiskUserRow; result: 'success' | 'partial'; failure_reason?: string; request_id?: string; batch_id?: string; retryable?: boolean; pending_step?: string }>(`/admin/users/${id}/risk-status/revoke-sessions`, { reason: reason.trim(), request_id: requestId, batch_id: batchId }, { headers: { 'Idempotency-Key': retryKey } })
	return { user: data.user || ({ id, email: '', status: 'disabled' } as RiskUserRow), result: data.result, failureReason: data.failure_reason, requestId: data.request_id || requestId, batchId: data.batch_id || batchId, retryable: data.retryable === true, pendingStep: data.pending_step }
}

async function retryBatchSessionRevocations(items: BatchStatusResult[], concurrency = 4): Promise<BatchStatusResult[]> {
	const results = [...items]
	const indexes = items.map((item, index) => ({ item, index })).filter(({ item }) => item.status === 'partial' && item.retryable && item.pendingStep && item.requestId && item.operationReason)
	let cursor = 0
	async function worker() {
		while (cursor < indexes.length) {
			const { item, index } = indexes[cursor++]
			try {
				const outcome = item.pendingStep === 'session_revocation'
					? await retryUserSessionRevocation(item.id, item.operationReason!, item.requestId!, item.batchId)
					: await setUserStatusResult(item.id, item.requestedStatus || item.user?.status || 'disabled', item.operationReason!, item.batchId, item.requestId!)
				results[index] = { ...item, status: outcome.result, user: outcome.user, reason: outcome.failureReason, retryable: outcome.retryable, pendingStep: outcome.pendingStep }
			} catch (error) {
				results[index] = isDefinitiveStatusFailure(error)
					? { ...item, status: 'failed', reason: errorMessage(error), retryable: false, pendingStep: undefined }
					: { ...item, reason: errorMessage(error) }
			}
		}
	}
	await Promise.all(Array.from({ length: Math.min(Math.max(concurrency, 1), 4, indexes.length) }, () => worker()))
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
    code: rule.code, name: rule.name, description: rule.description, event_types: rule.eventTypes?.length ? rule.eventTypes : [rule.code], count_strategy: rule.countStrategy, enabled: rule.enabled, window_seconds: rule.windowSeconds, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, revision: rule.revision, reason: rule.reason,
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
	const sample = (input.sample || input) as Record<string, unknown>
	const { data } = await mainAdminClient.post<{ matched: boolean; score?: number; configured_action?: string; effective_action?: string; excluded_reasons?: string[]; evaluation?: Array<{ step: string; passed: boolean; detail: unknown }>; decision?: { score?: number; risk_level?: string; action?: string; rule_codes?: string[]; reason?: string } }>('/admin/user-risk-control/rules/test', { sample, rule: { code: rule.code, name: rule.name, enabled: rule.enabled, window_seconds: rule.windowSeconds, threshold: rule.threshold, score: rule.score, risk_level: rule.riskLevel, action: rule.action, event_types: rule.eventTypes, count_strategy: rule.countStrategy } })
	return { matched: data.matched, score: data.score ?? data.decision?.score ?? 0, riskLevel: data.decision?.risk_level || rule.riskLevel, action: data.effective_action || data.decision?.action || rule.action, configuredAction: data.configured_action || rule.action, conditions: data.decision?.rule_codes || [], reason: data.decision?.reason || '', excludedReasons: data.excluded_reasons || [], evaluation: data.evaluation || [] }
}

async function listAudit(filters: AuditFilters = {}): Promise<RiskListResponse<RiskAuditRecord>> {
  const action = filters.action === 'rule_update' ? 'update_rule' : filters.action
  const actorQuery = filters.actor?.trim()
	const actorID = actorQuery && /^\d+$/.test(actorQuery) ? Number(actorQuery) : undefined
  const { data } = await mainAdminClient.get<{ items: Array<Record<string, unknown>>; total: number; page?: number; page_size?: number }>('/admin/user-risk-control/audit', {
    params: compactParams({ category: filters.category || 'disposition', action, target_user_id: filters.targetUserId, target: filters.target, actor_id: actorID, actor: actorID ? undefined : actorQuery, result: filters.result, from: filters.from, to: filters.to, sort_by: filters.sortBy, sort_order: filters.sortOrder, page: filters.page || 1, limit: filters.pageSize || 20 }),
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

async function claimReviewCase(id: number): Promise<{ status: RiskCaseStatus; revision?: number }> {
	const { data } = await mainAdminClient.post<{ status: RiskCaseStatus; revision?: number }>(`/admin/user-risk-control/review-cases/${id}/claim`, {})
	return data
}

async function getReviewCase(id: number): Promise<RiskReviewCaseDetail> {
	const { data } = await mainAdminClient.get<RiskReviewCaseDetail>(`/admin/user-risk-control/review-cases/${id}`)
	return data
}

async function submitReviewFeedback(id: number, feedback: RiskFeedback, reason: string): Promise<void> {
	await mainAdminClient.post(`/admin/user-risk-control/review-cases/${id}/feedback`, { feedback, reason: reason.trim() })
}

async function createReviewCase(userId: number, reason: string, status: 'pending' | 'observing' = 'pending', signalFamily = 'manual_review', observation?: { reviewDueAt: string; goal: string }): Promise<{ id: number; status: RiskCaseStatus; review_due_at?: string; observation_goal?: string; revision?: number }> {
	const { data } = await mainAdminClient.post<{ id: number; status: RiskCaseStatus; review_due_at?: string; observation_goal?: string; revision?: number }>('/admin/user-risk-control/review-cases', { user_id: userId, signal_family: signalFamily, status, reason: reason.trim(), review_due_at: observation?.reviewDueAt, observation_goal: observation?.goal.trim() })
	return data
}

async function observeReviewCase(id: number, reason: string, reviewDueAt: string, observationGoal: string, expectedRevision = 0): Promise<{ status: RiskCaseStatus; review_due_at: string; observation_goal: string; revision: number }> {
	const { data } = await mainAdminClient.post<{ status: RiskCaseStatus; review_due_at: string; observation_goal: string; revision: number }>(`/admin/user-risk-control/review-cases/${id}/observe`, { reason: reason.trim(), review_due_at: reviewDueAt, observation_goal: observationGoal.trim(), expected_revision: expectedRevision })
	return data
}

async function resolveReviewCase(id: number, userId: number, resolution: RiskFeedback, reason: string, accountAction: 'none' | 'disable' | 'restore', expectedRevision: number, requestId?: string): Promise<ResolveRiskCaseResult> {
	const idempotencyKey = requestId || `risk-case-${id}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
	const { data } = await mainAdminClient.post<ResolveRiskCaseResult>(`/admin/user-risk/cases/${id}/resolve`, { user_id: userId, resolution, reason: reason.trim(), account_action: accountAction, expected_case_revision: expectedRevision }, { headers: { 'Idempotency-Key': idempotencyKey } })
	return data
}

export const userRiskControlV2API = { listUsers, getWorkOverview, getUserDetail, getUserIdentitySummary, listUserIPIdentities, listUserDeviceIdentities, listAssociatedUsers, getIdentityHealth, listIdentityRules, listIdentityRuleEffects, listIdentityRuleVersions, saveIdentityRuleDraft, simulateIdentityRule, identityRuleLifecycle, disableIdentityRule, dryRunIdentityRebuild, applyIdentityRebuild, previewNetworkLabel, applyNetworkLabel, revokeNetworkLabel, claimReviewCase, getReviewCase, createReviewCase, observeReviewCase, submitReviewFeedback, resolveReviewCase, setUserStatus, retryUserSessionRevocation, batchSetUserStatus, retryBatchSessionRevocations, markUsersProcessed, listRules, updateRule, createRule, testRule, listAudit }
export default userRiskControlV2API
