import apiClient from '@/api/client'

const BASE_PATH = '/admin/extensions-self/account-monitor'
const GROUP_RANGES = ['6h', '24h', '7d', '30d'] as const

export type AccountPageSize = number
export type GroupPageSize = number
export type GroupRange = typeof GROUP_RANGES[number]
export type SortOrder = 'asc' | 'desc'
export type HealthLevel = 'normal' | 'attention' | 'abnormal' | 'critical'
export type GroupCallStatus = 'normal' | 'partial_failure' | 'all_failed' | 'recently_idle' | 'no_data'
export type GroupCallFilter = 'has_calls' | GroupCallStatus

export interface TimeRange {
  from?: string
  to?: string
}

export interface AccountMonitorHealth {
  risk_score: number
  risk_score_available: boolean
  level: HealthLevel
  reasons: string[]
}

export interface AccountMonitorOverview {
  attempts: number
  successes: number
  failures: number
  requests: number
  request_successes: number
  active_accounts: number
  abnormal_accounts: number
  average_risk_score: number
  high_risk_accounts: number
  users: number
  tokens: number
  user_cost: number
  account_cost: number
  average_duration_ms: number
  p95_duration_ms: number
  last_sync_at: string
  sync_lag_seconds: number
}

export interface AccountMonitorAccount {
  account_id: number
  parent_account_id?: number
  account_name: string
  account_identity?: string
  platform: string
  status: string
  attempts: number
  successes: number
  failures: number
  tokens: number
  user_cost: number
  account_cost: number
  average_duration_ms: number
  p95_duration_ms: number
  last_success_at?: string
  last_failure_at?: string
  model_count: number
  user_count: number
  image_count: number
  video_count: number
  video_duration_seconds: number
  groups: AccountGroupSummary[]
  health: AccountMonitorHealth
}

export interface AccountGroupSummary {
  group_id: number
  name: string
  platform: string
  status: string
  rate_multiplier: number
}

export interface PageResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface AccountPageResponse extends PageResponse<AccountMonitorAccount> {
  groups?: AccountGroupSummary[]
}

export interface AccountFilters extends TimeRange {
  page?: number
  pageSize?: AccountPageSize | number
  sortBy?: string
  sortOrder?: SortOrder
  platform?: string
  query?: string
  accountID?: number
  parentAccountID?: number
  accountStatus?: string
  model?: string
  userID?: number
  apiKeyID?: number
  requestType?: number
  result?: string
  errorCategory?: string
  statusCode?: number
  rollup?: 'physical' | 'parent'
  minRiskScore?: number
  maxRiskScore?: number
  groupID?: number | 'ungrouped'
}

export interface AccountModelRow {
  actual_model: string
  model_attribution: string
  attempts: number
  successes: number
  failures: number
  tokens: number
  user_cost: number
  account_cost: number
  average_duration_ms: number
  p95_duration_ms: number
}

export interface AccountUserRow {
  user_id: number
  api_key_id: number
  email?: string
  api_key_name?: string
  attempts: number
  successes: number
  failures: number
	success_rate: number
  tokens: number
  user_cost: number
	last_attempted_at: string
}

export interface AccountErrorRow {
  error_category: string
  upstream_status_code: number
  provider_error_code: string
  failures: number
  recovered_failures: number
  last_failure_at: string
}

export interface AccountTrendRow {
  bucket: string
  attempts: number
  successes: number
  failures: number
  tokens: number
  user_cost: number
  account_cost: number
  average_duration_ms: number
  p95_duration_ms: number
}

export interface AccountAttemptRow {
  event_key: string
  request_key: string
  attempted_at: string
  account_id: number
  platform: string
  actual_model: string
  model_attribution: string
  user_id: number
  api_key_id: number
  request_type: number
  result: string
  recovered: boolean
  error_category: string
  status_code: number
  upstream_status_code: number
  provider_error_code: string
  tokens: number
  user_cost: number
  account_cost: number
  duration_ms: number
  image_count: number
  image_size: string
  video_count: number
  video_resolution: string
  video_duration_seconds: number
  identity_quality: string
}

export interface CursorQuality {
  cursor_time: string | null
  cursor_id: number
  last_success_at: string | null
  last_error?: string
}

export interface DataQualitySnapshot {
  data_as_of: string | null
  collection_lag_seconds: number | null
  stale_data_warning?: string
  usage_cursor: CursorQuality
  error_cursor: CursorQuality
  recent_source_error?: string
  available_from: string | null
  available_to: string | null
  missing_group_requests: number
  exact_model_requests: number
  estimated_model_requests: number
}

