<template>
    <TablePageLayout data-testid="account-monitor-panel" :title="t('admin.accountMonitor.title')" :description="t('admin.accountMonitor.description')">
      <template #actions>
        <div class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ lastUpdated ? `更新于 ${lastUpdated.toLocaleTimeString()}` : '尚未更新' }}</p>
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-secondary" data-testid="account-thresholds-open" @click="thresholdOpen = true"><Icon name="cog" size="sm" />阈值</button>
              <button type="button" class="btn btn-secondary" data-testid="account-rebuild-open" @click="rebuildOpen = true"><Icon name="database" size="sm" />历史重建</button>
              <button type="button" class="btn btn-primary" data-testid="account-monitor-refresh" :disabled="loading" @click="refresh"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />刷新</button>
            </div>
          </div>
          <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300" role="alert">{{ error }}</div>
          <AccountMonitorOverview :overview="overview" :quality="quality" />
        </div>
      </template>
		<template #filters><AccountMonitorFilters :state="state" :groups="groupOptions" @apply="setFilters" @reset="resetFilters" /></template>
      <template #table><AccountMonitorTable :accounts="accounts" :loading="loading" :sort-by="state.sortBy" :sort-order="state.sortOrder" @select="openAccount" @sort="sortAccounts" /></template>
		<template #pagination><Pagination v-if="total" :page="state.page" :total="total" :page-size="state.pageSize" @update:page="setPage" @update:pageSize="changePageSize" /></template>
    </TablePageLayout>
    <AccountMonitorDrawer :show="Boolean(selectedAccount)" :account="selectedAccount" :filters="requestFilters" :tab="state.detailTab" @close="closeAccount" @update:tab="changeDetailTab" />
    <AccountMonitorThresholdDialog :show="thresholdOpen" @close="thresholdOpen = false" @saved="refresh" />
    <AccountMonitorRebuildDialog :show="rebuildOpen" @close="rebuildOpen = false" />
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { accountMonitorAPI, type AccountDataQuality, type AccountFilters, type AccountMonitorAccount, type AccountMonitorOverview as AccountMonitorOverviewResponse, type AccountPageSize } from '@/api/admin/accountMonitor'
import AccountMonitorOverview from './AccountMonitorOverview.vue'
import AccountMonitorFilters from './AccountMonitorFilters.vue'
import AccountMonitorTable from './AccountMonitorTable.vue'
import AccountMonitorDrawer from './AccountMonitorDrawer.vue'
import AccountMonitorThresholdDialog from './AccountMonitorThresholdDialog.vue'
import AccountMonitorRebuildDialog from './AccountMonitorRebuildDialog.vue'
import { resolveTimeRange, useAccountMonitorFilters, type AccountDetailTab } from './useAccountMonitorFilters'

const { t } = useI18n()

const overview = ref<AccountMonitorOverviewResponse | null>(null)
const quality = ref<AccountDataQuality | null>(null)
const accounts = ref<AccountMonitorAccount[]>([])
const groupOptions = ref<AccountMonitorAccount['groups']>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const lastUpdated = ref<Date | null>(null)
const selectedAccount = ref<AccountMonitorAccount | null>(null)
const thresholdOpen = ref(false)
const rebuildOpen = ref(false)
let requestID = 0
const { state, refresh, setFilters, resetFilters, setPage, setPageSize, selectAccount, setDetailTab } = useAccountMonitorFilters(loadAll)
function accountGroups(items: AccountMonitorAccount[]) {
	const groups = new Map<number, AccountMonitorAccount['groups'][number]>()
	for (const account of items) for (const group of account.groups || []) groups.set(group.group_id, group)
	return [...groups.values()].sort((left, right) => left.platform.localeCompare(right.platform) || left.name.localeCompare(right.name) || left.group_id - right.group_id)
}
const requestFilters = computed<AccountFilters>(() => {
  const range = resolveTimeRange(state)
	return { ...range, page: state.page, pageSize: state.pageSize, sortBy: state.sortBy, sortOrder: state.sortOrder, platform: state.platform, query: state.query, accountID: state.accountID, parentAccountID: state.parentAccountID, accountStatus: state.accountStatus, model: state.model, userID: state.userID, apiKeyID: state.apiKeyID, requestType: state.requestType, result: state.result, errorCategory: state.errorCategory, statusCode: state.statusCode, rollup: state.rollup, minRiskScore: state.minRiskScore, maxRiskScore: state.maxRiskScore, groupID: state.groupID }
})
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '账号监控加载失败'
async function loadAll() {
  const id = ++requestID; loading.value = true; error.value = ''
  const range = resolveTimeRange(state)
  const [overviewResult, accountResult, qualityResult] = await Promise.allSettled([accountMonitorAPI.getOverview(range), accountMonitorAPI.listAccounts(requestFilters.value), accountMonitorAPI.getDataQuality(range)])
  if (id !== requestID) return
  const failures: string[] = []
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value; else failures.push(message(overviewResult.reason))
  if (qualityResult.status === 'fulfilled') quality.value = qualityResult.value; else failures.push(message(qualityResult.reason))
  if (accountResult.status === 'fulfilled') {
		accounts.value = accountResult.value.items
		groupOptions.value = accountResult.value.groups || accountGroups(accountResult.value.items)
		total.value = accountResult.value.total
	} else failures.push(message(accountResult.reason))
  if (state.selectedAccountID) {
    selectedAccount.value = accounts.value.find((item) => item.account_id === state.selectedAccountID) || selectedAccount.value
    if (!selectedAccount.value) try { selectedAccount.value = await accountMonitorAPI.getAccount(state.selectedAccountID, requestFilters.value) } catch (value) { failures.push(message(value)) }
  }
  error.value = [...new Set(failures)].join('；')
  lastUpdated.value = new Date(); loading.value = false
}
async function openAccount(account: AccountMonitorAccount) { selectedAccount.value = account; await selectAccount(account.account_id) }
async function closeAccount() { selectedAccount.value = null; await selectAccount(undefined) }
async function changeDetailTab(tab: AccountDetailTab) { await setDetailTab(tab) }
async function changePageSize(value: number) { await setPageSize(value as AccountPageSize) }
async function sortAccounts(key: string, order: 'asc' | 'desc') {
  if (key !== 'risk') return
  await setFilters({ sortBy: 'risk_score', sortOrder: order })
}
onMounted(loadAll)
</script>
