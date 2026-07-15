<template>
  <section class="flex flex-wrap items-center gap-3 border-y border-gray-200 py-3 dark:border-dark-700">
    <SearchInput :model-value="draft.query" class="w-full sm:w-56" data-testid="group-query" placeholder="分组名称" @update:model-value="draft.query = $event" @search="apply" />
    <Select v-model="draft.platform" class="w-full sm:w-40" data-testid="group-platform" :options="platformOptions" />
    <Select v-model="draft.groupStatus" class="w-full sm:w-36" data-testid="group-status" :options="groupStatusOptions" />
    <Select :model-value="draft.callStatus || ''" class="w-full sm:w-40" data-testid="group-call-status" :options="callStatusOptions" @update:model-value="draft.callStatus = String($event || '') as GroupCallStatus || undefined" />
    <Select v-model="draft.range" class="w-full sm:w-36" data-testid="group-range" :options="rangeOptions" />
    <button type="button" class="btn btn-primary" data-testid="group-filter-apply" @click="apply"><Icon name="search" size="sm" />应用</button>
    <button type="button" class="btn btn-ghost" @click="$emit('reset')"><Icon name="x" size="sm" />重置</button>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupCallStatus } from '@/api/admin/accountMonitor'
import type { GroupMonitorFilterState } from './useGroupMonitorFilters'
const props = defineProps<{ state: GroupMonitorFilterState; platforms: string[] }>()
const emit = defineEmits<{ apply: [state: Partial<GroupMonitorFilterState>]; reset: [] }>()
const draft = reactive({ ...props.state })
watch(() => props.state, (value) => Object.assign(draft, value), { deep: true })
const platformOptions = computed(() => [{ value: '', label: '全部平台' }, ...props.platforms.map((value) => ({ value, label: value }))])
const groupStatusOptions = [{ value: 'active', label: '有效分组' }, { value: 'inactive', label: '停用分组' }, { value: 'all', label: '全部未删除分组' }]
const callStatusOptions = [{ value: '', label: '全部调用状态' }, { value: 'normal', label: '正常' }, { value: 'partial_failure', label: '部分失败' }, { value: 'all_failed', label: '全部失败' }, { value: 'recently_idle', label: '近期空闲' }, { value: 'no_data', label: '无调用' }]
const rangeOptions = [{ value: '1h', label: '近 1 小时' }, { value: '6h', label: '近 6 小时' }, { value: '12h', label: '近 12 小时' }, { value: '24h', label: '近 24 小时' }]
function apply() { emit('apply', { ...draft }) }
</script>