export interface AccountDataQuality extends DataQualitySnapshot {
  source_connected: boolean
  error_attribution_rate?: number | null
  unattributed_errors: number
  recovered_failures: number
  exact_models: number
  estimated_models: number
  fallback_identities: number
  data_source: string
}

export interface AccountThreshold {
  scope: 'global' | 'platform' | 'parent' | 'account'
  scope_id: number
  success_rate: number
}

export interface RebuildJob {
  id: number
  from: string
  to: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  processed_rows: number
  error?: string
  requested_by?: number
  created_at?: string
  started_at?: string | null
  completed_at?: string | null
}

export interface GroupMonitorBucket {
  bucket_at: string
  total: number
  successes: number
  failures: number
  status: Exclude<GroupCallStatus, 'recently_idle'>
}

export interface GroupMonitorCard {
  group_id: number
  name: string
  platform: string
  group_status: string
  call_status: GroupCallStatus
  total_requests: number
  successes: number
  failures: number
  success_rate: number | null
  timeline: GroupMonitorBucket[]
}

export type GroupMonitorQuality = DataQualitySnapshot

export interface GroupMonitorGroupsResponse extends PageResponse<GroupMonitorCard> {
	bucket_seconds?: number
  platforms: string[]
  data_as_of: string | null
  data_quality: GroupMonitorQuality
}

export interface GroupMonitorModel {
  actual_model: string
  total_requests: number
  successes: number
  failures: number
  exact_model_requests: number
  estimated_model_requests: number
  success_rate: number | null
  timeline: GroupMonitorBucket[]
}

export interface GroupMonitorDetailResponse {
  group: GroupMonitorCard
  models: GroupMonitorModel[]
  data_as_of: string
	bucket_seconds?: number
}

export interface GroupFilters {
  page?: number
  pageSize?: GroupPageSize | number
  range?: GroupRange
  query?: string
  platform?: string
  groupStatus?: 'active' | 'inactive' | 'all'
  callStatus?: GroupCallFilter | 'all'
}

type QueryValue = string | number | boolean | undefined | null

function compact(values: Record<string, QueryValue>): Record<string, string | number | boolean> {
  return Object.fromEntries(Object.entries(values).filter(([, value]) => value !== undefined && value !== null && value !== '')) as Record<string, string | number | boolean>
}

function validatePositiveID(id: number, label: string) {
  if (!Number.isInteger(id) || id <= 0) throw new Error(`${label} must be a positive integer`)
}

function validatePage(page: number | undefined) {
  if (page !== undefined && (!Number.isInteger(page) || page <= 0)) throw new Error('page must be a positive integer')
}

function validatePageSize(pageSize: number, label: string) {
  if (!Number.isInteger(pageSize) || pageSize < 5 || pageSize > 1000) throw new Error(`${label} page size must be an integer from 5 to 1000`)
}

function accountParams(filters: AccountFilters = {}) {
  validatePage(filters.page)
  const pageSize = filters.pageSize ?? 20
  validatePageSize(pageSize, 'account')
  return compact({
    from: filters.from,
    to: filters.to,
    page: filters.page ?? 1,
    page_size: pageSize,
    sort_by: filters.sortBy,
    sort_order: filters.sortOrder,
    platform: filters.platform,
    query: filters.query,
    account_id: filters.accountID,
    parent_account_id: filters.parentAccountID,
    account_status: filters.accountStatus,
    model: filters.model,
    user_id: filters.userID,
    api_key_id: filters.apiKeyID,
    request_type: filters.requestType,
    result: filters.result,
    error_category: filters.errorCategory,
    status_code: filters.statusCode,
    rollup: filters.rollup,
    min_risk_score: filters.minRiskScore,
    max_risk_score: filters.maxRiskScore,
    group_id: filters.groupID,
  })
}

function rangeParams(range: TimeRange = {}) {
  return compact({ from: range.from, to: range.to })
}

class AccountMonitorAPI {
  private controllers = new Map<string, AbortController>()

  private nextSignal(family: string): AbortSignal {
    this.controllers.get(family)?.abort()
    const controller = new AbortController()
    this.controllers.set(family, controller)
    return controller.signal
  }

  dispose() {
    this.controllers.forEach((controller) => controller.abort())
    this.controllers.clear()
  }

  private cancelMatching(prefix: string) {
    for (const [family, controller] of this.controllers) {
      if (family === prefix || family.startsWith(`${prefix}-`)) {
        controller.abort()
        this.controllers.delete(family)
      }
    }
  }

  cancelAccountDetails(accountID: number) {
    this.cancelMatching(`detail-${accountID}`)
    this.cancelMatching(`account-${accountID}`)
  }

