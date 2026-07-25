import { inject, onBeforeUnmount, reactive, watch } from 'vue'
import { routeLocationKey, routerKey, type LocationQuery, type LocationQueryRaw } from 'vue-router'

import { accountMonitorAPI, type AccountFilters, type AccountPageSize } from '@/api/admin/accountMonitor'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'

export type AccountRange = 'today' | '24h' | '7d' | '30d' | '90d' | 'custom'
export type AccountDetailTab = 'models' | 'users' | 'errors' | 'trends' | 'media'

export interface AccountMonitorFilterState extends AccountFilters {
  range: AccountRange
  page: number
  pageSize: AccountPageSize
  sortBy: string
  sortOrder: 'asc' | 'desc'
  rollup: 'physical' | 'parent'
  selectedAccountID?: number
  detailTab: AccountDetailTab
}

const RANGES: AccountRange[] = ['today', '24h', '7d', '30d', '90d', 'custom']
const TABS: AccountDetailTab[] = ['models', 'users', 'errors', 'trends', 'media']
const PAGE_SIZES = () => getConfiguredTablePageSizeOptions()

function queryText(query: LocationQuery | Record<string, unknown>, key: string): string {
  const value = query[key]
  return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '')
}

function positiveInteger(raw: string): number | undefined {
  const value = Number(raw)
  return Number.isInteger(value) && value > 0 ? value : undefined
}

function boundedRisk(raw: string): number | undefined {
  if (!raw.trim()) return undefined
  const value = Number(raw)
  if (!Number.isFinite(value)) return undefined
  return Math.min(100, Math.max(0, Math.round(value)))
}

export function parseAccountMonitorQuery(query: LocationQuery | Record<string, unknown>): AccountMonitorFilterState {
  const rangeValue = queryText(query, 'range') as AccountRange
  const range = RANGES.includes(rangeValue) ? rangeValue : '7d'
  const pageSizeValue = positiveInteger(queryText(query, 'page_size')) as AccountPageSize | undefined
  const detailTabValue = queryText(query, 'tab') as AccountDetailTab
  const rollupValue = queryText(query, 'rollup')
	const groupValue = queryText(query, 'group_id')
  return {
    range,
    from: queryText(query, 'from') || undefined,
    to: queryText(query, 'to') || undefined,
    page: positiveInteger(queryText(query, 'page')) ?? 1,
		pageSize: pageSizeValue && PAGE_SIZES().includes(pageSizeValue) ? pageSizeValue : normalizeTablePageSize(pageSizeValue),
    sortBy: queryText(query, 'sort_by') || 'attempts',
    sortOrder: queryText(query, 'sort_order') === 'asc' ? 'asc' : 'desc',
    platform: queryText(query, 'platform') || undefined,
    query: queryText(query, 'query') || undefined,
    accountID: positiveInteger(queryText(query, 'account_id')),
    parentAccountID: positiveInteger(queryText(query, 'parent_account_id')),
    accountStatus: queryText(query, 'account_status') || undefined,
    model: queryText(query, 'model') || undefined,
    userID: positiveInteger(queryText(query, 'user_id')),
    apiKeyID: positiveInteger(queryText(query, 'api_key_id')),
    requestType: positiveInteger(queryText(query, 'request_type')),
    result: queryText(query, 'result') || undefined,
    errorCategory: queryText(query, 'error_category') || undefined,
    statusCode: positiveInteger(queryText(query, 'status_code')),
    rollup: rollupValue === 'parent' ? 'parent' : 'physical',
    minRiskScore: boundedRisk(queryText(query, 'min_risk_score')),
    maxRiskScore: boundedRisk(queryText(query, 'max_risk_score')),
		groupID: groupValue === 'ungrouped' ? 'ungrouped' : positiveInteger(groupValue),
    selectedAccountID: positiveInteger(queryText(query, 'account')),
    detailTab: TABS.includes(detailTabValue) ? detailTabValue : 'models',
  }
}

