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

      <section class="card p-4">
        <div class="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          <input v-model="draft.search" class="form-input lg:col-span-2" :placeholder="t('admin.userRiskControl.searchPlaceholder')" @keyup.enter="applyFilters" />
          <select v-model="draft.status" class="form-input" data-testid="account-status-filter">
            <option value="">{{ t('admin.userRiskControl.allStatuses') }}</option>
            <option v-for="option in accountStatusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <select v-model="draft.riskType" class="form-input" data-testid="risk-type-filter">
            <option value="">{{ t('admin.userRiskControl.allRiskTypes') }}</option>
            <option v-for="option in riskTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <select v-model="draft.riskLevel" class="form-input" data-testid="risk-level-filter">
            <option value="">{{ t('admin.userRiskControl.allRiskLevels') }}</option>
            <option v-for="option in riskLevelOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <select v-model="draft.processingStatus" class="form-input" data-testid="processing-status-filter">
            <option value="">全部处理状态</option>
            <option v-for="option in processingStatusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <input v-model.number="draft.minScore" type="number" min="0" max="100" class="form-input" placeholder="最低风险分" />
          <input v-model.number="draft.maxScore" type="number" min="0" max="100" class="form-input" placeholder="最高风险分" />
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <input v-model="draft.riskOnly" type="checkbox" class="rounded border-gray-300 text-primary-600" />
            只看有风险记录
          </label>
        </div>
        <button type="button" class="btn btn-primary mt-4" data-testid="apply-filters" @click="applyFilters">{{ t('common.apply') }}</button>
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
          <table class="min-w-[1280px] divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="w-12 px-4 py-3 text-center"><input v-model="allSelected" type="checkbox" data-testid="select-current-page" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" /></th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.account') }}</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.id') }}</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.status') }}</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.riskType') }}</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-risk-score" :aria-sort="sortAria('risk_score')" @click="toggleSort('risk_score')">风险分 {{ sortIndicator('risk_score') }}</button></th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-risk-level" :aria-sort="sortAria('risk_level')" @click="toggleSort('risk_level')">{{ t('admin.userRiskControl.level') }} {{ sortIndicator('risk_level') }}</button></th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-event-count" :aria-sort="sortAria('event_count')" @click="toggleSort('event_count')">事件次数 {{ sortIndicator('event_count') }}</button></th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="sort-last-event" :aria-sort="sortAria('last_event_at')" @click="toggleSort('last_event_at')">最近事件 {{ sortIndicator('last_event_at') }}</button></th>
                <th class="min-w-72 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.reason') }}</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">处理状态</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="user in users" :key="user.id" class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800/60" :data-testid="`user-row-${user.id}`" @click="selectedUser = user">
                <td class="px-4 py-4 text-center" @click.stop><input :checked="selectedIds.has(user.id)" type="checkbox" :data-testid="`user-select-${user.id}`" class="rounded border-gray-300 text-primary-600" @change="toggleSelection(user.id)" /></td>
                <td class="px-4 py-4"><p class="font-medium text-gray-900 dark:text-white">{{ user.username || '未设置用户名' }}</p><p class="text-xs text-gray-500">{{ user.email || '暂无账号标识' }}</p></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">#{{ user.id }}</td>
                <td class="px-4 py-4"><span class="rounded-full px-2 py-1 text-xs font-medium" :class="statusClass(user.status)">{{ formatAccountStatus(user.status) }}</span></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskType(user.risk_type) }}</td>
                <td class="px-4 py-4 text-sm font-semibold text-gray-900 dark:text-white">{{ user.risk_score ?? 0 }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskLevel(user.risk_level) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ user.event_count ?? 0 }}<span class="block text-xs text-gray-400">IP {{ user.ip_count ?? 0 }} / 设备 {{ user.device_count ?? 0 }}</span></td>
                <td class="px-4 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatDate(user.last_event_at) }}</td>
                <td class="max-w-md px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ displayReason(user) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatProcessingStatus(user.processing_status || (user.pending ? 'pending' : user.last_action ? 'observed' : '')) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer v-if="total" class="flex flex-col gap-3 border-t border-gray-200 px-5 py-3 text-sm text-gray-500 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
          <span>{{ t('admin.userRiskControl.total', { total }) }}</span>
          <div class="flex items-center gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('common.previous') }}</button><span>{{ page }} / {{ totalPages }}</span><button type="button" class="btn btn-secondary btn-sm" :disabled="page >= totalPages || loading" @click="changePage(page + 1)">{{ t('common.next') }}</button></div>
        </footer>
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
const pageSize = 20
const total = ref(0)
const sortBy = ref<RiskSortBy | undefined>(undefined)
const sortOrder = ref<'asc' | 'desc'>('desc')
const batchAction = ref<'disabled' | 'active' | 'processed' | null>(null)
const batchReason = ref('')
const batchValidationError = ref('')
const batchResults = ref<Array<{ id: number; status: 'success' | 'failed'; reason?: string }>>([])
const draft = reactive<UserRiskFilters>({ search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false })
const activeFilters = reactive<UserRiskFilters>({ ...draft })

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
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
  loading.value = true
  error.value = ''
  try {
    const response = await userRiskControlV2API.listUsers({ ...activeFilters, page: page.value, pageSize, sortBy: sortBy.value, sortOrder: sortOrder.value })
    users.value = response.items
    total.value = response.total
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function applyFilters() {
  Object.assign(activeFilters, draft)
  page.value = 1
  await loadUsers()
}

async function changePage(next: number) {
  page.value = next
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
function displayReason(user: RiskUserRow) { return formatRiskReason(user.risk_reason, { eventType: user.risk_type, count: user.event_count }) }
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
