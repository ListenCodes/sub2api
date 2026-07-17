<template>
	<BaseDialog :show="show" :title="detail?.group.name || '分组详情'" width="full" :close-on-click-outside="true" :z-index="70" @close="$emit('close')">
    <div v-if="loading" class="space-y-4" data-testid="group-detail-skeleton" role="status" aria-label="正在加载分组详情"><div class="grid grid-cols-2 gap-3 sm:grid-cols-5"><span v-for="index in 5" :key="index" class="h-16 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div><div class="h-72 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div>
    <div v-else-if="error" class="flex min-h-64 flex-col items-center justify-center gap-3 text-center" role="alert"><p class="text-sm text-red-600 dark:text-red-400">{{ error }}</p><button type="button" class="btn btn-secondary" @click="load">重试</button></div>
    <div v-else-if="detail" class="min-w-0" data-testid="group-detail-dialog">
      <p v-if="originalStatus && detail.group.group_status !== originalStatus" class="mb-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:border-amber-700/40 dark:bg-amber-900/20 dark:text-amber-300">状态已变化：{{ detail.group.group_status === 'active' ? '已启用' : '已停用' }}</p>
      <div class="grid grid-cols-2 border-y border-gray-200 dark:border-dark-700 sm:grid-cols-5"><div v-for="metric in metrics" :key="metric.label" class="border-b border-r border-gray-200 p-3 dark:border-dark-700 sm:border-b-0"><p class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</p><p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ metric.value }}</p></div></div>
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">数据截至 {{ new Date(detail.data_as_of).toLocaleString() }}</p>
      <div class="mt-4 max-w-full overflow-x-scroll pb-2" style="scrollbar-gutter: stable" data-testid="group-model-timeline-scroll">
				<table class="w-max border-separate border-spacing-0 text-xs" style="min-width: 2112px">
          <thead class="sticky top-0 z-20 bg-white dark:bg-dark-800/50"><tr><th class="sticky left-0 z-30 min-w-48 border-b border-r border-gray-200 bg-white px-5 py-4 text-left dark:border-dark-700 dark:bg-dark-800/50">实际模型</th><th v-for="bucket in headers" :key="bucket.bucket_at" class="min-w-20 border-b border-gray-200 px-5 py-4 text-center font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ time(bucket.bucket_at) }}</th></tr></thead>
          <tbody><tr v-for="model in sortedModels" :key="model.actual_model"><th class="sticky left-0 z-10 border-b border-r border-gray-200 bg-white px-5 py-4 text-left font-medium dark:border-dark-700 dark:bg-dark-800/50">{{ model.actual_model }}</th><td v-for="bucket in model.timeline" :key="bucket.bucket_at" class="border-b border-gray-100 px-5 py-4 text-center dark:border-dark-800"><button type="button" class="h-9 w-full min-w-16 rounded-md px-1 focus:outline-none focus:ring-2 focus:ring-primary-500" :class="bucketClass(bucket.status)" data-testid="model-bucket-cell" :title="bucketTitle(bucket)" @click="selectedBucket = { model: model.actual_model, bucket }">{{ bucket.successes }} / {{ bucket.total }}</button></td></tr></tbody>
        </table>
      </div>
      <div v-if="selectedBucket" class="mt-3 rounded-lg border border-gray-200 px-4 py-3 text-sm dark:border-dark-700"><strong>{{ selectedBucket.model }}</strong> · {{ new Date(selectedBucket.bucket.bucket_at).toLocaleString() }} · 总数 {{ selectedBucket.bucket.total }} · 成功 {{ selectedBucket.bucket.successes }} · 失败 {{ selectedBucket.bucket.failures }} · 成功率 {{ selectedBucket.bucket.total ? `${(selectedBucket.bucket.successes * 100 / selectedBucket.bucket.total).toFixed(1)}%` : '暂无' }}</div>
      <EmptyState v-if="!sortedModels.length" title="当前范围没有实际模型调用" />
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { accountMonitorAPI, type GroupMonitorBucket, type GroupMonitorDetailResponse, type GroupRange } from '@/api/admin/accountMonitor'
const props = defineProps<{ show: boolean; groupID?: number; range: GroupRange; originalStatus?: string }>()
const emit = defineEmits<{ close: []; removed: [] }>()
const detail = ref<GroupMonitorDetailResponse | null>(null)
const loading = ref(false)
const error = ref('')
const selectedBucket = ref<{ model: string; bucket: GroupMonitorBucket } | null>(null)
const sortedModels = computed(() => [...(detail.value?.models || [])].sort((a, b) => a.actual_model.localeCompare(b.actual_model, undefined, { sensitivity: 'base' })))
const headers = computed(() => sortedModels.value[0]?.timeline || detail.value?.group.timeline || [])
const metrics = computed(() => detail.value ? [{ label: '总调用', value: detail.value.group.total_requests }, { label: '成功', value: detail.value.group.successes }, { label: '失败', value: detail.value.group.failures }, { label: '成功率', value: detail.value.group.success_rate == null ? '暂无' : `${(detail.value.group.success_rate * 100).toFixed(1)}%` }, { label: '实际模型', value: `${detail.value.models.length} 个实际模型` }] : [])
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '分组详情加载失败'
async function load() { if (!props.show || !props.groupID) return; loading.value = true; error.value = ''; selectedBucket.value = null; try { detail.value = await accountMonitorAPI.getGroup(props.groupID, { range: props.range }) } catch (value) { if (typeof value === 'object' && value && 'status' in value && value.status === 404) { emit('removed'); return } error.value = message(value) } finally { loading.value = false } }
const time = (value: string) => new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
const bucketClass = (status: string) => status === 'normal' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300' : status === 'partial_failure' ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300' : status === 'all_failed' ? 'bg-red-50 text-red-700 dark:bg-red-950/30 dark:text-red-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
const bucketTitle = (bucket: GroupMonitorBucket) => `总数 ${bucket.total}，成功 ${bucket.successes}，失败 ${bucket.failures}`
watch(() => [props.show, props.groupID, props.range], ([show], previous) => {
  const previousID = previous?.[1]
  if (!show && typeof previousID === 'number') accountMonitorAPI.cancelGroupDetail(previousID)
  void load()
}, { immediate: true })
onBeforeUnmount(() => { if (props.groupID) accountMonitorAPI.cancelGroupDetail(props.groupID) })
</script>
