<template>
  <DataTable :key="`account-monitor-${tableSortKey}-${sortOrder}`" :columns="columns" :data="accounts" :loading="loading" row-key="account_id" :clickable-rows="true" :server-side-sort="true" :default-sort-key="tableSortKey" :default-sort-order="sortOrder" @row-click="$emit('select', $event)" @sort="handleSort">
    <template #cell-account="{ row }"><div :data-testid="`account-row-${row.account_id}`" class="min-w-0 max-w-[50vw] sm:max-w-none"><p class="truncate font-medium text-gray-900 dark:text-white">{{ row.account_name || `账号 ${row.account_id}` }}</p><p v-if="row.account_identity" class="truncate text-xs text-gray-600 dark:text-gray-300">{{ row.account_identity }}</p><p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">ID {{ row.account_id }}<span v-if="row.parent_account_id"> · 母账号 {{ row.parent_account_id }}</span></p></div></template>
		<template #cell-platform="{ row }"><PlatformBadge :platform="row.platform" /><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ statusLabel(row.status) }}</p></template>
		<template #cell-groups="{ row }"><div v-if="row.groups?.length" class="flex max-w-72 flex-wrap gap-x-3 gap-y-1.5"><span v-for="group in row.groups" :key="group.group_id" :data-testid="`account-group-${group.group_id}`" class="inline-flex items-center text-xs text-gray-700 dark:text-gray-200">{{ group.name }} · {{ multiplier(group.rate_multiplier) }}x</span></div><span v-else class="text-xs text-gray-500 dark:text-gray-400">未分组</span></template>
    <template #cell-success="{ row }">{{ percent(row.successes, row.attempts) }}</template>
    <template #cell-cost="{ row }"><p>${{ number(row.user_cost) }}</p><p class="text-xs text-gray-500 dark:text-gray-400">账号 ${{ number(row.account_cost) }}</p></template>
    <template #cell-latency="{ row }"><p>{{ number(row.average_duration_ms) }} ms</p><p class="text-xs text-gray-500 dark:text-gray-400">P95 {{ number(row.p95_duration_ms) }} ms</p></template>
    <template #cell-risk="{ row }"><RiskScoreBadge :score="row.health?.risk_score" :available="Boolean(row.health?.risk_score_available)" /><p class="mt-1 max-w-64 whitespace-normal text-xs text-gray-500 dark:text-gray-400" :title="row.health?.reasons?.join(' ')">{{ row.health?.reasons?.join(' ') || '当前未触发异常规则' }}</p></template>
    <template #empty><EmptyState title="当前范围没有符合条件的账号调用" /></template>
  </DataTable>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'
import PlatformBadge from '@/components/common/PlatformBadge.vue'
import type { Column } from '@/components/common/types'
import type { AccountMonitorAccount } from '@/api/admin/accountMonitor'
const emit = defineEmits<{ select: [account: AccountMonitorAccount]; sort: [key: string, order: 'asc' | 'desc'] }>()
const props = defineProps<{ accounts: AccountMonitorAccount[]; loading: boolean; sortBy: string; sortOrder: 'asc' | 'desc' }>()
const tableSortKey = computed(() => props.sortBy === 'risk_score' ? 'risk' : '')
function handleSort(key: string, order: 'asc' | 'desc') { emit('sort', key, order) }
const columns: Column[] = [
	{ key: 'account', label: '账号' }, { key: 'platform', label: '平台 / 状态' }, { key: 'groups', label: '分组' },
  { key: 'attempts', label: '尝试' }, { key: 'success', label: '成功率' },
  { key: 'failures', label: '失败' }, { key: 'model_count', label: '模型' },
  { key: 'user_count', label: '用户' }, { key: 'tokens', label: 'Token' },
  { key: 'cost', label: '计费 / 成本' }, { key: 'latency', label: '延迟' },
  { key: 'risk', label: '风险分', sortable: true },
]
const formatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })
const number = (value: number) => formatter.format(Number(value || 0))
const multiplier = (value: number) => new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(Number(value ?? 1))
const percent = (successes: number, total: number) => total ? `${(successes * 100 / total).toFixed(1)}%` : '暂无'
const statusLabel = (status: string) => ({ active: '正常', inactive: '停用', error: '错误' }[status] || status || '未知')
</script>
