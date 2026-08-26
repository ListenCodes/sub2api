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
		  <p v-if="draft.category === 'sensitive'" class="text-xs text-gray-500 dark:text-gray-400" data-testid="sensitive-audit-scope">按一次抽屉查看会话合并记录分区，不展示完整 IP 或原始会话标识。</p>
          <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            :model-value="draft.actor || ''"
            class="w-full sm:w-56"
            data-testid="audit-actor-filter"
            placeholder="管理员账号或 ID"
            @update:model-value="setTextFilter('actor', $event)"
            @search="runFiltersNow"
          />
          <SearchInput
            :model-value="draft.target || ''"
            class="w-full sm:w-64"
            data-testid="audit-target-filter"
			:placeholder="auditTargetPlaceholder"
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
        <DataTable :key="`risk-audit-${tableSortKey}-${sortOrder}`" class="audit-table" :columns="columns" :data="records" :loading="loading" row-key="id" :server-side-sort="true" :default-sort-key="tableSortKey" :default-sort-order="sortOrder" @sort="handleTableSort">
          <template #cell-time="{ row: record }">{{ formatDate(record.created_at) }}</template>
          <template #cell-actor="{ row: record }"><div class="max-w-52 whitespace-normal break-all"><p class="font-medium text-gray-900 dark:text-white">{{ accountPrimary(record.actor_account) || '管理员账号不可用' }}</p><p class="mt-0.5 text-xs text-gray-400">{{ record.actor ? `管理员 #${record.actor}` : 'ID 未知' }}</p></div></template>
          <template #cell-action="{ row: record }"><span class="font-medium text-gray-900 dark:text-white">{{ formatRiskAction(record.action) }}</span></template>
          <template #cell-target="{ row: record }">{{ targetLabel(record) }}</template>
          <template #cell-statusChange="{ row: record }"><span :data-testid="`audit-status-change-${record.id}`">{{ statusChange(record) }}</span></template>
          <template #cell-result="{ row: record }"><span :class="record.result === 'success' ? 'text-emerald-600 dark:text-emerald-400' : record.result === 'partial' ? 'text-amber-600 dark:text-amber-400' : 'text-red-600 dark:text-red-400'">{{ formatAuditResult(record.result) }}</span></template>
          <template #cell-reason="{ row: record }">
            <div class="max-w-xl whitespace-normal break-words text-left">
			  <div v-if="record.action === 'view_identity_detail'"><button type="button" class="inline-flex items-center gap-1 text-sm font-medium text-gray-700 dark:text-gray-200" :title="sensitiveExpanded(record.id) ? '折叠详情' : '展开详情'" :aria-label="sensitiveExpanded(record.id) ? '折叠详情' : '展开详情'" :data-testid="`toggle-sensitive-audit-${record.id}`" @click="toggleSensitive(record.id)"><Icon :name="sensitiveExpanded(record.id) ? 'chevronDown' : 'chevronRight'" size="sm" />{{ sensitiveSummary(record) }}</button><p v-if="sensitiveExpanded(record.id)" class="mt-1 text-xs text-gray-500">查看分区：{{ formatSensitiveSection(record) || '其他身份分区' }}</p></div>
			  <template v-else><p>{{ record.reason || '无操作原因' }}</p><p v-if="record.metadata?.diff" class="mt-1 text-xs text-gray-500">字段变化：{{ formatRuleDiff(record.metadata.diff) }}</p></template>
			  <p v-if="record.failure_reason && (record.action !== 'view_identity_detail' || record.result !== 'success')" class="mt-1 text-red-600 dark:text-red-400">失败原因：{{ record.action === 'view_identity_detail' ? sensitiveFailureLabel(record) : record.failure_reason }}</p>
              <button v-if="hasTechnicalDetails(record)" type="button" class="mt-1 text-xs font-medium text-gray-500 underline" :data-testid="`audit-technical-${record.id}`" @click="toggleTechnical(record.id)">技术详情</button>
              <p v-if="technicalExpanded(record.id)" class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ technicalDetails(record) }}</p>
            </div>
          </template>
		  <template #empty><EmptyState :title="auditEmptyTitle" /></template>
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
import { auditResultOptions, formatAccountStatus, formatAuditResult, formatRiskAction, formatRiskLevel, formatRiskType, riskActionOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const route = inject(routeLocationKey, null)
const router = inject(routerKey, null)
const records = ref<RiskAuditRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(responsivePageSize(getPersistedPageSize(20)))
const loading = ref(true)
const error = ref('')
const sortBy = ref<AuditFilters['sortBy']>()
const sortOrder = ref<'asc' | 'desc'>('desc')
const draft = reactive<AuditFilters>({ category: 'disposition', action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' })
const activeFilters = reactive<AuditFilters>({ ...draft })
const expandedSensitiveIDs = ref<Set<number>>(new Set())
const expandedTechnicalIDs = ref<Set<number>>(new Set())
const columns: Column[] = [
  { key: 'time', label: t('admin.userRiskControl.table.time'), sortable: true, class: 'w-28 min-w-24 whitespace-normal' },
  { key: 'actor', label: t('admin.userRiskControl.table.actor'), class: 'w-36 min-w-32 whitespace-normal' },
	{ key: 'action', label: '操作类型', class: 'w-28 min-w-24 whitespace-normal break-all' },
  { key: 'target', label: t('admin.userRiskControl.table.target'), sortable: true, class: 'w-40 min-w-32 whitespace-normal break-all' },
  { key: 'statusChange', label: t('admin.userRiskControl.table.statusChange'), class: 'w-24 min-w-20 whitespace-normal' },
  { key: 'result', label: t('admin.userRiskControl.table.result'), sortable: true, class: 'w-16 min-w-14' },
  { key: 'reason', label: t('admin.userRiskControl.table.reason'), class: 'w-44 min-w-36 whitespace-normal' },
]
let loadRequestID = 0
let writingQuery = false
const auditCategories: Array<{ value: NonNullable<AuditFilters['category']>; label: string }> = [
	{ value: 'disposition', label: '处置' },
	{ value: 'configuration', label: '配置变更' },
	{ value: 'testing', label: '测试与模拟' },
	{ value: 'sensitive', label: '敏感查询' },
]
const auditActionsByCategory: Record<NonNullable<AuditFilters['category']>, string[]> = {
	disposition: ['ban', 'unban', 'auto_ban', 'identity_reject_candidate', 'mark_processed', 'create_risk_review_case', 'claim_risk_review_case', 'observe_risk_review_case', 'review_risk_case', 'resolve_risk_review_case'],
	configuration: ['create_rule', 'update_rule', 'publish_identity_rule', 'enable_identity_rule', 'disable_identity_rule', 'rollback_identity_rule', 'purge_legacy_v1', 'identity_rebuild', 'label_shared_network', 'revoke_shared_network_label'],
	testing: ['rule_test', 'simulate_identity_rule', 'identity_rebuild_dry_run'],
  sensitive: ['view_identity_detail'],
}
const auditActionFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allActions') }, ...riskActionOptions.filter((option) => auditActionsByCategory[draft.category || 'disposition'].includes(option.value))])
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
const auditTargetPlaceholder = computed(() => draft.category === 'disposition' ? '目标账号、用户 ID 或案件 ID' : draft.category === 'configuration' ? '规则或网络标识' : draft.category === 'testing' ? '规则或模拟对象' : '用户账号或用户 ID')
const auditEmptyTitle = computed(() => hasFilters.value ? '当前筛选条件下没有匹配的操作记录' : ({ disposition: '暂无处置记录', configuration: '暂无配置变更记录', testing: '暂无测试与模拟记录', sensitive: '暂无敏感查询记录' } as Record<string, string>)[draft.category || 'disposition'])

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
	category: ['disposition', 'configuration', 'testing', 'sensitive'].includes(queryText('category')) ? queryText('category') as AuditFilters['category'] : ({ security: 'disposition', rules: 'configuration' } as Record<string, AuditFilters['category']>)[queryText('category')] || 'disposition',
    actor: queryText('actor'),
    target: queryText('target'),
    action: queryText('action'),
    result: queryText('result'),
    from: queryText('from'),
    to: queryText('to'),
  })
  Object.assign(activeFilters, draft)
  page.value = positiveInteger(queryText('page'), 1)
	pageSize.value = responsivePageSize(positiveInteger(queryText('page_size'), getPersistedPageSize(20)))
  sortBy.value = ['created_at', 'result', 'target'].includes(nextSort) ? nextSort as NonNullable<AuditFilters['sortBy']> : undefined
  sortOrder.value = queryText('sort_order') === 'asc' ? 'asc' : 'desc'
}

