<template>
  <DataTable :columns="columns" :data="accounts" :loading="loading" row-key="account_id" :clickable-rows="true" :sticky-first-column="false" :sticky-actions-column="false" @row-click="$emit('select', $event)">
    <template #header-risk><button type="button" data-testid="sort-risk-score" :aria-sort="sortBy === 'risk_score' ? sortOrder === 'desc' ? 'descending' : 'ascending' : 'none'" @click.stop="$emit('sort-risk')">风险分 {{ sortBy === 'risk_score' ? sortOrder === 'desc' ? '↓' : '↑' : '↕' }}</button></template>
    <template #cell-account="{ row }"><div :data-testid="`account-row-${row.account_id}`" class="min-w-0"><p class="truncate font-medium text-gray-950 dark:text-white">{{ row.account_name || `账号 ${row.account_id}` }}</p><p class="text-xs text-gray-500">ID {{ row.account_id }}<span v-if="row.parent_account_id"> · 母账号 {{ row.parent_account_id }}</span></p></div></template>
		<template #cell-platform="{ row }"><PlatformBadge :platform="row.platform" /><p class="mt-1 text-xs text-gray-500">{{ statusLabel(row.status) }}</p></template>
		<template #cell-groups="{ row }"><div v-if="row.groups?.length" class="flex max-w-72 flex-wrap gap-x-3 gap-y-1.5"><span v-for="group in row.groups" :key="group.group_id" :data-testid="`account-group-${group.group_id}`" class="inline-flex items-center text-xs text-gray-700 dark:text-gray-200">{{ group.name }} · {{ multiplier(group.rate_multiplier) }}x</span></div><span v-else class="text-xs text-gray-500">未分组</span></template>
    <template #cell-success="{ row }">{{ percent(row.successes, row.attempts) }}</template>
    <template #cell-cost="{ row }"><p>${{ number(row.user_cost) }}</p><p class="text-xs text-gray-500">账号 ${{ number(row.account_cost) }}</p></template>
    <template #cell-latency="{ row }"><p>{{ number(row.average_duration_ms) }} ms</p><p class="text-xs text-gray-500">P95 {{ number(row.p95_duration_ms) }} ms</p></template>
    <template #cell-risk="{ row }"><RiskScoreBadge :score="row.health?.risk_score" :available="Boolean(row.health?.risk_score_available)" /><p class="mt-1 max-w-64 whitespace-normal text-xs text-gray-500" :title="row.health?.reasons?.join(' ')">{{ row.health?.reasons?.join(' ') || '当前未触发异常规则' }}</p></template>
    <template #empty><EmptyState title="当前范围没有符合条件的账号调用" /></template>
  </DataTable>
</template>

<script setup lang="ts">
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'
import PlatformBadge from '@/components/common/PlatformBadge.vue'
import type { Column } from '@/components/common/types'
import type { AccountMonitorAccount } from '@/api/admin/accountMonitor'
defineProps<{ accounts: AccountMonitorAccount[]; loading: boolean; sortBy: string; sortOrder: 'asc' | 'desc' }>()
defineEmits<{ select: [account: AccountMonitorAccount]; 'sort-risk': [] }>()
const columns: Column[] = [
	{ key: 'account', label: '账号', class: 'min-w-52' }, { key: 'platform', label: '平台 / 状态', class: 'min-w-32' }, { key: 'groups', label: '分组', class: 'min-w-52' },
  { key: 'attempts', label: '尝试', class: 'min-w-24' }, { key: 'success', label: '成功率', class: 'min-w-24' },
  { key: 'failures', label: '失败', class: 'min-w-20' }, { key: 'model_count', label: '模型', class: 'min-w-20' },
  { key: 'user_count', label: '用户', class: 'min-w-20' }, { key: 'tokens', label: 'Token', class: 'min-w-28' },
  { key: 'cost', label: '计费 / 成本', class: 'min-w-32' }, { key: 'latency', label: '延迟', class: 'min-w-32' },
  { key: 'risk', label: '风险分', class: 'min-w-72' },
]
const formatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })
const number = (value: number) => formatter.format(Number(value || 0))
const multiplier = (value: number) => new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(Number(value ?? 1))
const percent = (successes: number, total: number) => total ? `${(successes * 100 / total).toFixed(1)}%` : '暂无'
const statusLabel = (status: string) => ({ active: '正常', inactive: '停用', error: '错误' }[status] || status || '未知')
</script>
