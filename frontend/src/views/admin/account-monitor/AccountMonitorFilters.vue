<template>
  <section class="flex flex-wrap items-center gap-3" data-testid="account-monitor-filters">
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-range-label">时间范围</span><Select v-model="draft.range" class="w-full sm:w-36" :options="rangeOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-query-label">账号</span><SearchInput :model-value="draft.query || ''" class="w-full sm:w-64" placeholder="搜索账号名称或实际账号" @update:model-value="updateQuery" @search="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-platform-label">平台</span><Select v-model="draft.platform" class="w-full sm:w-40" :options="platformOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-group-label">分组</span><Select v-model="draft.groupID" class="w-full sm:w-48" :options="groupOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-model-label">实际模型</span><SearchInput :model-value="draft.model || ''" class="w-full sm:w-52" placeholder="输入模型名称" @update:model-value="updateModel" @search="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-status-label">账号状态</span><Select v-model="draft.accountStatus" class="w-full sm:w-36" :options="statusOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-result-label">调用结果</span><Select v-model="draft.result" class="w-full sm:w-36" :options="resultOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-rollup-label">账号口径</span><Select v-model="draft.rollup" class="w-full sm:w-36" :options="rollupOptions" @update:model-value="runImmediate" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="account-filter-risk-label">风险分</span><span class="flex min-w-0 flex-1 items-center gap-2 sm:flex-none"><input v-model.number="draft.minRiskScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-20 sm:flex-none" placeholder="最低" @input="schedule" /><span class="text-gray-400 dark:text-gray-500">-</span><input v-model.number="draft.maxRiskScore" type="number" min="0" max="100" class="input min-w-0 flex-1 sm:w-20 sm:flex-none" placeholder="最高" @input="schedule" /></span></label>
    <template v-if="draft.range === 'custom'">
      <label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0">开始</span><input v-model="customFrom" type="datetime-local" class="input min-w-0 flex-1 sm:w-auto" @change="runImmediate" /></label>
      <label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0">结束</span><input v-model="customTo" type="datetime-local" class="input min-w-0 flex-1 sm:w-auto" @change="runImmediate" /></label>
    </template>
    <button type="button" class="btn btn-ghost btn-icon" data-testid="account-filter-reset" title="重置筛选" aria-label="重置筛选" @click="$emit('reset')"><Icon name="x" size="sm" /></button>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import type { AccountGroupSummary } from '@/api/admin/accountMonitor'
import { SUPPORTED_PLATFORM_OPTIONS } from '@/utils/platformColors'
import { useDebouncedAction } from '@/composables/useDebouncedAction'
import type { AccountMonitorFilterState } from './useAccountMonitorFilters'

const props = defineProps<{ state: AccountMonitorFilterState; groups: AccountGroupSummary[] }>()
const emit = defineEmits<{ apply: [value: Partial<AccountMonitorFilterState>]; reset: [] }>()
const draft = reactive({ ...props.state })
watch(() => props.state, (value) => Object.assign(draft, value), { deep: true })
const toLocal = (value?: string) => value ? new Date(value).toISOString().slice(0, 16) : ''
const customFrom = computed({ get: () => toLocal(draft.from), set: (value: string) => { draft.from = value ? new Date(value).toISOString() : undefined } })
const customTo = computed({ get: () => toLocal(draft.to), set: (value: string) => { draft.to = value ? new Date(value).toISOString() : undefined } })
const rangeOptions = [{ value: 'today', label: '今天' }, { value: '24h', label: '近 24 小时' }, { value: '7d', label: '近 7 天' }, { value: '30d', label: '近 30 天' }, { value: '90d', label: '近 90 天' }, { value: 'custom', label: '自定义' }]
const platformOptions = [{ value: '', label: '全部平台' }, ...SUPPORTED_PLATFORM_OPTIONS]
const groupOptions = computed(() => [{ value: '', label: '全部分组' }, { value: 'ungrouped', label: '未分组' }, ...props.groups.map((group) => ({ value: group.group_id, label: group.name }))])
const statusOptions = [{ value: '', label: '全部账号状态' }, { value: 'active', label: '正常' }, { value: 'inactive', label: '停用' }, { value: 'error', label: '错误' }]
const resultOptions = [{ value: '', label: '全部结果' }, { value: 'succeeded', label: '成功' }, { value: 'failed', label: '失败' }]
const rollupOptions = [{ value: 'physical', label: '物理账号' }, { value: 'parent', label: '母账号汇总' }]
function apply() { emit('apply', { ...draft, minRiskScore: draft.minRiskScore === undefined ? undefined : Math.max(0, Math.min(100, Number(draft.minRiskScore))), maxRiskScore: draft.maxRiskScore === undefined ? undefined : Math.max(0, Math.min(100, Number(draft.maxRiskScore))) }) }
const { schedule, runNow: runImmediate } = useDebouncedAction(apply, 300)
function updateQuery(value: string) { draft.query = value }
function updateModel(value: string) { draft.model = value; schedule() }
</script>