async function syncRouteState() {
  if (!route || !router) return
  const query: LocationQueryRaw = { ...route.query }
  const values: Record<string, string | undefined> = {
	category: activeFilters.category === 'disposition' ? undefined : activeFilters.category,
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
  const result: AuditFilters = { category: activeFilters.category || 'disposition', action: activeFilters.action, result: activeFilters.result, page: page.value, pageSize: pageSize.value }
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
	if (requestID === loadRequestID) { records.value = []; total.value = 0; error.value = errorMessage(err) }
  } finally {
    if (requestID === loadRequestID) loading.value = false
  }
}
async function applyFilters() { Object.assign(activeFilters, draft); page.value = 1; await syncRouteState(); await loadAudit() }
const { schedule: scheduleFilters, runNow: runFiltersNow } = useDebouncedAction(applyFilters, 300)
function setTextFilter(key: 'actor' | 'target', value: string) { draft[key] = value; scheduleFilters() }
function setFilter(key: 'action' | 'result', value: string | number | boolean | null) { Object.assign(draft, { [key]: String(value ?? '') }); void runFiltersNow() }
function setCategory(category: NonNullable<AuditFilters['category']>) { draft.category = category; draft.action = ''; draft.target = ''; expandedSensitiveIDs.value = new Set(); void runFiltersNow() }
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
async function changePageSize(next: number) { pageSize.value = responsivePageSize(next); page.value = 1; await syncRouteState(); await loadAudit() }
function isNarrowViewport() { return typeof window !== 'undefined' && window.innerWidth > 0 && window.innerWidth < 768 }
function responsivePageSize(value: number) { return isNarrowViewport() ? Math.min(value, 20) : value }
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
  if (record.target_type === 'user' && record.target_account) return `${accountPrimary(record.target_account) || '账号不可用'} · 账号 #${record.target_user_id || record.target_id}`
	if (record.target_type === 'user') return `账号信息不可用 · 账号 #${record.target_user_id || record.target_id || '-'}`
	if (record.target_type === 'identity_rule') return identityRuleTargetName(record.target_id)
  const labels: Record<string, string> = { user: '用户账号', rule: '事件规则', risk_review_case: '人工复核案件', network_identity: '网络身份证据', identity: '全部身份风险数据' }
	const label = labels[record.target_type || ''] || '其他操作目标'
	return record.target_id ? `${label} · #${record.target_id}` : label
}
function identityRuleTargetName(code?: string) { return ({ v2_registration_email_retries: '同邮箱重复注册尝试', v2_registration_ip_accounts: '同真实 IP 多账号注册', v2_registration_device_accounts: '同浏览器实例多账号注册', v2_registration_composite_accounts: '同 IP 与浏览器实例多账号注册', v2_api_client_accounts: 'API 客户端观察' } as Record<string, string>)[code || ''] || '身份关联规则' }
function technicalTargetID(record: RiskAuditRecord) { return record.target_type && record.target_type !== 'user' ? record.target_id || '' : '' }
function hasTechnicalDetails(record: RiskAuditRecord) { return Boolean(record.batch_id || record.request_id || technicalTargetID(record)) }
function technicalDetails(record: RiskAuditRecord) { return [`目标标识：${technicalTargetID(record) || '-'}`, `批次：${record.batch_id || '-'}`, `请求：${record.request_id || '-'}`].join(' · ') }
function accountPrimary(account?: RiskAuditRecord['actor_account']) { return account?.email || account?.username || '' }
function statusChange(record: RiskAuditRecord) { return record.before_status && record.after_status ? `${formatAccountStatus(record.before_status)} → ${formatAccountStatus(record.after_status)}` : '-' }
function sensitiveExpanded(id: number) { return expandedSensitiveIDs.value.has(id) }
function toggleSensitive(id: number) { const next = new Set(expandedSensitiveIDs.value); next.has(id) ? next.delete(id) : next.add(id); expandedSensitiveIDs.value = next }
function technicalExpanded(id: number) { return expandedTechnicalIDs.value.has(id) }
function toggleTechnical(id: number) { const next = new Set(expandedTechnicalIDs.value); next.has(id) ? next.delete(id) : next.add(id); expandedTechnicalIDs.value = next }
function formatSensitiveSection(record: RiskAuditRecord) {
	const labels = ({ 'identity-summary': '身份摘要', 'ip-identities': 'IP 身份', 'device-identities': '设备身份', 'associated-users': '关联账号' } as Record<string, string>)
	const sections = Array.isArray(record.metadata?.sections) ? record.metadata.sections.map(String) : [String(record.metadata?.section || '')]
	return [...new Set(sections.filter(Boolean).map((section) => labels[section] || '其他身份分区'))].join('、')
}
function sensitiveSectionCount(record: RiskAuditRecord) { const raw = Array.isArray(record.metadata?.sections) ? record.metadata.sections : [record.metadata?.section]; return new Set(raw.filter(Boolean).map(String)).size }
function sensitiveSummary(record: RiskAuditRecord) { const count = sensitiveSectionCount(record); return `查看身份详情 · ${count || 1} 个分区` }
function sensitiveFailureLabel(record: RiskAuditRecord) { return record.result === 'success' ? '' : '敏感详情查询未完成' }
function countStrategyValue(value: unknown) { return ({ user_events: '按用户事件计数', email_subject_events: '按邮箱主体事件计数', ip_distinct_success_users: 'IP 去重成功用户', browser_instance_distinct_success_users: '浏览器实例去重成功用户', api_client_distinct_users: 'API 客户端去重用户', ip_browser_cooccurrence: 'IP 与浏览器同现用户' } as Record<string, string>)[String(value || '')] || String(value ?? '-') }
function diffValue(key: string, value: unknown) { if (value == null || value === '') return '-'; if (key === 'enabled') return value === true || value === 'true' ? '已启用' : '已停用'; if (key === 'action' || key === 'configured_action') return formatRiskAction(String(value)); if (key === 'risk_level') return formatRiskLevel(String(value)); if (key === 'count_strategy') return countStrategyValue(value); if (key === 'event_types') return (Array.isArray(value) ? value : [value]).map((item) => formatRiskType(String(item))).join('、'); return String(value) }
function formatRuleDiff(value: unknown) { if (!value || typeof value !== 'object') return '-'; const labels: Record<string, string> = { enabled: '启用状态', window_seconds: '时间窗口', threshold: '阈值', score: '风险分', risk_level: '风险等级', action: '配置动作', configured_action: '配置动作', count_strategy: '计数口径', event_types: '事件类型', revision: '版本' }; return Object.entries(value as Record<string, unknown>).map(([key, raw]) => { const change = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {}; return `${labels[key] || key}：${diffValue(key, change.before)} → ${diffValue(key, change.after)}` }).join('；') || '-' }
restoreRouteState()
watch(() => route?.fullPath, () => {
  if (writingQuery) return
  restoreRouteState()
  void loadAudit()
})
onMounted(loadAudit)
</script>

<style scoped>
@media (min-width: 1200px) {
  :deep(.audit-table table) {
    min-width: 100% !important;
    table-layout: fixed;
  }

  :deep(.audit-table th),
  :deep(.audit-table td) {
    overflow-wrap: anywhere;
    vertical-align: top;
    white-space: normal !important;
  }
}
</style>
