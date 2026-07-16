import { inject, reactive, watch } from 'vue'
import { routeLocationKey, routerKey, type LocationQuery, type LocationQueryRaw } from 'vue-router'
import type { GroupCallFilter, GroupPageSize, GroupRange } from '@/api/admin/accountMonitor'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'

export interface GroupMonitorFilterState {
  range: GroupRange
  query: string
  platform: string
  groupStatus: 'active' | 'inactive' | 'all'
  callStatus?: GroupCallFilter
  page: number
  pageSize: GroupPageSize
  selectedGroupID?: number
}

function text(query: LocationQuery | Record<string, unknown>, key: string) { const value = query[key]; return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '') }
function positive(raw: string) { const value = Number(raw); return Number.isInteger(value) && value > 0 ? value : undefined }
const ranges: GroupRange[] = ['6h', '24h', '7d', '30d']
const sizes = () => getConfiguredTablePageSizeOptions()
const statuses: GroupCallFilter[] = ['has_calls', 'normal', 'partial_failure', 'all_failed', 'recently_idle', 'no_data']

export function parseGroupMonitorQuery(query: LocationQuery | Record<string, unknown>): GroupMonitorFilterState {
  const range = text(query, 'range') as GroupRange
  const size = positive(text(query, 'page_size')) as GroupPageSize | undefined
  const groupStatus = text(query, 'group_status')
  const callStatus = text(query, 'call_status') as GroupCallFilter
	return { range: ranges.includes(range) ? range : '6h', query: text(query, 'query'), platform: text(query, 'platform'), groupStatus: groupStatus === 'inactive' || groupStatus === 'all' ? groupStatus : 'active', callStatus: statuses.includes(callStatus) ? callStatus : undefined, page: positive(text(query, 'page')) || 1, pageSize: size && sizes().includes(size) ? size : normalizeTablePageSize(size), selectedGroupID: positive(text(query, 'group')) }
}

export function serializeGroupMonitorQuery(state: GroupMonitorFilterState): LocationQueryRaw {
  const values: Record<string, string | undefined> = { range: state.range, query: state.query || undefined, platform: state.platform || undefined, group_status: state.groupStatus, call_status: state.callStatus, page: String(state.page), page_size: String(state.pageSize), group: state.selectedGroupID ? String(state.selectedGroupID) : undefined }
  return Object.fromEntries(Object.entries(values).filter(([, value]) => value !== undefined))
}

export function useGroupMonitorFilters(refresh: () => Promise<void> | void) {
  const route = inject(routeLocationKey, null)
  const router = inject(routerKey, null)
  const state = reactive(parseGroupMonitorQuery(route?.query || {}))
  let writing = false
  async function sync() { if (!route || !router) return; writing = true; try { await router.replace({ path: route.path, query: serializeGroupMonitorQuery(state) }) } finally { writing = false } }
  async function setFilters(next: Partial<GroupMonitorFilterState>) { Object.assign(state, next, { page: 1 }); await sync(); await refresh() }
  async function resetFilters() { const selectedGroupID = state.selectedGroupID; Object.assign(state, parseGroupMonitorQuery({}), { selectedGroupID }); await sync(); await refresh() }
  async function setPage(page: number) { state.page = Math.max(1, Math.floor(page)); await sync(); await refresh() }
	async function setPageSize(pageSize: GroupPageSize) { state.pageSize = sizes().includes(pageSize) ? pageSize : normalizeTablePageSize(pageSize); state.page = 1; await sync(); await refresh() }
  async function selectGroup(selectedGroupID?: number) { state.selectedGroupID = selectedGroupID; await sync() }
  watch(() => route?.fullPath, () => { if (!route || writing) return; Object.assign(state, parseGroupMonitorQuery(route.query)); void refresh() })
  return { state, setFilters, resetFilters, setPage, setPageSize, selectGroup }
}
