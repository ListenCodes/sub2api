<template>
  <div class="space-y-4" data-testid="account-monitor-overview">
    <div v-if="quality?.stale_data_warning" class="border-y border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300" role="alert">
      {{ quality.stale_data_warning }}；已保留最近成功数据。
    </div>
    <div class="grid grid-cols-2 border-y border-gray-200 dark:border-dark-700 sm:grid-cols-4 xl:grid-cols-8">
      <div v-for="metric in metrics" :key="metric.label" class="min-w-0 border-b border-r border-gray-200 px-3 py-3 last:border-r-0 dark:border-dark-700 sm:[&:nth-last-child(-n+4)]:border-b-0 xl:border-b-0">
        <p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
        <p class="mt-1 truncate text-lg font-semibold tabular-nums text-gray-950 dark:text-white" :title="metric.value">{{ metric.value }}</p>
        <p class="mt-0.5 truncate text-xs text-gray-400" :title="metric.detail">{{ metric.detail }}</p>
      </div>
    </div>
    <div class="flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 lg:flex-row lg:items-start lg:justify-between">
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ syncText }}</p>
      <div class="min-w-0">
        <p class="text-xs font-semibold uppercase text-gray-500">数据质量</p>
        <div class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-gray-300">
          <span>主库 {{ quality?.source_connected ? '正常' : '不可用' }}</span>
          <span>错误归属 {{ quality?.error_attribution_rate == null ? '暂无' : percent(quality.error_attribution_rate) }}</span>
          <span>精确/估算最终模型 {{ number(quality?.exact_model_requests) }}/{{ number(quality?.estimated_model_requests) }}</span>
          <span>缺失分组 {{ number(quality?.missing_group_requests) }}</span>
          <span>用量游标 {{ number(quality?.usage_cursor.cursor_id) }}</span>
          <span>错误游标 {{ number(quality?.error_cursor.cursor_id) }}</span>
          <span>{{ historyText }}</span>
          <span>{{ quality?.data_source || '数据源未知' }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { AccountDataQuality, AccountMonitorOverview } from '@/api/admin/accountMonitor'

const props = defineProps<{ overview: AccountMonitorOverview | null; quality: AccountDataQuality | null }>()
const formatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })
const number = (value?: number | null) => formatter.format(Number(value || 0))
const money = (value?: number | null) => `$${number(value)}`
const percent = (value?: number | null) => value == null ? '暂无' : `${(value * 100).toFixed(1)}%`

const metrics = computed(() => {
  const value = props.overview
  return [
    { label: '账号尝试', value: number(value?.attempts), detail: `${number(value?.successes)} 成功 / ${number(value?.failures)} 失败` },
    { label: '账号成功率', value: value?.attempts ? percent(value.successes / value.attempts) : '暂无', detail: '上游尝试口径' },
    { label: '最终请求', value: number(value?.requests), detail: value?.requests ? `${percent((value.request_successes || 0) / value.requests)} 成功` : '暂无请求' },
    { label: '活跃 / 异常', value: `${number(value?.active_accounts)} / ${number(value?.abnormal_accounts)}`, detail: `${number(value?.users)} 位用户` },
    { label: '平均风险分', value: number(value?.average_risk_score), detail: `${number(value?.high_risk_accounts)} 个高风险` },
    { label: 'Token', value: number(value?.tokens), detail: `用户计费 ${money(value?.user_cost)}` },
    { label: '账号成本', value: money(value?.account_cost), detail: '上游账号成本' },
    { label: 'P95 延迟', value: value?.p95_duration_ms ? `${number(value.p95_duration_ms)} ms` : '暂无', detail: value?.average_duration_ms ? `平均 ${number(value.average_duration_ms)} ms` : '平均暂无' },
  ]
})

const syncText = computed(() => props.overview?.last_sync_at
  ? `最近同步 ${new Date(props.overview.last_sync_at).toLocaleString()}，延迟 ${number(props.overview.sync_lag_seconds)} 秒`
  : '同步状态暂不可用')

const historyText = computed(() => {
  const from = props.quality?.available_from
  const to = props.quality?.available_to
  if (!from || !to) return '可用历史暂无'
  return `可用历史 ${new Date(from).toLocaleString()} 至 ${new Date(to).toLocaleString()}`
})
</script>
