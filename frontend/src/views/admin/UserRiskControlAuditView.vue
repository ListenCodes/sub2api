<template>
  <AppLayout>
    <TablePageLayout :title="t('admin.userRiskControl.auditPageTitle')" :description="t('admin.userRiskControl.auditPageDescription')">
      <template v-if="error" #actions><div class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div></template>
      <template #filters>
        <section class="flex flex-wrap items-center gap-3 border-y border-gray-200 py-3 dark:border-dark-700" data-testid="audit-filters">
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
          <Select :model-value="draft.action || ''" class="w-full sm:w-40" data-testid="audit-action-filter" :options="auditActionFilterOptions" @update:model-value="setFilter('action', $event)" />
          <Select :model-value="draft.result || ''" class="w-full sm:w-36" data-testid="audit-result-filter" :options="auditResultFilterOptions" @update:model-value="setFilter('result', $event)" />
          <DateRangePicker :start-date="draft.from || ''" :end-date="draft.to || ''" data-testid="audit-date-range" @change="setDateRange" />
          <Select :model-value="mobileSortValue" class="w-full md:hidden" data-testid="mobile-audit-sort" :options="mobileSortOptions" @update:model-value="setMobileSort" />
          <button type="button" class="btn-ghost btn-icon" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-audit-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
        </section>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="records" :loading="loading" row-key="id" :sticky-first-column="false" :sticky-actions-column="false">
          <template #header-time><button type="button" data-testid="audit-sort-time" :aria-sort="sortAria('created_at')" @click.stop="toggleSort('created_at')">{{ t('admin.userRiskControl.table.time') }} {{ sortIndicator('created_at') }}</button></template>
          <template #header-target><button type="button" data-testid="audit-sort-target" :aria-sort="sortAria('target')" @click.stop="toggleSort('target')">{{ t('admin.userRiskControl.table.target') }} {{ sortIndicator('target') }}</button></template>
          <template #header-result><button type="button" data-testid="audit-sort-result" :aria-sort="sortAria('result')" @click.stop="toggleSort('result')">{{ t('admin.userRiskControl.table.result') }} {{ sortIndicator('result') }}</button></template>
          <template #cell-time="{ row: record }">{{ formatDate(record.created_at) }}</template>
          <template #cell-actor="{ row: record }">{{ record.actor || '管理员 ID 未知' }}</template>
          <template #cell-action="{ row: record }"><span class="font-medium text-gray-900 dark:text-white">{{ formatRiskAction(record.action) }}</span></template>
          <template #cell-target="{ row: record }">{{ targetLabel(record) }}</template>
          <template #cell-statusChange="{ row: record }"><span :data-testid="`audit-status-change-${record.id}`">{{ statusChange(record) }}</span></template>
          <template #cell-result="{ row: record }"><span :class="record.result === 'success' ? 'text-emerald-600' : record.result === 'partial' ? 'text-amber-600' : 'text-red-600'">{{ formatAuditResult(record.result) }}</span></template>
          <template #cell-reason="{ row: record }"><div class="max-w-xl whitespace-normal break-words text-left"><p>{{ record.reason || '无操作原因' }}</p><p v-if="record.failure_reason" class="mt-1 text-red-600">失败原因：{{ record.failure_reason }}</p><p v-if="record.batch_id || record.request_id" class="mt-1 text-xs text-gray-400">批次：{{ record.batch_id || '-' }} · 请求：{{ record.request_id || '-' }}</p></div></template>
          <template #empty><EmptyState :title="t('admin.userRiskControl.empty')" /></template>
        </DataTable>
      </template>
      <template v-if="total" #pagination><Pagination :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:page-size="changePageSize" /></template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import type { Column } from '@/components/common/types'
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
const columns: Column[] = [
  { key: 'time', label: t('admin.userRiskControl.table.time'), class: 'min-w-44' },
  { key: 'actor', label: t('admin.userRiskControl.table.actor'), class: 'min-w-40' },
  { key: 'action', label: t('admin.userRiskControl.table.action'), class: 'min-w-32' },
  { key: 'target', label: t('admin.userRiskControl.table.target'), class: 'min-w-40' },
  { key: 'statusChange', label: t('admin.userRiskControl.table.statusChange'), class: 'min-w-44' },
  { key: 'result', label: t('admin.userRiskControl.table.result'), class: 'min-w-24' },
  { key: 'reason', label: t('admin.userRiskControl.table.reason'), class: 'min-w-80' },
]
let loadRequestID = 0
const auditActionOptions = riskActionOptions.filter((option) => ['ban', 'unban', 'auto_ban', 'create_rule', 'update_rule', 'rule_test', 'mark_processed'].includes(option.value))
const auditActionFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allActions') }, ...auditActionOptions])
const auditResultFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allResults') }, ...auditResultOptions])
const mobileSortOptions = [
  { value: '', label: '默认排序' },
  { value: 'created_at:desc', label: '操作时间：新到旧' },
  { value: 'created_at:asc', label: '操作时间：旧到新' },
  { value: 'result:asc', label: '执行结果：升序' },
  { value: 'result:desc', label: '执行结果：降序' },
  { value: 'target:asc', label: '操作目标：升序' },
  { value: 'target:desc', label: '操作目标：降序' },
]
const mobileSortValue = computed(() => sortBy.value ? `${sortBy.value}:${sortOrder.value}` : '')
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
function setDateRange(range: { startDate: string; endDate: string }) { draft.from = range.startDate; draft.to = range.endDate; void applyFilters() }
function setMobileSort(value: string | number | boolean | null) {
  if (!value || typeof value === 'boolean') sortBy.value = undefined
  else {
    const [field, order] = String(value).split(':')
    sortBy.value = field as NonNullable<AuditFilters['sortBy']>
    sortOrder.value = order === 'asc' ? 'asc' : 'desc'
  }
  page.value = 1
  void loadAudit()
}
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
function targetLabel(record: RiskAuditRecord) { return record.target_type === 'user' ? `用户 #${record.target_id || record.target_user_id}` : record.target_type === 'rule' ? `规则 ${record.target_id || '-'}` : `未知目标：${record.target_id || '-'}` }
function statusChange(record: RiskAuditRecord) { return record.before_status && record.after_status ? `${formatAccountStatus(record.before_status)} → ${formatAccountStatus(record.after_status)}` : '-' }
onMounted(loadAudit)
</script>
