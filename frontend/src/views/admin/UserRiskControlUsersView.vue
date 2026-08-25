<template>
  <TablePageLayout :title="t('admin.userRiskControl.usersTitle')" :description="t('admin.userRiskControl.usersDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
          <div class="flex items-center justify-between gap-3">
            <div v-if="error" class="min-w-0 flex-1 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
            <div v-else class="flex-1" />
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="refreshWorkspace">
              <Icon name="refresh" size="sm" />
              {{ t('admin.userRiskControl.refresh') }}
            </button>
          </div>
          <div v-if="batchResults.length && batchResultsVisible" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/50" data-testid="batch-result-summary">
            <div class="flex flex-wrap items-center justify-between gap-3">
			  <div><strong class="text-sm text-gray-900 dark:text-white">{{ formatAuditResult(batchSummary) }}</strong><p class="mt-1 text-xs text-gray-500">成功 {{ batchSuccessCount }} · 部分完成 {{ batchPartialCount }} · 失败 {{ batchFailedCount }}（共 {{ batchResults.length }}）</p></div>
			  <button v-if="batchRetryableCount" type="button" class="btn btn-secondary btn-sm" :disabled="batchSaving" data-testid="retry-batch-session-revocations" @click="retryBatchCleanup">重试 {{ batchRetryableCount }} 个未完成步骤</button>
              <button type="button" class="btn btn-ghost btn-icon" :aria-label="t('common.close')" @click="closeBatchResults"><Icon name="x" size="sm" /></button>
            </div>
            <ul class="mt-3 space-y-2 text-sm">
              <li v-for="result in batchResults" :key="result.id" class="flex flex-wrap gap-x-2 gap-y-1">
				<span class="max-w-72 break-all font-medium text-gray-900 dark:text-white">{{ result.user?.email || result.user?.username || `账号 #${result.id}` }}</span>
				<span :class="result.status === 'success' ? 'text-emerald-600' : result.status === 'partial' ? 'text-amber-600' : 'text-red-600'">{{ formatAuditResult(result.status) }}</span>
                <span v-if="result.reason" class="text-gray-600 dark:text-gray-300">{{ result.reason }}</span>
              </li>
            </ul>
          </div>
		  <div v-else-if="batchRetryableCount" class="flex flex-wrap items-center justify-between gap-3 border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-300" data-testid="batch-recovery-reminder"><span>{{ batchRetryableCount }} 个账号仍有步骤未完成</span><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" @click="batchResultsVisible = true">查看</button><button type="button" class="btn btn-secondary btn-sm" :disabled="batchSaving" @click="retryBatchCleanup">立即重试</button></div></div>
        </div>
      </template>

      <template #filters>
			<section class="mb-2 flex overflow-x-auto border-y border-gray-200 py-1 sm:grid sm:grid-cols-4 sm:overflow-visible dark:border-dark-700" data-testid="risk-work-overview" aria-live="polite">
				<button v-for="item in workOverviewItems" :key="item.key" type="button" class="flex min-h-10 min-w-48 items-center justify-between gap-2 px-3 py-1.5 text-left text-sm hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-70 sm:min-w-0 dark:hover:bg-dark-800" :disabled="!workOverviewLoaded || workOverviewLoading" :data-testid="`work-count-${item.key}`" @click="openWorkView(item.key)"><span class="whitespace-nowrap text-gray-600 dark:text-gray-300">{{ item.label }}</span><strong class="text-gray-900 dark:text-white">{{ item.count }}</strong></button>
			</section>
			<div v-if="workOverviewError" class="mb-2 flex items-center justify-between gap-3 text-xs text-amber-700 dark:text-amber-300" data-testid="work-overview-error" role="alert"><span>{{ workOverviewLoaded ? '工作概览刷新失败，当前数量可能已过期' : `工作概览不可用：${workOverviewError}` }}</span><button type="button" class="font-medium underline" data-testid="retry-work-overview" :disabled="workOverviewLoading" @click="loadWorkOverview">重试</button></div>
			<div class="mb-3 inline-flex border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-800" role="tablist" aria-label="工作模式" data-testid="risk-work-modes">
				<button type="button" role="tab" class="min-h-9 px-3 text-sm font-medium" :class="mode === 'queue' ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-selected="mode === 'queue'" @click="setMode('queue')">处置队列</button>
				<button type="button" role="tab" class="min-h-9 px-3 text-sm font-medium" :class="mode === 'users' ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-selected="mode === 'users'" @click="setMode('users')">用户查询</button>
			</div>
			<div v-if="mode === 'queue'" class="mb-3 ml-0 inline-flex max-w-full overflow-x-auto border-b border-gray-200 dark:border-dark-700 sm:ml-3" role="tablist" aria-label="处置队列" data-testid="risk-case-views">
				<button v-for="option in caseViews" :key="option.value" type="button" role="tab" class="min-h-9 whitespace-nowrap px-3 text-sm font-medium" :class="view === option.value ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-selected="view === option.value" @click="setView(option.value)">{{ option.label }}</button>
			</div>
			<section class="flex flex-wrap items-center gap-3" data-testid="primary-risk-filters">
          <SearchInput
            :model-value="draft.search || ''"
            :placeholder="t('admin.userRiskControl.searchPlaceholder')"
            class="w-full sm:w-72"
            data-testid="risk-user-search"
            @update:model-value="updateSearch"
            @search="runFiltersNow"
          />
          <Select :model-value="draft.status || ''" class="w-[calc(50%-0.375rem)] sm:w-40" data-testid="account-status-filter" :options="accountStatusFilterOptions" @update:model-value="setFilter('status', $event)" />
          <Select :model-value="draft.riskLevel || ''" class="w-[calc(50%-0.375rem)] sm:w-40" data-testid="risk-level-filter" :options="riskLevelFilterOptions" @update:model-value="setFilter('riskLevel', $event)" />
          <label class="flex h-10 w-[calc(50%-0.375rem)] items-center gap-2 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300 sm:w-auto">
            <Toggle :model-value="Boolean(draft.riskOnly)" data-testid="risk-only-filter" @update:model-value="setRiskOnly" />
            只看有风险记录
          </label>
          <Select :model-value="mobileSortValue" class="w-[calc(50%-0.375rem)] md:hidden" data-testid="mobile-user-sort" :options="mobileSortOptions" @update:model-value="setMobileSort" />
          <button type="button" class="btn btn-ghost btn-icon hidden sm:inline-flex" :disabled="!hasFilters || loading" title="重置筛选" aria-label="重置筛选" data-testid="reset-filters" @click="resetFilters">
            <Icon name="x" size="md" />
          </button>
		  <button type="button" class="btn btn-ghost btn-sm sm:hidden" :disabled="!hasFilters || loading" data-testid="mobile-reset-filters" @click="resetFilters"><Icon name="x" size="sm" />重置</button>
          </section>
			<details class="mt-3 border-t border-gray-200 pt-3 dark:border-dark-700" data-testid="advanced-risk-filters">
				<summary class="cursor-pointer text-sm font-medium text-gray-600 dark:text-gray-300">高级筛选</summary>
				<div class="mt-3 flex flex-wrap items-center gap-3">
					<Select :model-value="draft.riskType || ''" class="w-full sm:w-52" data-testid="risk-type-filter" :options="riskTypeFilterOptions" @update:model-value="setFilter('riskType', $event)" />
					<Select v-if="mode === 'users'" :model-value="draft.processingStatus || ''" class="w-full sm:w-44" data-testid="processing-status-filter" :options="processingStatusFilterOptions" @update:model-value="setFilter('processingStatus', $event)" />
					<div class="flex w-full items-center gap-2 sm:w-auto" aria-label="风险分范围"><input v-model.number="draft.minScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-24 sm:flex-none" placeholder="最低分" data-testid="min-score-filter" @input="scheduleFilters" /><span class="text-sm text-gray-400">至</span><input v-model.number="draft.maxScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-24 sm:flex-none" placeholder="最高分" data-testid="max-score-filter" @input="scheduleFilters" /></div>
				</div>
			</details>
      </template>

      <template #table>
        <div class="flex min-h-0 w-full flex-1 flex-col gap-3" data-testid="risk-users-table">
		  <label v-if="users.length" class="flex min-h-10 items-center gap-2 border-b border-gray-200 px-1 text-sm text-gray-600 dark:border-dark-700 dark:text-gray-300 md:hidden" data-testid="mobile-select-current-page"><input v-model="allSelected" type="checkbox" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" />选择当前页</label>
          <section v-if="selectedIds.size" class="flex flex-col gap-3 rounded-lg border border-primary-200 bg-primary-50 p-3 dark:border-primary-700/40 dark:bg-primary-900/20 sm:flex-row sm:items-center sm:justify-between" data-testid="batch-action-bar">
			<div><span class="text-sm font-medium text-primary-800 dark:text-primary-200" data-testid="selected-count">已选择 {{ selectedIds.size }} 个账号</span><p class="mt-1 text-xs text-primary-700 dark:text-primary-300">可封禁 {{ batchBanUsers.length }} · 可解封 {{ batchUnbanUsers.length }}<span v-if="batchIneligibleCount"> · 暂不可操作 {{ batchIneligibleCount }}</span></p></div>
            <div class="flex flex-wrap gap-2">
			  <button type="button" class="btn btn-danger btn-sm" data-testid="batch-ban" :disabled="!canBatchBan" @click="openBatchAction('disabled')">{{ formatRiskAction('ban') }} {{ batchBanUsers.length }}</button>
			  <button type="button" class="btn btn-primary btn-sm" data-testid="batch-unban" :disabled="!canBatchUnban" @click="openBatchAction('active')">{{ formatRiskAction('unban') }} {{ batchUnbanUsers.length }}</button>
              <button type="button" class="btn btn-ghost btn-sm" data-testid="clear-selection" @click="clearSelection">取消选择</button>
            </div>
          </section>
          <DataTable :key="`risk-users-${tableSortKey}-${sortOrder}`" :columns="columns" :data="users" :loading="loading" row-key="id" :clickable-rows="true" :server-side-sort="true" :default-sort-key="tableSortKey" :default-sort-order="sortOrder" @row-click="selectedUser = $event" @sort="handleTableSort">
            <template #header-select><input v-model="allSelected" type="checkbox" data-testid="select-current-page" class="rounded border-gray-300 text-primary-600" aria-label="选择当前页" @click.stop /></template>
            <template #cell-select="{ row: user }"><input :checked="selectedIds.has(user.id)" type="checkbox" :data-testid="`user-select-${user.id}`" class="rounded border-gray-300 text-primary-600" :aria-label="`选择账号 ${user.email || user.username || user.id}`" @click.stop @change.stop="toggleSelection(user.id)" /></template>
			<template #cell-account="{ row: user }"><div class="min-w-0 max-w-[50vw] text-left sm:max-w-none" :data-testid="`user-row-${user.id}`"><p class="truncate font-medium text-gray-900 dark:text-white" :title="user.email || user.username || `用户 #${user.id}`" :data-testid="`account-primary-${user.id}`">{{ user.email || user.username || `用户 #${user.id}` }}</p><p class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400" :data-testid="`account-secondary-${user.id}`"><span v-if="user.username">{{ user.username }} · </span>#{{ user.id }}</p></div></template>
			<template #cell-accountStatus="{ row: user }"><span class="font-medium">{{ formatAccountStatus(user.status) }}</span></template>
			<template #cell-evaluation="{ row: user }"><span>{{ evaluationCoverageLabel(user) }}</span><span v-if="user.identity" class="mt-1 block text-xs text-gray-400">{{ user.identity.active_signal_count == null ? '有效身份信号待同步' : `${user.identity.active_signal_count} 条有效身份信号` }}</span></template>
			<template #cell-riskType="{ row: user }"><div class="max-w-sm whitespace-normal text-left"><p class="font-medium text-gray-800 dark:text-gray-200">{{ displayReason(user) }}</p><p class="mt-1 text-xs text-gray-400">证据强度：{{ evidenceStrengthLabel(user.evidence_strength) }}</p></div></template>
			<template #cell-riskScore="{ row: user }"><RiskScoreBadge :score="user.risk_score" :available="user.risk_score !== null && user.risk_score !== undefined && Boolean(user.risk_level)" :explicit-level="user.risk_level" /><span v-if="(user.historical_max_score || 0) > (user.risk_score || 0)" class="mt-1 block text-xs text-gray-400">历史最高 {{ user.historical_max_score }}</span></template>
            <template #cell-lastEvent="{ row: user }">{{ formatDate(user.last_event_at) }}</template>
			<template #cell-processing="{ row: user }"><span>{{ caseStatusLabel(user.case_status || user.processing_status) }}</span><span v-if="user.assignee_id" class="mt-1 block text-xs text-gray-400">处理人 #{{ user.assignee_id }}</span></template>
            <template #empty><EmptyState :title="t('admin.userRiskControl.empty')" /></template>
          </DataTable>
        </div>
      </template>

      <template #pagination><Pagination v-if="total" :page="page" :total="total" :page-size="pageSize" @update:page="changePage" @update:pageSize="changePageSize" /></template>
    </TablePageLayout>

    <UserRiskControlUserDrawer v-if="selectedUser" :user="selectedUser" @close="selectedUser = null" @case-claimed="handleCaseClaimed" @status-partial="handlePartialStatus" @status-recovery="handleStatusRecovery" @updated="handleUpdated" />
    <BaseDialog :show="Boolean(batchAction)" :title="batchDialogTitle" width="narrow" :close-on-click-outside="true" :z-index="80" @close="closeBatchAction">
	  <p class="text-sm text-gray-500 dark:text-gray-400">将处理 {{ batchTargetIds.length }} 个适用账号；跳过 {{ selectedIds.size - batchTargetIds.length }} 个其他状态账号。每个账号都会单独记录结果。</p>
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
import { userRiskControlV2API, type BatchStatusResult, type RiskCaseView, type RiskSortBy, type RiskUserRow, type UserRiskFilters } from '@/api/admin/userRiskControlV2'
import { accountStatusOptions, formatAccountStatus, formatAuditResult, formatIdentitySignal, formatRiskAction, formatRiskReason, identitySignalOptions, processingStatusOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

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
const pageSize = ref(responsivePageSize(getPersistedPageSize(20)))
const total = ref(0)
const sortBy = ref<RiskSortBy | undefined>('risk_score')
const sortOrder = ref<'asc' | 'desc'>('desc')
const view = ref<RiskCaseView>('users')
const batchAction = ref<'disabled' | 'active' | null>(null)
const batchReason = ref('')
const batchValidationError = ref('')
const batchResults = ref<BatchStatusResult[]>([])
const batchResultsVisible = ref(true)
const batchTargetIds = ref<number[]>([])
const workOverview = ref({ unassignedPending: 0, myInReview: 0, reviewDue: 0, allOpen: 0 })
const workOverviewLoaded = ref(false)
const workOverviewLoading = ref(true)
const workOverviewError = ref('')
let workOverviewRequestID = 0
const columns: Column[] = [
  { key: 'select', label: '选择', class: 'w-12 text-center' },
  { key: 'account', label: t('admin.userRiskControl.table.account') },
	{ key: 'accountStatus', label: '账号状态' },
	{ key: 'evaluation', label: '评估覆盖' },
	{ key: 'riskScore', label: '当前风险', sortable: true },
	{ key: 'riskType', label: '主信号' },
	{ key: 'lastEvent', label: '最近命中', sortable: true },
	{ key: 'processing', label: '案件状态' },
]

const caseViews: Array<{ value: RiskCaseView; label: string }> = [
	{ value: 'unassigned', label: '待领取' },
	{ value: 'mine', label: '我的处理中' },
	{ value: 'due', label: '待复查' },
	{ value: 'open', label: '全部未结' },
]
const mode = computed<'queue' | 'users'>(() => view.value === 'users' ? 'users' : 'queue')
const draft = reactive<UserRiskFilters>({ search: '', status: '', riskType: '', riskLevel: '', processingStatus: '', pendingOnly: false, riskOnly: false })
const activeFilters = reactive<UserRiskFilters>({ ...draft })
let loadRequestID = 0
let writingQuery = false

const accountStatusFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allStatuses') }, ...accountStatusOptions])
const riskTypeFilterOptions = computed(() => [{ value: '', label: '全部主信号' }, ...riskTypeOptions, ...identitySignalOptions])
const riskLevelFilterOptions = computed(() => [{ value: '', label: t('admin.userRiskControl.allRiskLevels') }, ...riskLevelOptions])
const processingStatusFilterOptions = computed(() => [{ value: '', label: '全部处理状态' }, ...processingStatusOptions])
const workOverviewItems = computed(() => [
	{ key: 'unassigned', label: '待领取', count: workOverviewLoaded.value ? workOverview.value.unassignedPending : '—' },
	{ key: 'mine', label: '我的处理中', count: workOverviewLoaded.value ? workOverview.value.myInReview : '—' },
	{ key: 'due', label: '待复查', count: workOverviewLoaded.value ? workOverview.value.reviewDue : '—' },
	{ key: 'open', label: '全部未结', count: workOverviewLoaded.value ? workOverview.value.allOpen : '—' },
])
const mobileSortOptions = [
  { value: 'risk_score:desc', label: '风险优先' },
  { value: 'risk_score:asc', label: '风险分：低到高' },
  { value: 'last_event_at:desc', label: '最近命中：新到旧' },
  { value: 'last_event_at:asc', label: '最近命中：旧到新' },
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
const selectedUsers = computed(() => users.value.filter((user) => selectedIds.value.has(user.id)))
const accountRecoveryUserIds = ref(new Set<number>())
const unresolvedBatchUserIds = computed(() => new Set([...accountRecoveryUserIds.value, ...batchResults.value.filter((result) => result.status === 'partial' && result.retryable && Boolean(result.pendingStep)).map((result) => result.id)]))
const batchBanUsers = computed(() => selectedUsers.value.filter((user) => user.status === 'active' && !unresolvedBatchUserIds.value.has(user.id)))
const batchUnbanUsers = computed(() => selectedUsers.value.filter((user) => user.status === 'disabled' && !unresolvedBatchUserIds.value.has(user.id)))
const batchIneligibleCount = computed(() => selectedUsers.value.filter((user) => unresolvedBatchUserIds.value.has(user.id) || (user.status !== 'active' && user.status !== 'disabled')).length)
const canBatchBan = computed(() => batchBanUsers.value.length > 0)
const canBatchUnban = computed(() => batchUnbanUsers.value.length > 0)
const batchSuccessCount = computed(() => batchResults.value.filter((result) => result.status === 'success').length)
const batchPartialCount = computed(() => batchResults.value.filter((result) => result.status === 'partial').length)
const batchFailedCount = computed(() => batchResults.value.filter((result) => result.status === 'failed').length)
const batchRetryableCount = computed(() => batchResults.value.filter((result) => result.status === 'partial' && result.retryable && Boolean(result.pendingStep)).length)
const batchSummary = computed(() => batchSuccessCount.value === batchResults.value.length ? 'success' : batchResults.value.some((result) => result.status === 'partial' || result.status === 'success') ? 'partial' : 'failed')
const batchRecoveryStorageKey = 'sub2api:risk-batch-recovery'
function accountRecoveryStorageKey(id: number) { return `sub2api:risk-account-recovery:${id}` }
function isPendingBatchResult(item: BatchStatusResult) { return item.status === 'partial' && item.retryable && Boolean(item.pendingStep) }
function mergeBatchResults(items: BatchStatusResult[]) {
	const incoming = new Set(items.map((item) => item.id))
	return [...batchResults.value.filter((item) => isPendingBatchResult(item) && !incoming.has(item.id)), ...items]
}
function persistBatchRecovery(): boolean {
	const keys = [...new Set([batchRecoveryStorageKey, ...batchResults.value.map((item) => accountRecoveryStorageKey(item.id))])]
	const previous = new Map<string, string | null>()
	try {
		keys.forEach((key) => previous.set(key, sessionStorage.getItem(key)))
		const pending = batchResults.value.filter(isPendingBatchResult)
		if (pending.length) sessionStorage.setItem(batchRecoveryStorageKey, JSON.stringify(pending))
		else sessionStorage.removeItem(batchRecoveryStorageKey)
		const nextRecoveryIDs = new Set(accountRecoveryUserIds.value)
		batchResults.value.forEach((item) => {
			const key = accountRecoveryStorageKey(item.id)
			if (item.status === 'partial' && item.retryable && item.pendingStep && item.operationReason && item.requestId && item.requestedStatus) {
				sessionStorage.setItem(key, JSON.stringify({ reason: item.operationReason, requestId: item.requestId, status: item.requestedStatus, pendingStep: item.pendingStep, batchId: item.batchId }))
				nextRecoveryIDs.add(item.id)
			} else { sessionStorage.removeItem(key); nextRecoveryIDs.delete(item.id) }
		})
		accountRecoveryUserIds.value = nextRecoveryIDs
		return true
	} catch {
		try {
			keys.forEach((key) => sessionStorage.removeItem(key))
			previous.forEach((value, key) => { if (value !== null) sessionStorage.setItem(key, value) })
		} catch (rollbackError) { void rollbackError }
		return false
	}
}
function restoreAccountRecoveryUserIds() {
	const restored = new Set<number>()
	try {
		for (let index = 0; index < sessionStorage.length; index++) {
			const key = sessionStorage.key(index) || ''
			const match = key.match(/^sub2api:risk-account-recovery:(\d+)$/)
			if (!match) continue
			const value = JSON.parse(sessionStorage.getItem(key) || '{}') as { reason?: string; requestId?: string; status?: string; pendingStep?: string }
			if (value.reason && value.requestId && (value.status === 'active' || value.status === 'disabled') && value.pendingStep) restored.add(Number(match[1]))
		}
	} catch (error) { void error }
	accountRecoveryUserIds.value = restored
}
function restoreBatchRecovery() {
	try {
		const raw = sessionStorage.getItem(batchRecoveryStorageKey)
		const items = raw ? JSON.parse(raw) as BatchStatusResult[] : []
		if (Array.isArray(items) && items.length) { batchResults.value = items; persistBatchRecovery() }
	} catch (error) { void error }
}
function closeBatchResults() { batchResultsVisible.value = false; if (!batchRetryableCount.value) batchResults.value = [] }
const batchDialogTitle = computed(() => batchAction.value === 'disabled' ? '确认批量封禁' : '确认批量解封')

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
	const legacyView = ({ pending: 'unassigned', my: 'mine', observing: 'due', all: 'users', resolved: 'users' } as Record<string, RiskCaseView>)[queryText('view')]
	const requestedView = (legacyView || queryText('view')) as RiskCaseView
	view.value = requestedView === 'users' || caseViews.some((option) => option.value === requestedView) ? requestedView : 'users'
  const nextSort = queryText('sort_by')
  const allowedSorts: RiskSortBy[] = ['risk_score', 'risk_level', 'event_count', 'last_event_at', 'created_at']
  Object.assign(draft, {
    search: queryText('search'),
    status: queryText('status'),
    riskType: queryText('risk_type'),
    riskLevel: queryText('risk_level'),
	processingStatus: view.value === 'users' ? queryText('processing_status') : '',
    riskOnly: queryText('risk_only') === 'true',
    minScore: normalizeScore(queryText('min_score')),
    maxScore: normalizeScore(queryText('max_score')),
  })
  Object.assign(activeFilters, draft)
  page.value = positiveInteger(queryText('page'), 1)
  pageSize.value = responsivePageSize(positiveInteger(queryText('page_size'), getPersistedPageSize(20)))
  sortBy.value = allowedSorts.includes(nextSort as RiskSortBy) ? nextSort as RiskSortBy : 'risk_score'
  sortOrder.value = queryText('sort_order') === 'asc' ? 'asc' : 'desc'
}

async function syncRouteState() {
  if (!route || !router) return
  const query: LocationQueryRaw = { ...route.query }
  const values: Record<string, string | undefined> = {
		view: view.value === 'users' ? undefined : view.value,
    search: String(activeFilters.search || '') || undefined,
    status: String(activeFilters.status || '') || undefined,
    risk_type: String(activeFilters.riskType || '') || undefined,
    risk_level: String(activeFilters.riskLevel || '') || undefined,
	processing_status: view.value === 'users' ? String(activeFilters.processingStatus || '') || undefined : undefined,
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
		const response = await userRiskControlV2API.listUsers({ ...activeFilters, view: view.value, page: page.value, pageSize: pageSize.value, sortBy: sortBy.value, sortOrder: sortOrder.value })
    if (requestID !== loadRequestID) return
    users.value = response.items
    total.value = response.total
	} catch (err) {
		if (requestID === loadRequestID) {
			users.value = []
			total.value = 0
			clearSelection()
			error.value = errorMessage(err)
		}
  } finally {
    if (requestID === loadRequestID) loading.value = false
  }
}

async function loadWorkOverview() {
	const requestID = ++workOverviewRequestID
	workOverviewLoading.value = true
	workOverviewError.value = ''
	try {
		const result = await userRiskControlV2API.getWorkOverview()
		if (requestID !== workOverviewRequestID) return
		workOverview.value = result
		workOverviewLoaded.value = true
	} catch (err) {
		if (requestID === workOverviewRequestID) workOverviewError.value = errorMessage(err)
	} finally {
		if (requestID === workOverviewRequestID) workOverviewLoading.value = false
	}
}

async function refreshWorkspace() { await Promise.all([loadUsers(), loadWorkOverview()]) }

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

async function setView(next: RiskCaseView) {
	if (view.value === next || loading.value) return
	view.value = next
	clearProcessingFilterForQueue()
	page.value = 1
	clearSelection()
	await syncRouteState()
	await loadUsers()
}

async function setMode(next: 'queue' | 'users') {
	if (mode.value === next || loading.value) return
	view.value = next === 'users' ? 'users' : 'unassigned'
	if (next === 'queue') clearProcessingFilterForQueue()
	page.value = 1
	clearSelection()
	await syncRouteState()
	await loadUsers()
}

async function openWorkView(key: string) {
	if (!caseViews.some((option) => option.value === key)) return
	view.value = key as RiskCaseView
	clearProcessingFilterForQueue()
  page.value = 1
  clearSelection()
  await syncRouteState()
  await loadUsers()
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
	pageSize.value = responsivePageSize(next)
  page.value = 1
  clearSelection()
  await syncRouteState()
  await loadUsers()
}
function isNarrowViewport() { return typeof window !== 'undefined' && window.innerWidth > 0 && window.innerWidth < 768 }
function responsivePageSize(value: number) { return isNarrowViewport() ? Math.min(value, 20) : value }

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
function clearProcessingFilterForQueue() { draft.processingStatus = ''; activeFilters.processingStatus = '' }
function displayReason(user: RiskUserRow) { return user.risk_type?.startsWith('v2_') ? formatIdentitySignal(user.risk_type) : formatRiskReason(user.risk_reason, { eventType: user.risk_type || undefined, count: user.event_count }) }
function evidenceStrengthLabel(value?: string) { return ({ observation: '仅观察', weak: '弱', medium_high: '中高', high: '高' } as Record<string, string>)[value || ''] || '未评估' }
function evaluationCoverageLabel(user: RiskUserRow) { const state = user.identity?.quality_state; return ({ healthy: '评估完整', degraded: '部分覆盖', not_evaluable: '不可评估', paused: '质量保护暂停', disabled: '评估关闭' } as Record<string, string>)[state || ''] || '尚无身份数据' }
function caseStatusLabel(value?: string | null) { return ({ pending: '待领取', in_review: '处理中', observing: '待复查', resolved: '已结案' } as Record<string, string>)[value || ''] || '无案件' }
function formatDate(value?: string | null) { return value ? new Date(value).toLocaleString() : '-' }
function handleUpdated(updated: RiskUserRow) {
  const index = users.value.findIndex((user) => user.id === updated.id)
  if (index >= 0) users.value[index] = { ...users.value[index], ...updated }
	void Promise.all([loadUsers(), loadWorkOverview()])
}
function handleCaseClaimed(updated: RiskUserRow) {
  const index = users.value.findIndex((user) => user.id === updated.id)
  if (index >= 0) users.value[index] = { ...users.value[index], ...updated }
	void Promise.all([loadUsers(), loadWorkOverview()])
}
function handlePartialStatus(updated: RiskUserRow) {
	const index = users.value.findIndex((user) => user.id === updated.id)
	if (index >= 0) users.value[index] = { ...users.value[index], ...updated }
}
function handleStatusRecovery(userID: number, pending: boolean) {
	const next = new Set(accountRecoveryUserIds.value)
	if (pending) next.add(userID)
	else {
		next.delete(userID)
		batchResults.value = batchResults.value.filter((item) => item.id !== userID || !isPendingBatchResult(item))
		persistBatchRecovery()
	}
	accountRecoveryUserIds.value = next
}
function openBatchAction(action: 'disabled' | 'active') {
  if ((action === 'disabled' && !canBatchBan.value) || (action === 'active' && !canBatchUnban.value)) return
  batchAction.value = action
	batchTargetIds.value = (action === 'disabled' ? batchBanUsers.value : batchUnbanUsers.value).map((user) => user.id)
  batchReason.value = ''
  batchValidationError.value = ''
}
function closeBatchAction() { if (!batchSaving.value) { batchAction.value = null; batchTargetIds.value = [] } }
async function confirmBatchAction() {
  const reason = batchReason.value.trim()
  if (!reason) {
    batchValidationError.value = '操作原因不能为空或仅包含空格。'
    return
  }
  if (!batchAction.value) return
  batchSaving.value = true
  batchValidationError.value = ''
	const ids = [...batchTargetIds.value]
  try {
		const results = await userRiskControlV2API.batchSetUserStatus(ids, batchAction.value, reason, 4, (prepared) => {
			const previous = batchResults.value
			batchResults.value = mergeBatchResults(prepared)
			batchResultsVisible.value = true
			if (!persistBatchRecovery()) {
				batchResults.value = previous
				batchResultsVisible.value = previous.length > 0
				throw new Error('无法保存批量恢复信息，请检查浏览器会话存储后重试')
			}
		})
		batchResults.value = mergeBatchResults(results)
		batchResultsVisible.value = true
		if (!persistBatchRecovery()) batchValidationError.value = '操作已完成，但浏览器无法更新恢复信息；请保持当前页面并核对结果。'
    clearSelection()
    batchAction.value = null
		batchTargetIds.value = []
    await loadUsers()
  } catch (err) {
    batchValidationError.value = errorMessage(err)
  } finally {
    batchSaving.value = false
  }
}
async function retryBatchCleanup() {
	if (!batchRetryableCount.value || batchSaving.value) return
	batchSaving.value = true
	try {
		batchResults.value = await userRiskControlV2API.retryBatchSessionRevocations(batchResults.value)
		persistBatchRecovery()
		await loadUsers()
	} catch (err) {
		error.value = errorMessage(err)
	} finally { batchSaving.value = false }
}

restoreRouteState()
restoreAccountRecoveryUserIds()
restoreBatchRecovery()
watch(() => route?.fullPath, () => {
  if (writingQuery) return
  restoreRouteState()
  clearSelection()
  void loadUsers()
})
onMounted(refreshWorkspace)
</script>
