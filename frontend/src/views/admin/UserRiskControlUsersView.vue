<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p>
          <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.usersTitle') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.usersDescription') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadUsers">{{ t('admin.userRiskControl.refresh') }}</button>
      </header>

      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <div v-if="batchResults.length" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900" data-testid="batch-result-summary">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <strong class="text-sm text-gray-900 dark:text-white">{{ formatAuditResult(batchSummary) }}：{{ batchSuccessCount }}/{{ batchResults.length }}</strong>
          <button type="button" class="text-sm text-gray-500 underline" @click="batchResults = []">{{ t('common.close') }}</button>
        </div>
        <ul class="mt-3 space-y-2 text-sm">
          <li v-for="result in batchResults" :key="result.id" class="flex flex-wrap gap-x-2 gap-y-1">
            <span class="font-medium text-gray-900 dark:text-white">#{{ result.id }}</span>
            <span :class="result.status === 'success' ? 'text-emerald-600' : 'text-red-600'">{{ formatAuditResult(result.status) }}</span>
            <span v-if="result.reason" class="text-gray-600 dark:text-gray-300">{{ result.reason }}</span>
          </li>
        </ul>
      </div>

      <section class="border-y border-gray-200 py-3 dark:border-dark-700" data-testid="risk-user-filters">
        <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            :model-value="draft.search || ''"
            :placeholder="t('admin.userRiskControl.searchPlaceholder')"
            class="w-full sm:w-72"
            data-testid="risk-user-search"
            @update:model-value="draft.search = $event"
            @search="applyFilters"
          />
          <Select :model-value="draft.status || ''" class="w-40" data-testid="account-status-filter" :options="accountStatusFilterOptions" @update:model-value="setFilter('status', $event)" />
          <Select :model-value="draft.riskType || ''" class="w-44" data-testid="risk-type-filter" :options="riskTypeFilterOptions" @update:model-value="setFilter('riskType', $event)" />
          <Select :model-value="draft.riskLevel || ''" class="w-40" data-testid="risk-level-filter" :options="riskLevelFilterOptions" @update:model-value="setFilter('riskLevel', $event)" />
          <Select :model-value="draft.processingStatus || ''" class="w-40" data-testid="processing-status-filter" :options="processingStatusFilterOptions" @update:model-value="setFilter('processingStatus', $event)" />
          <div class="flex h-10 items-center overflow-hidden rounded-md border border-gray-300 bg-white dark:border-dark-600 dark:bg-dark-800" aria-label="风险分范围">
            <input v-model.number="draft.minScore" type="number" min="0" max="100" class="h-full w-24 border-0 bg-transparent px-3 text-sm focus:ring-0" placeholder="最低分" data-testid="min-score-filter" @change="applyFilters" />
            <span class="text-gray-300 dark:text-dark-500">-</span>
            <input v-model.number="draft.maxScore" type="number" min="0" max="100" class="h-full w-24 border-0 bg-transparent px-3 text-sm focus:ring-0" placeholder="最高分" data-testid="max-score-filter" @change="applyFilters" />
          </div>
          <label class="flex h-10 items-center gap-2 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">
            <input v-model="draft.riskOnly" type="checkbox" class="rounded border-gray-300 text-primary-600" data-testid="risk-only-filter" @change="applyFilters" />
            只看有风险记录
          </label>
          <button type="button" class="btn-ghost btn-icon" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
        </div>
      </section>

      <section v-if="selectedIds.size" class="flex flex-col gap-3 rounded-lg border border-primary-200 bg-primary-50 p-3 dark:border-primary-900/50 dark:bg-primary-950/20 sm:flex-row sm:items-center sm:justify-between" data-testid="batch-action-bar">
        <span class="text-sm font-medium text-primary-800 dark:text-primary-200" data-testid="selected-count">已选择 {{ selectedIds.size }} 个账号</span>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-danger btn-sm" data-testid="batch-ban" @click="openBatchAction('disabled')">{{ formatRiskAction('ban') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" data-testid="batch-unban" @click="openBatchAction('active')">{{ formatRiskAction('unban') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" data-testid="batch-mark-processed" @click="openBatchAction('processed')">标记已处理</button>
          <button type="button" class="btn btn-ghost btn-sm" data-testid="clear-selection" @click="clearSelection">取消选择</button>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div v-if="loading" class="px-5 py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</div>
        <div v-else-if="!users.length" class="px-5 py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.empty') }}</div>
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[1280px] table-fixed divide-y divide-gray-200 dark:divide-dark-700" data-testid="risk-users-table">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="w-12 px-4 py-3 text-center"><input v-model="allSelected" type="checkbox" data-testid="select-current-page" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" /></th>
                <th class="w-56 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.account') }}</th>
                <th class="w-20 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.id') }}</th>
                <th class="w-24 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.status') }}</th>
                <th class="w-32 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.riskType') }}</th>
                <th class="w-20 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-risk-score" :aria-sort="sortAria('risk_score')" @click="toggleSort('risk_score')">风险分 {{ sortIndicator('risk_score') }}</button></th>
                <th class="w-28 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-risk-level" :aria-sort="sortAria('risk_level')" @click="toggleSort('risk_level')">{{ t('admin.userRiskControl.level') }} {{ sortIndicator('risk_level') }}</button></th>
                <th class="w-36 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-event-count" :aria-sort="sortAria('event_count')" @click="toggleSort('event_count')">事件次数 {{ sortIndicator('event_count') }}</button></th>
                <th class="w-44 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-last-event" :aria-sort="sortAria('last_event_at')" @click="toggleSort('last_event_at')">最近事件 {{ sortIndicator('last_event_at') }}</button></th>
                <th class="w-auto min-w-80 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.reason') }}</th>
                <th class="w-28 px-4 py-3 text-left text-xs font-medium text-gray-500">处理状态</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="user in users" :key="user.id" class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800/60" :data-testid="`user-row-${user.id}`" @click="selectedUser = user">
                <td class="px-4 py-4 text-center" @click.stop><input :checked="selectedIds.has(user.id)" type="checkbox" :data-testid="`user-select-${user.id}`" class="rounded border-gray-300 text-primary-600" @change="toggleSelection(user.id)" /></td>
                <td class="px-4 py-4"><p class="truncate font-medium text-gray-900 dark:text-white" :title="user.email || user.username || `用户 #${user.id}`" :data-testid="`account-primary-${user.id}`">{{ user.email || user.username || `用户 #${user.id}` }}</p><p v-if="user.email && user.username" class="mt-0.5 truncate text-xs text-gray-500" :title="user.username" :data-testid="`account-secondary-${user.id}`">{{ user.username }}</p></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">#{{ user.id }}</td>
                <td class="px-4 py-4"><span class="rounded-full px-2 py-1 text-xs font-medium" :class="statusClass(user.status)">{{ formatAccountStatus(user.status) }}</span></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskType(user.risk_type) }}</td>
                <td class="px-4 py-4 text-sm font-semibold text-gray-900 dark:text-white">{{ user.risk_score ?? 0 }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskLevel(user.risk_level) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ user.event_count ?? 0 }}<span class="block text-xs text-gray-400">IP {{ user.ip_count ?? 0 }} / 设备 {{ user.device_count ?? 0 }}</span></td>
                <td class="px-4 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatDate(user.last_event_at) }}</td>
                <td class="break-words px-4 py-4 text-sm leading-5 text-gray-600 dark:text-gray-300">{{ displayReason(user) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatProcessingStatus(user.processing_status || (user.pending ? 'pending' : user.last_action ? 'observed' : '')) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="total" :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:page-size="changePageSize" />
      </section>
    </div>

    <UserRiskControlUserDrawer v-if="selectedUser" :user="selectedUser" @close="selectedUser = null" @updated="handleUpdated" />

    <Teleport to="body">
      <div v-if="batchAction" class="fixed inset-0 z-[80] flex items-center justify-center bg-gray-950/40 p-4" data-testid="batch-action-modal">
        <div class="w-full max-w-md rounded-xl bg-white p-5 shadow-xl dark:bg-dark-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ batchAction === 'disabled' ? '确认批量封禁' : batchAction === 'active' ? '确认批量解封' : '确认批量标记已处理' }}</h2>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">将处理 {{ selectedIds.size }} 个账号；每个账号都会单独记录结果。</p>
          <textarea v-model="batchReason" class="form-input mt-4 min-h-24 w-full" data-testid="batch-reason" placeholder="填写操作原因（必填）" @input="batchValidationError = ''" />
          <p v-if="batchValidationError" class="mt-2 text-sm text-red-600 dark:text-red-300" data-testid="batch-reason-error">{{ batchValidationError }}</p>
          <div class="mt-4 flex justify-end gap-3"><button type="button" class="btn btn-secondary" @click="closeBatchAction">{{ t('common.cancel') }}</button><button type="button" class="btn btn-danger" data-testid="batch-confirm" :disabled="batchSaving" @click="confirmBatchAction">{{ batchSaving ? t('common.saving') : t('common.confirm') }}</button></div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserRiskControlUserDrawer from '@/components/admin/UserRiskControlUserDrawer.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { userRiskControlV2API, type AccountStatus, type RiskSortBy, type RiskUserRow, type UserRiskFilters } from '@/api/admin/userRiskControlV2'
import { accountStatusOptions, formatAccountStatus, formatAuditResult, formatProcessingStatus, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType, processingStatusOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
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
const draft = reactive<UserRiskFilters>({ search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false })
const activeFilters = reactive<UserRiskFilters>({ ...draft })
let loadRequestID = 0

const accountStatusFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allStatuses') }, ...accountStatusOptions])
const riskTypeFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allRiskTypes') }, ...riskTypeOptions])
const riskLevelFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allRiskLevels') }, ...riskLevelOptions])
const processingStatusFilterOptions = computed(() => [{ value: '', label: '全部处理状态' }, ...processingStatusOptions])
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

function errorMessage(err: unknown) {
  return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed')
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
  await loadUsers()
}

function normalizeScore(value: unknown): number | undefined {
  if (value === '' || value === null || value === undefined) return undefined
  const score = Number(value)
  return Number.isFinite(score) ? score : undefined
}

function setFilter(key: 'status' | 'riskType' | 'riskLevel' | 'processingStatus', value: string | number | boolean | null) {
  Object.assign(draft, { [key]: String(value ?? '') })
  void applyFilters()
}

async function resetFilters() {
  Object.assign(draft, { search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false, minScore: undefined, maxScore: undefined })
  await applyFilters()
}

async function changePage(next: number) {
  page.value = next
  clearSelection()
  await loadUsers()
}

async function changePageSize(next: number) {
  pageSize.value = next
  page.value = 1
  clearSelection()
  await loadUsers()
}

function toggleSort(next: RiskSortBy) {
  if (sortBy.value === next) sortOrder.value = sortOrder.value === 'desc' ? 'asc' : 'desc'
  else {
    sortBy.value = next
    sortOrder.value = 'desc'
  }
  activeFilters.sortBy = sortBy.value
  activeFilters.sortOrder = sortOrder.value
  page.value = 1
  void loadUsers()
}

function sortIndicator(value: RiskSortBy) { return sortBy.value === value ? sortOrder.value === 'desc' ? '↓' : '↑' : '↕' }
function sortAria(value: RiskSortBy) { return sortBy.value === value ? sortOrder.value === 'desc' ? 'descending' : 'ascending' : 'none' }
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

onMounted(loadUsers)
</script>
