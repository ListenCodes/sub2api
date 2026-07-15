<template>
  <section class="space-y-3 border-y border-gray-200 py-3 dark:border-dark-700" data-testid="account-monitor-filters">
    <div class="flex flex-wrap items-center gap-3">
      <Select v-model="draft.range" class="w-full sm:w-36" :options="rangeOptions" />
      <SearchInput :model-value="draft.platform || ''" class="w-full sm:w-40" placeholder="平台" @update:model-value="draft.platform = $event" @search="apply" />
      <SearchInput :model-value="draft.model || ''" class="w-full sm:w-52" placeholder="实际上游模型" @update:model-value="draft.model = $event" @search="apply" />
      <Select v-model="draft.accountStatus" class="w-full sm:w-36" :options="statusOptions" />
      <Select v-model="draft.result" class="w-full sm:w-36" :options="resultOptions" />
      <Select v-model="draft.rollup" class="w-full sm:w-36" :options="rollupOptions" />
      <label class="flex w-full items-center gap-2 sm:w-auto"><span class="text-xs text-gray-500">风险分</span><input v-model.number="draft.minRiskScore" type="number" min="0" max="100" class="input w-20" placeholder="最低" /><span class="text-gray-400">-</span><input v-model.number="draft.maxRiskScore" type="number" min="0" max="100" class="input w-20" placeholder="最高" /></label>
      <button type="button" class="btn btn-primary" data-testid="account-filter-apply" @click="apply"><Icon name="search" size="sm" />应用</button>
      <button type="button" class="btn btn-ghost" data-testid="account-filter-reset" @click="$emit('reset')"><Icon name="x" size="sm" />重置</button>
    </div>
    <div v-if="draft.range === 'custom'" class="flex flex-wrap items-center gap-3">
      <label class="text-xs text-gray-500">开始<input v-model="customFrom" type="datetime-local" class="input ml-2" /></label>
      <label class="text-xs text-gray-500">结束<input v-model="customTo" type="datetime-local" class="input ml-2" /></label>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import type { AccountMonitorFilterState } from './useAccountMonitorFilters'

const props = defineProps<{ state: AccountMonitorFilterState }>()
const emit = defineEmits<{ apply: [value: Partial<AccountMonitorFilterState>]; reset: [] }>()
const draft = reactive({ ...props.state })
watch(() => props.state, (value) => Object.assign(draft, value), { deep: true })
const toLocal = (value?: string) => value ? new Date(value).toISOString().slice(0, 16) : ''
const customFrom = computed({ get: () => toLocal(draft.from), set: (value: string) => { draft.from = value ? new Date(value).toISOString() : undefined } })
const customTo = computed({ get: () => toLocal(draft.to), set: (value: string) => { draft.to = value ? new Date(value).toISOString() : undefined } })
const rangeOptions = [{ value: 'today', label: '今天' }, { value: '24h', label: '近 24 小时' }, { value: '7d', label: '近 7 天' }, { value: '30d', label: '近 30 天' }, { value: '90d', label: '近 90 天' }, { value: 'custom', label: '自定义' }]
const statusOptions = [{ value: '', label: '全部账号状态' }, { value: 'active', label: '正常' }, { value: 'inactive', label: '停用' }, { value: 'error', label: '错误' }]
const resultOptions = [{ value: '', label: '全部结果' }, { value: 'success', label: '成功' }, { value: 'failure', label: '失败' }]
const rollupOptions = [{ value: 'physical', label: '物理账号' }, { value: 'parent', label: '母账号汇总' }]
function apply() { emit('apply', { ...draft, minRiskScore: draft.minRiskScore === undefined ? undefined : Math.max(0, Math.min(100, Number(draft.minRiskScore))), maxRiskScore: draft.maxRiskScore === undefined ? undefined : Math.max(0, Math.min(100, Number(draft.maxRiskScore))) }) }
</script>
