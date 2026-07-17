<template>
	<BaseDialog :show="show" :title="account?.account_name || `账号 ${account?.account_id || ''}`" width="full" :close-on-click-outside="true" :z-index="70" @close="$emit('close')">
		<div v-if="account" class="flex h-[80vh] max-h-[80vh] min-w-0 flex-col" data-testid="account-monitor-drawer">
			<div class="mb-4 flex shrink-0 flex-wrap items-center gap-3 border-b border-gray-200 pb-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
        <span>ID {{ account.account_id }}</span><span>{{ account.platform }}</span><span>{{ account.attempts }} 次尝试</span><RiskScoreBadge :score="account.health?.risk_score" :available="Boolean(account.health?.risk_score_available)" />
      </div>
			<div class="max-w-full shrink-0 overflow-x-auto"><nav class="tabs" aria-label="账号详情">
        <button v-for="item in tabs" :key="item.value" type="button" class="tab shrink-0" :class="{ 'tab-active': tab === item.value }" :data-testid="`account-detail-tab-${item.value}`" @click="openTab(item.value)">{{ item.label }}</button>
      </nav></div>
			<div class="mt-4 min-h-0 flex-1 overflow-auto" data-testid="account-detail-content">
        <div v-if="loading" class="space-y-3" data-testid="account-detail-skeleton" role="status" aria-label="正在加载详情"><div class="h-10 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /><div v-for="index in 5" :key="index" class="grid grid-cols-3 gap-3"><span class="h-4 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /><span class="h-4 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /><span class="h-4 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div></div>
        <div v-else-if="error" class="flex min-h-48 flex-col items-center justify-center gap-3 text-center" role="alert"><p class="text-sm text-red-600 dark:text-red-400">{{ error }}</p><button type="button" class="btn btn-secondary" data-testid="account-detail-retry" @click="load">重试</button></div>
        <template v-else-if="tab === 'media'"><div class="grid grid-cols-1 gap-3 sm:grid-cols-3"><div class="border-b border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">图片生成</p><strong class="mt-1 block text-xl">{{ account.image_count }}</strong></div><div class="border-b border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">视频生成</p><strong class="mt-1 block text-xl">{{ account.video_count }}</strong></div><div class="border-b border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">视频时长</p><strong class="mt-1 block text-xl">{{ account.video_duration_seconds }} 秒</strong></div></div></template>
        <template v-else-if="tab === 'trends'"><div v-if="rows.length" class="space-y-2"><div v-for="row in rows" :key="String(row.bucket)" class="grid grid-cols-[9rem_1fr_5rem] items-center gap-3 text-xs"><time>{{ date(row.bucket) }}</time><div class="h-2 bg-gray-100 dark:bg-dark-700"><i class="block h-full bg-emerald-500" :style="{ width: trendWidth(row) }" /></div><span class="text-right">{{ row.attempts }} 次</span></div></div><EmptyState v-else title="当前范围没有趋势数据" /></template>
				<template v-else><table v-if="rows.length" class="min-w-full text-left text-sm"><thead><tr><th v-for="column in detailColumns" :key="column.key" class="sticky top-0 z-10 whitespace-nowrap border-b border-gray-200 bg-white px-5 py-4 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/50 dark:text-gray-400">{{ column.label }}</th></tr></thead><tbody><tr v-for="(row, index) in rows" :key="String(row.actual_model || row.user_id || row.error_category || index)"><td v-for="column in detailColumns" :key="column.key" class="whitespace-nowrap border-b border-gray-100 px-5 py-4 dark:border-dark-800">{{ cell(row, column.key) }}</td></tr></tbody></table><EmptyState v-else title="当前范围没有明细" /></template>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'
import { accountMonitorAPI, type AccountFilters, type AccountMonitorAccount } from '@/api/admin/accountMonitor'
import type { AccountDetailTab } from './useAccountMonitorFilters'

