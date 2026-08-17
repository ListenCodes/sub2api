<template>
  <TablePageLayout :title="t('admin.userRiskControl.auditPageTitle')" :description="t('admin.userRiskControl.auditPageDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
          <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
        </div>
      </template>
      <template #filters>
        <section class="space-y-3" data-testid="audit-filters">
          <div class="inline-flex max-w-full overflow-x-auto rounded-md border border-gray-200 p-1 dark:border-gray-700" role="tablist" aria-label="审计分类">
            <button v-for="category in auditCategories" :key="category.value" type="button" role="tab" class="btn btn-sm shrink-0" :class="draft.category === category.value ? 'btn-primary' : 'btn-ghost'" :aria-selected="draft.category === category.value" :data-testid="`audit-category-${category.value}`" @click="setCategory(category.value)">{{ category.label }}</button>
          </div>
          <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            :model-value="draft.actor || ''"
            class="w-full sm:w-56"
            data-testid="audit-actor-filter"
            placeholder="管理员 ID"
            @update:model-value="setTextFilter('actor', $event)"
            @search="runFiltersNow"
          />
          <SearchInput
            :model-value="draft.target || ''"
            class="w-full sm:w-64"
            data-testid="audit-target-filter"
            :placeholder="t('admin.userRiskControl.targetUserPlaceholder')"
            @update:model-value="setTextFilter('target', $event)"
            @search="runFiltersNow"
          />
          <Select :model-value="draft.action || ''" class="w-full sm:w-40" data-testid="audit-action-filter" :options="auditActionFilterOptions" @update:model-value="setFilter('action', $event)" />
          <Select :model-value="draft.result || ''" class="w-full sm:w-36" data-testid="audit-result-filter" :options="auditResultFilterOptions" @update:model-value="setFilter('result', $event)" />
          <DateRangePicker :start-date="draft.from || ''" :end-date="draft.to || ''" data-testid="audit-date-range" @change="setDateRange" />
          <Select :model-value="mobileSortValue" class="w-full md:hidden" data-testid="mobile-audit-sort" :options="mobileSortOptions" @update:model-value="setMobileSort" />
          <button type="button" class="btn btn-ghost btn-icon" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-audit-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
          </div>
        </section>
      </template>
      <template #table>
        <DataTable :key="`risk-audit-${tableSortKey}-${sortOrder}`" :columns="columns" :data="records" :loading="loading" row-key="id" :server-side-sort="true" :default-sort-key="tableSortKey" :default-sort-order="sortOrder" @sort="handleTableSort">
          <template #cell-time="{ row: record }">{{ formatDate(record.created_at) }}</template>
          <template #cell-actor="{ row: record }">{{ record.actor || '管理员 ID 未知' }}</template>
          <template #cell-action="{ row: record }"><span class="font-medium text-gray-900 dark:text-white">{{ formatRiskAction(record.action) }}</span></template>
          <template #cell-target="{ row: record }">{{ targetLabel(record) }}</template>
          <template #cell-statusChange="{ row: record }"><span :data-testid="`audit-status-change-${record.id}`">{{ statusChange(record) }}</span></template>
          <template #cell-result="{ row: record }"><span :class="record.result === 'success' ? 'text-emerald-600 dark:text-emerald-400' : record.result === 'partial' ? 'text-amber-600 dark:text-amber-400' : 'text-red-600 dark:text-red-400'">{{ formatAuditResult(record.result) }}</span></template>
          <template #cell-reason="{ row: record }">
            <div class="max-w-xl whitespace-normal break-words text-left">
              <button v-if="record.action === 'view_identity_detail'" type="button" class="btn btn-ghost btn-icon" :title="sensitiveExpanded(record.id) ? '折叠详情' : '展开详情'" :aria-label="sensitiveExpanded(record.id) ? '折叠详情' : '展开详情'" :data-testid="`toggle-sensitive-audit-${record.id}`" @click="toggleSensitive(record.id)">
                <Icon :name="sensitiveExpanded(record.id) ? 'chevronDown' : 'chevronRight'" size="sm" />
              </button>
              <template v-if="record.action !== 'view_identity_detail' || sensitiveExpanded(record.id)">
                <p>{{ record.reason || formatSensitiveSection(record) || '无操作原因' }}</p>
                <p v-if="record.failure_reason" class="mt-1 text-red-600 dark:text-red-400">失败原因：{{ record.failure_reason }}</p>
                <p v-if="record.batch_id || record.request_id" class="mt-1 text-xs text-gray-400 dark:text-gray-500">批次：{{ record.batch_id || '-' }} · 请求：{{ record.request_id || '-' }}</p>
              </template>
            </div>
          </template>
          <template #empty><EmptyState :title="t('admin.userRiskControl.empty')" /></template>
        </DataTable>
      </template>
      <template #pagination><Pagination v-if="total" :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:pageSize="changePageSize" /></template>
    </TablePageLayout>
