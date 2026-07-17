<template>
  <TablePageLayout :title="t('admin.userRiskControl.usersTitle')" :description="t('admin.userRiskControl.usersDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
          <div class="flex items-center justify-between gap-3">
            <div v-if="error" class="min-w-0 flex-1 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
            <div v-else class="flex-1" />
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadUsers">
              <Icon name="refresh" size="sm" />
              {{ t('admin.userRiskControl.refresh') }}
            </button>
          </div>
          <div v-if="batchResults.length" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/50" data-testid="batch-result-summary">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <strong class="text-sm text-gray-900 dark:text-white">{{ formatAuditResult(batchSummary) }}：{{ batchSuccessCount }}/{{ batchResults.length }}</strong>
              <button type="button" class="btn btn-ghost btn-icon" :aria-label="t('common.close')" @click="batchResults = []"><Icon name="x" size="sm" /></button>
            </div>
            <ul class="mt-3 space-y-2 text-sm">
              <li v-for="result in batchResults" :key="result.id" class="flex flex-wrap gap-x-2 gap-y-1">
                <span class="font-medium text-gray-900 dark:text-white">#{{ result.id }}</span>
                <span :class="result.status === 'success' ? 'text-emerald-600' : 'text-red-600'">{{ formatAuditResult(result.status) }}</span>
                <span v-if="result.reason" class="text-gray-600 dark:text-gray-300">{{ result.reason }}</span>
              </li>
            </ul>
          </div>
        </div>
      </template>

      <template #filters>
          <section class="flex flex-wrap items-center gap-3" data-testid="risk-user-filters">
          <SearchInput
            :model-value="draft.search || ''"
            :placeholder="t('admin.userRiskControl.searchPlaceholder')"
            class="w-full sm:w-72"
            data-testid="risk-user-search"
            @update:model-value="updateSearch"
            @search="runFiltersNow"
          />
          <Select :model-value="draft.status || ''" class="w-full sm:w-40" data-testid="account-status-filter" :options="accountStatusFilterOptions" @update:model-value="setFilter('status', $event)" />
          <Select :model-value="draft.riskType || ''" class="w-full sm:w-44" data-testid="risk-type-filter" :options="riskTypeFilterOptions" @update:model-value="setFilter('riskType', $event)" />
          <Select :model-value="draft.riskLevel || ''" class="w-full sm:w-40" data-testid="risk-level-filter" :options="riskLevelFilterOptions" @update:model-value="setFilter('riskLevel', $event)" />
          <Select :model-value="draft.processingStatus || ''" class="w-full sm:w-40" data-testid="processing-status-filter" :options="processingStatusFilterOptions" @update:model-value="setFilter('processingStatus', $event)" />
          <div class="flex w-full items-center gap-2 sm:w-auto" aria-label="风险分范围">
            <input v-model.number="draft.minScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-24 sm:flex-none" placeholder="最低分" data-testid="min-score-filter" @input="scheduleFilters" />
            <span class="text-sm text-gray-400">至</span>
            <input v-model.number="draft.maxScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-24 sm:flex-none" placeholder="最高分" data-testid="max-score-filter" @input="scheduleFilters" />
          </div>
          <label class="flex h-10 items-center gap-2 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">
            <Toggle :model-value="Boolean(draft.riskOnly)" data-testid="risk-only-filter" @update:model-value="setRiskOnly" />
            只看有风险记录
          </label>
          <Select :model-value="mobileSortValue" class="w-full md:hidden" data-testid="mobile-user-sort" :options="mobileSortOptions" @update:model-value="setMobileSort" />
          <label class="flex h-10 w-full items-center gap-2 text-sm text-gray-600 dark:text-gray-300 md:hidden">
            <input v-model="allSelected" type="checkbox" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" />
            选择当前页
          </label>
          <button type="button" class="btn btn-ghost btn-icon" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
          </section>
      </template>

      <template #table>
        <div class="flex min-h-0 w-full flex-1 flex-col gap-3" data-testid="risk-users-table">
          <section v-if="selectedIds.size" class="flex flex-col gap-3 rounded-lg border border-primary-200 bg-primary-50 p-3 dark:border-primary-700/40 dark:bg-primary-900/20 sm:flex-row sm:items-center sm:justify-between" data-testid="batch-action-bar">
            <span class="text-sm font-medium text-primary-800 dark:text-primary-200" data-testid="selected-count">已选择 {{ selectedIds.size }} 个账号</span>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-danger btn-sm" data-testid="batch-ban" @click="openBatchAction('disabled')">{{ formatRiskAction('ban') }}</button>
              <button type="button" class="btn btn-primary btn-sm" data-testid="batch-unban" @click="openBatchAction('active')">{{ formatRiskAction('unban') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" data-testid="batch-mark-processed" @click="openBatchAction('processed')">标记已处理</button>
              <button type="button" class="btn btn-ghost btn-sm" data-testid="clear-selection" @click="clearSelection">取消选择</button>
            </div>
          </section>
          <DataTable :key="`risk-users-${tableSortKey}-${sortOrder}`" :columns="columns" :data="users" :loading="loading" row-key="id" :clickable-rows="true" :server-side-sort="true" :default-sort-key="tableSortKey" :default-sort-order="sortOrder" @row-click="selectedUser = $event" @sort="handleTableSort">
            <template #header-select><input v-model="allSelected" type="checkbox" data-testid="select-current-page" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" @click.stop /></template>
            <template #cell-select="{ row: user }"><input :checked="selectedIds.has(user.id)" type="checkbox" :data-testid="`user-select-${user.id}`" class="rounded border-gray-300 text-primary-600" :aria-label="`选择账号 ${user.email || user.username || user.id}`" @click.stop @change.stop="toggleSelection(user.id)" /></template>
            <template #cell-account="{ row: user }"><div class="min-w-0 max-w-[50vw] text-left sm:max-w-none" :data-testid="`user-row-${user.id}`"><p class="truncate font-medium text-gray-900 dark:text-white" :title="user.email || user.username || `用户 #${user.id}`" :data-testid="`account-primary-${user.id}`">{{ user.email || user.username || `用户 #${user.id}` }}</p><p v-if="user.email && user.username" class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400" :title="user.username" :data-testid="`account-secondary-${user.id}`">{{ user.username }}</p></div></template>
            <template #cell-id="{ row: user }">#{{ user.id }}</template>
            <template #cell-status="{ row: user }"><span class="rounded-md px-2 py-0.5 text-xs font-medium" :class="statusClass(user.status)">{{ formatAccountStatus(user.status) }}</span></template>
            <template #cell-riskType="{ row: user }">{{ formatRiskType(user.risk_type) }}</template>
            <template #cell-riskScore="{ row: user }"><RiskScoreBadge :score="user.risk_score" :available="user.risk_score !== null && user.risk_score !== undefined && Boolean(user.risk_level)" :explicit-level="user.risk_level" /></template>
            <template #cell-riskLevel="{ row: user }">{{ formatRiskLevel(user.risk_level) }}</template>
            <template #cell-eventCount="{ row: user }"><span>{{ user.event_count ?? 0 }}</span><span class="block text-xs text-gray-400 dark:text-gray-500">IP {{ user.ip_count ?? 0 }} / 设备 {{ user.device_count ?? 0 }}</span></template>
            <template #cell-lastEvent="{ row: user }">{{ formatDate(user.last_event_at) }}</template>
            <template #cell-reason="{ row: user }"><p class="max-w-xl whitespace-normal break-words text-left leading-5">{{ displayReason(user) }}</p></template>
            <template #cell-processing="{ row: user }">{{ formatProcessingStatus(user.processing_status || (user.pending ? 'pending' : user.last_action ? 'observed' : '')) }}</template>
            <template #empty><EmptyState :title="t('admin.userRiskControl.empty')" /></template>
          </DataTable>
        </div>
      </template>

      <template #pagination><Pagination v-if="total" :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:pageSize="changePageSize" /></template>
    </TablePageLayout>

    <UserRiskControlUserDrawer v-if="selectedUser" :user="selectedUser" @close="selectedUser = null" @updated="handleUpdated" />
    <BaseDialog :show="Boolean(batchAction)" :title="batchDialogTitle" width="narrow" :close-on-click-outside="true" :z-index="80" @close="closeBatchAction">
      <p class="text-sm text-gray-500 dark:text-gray-400">将处理 {{ selectedIds.size }} 个账号；每个账号都会单独记录结果。</p>
      <TextArea v-model="batchReason" class="mt-4" data-testid="batch-reason" label="操作原因" required placeholder="填写操作原因（必填）" :error="batchValidationError" @update:model-value="batchValidationError = ''" />
      <template #footer><button type="button" class="btn btn-secondary" @click="closeBatchAction">{{ t('common.cancel') }}</button><button type="button" class="btn" :class="batchAction === 'disabled' ? 'btn-danger' : 'btn-primary'" data-testid="batch-confirm" :disabled="batchSaving" @click="confirmBatchAction">{{ batchSaving ? t('common.saving') : t('common.confirm') }}</button></template>
    </BaseDialog>
</template>

<script setup lang="ts">
import { computed, inject, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { routeLocationKey, routerKey, type LocationQueryRaw } from 'vue-router'
import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'
import UserRiskControlUserDrawer from '@/components/admin/UserRiskControlUserDrawer.vue'
import Icon from '@/components/icons/Icon.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import UserRiskControlTabs from '@/views/admin/extensions/UserRiskControlTabs.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { Column } from '@/components/common/types'
import { useDebouncedAction } from '@/composables/useDebouncedAction'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { userRiskControlV2API, type AccountStatus, type RiskSortBy, type RiskUserRow, type UserRiskFilters } from '@/api/admin/userRiskControlV2'
import { accountStatusOptions, formatAccountStatus, formatAuditResult, formatProcessingStatus, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType, processingStatusOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const users = ref<RiskUserRow[]>([])
const selectedUser = ref<RiskUserRow | null>(null)
const selectedIds = ref(new Set<number>())
const loading = ref(true)
const batchSaving = ref(false)
const error = ref('')
const page = ref(1)
const pageSize = ref(getPersistedPageSize(20))
const total = ref(0)
const sortBy = ref<RiskSortBy | undefined>(undefined)
const sortOrder = ref<'asc' | 'desc'>('desc')
const batchAction = ref<'disabled' | 'active' | 'processed' | null>(null)
const batchReason = ref('')
const batchValidationError = ref('')
const batchResults = ref<Array<{ id: number; status: 'success' | 'failed'; reason?: string }>>([])
const columns: Column[] = [
  { key: 'select', label: '选择', class: 'w-12 text-center' },
  { key: 'account', label: t('admin.userRiskControl.table.account') },
  { key: 'id', label: t('admin.userRiskControl.table.id') },
  { key: 'status', label: t('admin.userRiskControl.table.status') },
  { key: 'riskType', label: t('admin.userRiskControl.table.riskType') },
  { key: 'riskScore', label: '风险分', sortable: true },
  { key: 'riskLevel', label: t('admin.userRiskControl.level'), sortable: true },
  { key: 'eventCount', label: '事件次数', sortable: true },
  { key: 'lastEvent', label: '最近事件', sortable: true },
  { key: 'reason', label: t('admin.userRiskControl.table.reason') },
  { key: 'processing', label: '处理状态' },
]
const draft = reactive<UserRiskFilters>({ search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false })
const activeFilters = reactive<UserRiskFilters>({ ...draft })
let loadRequestID = 0
let writingQuery = false

const accountStatusFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allStatuses') }, ...accountStatusOptions])
const riskTypeFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allRiskTypes') }, ...riskTypeOptions])
const riskLevelFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allRiskLevels') }, ...riskLevelOptions])
const processingStatusFilterOptions = computed(() => [{ value: '', label: '全部处理状态' }, ...processingStatusOptions])
const mobileSortOptions = [
  { value: '', label: '默认排序' },
  { value: 'risk_score:desc', label: '风险分：高到低' },
  { value: 'risk_score:asc', label: '风险分：低到高' },
  { value: 'risk_level:desc', label: '风险等级：高到低' },
  { value: 'risk_level:asc', label: '风险等级：低到高' },
  { value: 'event_count:desc', label: '事件次数：多到少' },
  { value: 'event_count:asc', label: '事件次数：少到多' },
  { value: 'last_event_at:desc', label: '最近事件：新到旧' },
  { value: 'last_event_at:asc', label: '最近事件：旧到新' },
]
const mobileSortValue = computed(() => sortBy.value ? `${sortBy.value}:${sortOrder.value}` : '')
const tableSortKey = computed(() => {
  switch (sortBy.value) {
    case 'risk_score': return 'riskScore'
    case 'risk_level': return 'riskLevel'
    case 'event_count': return 'eventCount'
    case 'last_event_at': return 'lastEvent'
    case 'created_at': return 'createdAt'
    default: return ''
  }
})
const hasFilters = computed(() => Boolean(draft.search || draft.status || draft.riskType || draft.riskLevel || draft.processingStatus || normalizeScore(draft.minScore) !== undefined || normalizeScore(draft.maxScore) !== undefined || draft.riskOnly))
const allSelected = computed({
  get: () => users.value.length > 0 && users.value.every((user) => selectedIds.value.has(user.id)),
  set: (value: boolean) => {
    const next = new Set(selectedIds.value)
    users.value.forEach((user) => value ? next.add(user.id) : next.delete(user.id))
    selectedIds.value = next
  },
})
const batchSuccessCount = computed(() => batchResults.value.filter((result) => result.status === 'success').length)
const batchSummary = computed(() => batchSuccessCount.value === batchResults.value.length ? 'success' : batchSuccessCount.value === 0 ? 'failed' : 'partial')
const batchDialogTitle = computed(() => batchAction.value === 'disabled' ? '确认批量封禁' : batchAction.value === 'active' ? '确认批量解封' : '确认批量标记已处理')

function errorMessage(err: unknown) {
  return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed')
}

function queryText(key: string): string {
  const value = route?.query[key]
  return Array.isArray(value) ? String(value[0] ?? '') : String(value ?? '')
}

function positiveInteger(value: string, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function restoreRouteState() {
  if (!route) return
  const nextSort = queryText('sort_by')
  const allowedSorts: RiskSortBy[] = ['risk_score', 'risk_level', 'event_count', 'last_event_at', 'created_at']
  Object.assign(draft, {
    search: queryText('search'),
    status: queryText('status'),
    riskType: queryText('risk_type'),
    riskLevel: queryText('risk_level'),
    processingStatus: queryText('processing_status'),
    riskOnly: queryText('risk_only') === 'true',
    minScore: normalizeScore(queryText('min_score')),
    maxScore: normalizeScore(queryText('max_score')),
  })
  Object.assign(activeFilters, draft)
  page.value = positiveInteger(queryText('page'), 1)
  pageSize.value = positiveInteger(queryText('page_size'), getPersistedPageSize(20))
  sortBy.value = allowedSorts.includes(nextSort as RiskSortBy) ? nextSort as RiskSortBy : undefined
  sortOrder.value = queryText('sort_order') === 'asc' ? 'asc' : 'desc'
}

async function syncRouteState() {
  if (!route || !router) return
  const query: LocationQueryRaw = { ...route.query }
  const values: Record<string, string | undefined> = {
    search: String(activeFilters.search || '') || undefined,
    status: String(activeFilters.status || '') || undefined,
    risk_type: String(activeFilters.riskType || '') || undefined,
    risk_level: String(activeFilters.riskLevel || '') || undefined,
    processing_status: String(activeFilters.processingStatus || '') || undefined,
    risk_only: activeFilters.riskOnly ? 'true' : undefined,
    min_score: normalizeScore(activeFilters.minScore)?.toString(),
    max_score: normalizeScore(activeFilters.maxScore)?.toString(),
    sort_by: sortBy.value,
    sort_order: sortBy.value ? sortOrder.value : undefined,
    page: page.value > 1 ? String(page.value) : undefined,
    page_size: String(pageSize.value),
  }
  Object.entries(values).forEach(([key, value]) => value === undefined ? delete query[key] : query[key] = value)
  writingQuery = true
  try {
    await router.replace({ path: route.path, query })
  } finally {
    writingQuery = false
  }
}

async function loadUsers() {
  const requestID = ++loadRequestID
  loading.value = true
  error.value = ''
  try {
    const response = await userRiskControlV2API.listUsers({ ...activeFilters, page: page.value, pageSize: pageSize.value, sortBy: sortBy.value, sortOrder: sortOrder.value })
    if (requestID !== loadRequestID) return
    users.value = response.items
    total.value = response.total
  } catch (err) {
    if (requestID === loadRequestID) error.value = errorMessage(err)
  } finally {
    if (requestID === loadRequestID) loading.value = false
  }
}

async function applyFilters() {
  Object.assign(activeFilters, draft, { minScore: normalizeScore(draft.minScore), maxScore: normalizeScore(draft.maxScore) })
  page.value = 1
  clearSelection()
  await syncRouteState()
  await loadUsers()
}

const { schedule: scheduleFilters, runNow: runFiltersNow } = useDebouncedAction(applyFilters, 300)

function updateSearch(value: string) {
  draft.search = value
  scheduleFilters()
}

function normalizeScore(value: unknown): number | undefined {
  if (value === '' || value === null || value === undefined) return undefined
  const score = Number(value)
  return Number.isFinite(score) ? score : undefined
}

function setFilter(key: 'status' | 'riskType' | 'riskLevel' | 'processingStatus', value: string | number | boolean | null) {
  Object.assign(draft, { [key]: String(value ?? '') })
  void runFiltersNow()
}

function setRiskOnly(value: boolean) {
  draft.riskOnly = value
  void runFiltersNow()
}

async function setMobileSort(value: string | number | boolean | null) {
  if (!value || typeof value === 'boolean') { sortBy.value = undefined; sortOrder.value = 'desc' }
  else {
    const [field, order] = String(value).split(':')
    sortBy.value = field as RiskSortBy
    sortOrder.value = order === 'asc' ? 'asc' : 'desc'
  }
  activeFilters.sortBy = sortBy.value
  activeFilters.sortOrder = sortOrder.value
  page.value = 1
  await syncRouteState()
  await loadUsers()
}

async function resetFilters() {
  Object.assign(draft, { search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false, minScore: undefined, maxScore: undefined })
  await runFiltersNow()
}

async function changePage(next: number) {
  page.value = next
  clearSelection()
  await syncRouteState()
  await loadUsers()
}

async function changePageSize(next: number) {
  pageSize.value = next
  page.value = 1
  clearSelection()
  await syncRouteState()
  await loadUsers()
}

async function handleTableSort(key: string, order: 'asc' | 'desc') {
  const next = ({ riskScore: 'risk_score', riskLevel: 'risk_level', eventCount: 'event_count', lastEvent: 'last_event_at', createdAt: 'created_at' } as const)[key as 'riskScore' | 'riskLevel' | 'eventCount' | 'lastEvent' | 'createdAt']
  if (!next) return
  sortBy.value = next
  sortOrder.value = order
  activeFilters.sortBy = sortBy.value
  activeFilters.sortOrder = sortOrder.value
  page.value = 1
  await syncRouteState()
  await loadUsers()
}
function toggleSelection(id: number) {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}
function clearSelection() { selectedIds.value = new Set() }
function displayReason(user: RiskUserRow) { return formatRiskReason(user.risk_reason, { eventType: user.risk_type || undefined, count: user.event_count }) }
function formatDate(value?: string | null) { return value ? new Date(value).toLocaleString() : '-' }
function statusClass(status?: AccountStatus) { return status === 'disabled' ? 'bg-red-100 text-red-700 dark:bg-red-950/30 dark:text-red-300' : status === 'pending' ? 'bg-amber-100 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300' : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300' }
function handleUpdated(updated: RiskUserRow) {
  const index = users.value.findIndex((user) => user.id === updated.id)
  if (index >= 0) users.value[index] = { ...users.value[index], ...updated }
  selectedUser.value = null
}
function openBatchAction(action: 'disabled' | 'active' | 'processed') {
  batchAction.value = action
  batchReason.value = ''
  batchValidationError.value = ''
}
function closeBatchAction() { if (!batchSaving.value) batchAction.value = null }
async function confirmBatchAction() {
  const reason = batchReason.value.trim()
  if (!reason) {
    batchValidationError.value = '操作原因不能为空或仅包含空格。'
    return
  }
  if (!batchAction.value) return
  batchSaving.value = true
  batchValidationError.value = ''
  const ids = Array.from(selectedIds.value)
  try {
    const results = batchAction.value === 'processed'
      ? await userRiskControlV2API.markUsersProcessed(ids, reason)
      : await userRiskControlV2API.batchSetUserStatus(ids, batchAction.value, reason)
    batchResults.value = results
    clearSelection()
    batchAction.value = null
    await loadUsers()
  } catch (err) {
    batchValidationError.value = errorMessage(err)
  } finally {
    batchSaving.value = false
  }
}

restoreRouteState()
watch(() => route?.fullPath, () => {
  if (writingQuery) return
  restoreRouteState()
  clearSelection()
  void loadUsers()
})
onMounted(loadUsers)
</script>