const props = defineProps<{ show: boolean; account: AccountMonitorAccount | null; filters: AccountFilters; tab: AccountDetailTab }>()
const emit = defineEmits<{ close: []; 'update:tab': [tab: AccountDetailTab] }>()
const loading = ref(false)
const error = ref('')
const rows = ref<Record<string, any>[]>([])
const tabs: Array<{ value: AccountDetailTab; label: string }> = [{ value: 'models', label: '模型' }, { value: 'users', label: '用户 / API Key' }, { value: 'errors', label: '错误' }, { value: 'trends', label: '趋势' }, { value: 'media', label: '媒体' }]
const columnsByTab: Record<string, Array<{ key: string; label: string }>> = {
  models: [{ key: 'actual_model', label: '实际上游模型' }, { key: 'model_attribution', label: '归属' }, { key: 'attempts', label: '调用' }, { key: 'successes', label: '成功' }, { key: 'failures', label: '失败' }, { key: 'tokens', label: 'Token' }, { key: 'p95_duration_ms', label: 'P95 延迟' }],
	users: [{ key: 'email', label: '用户' }, { key: 'api_key_name', label: 'API Key' }, { key: 'attempts', label: '调用' }, { key: 'successes', label: '成功' }, { key: 'failures', label: '失败' }, { key: 'success_rate', label: '成功率' }, { key: 'tokens', label: 'Token' }, { key: 'user_cost', label: '用户成本' }, { key: 'last_attempted_at', label: '最近调用' }],
  errors: [{ key: 'error_category', label: '失败原因' }, { key: 'upstream_status_code', label: '上游状态码' }, { key: 'provider_error_code', label: '上游错误码' }, { key: 'failures', label: '失败' }, { key: 'recovered_failures', label: '恢复型失败' }, { key: 'last_failure_at', label: '最近失败' }],
}
const detailColumns = computed(() => columnsByTab[props.tab] || [])
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '详情加载失败'
async function load() {
  if (!props.show || !props.account) return
  if (props.tab === 'media') { rows.value = []; error.value = ''; return }
  loading.value = true; error.value = ''
  try {
    const id = props.account.account_id
    const result = props.tab === 'models' ? await accountMonitorAPI.getModels(id, props.filters)
      : props.tab === 'users' ? await accountMonitorAPI.getUsers(id, props.filters)
        : props.tab === 'errors' ? await accountMonitorAPI.getErrors(id, props.filters)
					: await accountMonitorAPI.getTrends(id, props.filters)
    rows.value = Array.isArray(result) ? result as Record<string, any>[] : result.items as Record<string, any>[]
  } catch (value) { error.value = message(value) } finally { loading.value = false }
}
function openTab(tab: AccountDetailTab) { emit('update:tab', tab); if (tab === props.tab) void load() }
const date = (value: unknown) => value ? new Date(String(value)).toLocaleString() : '-'
function cell(row: Record<string, any>, key: string) { const value = row[key]; if (key === 'success_rate') return `${(Number(value || 0) * 100).toFixed(1)}%`; if (key === 'user_cost') return `$${Number(value || 0).toFixed(4)}`; if (key.endsWith('_at')) return date(value); if (key.endsWith('_ms')) return `${value || 0} ms`; return value ?? '-' }
function trendWidth(row: Record<string, any>) { const max = Math.max(...rows.value.map((item) => Number(item.attempts || 0)), 1); return `${Number(row.attempts || 0) * 100 / max}%` }
watch(() => [props.show, props.account?.account_id, props.tab, props.filters.from, props.filters.to], ([show], previous) => {
  const previousID = previous?.[1]
  if (!show && typeof previousID === 'number') accountMonitorAPI.cancelAccountDetails(previousID)
  void load()
}, { immediate: true })
onBeforeUnmount(() => { if (props.account?.account_id) accountMonitorAPI.cancelAccountDetails(props.account.account_id) })
</script>