  cancelGroupDetail(groupID: number) {
    this.cancelMatching(`group-${groupID}`)
  }

  async getOverview(range: TimeRange = {}) {
    const { data } = await apiClient.get<AccountMonitorOverview>(`${BASE_PATH}/overview`, { params: rangeParams(range), signal: this.nextSignal('overview') })
    return data
  }

  async getDataQuality(range: TimeRange = {}) {
    const { data } = await apiClient.get<AccountDataQuality>(`${BASE_PATH}/data-quality`, { params: rangeParams(range), signal: this.nextSignal('quality') })
    return data
  }

  async listAccounts(filters: AccountFilters = {}) {
    const { data } = await apiClient.get<AccountPageResponse>(`${BASE_PATH}/accounts`, { params: accountParams(filters), signal: this.nextSignal('accounts') })
    return data
  }

  async getAccount(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<AccountMonitorAccount>(`${BASE_PATH}/accounts/${accountID}`, { params: accountParams({ ...filters, page: 1, pageSize: 20 }), signal: this.nextSignal(`account-${accountID}`) })
    return data
  }

  async getModels(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<PageResponse<AccountModelRow>>(`${BASE_PATH}/accounts/${accountID}/models`, { params: accountParams(filters), signal: this.nextSignal(`detail-${accountID}`) })
    return data
  }

  async getUsers(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<PageResponse<AccountUserRow>>(`${BASE_PATH}/accounts/${accountID}/users`, { params: accountParams(filters), signal: this.nextSignal(`detail-${accountID}`) })
    return data
  }

  async getErrors(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<PageResponse<AccountErrorRow>>(`${BASE_PATH}/accounts/${accountID}/errors`, { params: accountParams(filters), signal: this.nextSignal(`detail-${accountID}`) })
    return data
  }

  async getTrends(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<AccountTrendRow[]>(`${BASE_PATH}/accounts/${accountID}/trends`, { params: accountParams(filters), signal: this.nextSignal(`detail-${accountID}`) })
    return data
  }

  async getAttempts(accountID: number, filters: AccountFilters = {}) {
    validatePositiveID(accountID, 'account ID')
    const { data } = await apiClient.get<PageResponse<AccountAttemptRow>>(`${BASE_PATH}/attempts`, { params: accountParams({ ...filters, accountID }), signal: this.nextSignal(`detail-${accountID}`) })
    return data
  }

  async getThreshold() {
    const { data } = await apiClient.get<AccountThreshold>(`${BASE_PATH}/thresholds`, { signal: this.nextSignal('threshold') })
    return data
  }

  async updateThreshold(input: AccountThreshold) {
    const { data } = await apiClient.put<AccountThreshold>(`${BASE_PATH}/thresholds`, input, { signal: this.nextSignal('threshold') })
    return data
  }

  async startRebuild(input: { from: string; to: string }) {
    const { data } = await apiClient.post<RebuildJob>(`${BASE_PATH}/rebuild-jobs`, input, { signal: this.nextSignal('rebuild-start') })
    return data
  }

  async getRebuildJob(jobID: number) {
    validatePositiveID(jobID, 'job ID')
    const { data } = await apiClient.get<RebuildJob>(`${BASE_PATH}/rebuild-jobs/${jobID}`, { signal: this.nextSignal(`rebuild-${jobID}`) })
    return data
  }

  async listGroups(filters: GroupFilters = {}) {
    validatePage(filters.page)
    const pageSize = filters.pageSize ?? 12
    validatePageSize(pageSize, 'group')
    const range = filters.range ?? '6h'
    if (!GROUP_RANGES.includes(range)) throw new Error('group range must be one of 6h, 24h, 7d, or 30d')
    const params = compact({ page: filters.page ?? 1, page_size: pageSize, range, query: filters.query, platform: filters.platform, group_status: filters.groupStatus, call_status: filters.callStatus })
    const { data } = await apiClient.get<GroupMonitorGroupsResponse>(`${BASE_PATH}/group-monitor/groups`, { params, signal: this.nextSignal('groups') })
    return data
  }

  async getGroup(groupID: number, filters: Pick<GroupFilters, 'range'> = {}) {
    validatePositiveID(groupID, 'group ID')
    const range = filters.range ?? '6h'
    if (!GROUP_RANGES.includes(range)) throw new Error('group range must be one of 6h, 24h, 7d, or 30d')
    const { data } = await apiClient.get<GroupMonitorDetailResponse>(`${BASE_PATH}/group-monitor/groups/${groupID}`, { params: { range }, signal: this.nextSignal(`group-${groupID}`) })
    return data
  }
}

export const accountMonitorAPI = new AccountMonitorAPI()