</template>

<script setup lang="ts">
import { computed, inject, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { routeLocationKey, routerKey, type LocationQueryRaw } from 'vue-router'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import UserRiskControlTabs from '@/views/admin/extensions/UserRiskControlTabs.vue'
import { useDebouncedAction } from '@/composables/useDebouncedAction'
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
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const records = ref<RiskAuditRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(getPersistedPageSize(20))
const loading = ref(true)
const error = ref('')
const sortBy = ref<AuditFilters['sortBy']>()
const sortOrder = ref<'asc' | 'desc'>('desc')
const draft = reactive<AuditFilters>({ category: 'security', action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' })
const activeFilters = reactive<AuditFilters>({ ...draft })
const expandedSensitiveIDs = ref<Set<number>>(new Set())
const columns: Column[] = [
  { key: 'time', label: t('admin.userRiskControl.table.time'), sortable: true },
  { key: 'actor', label: t('admin.userRiskControl.table.actor') },
  { key: 'action', label: t('admin.userRiskControl.table.action') },
  { key: 'target', label: t('admin.userRiskControl.table.target'), sortable: true },
  { key: 'statusChange', label: t('admin.userRiskControl.table.statusChange') },
  { key: 'result', label: t('admin.userRiskControl.table.result'), sortable: true },
  { key: 'reason', label: t('admin.userRiskControl.table.reason') },
]
let loadRequestID = 0
let writingQuery = false
const auditCategories: Array<{ value: NonNullable<AuditFilters['category']>; label: string }> = [
  { value: 'security', label: '安全处置' },
  { value: 'rules', label: '规则变更' },
  { value: 'sensitive', label: '敏感数据查看' },
]
const auditActionsByCategory: Record<NonNullable<AuditFilters['category']>, string[]> = {
  security: ['ban', 'unban', 'auto_ban', 'mark_processed', 'claim_risk_review_case', 'review_risk_case', 'label_shared_network'],
  rules: ['create_rule', 'update_rule', 'rule_test', 'disable_identity_rule', 'purge_legacy_v1', 'identity_rebuild_dry_run', 'identity_rebuild'],
  sensitive: ['view_identity_detail'],
}
const auditActionFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allActions') }, ...riskActionOptions.filter((option) => auditActionsByCategory[draft.category || 'security'].includes(option.value))])
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
const tableSortKey = computed(() => {
  switch (sortBy.value) {
    case 'created_at': return 'time'
    case 'target': return 'target'
    case 'result': return 'result'
    default: return ''
  }
})
const hasFilters = computed(() => Boolean(draft.actor?.trim() || draft.target?.trim() || draft.action || draft.result || draft.from || draft.to))

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
  Object.assign(draft, {
	category: ['security', 'rules', 'sensitive'].includes(queryText('category')) ? queryText('category') as AuditFilters['category'] : 'security',
    actor: queryText('actor'),
    target: queryText('target'),
    action: queryText('action'),
    result: queryText('result'),
    from: queryText('from'),
    to: queryText('to'),
  })
  Object.assign(activeFilters, draft)
  page.value = positiveInteger(queryText('page'), 1)
  pageSize.value = positiveInteger(queryText('page_size'), getPersistedPageSize(20))
  sortBy.value = ['created_at', 'result', 'target'].includes(nextSort) ? nextSort as NonNullable<AuditFilters['sortBy']> : undefined
  sortOrder.value = queryText('sort_order') === 'asc' ? 'asc' : 'desc'
}

async function syncRouteState() {
  if (!route || !router) return
  const query: LocationQueryRaw = { ...route.query }
  const values: Record<string, string | undefined> = {
	category: activeFilters.category === 'security' ? undefined : activeFilters.category,
    actor: String(activeFilters.actor || '').trim() || undefined,
    target: String(activeFilters.target || '').trim() || undefined,
    action: String(activeFilters.action || '') || undefined,
    result: String(activeFilters.result || '') || undefined,
    from: String(activeFilters.from || '') || undefined,
    to: String(activeFilters.to || '') || undefined,
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

function errorMessage(err: unknown) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed') }
function requestFilters(): AuditFilters {
  const result: AuditFilters = { category: activeFilters.category || 'security', action: activeFilters.action, result: activeFilters.result, page: page.value, pageSize: pageSize.value }
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
async function applyFilters() { Object.assign(activeFilters, draft); page.value = 1; await syncRouteState(); await loadAudit() }
const { schedule: scheduleFilters, runNow: runFiltersNow } = useDebouncedAction(applyFilters, 300)
function setTextFilter(key: 'actor' | 'target', value: string) { draft[key] = value; scheduleFilters() }
function setFilter(key: 'action' | 'result', value: string | number | boolean | null) { Object.assign(draft, { [key]: String(value ?? '') }); void runFiltersNow() }
function setCategory(category: NonNullable<AuditFilters['category']>) { draft.category = category; draft.action = ''; expandedSensitiveIDs.value = new Set(); void runFiltersNow() }
function setDateRange(range: { startDate: string; endDate: string }) { draft.from = range.startDate; draft.to = range.endDate; void runFiltersNow() }
async function setMobileSort(value: string | number | boolean | null) {
  if (!value || typeof value === 'boolean') sortBy.value = undefined
  else {
    const [field, order] = String(value).split(':')
    sortBy.value = field as NonNullable<AuditFilters['sortBy']>
    sortOrder.value = order === 'asc' ? 'asc' : 'desc'
  }
  page.value = 1
  await syncRouteState()
  await loadAudit()
}
async function resetFilters() { Object.assign(draft, { action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' }); await runFiltersNow() }
async function changePage(next: number) { page.value = next; await syncRouteState(); await loadAudit() }
async function changePageSize(next: number) { pageSize.value = next; page.value = 1; await syncRouteState(); await loadAudit() }
async function handleTableSort(key: string, order: 'asc' | 'desc') {
  const next = ({ time: 'created_at', target: 'target', result: 'result' } as const)[key as 'time' | 'target' | 'result']
  if (!next) return
  sortBy.value = next
  sortOrder.value = order
  page.value = 1
  await syncRouteState()
  await loadAudit()
}
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function targetLabel(record: RiskAuditRecord) {
  const labels: Record<string, string> = { user: '用户', rule: '规则', identity_rule: '身份规则', risk_review_case: '复核案件', network_identity: '网络身份' }
  return `${labels[record.target_type || ''] || '其他目标'} ${record.target_id || (record.target_user_id ? `#${record.target_user_id}` : '-')}`
}
function statusChange(record: RiskAuditRecord) { return record.before_status && record.after_status ? `${formatAccountStatus(record.before_status)} → ${formatAccountStatus(record.after_status)}` : '-' }
function sensitiveExpanded(id: number) { return expandedSensitiveIDs.value.has(id) }
function toggleSensitive(id: number) { const next = new Set(expandedSensitiveIDs.value); next.has(id) ? next.delete(id) : next.add(id); expandedSensitiveIDs.value = next }
function formatSensitiveSection(record: RiskAuditRecord) {
  const section = String(record.metadata?.section || '')
  return ({ 'identity-summary': '身份摘要', 'ip-identities': 'IP 身份', 'device-identities': '设备身份', 'associated-users': '关联账号' } as Record<string, string>)[section] || ''
}
restoreRouteState()
watch(() => route?.fullPath, () => {
  if (writingQuery) return
  restoreRouteState()
  void loadAudit()
})
onMounted(loadAudit)
</script>
