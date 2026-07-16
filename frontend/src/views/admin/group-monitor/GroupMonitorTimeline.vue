<template>
  <div class="grid h-16 w-full items-end gap-px" style="grid-template-columns: repeat(24, minmax(0, 1fr))" role="img" :aria-label="label" data-testid="group-timeline">
		<button v-for="bucket in buckets" :key="bucket.bucket_at" type="button" class="w-full min-w-0 rounded-t-sm focus:outline-none focus:ring-2 focus:ring-primary-500" :class="color(bucket.status)" :style="{ height: height(bucket.total) }" data-testid="group-timeline-bar" :title="title(bucket)" @click="$emit('bucket', bucket)" />
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { GroupMonitorBucket } from '@/api/admin/accountMonitor'
const props = withDefaults(defineProps<{ buckets: GroupMonitorBucket[]; bucketSeconds?: number }>(), { bucketSeconds: 900 })
defineEmits<{ bucket: [bucket: GroupMonitorBucket] }>()
const max = computed(() => Math.max(...props.buckets.map((item) => item.total), 1))
const height = (total: number) => total === 0 ? '4px' : `${Math.max(12, total * 100 / max.value)}%`
const color = (status: string) => status === 'normal' ? 'bg-emerald-500' : status === 'partial_failure' ? 'bg-amber-500' : status === 'all_failed' ? 'bg-red-500' : 'bg-gray-300 dark:bg-dark-600'
const title = (bucket: GroupMonitorBucket) => `${new Date(bucket.bucket_at).toLocaleString()}：${bucket.successes}/${bucket.total} 成功，失败 ${bucket.failures}`
const bucketLabel = computed(() => ({ 900: '15 分钟', 3600: '1 小时', 25200: '7 小时', 108000: '30 小时' }[props.bucketSeconds] || `${props.bucketSeconds} 秒`))
const label = computed(() => `${bucketLabel.value}调用时间线，共 ${props.buckets.length} 个时间桶`)
</script>
