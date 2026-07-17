<template>
  <button type="button" class="card card-hover flex h-full min-h-56 w-full flex-col p-4 text-left" :data-testid="`group-card-${group.group_id}`" @click="$emit('select', group)">
		<div class="flex w-full items-start justify-between gap-3"><div class="min-w-0"><h3 class="truncate text-base font-semibold text-gray-900 dark:text-white" :title="group.name">{{ group.name }}</h3><PlatformBadge class="mt-1" :platform="group.platform" /></div><span class="shrink-0 rounded-md px-2 py-0.5 text-xs" :class="group.group_status === 'active' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ group.group_status === 'active' ? '启用' : '停用' }}</span></div>
    <div class="mt-4 flex w-full items-end justify-between gap-3"><div><p class="text-xs text-gray-500 dark:text-gray-400">当前调用</p><p class="mt-1 text-sm font-medium" :class="status.color">{{ status.label }}</p></div><div class="text-right"><p class="text-xs text-gray-500 dark:text-gray-400">范围成功率</p><p class="mt-1 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ group.success_rate == null ? '暂无' : `${(group.success_rate * 100).toFixed(1)}%` }}</p></div></div>
		<div class="mt-auto w-full pt-4"><GroupMonitorTimeline :buckets="group.timeline" :bucket-seconds="bucketSeconds" /></div>
  </button>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { GroupMonitorCard } from '@/api/admin/accountMonitor'
import GroupMonitorTimeline from './GroupMonitorTimeline.vue'
import PlatformBadge from '@/components/common/PlatformBadge.vue'
const props = withDefaults(defineProps<{ group: GroupMonitorCard; bucketSeconds?: number }>(), { bucketSeconds: 900 })
defineEmits<{ select: [group: GroupMonitorCard] }>()
const labels = { normal: { label: '正常', color: 'text-emerald-600 dark:text-emerald-400' }, partial_failure: { label: '部分失败', color: 'text-amber-600 dark:text-amber-400' }, all_failed: { label: '全部失败', color: 'text-red-600 dark:text-red-400' }, recently_idle: { label: '近期空闲', color: 'text-gray-600 dark:text-gray-300' }, no_data: { label: '无调用', color: 'text-gray-500 dark:text-gray-400' } }
const status = computed(() => labels[props.group.call_status] || labels.no_data)
</script>