export function serializeAccountMonitorQuery(state: AccountMonitorFilterState): LocationQueryRaw {
  const values: Record<string, string | undefined> = {
    range: state.range,
    from: state.range === 'custom' ? state.from : undefined,
    to: state.range === 'custom' ? state.to : undefined,
    platform: state.platform || undefined,
    query: state.query || undefined,
    account_id: state.accountID ? String(state.accountID) : undefined,
    parent_account_id: state.parentAccountID ? String(state.parentAccountID) : undefined,
    account_status: state.accountStatus || undefined,
    model: state.model || undefined,
    user_id: state.userID ? String(state.userID) : undefined,
    api_key_id: state.apiKeyID ? String(state.apiKeyID) : undefined,
    request_type: state.requestType ? String(state.requestType) : undefined,
    result: state.result || undefined,
    error_category: state.errorCategory || undefined,
    status_code: state.statusCode ? String(state.statusCode) : undefined,
    rollup: state.rollup,
    min_risk_score: state.minRiskScore === undefined ? undefined : String(state.minRiskScore),
    max_risk_score: state.maxRiskScore === undefined ? undefined : String(state.maxRiskScore),
		group_id: state.groupID === undefined ? undefined : String(state.groupID),
    sort_by: state.sortBy,
    sort_order: state.sortOrder,
    page: String(state.page),
    page_size: String(state.pageSize),
    account: state.selectedAccountID ? String(state.selectedAccountID) : undefined,
    tab: state.detailTab,
  }
  return Object.fromEntries(Object.entries(values).filter(([, value]) => value !== undefined))
}

export function resolveTimeRange(state: Pick<AccountMonitorFilterState, 'range' | 'from' | 'to'>, now = new Date()): { from: string; to: string } {
  const to = new Date(now)
  const from = new Date(to)
  if (state.range === 'custom' && state.from && state.to) {
    const customFrom = new Date(state.from)
    const customTo = new Date(state.to)
    if (!Number.isNaN(customFrom.getTime()) && !Number.isNaN(customTo.getTime()) && customFrom < customTo && customTo.getTime() - customFrom.getTime() <= 90 * 86400000) {
      return { from: customFrom.toISOString(), to: customTo.toISOString() }
    }
  }
  if (state.range === 'today') from.setHours(0, 0, 0, 0)
  else {
    const duration = state.range === '24h' ? 1 : state.range === '30d' ? 30 : state.range === '90d' ? 90 : 7
    from.setTime(to.getTime() - duration * 86400000)
  }
  return { from: from.toISOString(), to: to.toISOString() }
}

export function useAccountMonitorFilters(refreshHandler?: () => Promise<void> | void) {
  const route = inject(routeLocationKey, null)
  const router = inject(routerKey, null)
  const state = reactive(parseAccountMonitorQuery(route?.query || {}))
  let writingQuery = false

  async function syncQuery() {
    if (!route || !router) return
    writingQuery = true
    try {
      await router.replace({ path: route.path, query: serializeAccountMonitorQuery(state) })
    } finally {
      writingQuery = false
    }
  }

  async function refresh() {
    await refreshHandler?.()
  }

  async function setFilters(next: Partial<AccountMonitorFilterState>) {
    Object.assign(state, next, { page: 1 })
    await syncQuery()
    await refresh()
  }

  async function resetFilters() {
    const selection = state.selectedAccountID
    const tab = state.detailTab
    Object.assign(state, parseAccountMonitorQuery({}), { selectedAccountID: selection, detailTab: tab })
    await syncQuery()
    await refresh()
  }

  async function setPage(page: number) {
    state.page = Math.max(1, Math.floor(page))
    await syncQuery()
    await refresh()
  }

  async function setPageSize(pageSize: AccountPageSize) {
		state.pageSize = PAGE_SIZES().includes(pageSize) ? pageSize : normalizeTablePageSize(pageSize)
    state.page = 1
    await syncQuery()
    await refresh()
  }

  async function selectAccount(accountID?: number) {
    state.selectedAccountID = accountID
    await syncQuery()
  }

  async function setDetailTab(tab: AccountDetailTab) {
    state.detailTab = TABS.includes(tab) ? tab : 'models'
    await syncQuery()
  }

  function dispose() {
    accountMonitorAPI.dispose()
  }

  watch(() => route?.fullPath, () => {
    if (!route || writingQuery) return
    Object.assign(state, parseAccountMonitorQuery(route.query))
    void refresh()
  })

  onBeforeUnmount(dispose)

  return { state, refresh, setFilters, resetFilters, setPage, setPageSize, selectAccount, setDetailTab, dispose }
}
