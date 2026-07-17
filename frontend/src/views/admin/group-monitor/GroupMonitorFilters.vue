<template>
  <section class="flex flex-wrap items-center gap-3">
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="group-filter-query-label">分组名称</span><SearchInput :model-value="draft.query" class="w-full sm:w-56" data-testid="group-query" placeholder="输入分组名称" @update:model-value="updateQuery" @search="runNow" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="group-filter-platform-label">平台</span><Select v-model="draft.platform" class="w-full sm:w-40" data-testid="group-platform" :options="platformOptions" @update:model-value="runNow" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="group-filter-status-label">分组状态</span><Select v-model="draft.groupStatus" class="w-full sm:w-36" data-testid="group-status" :options="groupStatusOptions" @update:model-value="runNow" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="group-filter-call-status-label">调用状态</span><Select :model-value="draft.callStatus || ''" class="w-full sm:w-40" data-testid="group-call-status" :options="callStatusOptions" @update:model-value="updateCallStatus" /></label>
		<label class="flex w-full items-center gap-2 sm:w-auto"><span class="input-label !mb-0 shrink-0" data-testid="group-filter-range-label">时间范围</span><Select v-model="draft.range" class="w-full sm:w-36" data-testid="group-range" :options="rangeOptions" @update:model-value="runNow" /></label>
    <button type="button" class="btn btn-ghost btn-icon" title="重置筛选" aria-label="重置筛选" @click="$emit('reset')"><Icon name="x" size="sm" /></button>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupCallFilter } from '@/api/admin/accountMonitor'
import type { GroupMonitorFilterState } from './useGroupMonitorFilters'
import { SUPPORTED_PLATFORM_OPTIONS } from '@/utils/platformColors'
import { useDebouncedAction } from '@/composables/useDebouncedAction'
const props = defineProps<{ state: GroupMonitorFilterState; platforms: string[] }>()
const emit = defineEmits<{ apply: [state: Partial<GroupMonitorFilterState>]; reset: [] }>()
const draft = reactive({ ...props.state })
watch(() => props.state, (value) => Object.assign(draft, value), { deep: true })
const platformOptions = computed(() => [{ value: '', label: '全部平台' }, ...SUPPORTED_PLATFORM_OPTIONS])
const groupStatusOptions = [{ value: 'active', label: '有效分组' }, { value: 'inactive', label: '停用分组' }, { value: 'all', label: '全部未删除分组' }]
const callStatusOptions = [{ value: '', label: '全部调用状态' }, { value: 'has_calls', label: '有调用记录' }, { value: 'normal', label: '正常' }, { value: 'partial_failure', label: '部分失败' }, { value: 'all_failed', label: '全部失败' }, { value: 'recently_idle', label: '近期空闲' }, { value: 'no_data', label: '无调用' }]
const rangeOptions = [{ value: '6h', label: '近 6 小时' }, { value: '24h', label: '近 24 小时' }, { value: '7d', label: '近 7 天' }, { value: '30d', label: '近 30 天' }]
function apply() { emit('apply', { ...draft }) }
const { schedule, runNow } = useDebouncedAction(apply, 300)
function updateQuery(value: string) { draft.query = value; schedule() }
function updateCallStatus(value: unknown) { draft.callStatus = String(value || '') as GroupCallFilter || undefined; void runNow() }
</script>
