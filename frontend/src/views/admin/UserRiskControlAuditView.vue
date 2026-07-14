<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p>
        <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.auditPageTitle') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.auditPageDescription') }}</p>
      </header>
      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">{{ error }}</div>
      <section class="border-y border-gray-200 py-3 dark:border-dark-700" data-testid="audit-filters">
        <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            :model-value="draft.actor || ''"
            class="w-full sm:w-56"
            data-testid="audit-actor-filter"
            placeholder="管理员账号或 ID"
            @update:model-value="draft.actor = $event"
            @search="applyFilters"
          />
          <SearchInput
            :model-value="draft.target || ''"
            class="w-full sm:w-64"
            data-testid="audit-target-filter"
            :placeholder="t('admin.userRiskControl.targetUserPlaceholder')"
            @update:model-value="draft.target = $event"
            @search="applyFilters"
          />
          <Select :model-value="draft.action || ''" class="w-40" data-testid="audit-action-filter" :options="auditActionFilterOptions" @update:model-value="setFilter('action', $event)" />
          <Select :model-value="draft.result || ''" class="w-36" data-testid="audit-result-filter" :options="auditResultFilterOptions" @update:model-value="setFilter('result', $event)" />
          <div class="flex h-10 items-center overflow-hidden rounded-md border border-gray-300 bg-white dark:border-dark-600 dark:bg-dark-800" aria-label="审计时间范围">
            <input v-model="draft.from" type="date" data-testid="audit-from-filter" class="h-full w-36 border-0 bg-transparent px-3 text-sm text-gray-700 focus:ring-0 dark:text-gray-200" title="开始时间" @change="applyFilters" />
            <span class="text-gray-300 dark:text-dark-500">-</span>
            <input v-model="draft.to" type="date" data-testid="audit-to-filter" class="h-full w-36 border-0 bg-transparent px-3 text-sm text-gray-700 focus:ring-0 dark:text-gray-200" title="结束时间" @change="applyFilters" />
          </div>
          <button type="button" class="btn-ghost btn-icon" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-audit-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
        </div>
      </section>
      <section class="card overflow-hidden">
        <div v-if="loading" class="p-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
        <div v-else-if="!records.length" class="p-12 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.empty') }}</div>
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[1180px] table-fixed divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800"><tr>
              <th class="w-44 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-time" :aria-sort="sortAria('created_at')" @click="toggleSort('created_at')">{{ t('admin.userRiskControl.table.time') }} {{ sortIndicator('created_at') }}</button></th>
              <th class="w-40 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.actor') }}</th>
              <th class="w-32 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.action') }}</th>
              <th class="w-40 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-target" :aria-sort="sortAria('target')" @click="toggleSort('target')">{{ t('admin.userRiskControl.table.target') }} {{ sortIndicator('target') }}</button></th>
              <th class="w-44 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.statusChange') }}</th>
              <th class="w-24 px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-result" :aria-sort="sortAria('result')" @click="toggleSort('result')">{{ t('admin.userRiskControl.table.result') }} {{ sortIndicator('result') }}</button></th>
              <th class="w-auto min-w-80 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.reason') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="record in records" :key="record.id">
                <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatDate(record.created_at) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ record.actor || '管理员 ID 未知' }}</td>
                <td class="px-4 py-4 text-sm font-medium text-gray-900 dark:text-white">{{ formatRiskAction(record.action) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ targetLabel(record) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatAccountStatus(record.before_status) }} → {{ formatAccountStatus(record.after_status) }}</td>
                <td class="px-4 py-4"><span :class="record.result === 'success' ? 'text-emerald-600' : record.result === 'partial' ? 'text-amber-600' : 'text-red-600'">{{ formatAuditResult(record.result) }}</span></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300"><p>{{ record.reason || '无操作原因' }}</p><p v-if="record.failure_reason" class="mt-1 text-red-600">失败原因：{{ record.failure_reason }}</p><p v-if="record.batch_id || record.request_id" class="mt-1 text-xs text-gray-400">批次：{{ record.batch_id || '-' }} · 请求：{{ record.request_id || '-' }}</p></td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="total" :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:page-size="changePageSize" />
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { userRiskControlV2API, type AuditFilters, type RiskAuditRecord } from '@/api/admin/userRiskControlV2'
import { auditResultOptions, formatAccountStatus, formatAuditResult, formatRiskAction, riskActionOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const records = ref<RiskAuditRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(getPersistedPageSize(20))
const loading = ref(true)
const error = ref('')
const sortBy = ref<AuditFilters['sortBy']>()
const sortOrder = ref<'asc' | 'desc'>('desc')
const draft = reactive<AuditFilters>({ action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' })
const activeFilters = reactive<AuditFilters>({ ...draft })
let loadRequestID = 0
const auditActionOptions = riskActionOptions.filter((option) => ['ban', 'unban', 'auto_ban', 'create_rule', 'update_rule', 'rule_test', 'mark_processed'].includes(option.value))
const auditActionFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allActions') }, ...auditActionOptions])
const auditResultFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allResults') }, ...auditResultOptions])
const hasFilters = computed(() => Boolean(draft.actor?.trim() || draft.target?.trim() || draft.action || draft.result || draft.from || draft.to))

function errorMessage(err: unknown) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed') }
function requestFilters(): AuditFilters {
  const result: AuditFilters = { action: activeFilters.action, result: activeFilters.result, page: page.value, pageSize: pageSize.value }
  if (activeFilters.targetUserId) result.targetUserId = activeFilters.targetUserId
  if (activeFilters.target?.trim()) result.target = activeFilters.target.trim()
  if (activeFilters.actor?.trim()) result.actor = activeFilters.actor.trim()
  if (activeFilters.from) result.from = activeFilters.from
  if (activeFilters.to) result.to = activeFilters.to
  if (sortBy.value) { result.sortBy = sortBy.value; result.sortOrder = sortOrder.value }
  return result
}
async function loadAudit() {
  const requestID = ++loadRequestID
  loading.value = true
  error.value = ''
  try {
    const response = await userRiskControlV2API.listAudit(requestFilters())
    if (requestID !== loadRequestID) return
    records.value = response.items
    total.value = response.total
    page.value = response.page || page.value
  } catch (err) {
    if (requestID === loadRequestID) error.value = errorMessage(err)
  } finally {
    if (requestID === loadRequestID) loading.value = false
  }
}
async function applyFilters() { Object.assign(activeFilters, draft); page.value = 1; await loadAudit() }
function setFilter(key: 'action' | 'result', value: string | number | boolean | null) { Object.assign(draft, { [key]: String(value ?? '') }); void applyFilters() }
async function resetFilters() { Object.assign(draft, { action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' }); await applyFilters() }
async function changePage(next: number) { page.value = next; await loadAudit() }
async function changePageSize(next: number) { pageSize.value = next; page.value = 1; await loadAudit() }
function toggleSort(next: NonNullable<AuditFilters['sortBy']>) {
  if (sortBy.value === next) sortOrder.value = sortOrder.value === 'desc' ? 'asc' : 'desc'
  else { sortBy.value = next; sortOrder.value = 'desc' }
  page.value = 1
  void loadAudit()
}
function sortIndicator(value: NonNullable<AuditFilters['sortBy']>) { return sortBy.value === value ? sortOrder.value === 'desc' ? '↓' : '↑' : '↕' }
function sortAria(value: NonNullable<AuditFilters['sortBy']>) { return sortBy.value === value ? sortOrder.value === 'desc' ? 'descending' : 'ascending' : 'none' }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function targetLabel(record: RiskAuditRecord) { return record.target_type === 'user' ? `用户 #${record.target_id || record.target_user_id}` : `${record.target_type || '未知目标'}：${record.target_id || '-'}` }
onMounted(loadAudit)
</script>
