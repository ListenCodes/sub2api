<template>
  <section class="min-w-0 space-y-5" data-testid="group-monitor-panel">
		<div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="text-lg font-semibold text-gray-950 dark:text-white">分组监控</h2><p class="mt-1 text-xs text-gray-500">{{ dataAsOf ? `数据截至 ${new Date(dataAsOf).toLocaleString()}` : '尚未获取聚合数据' }}</p></div><button type="button" class="btn btn-primary" data-testid="group-monitor-refresh" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />刷新</button></div>
    <div v-if="error" class="border-y border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300" role="alert">{{ error }}；已保留最近成功数据。</div>
    <div v-if="quality.stale_data_warning" class="border-y border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300" role="alert">{{ quality.stale_data_warning }}；已保留最近成功数据。</div>
    <GroupMonitorFilters :state="state" :platforms="platforms" @apply="setFilters" @reset="resetFilters" />
    <div class="flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-500"><span>缺失分组 {{ quality.missing_group_requests }}</span><span>精确模型 {{ quality.exact_model_requests }}</span><span>推定模型 {{ quality.estimated_model_requests }}</span></div>
    <div v-if="loading && !groups.length" class="flex min-h-64 items-center justify-center text-sm text-gray-500" role="status">正在加载分组监控</div>
		<div v-else-if="groups.length" class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4" data-testid="group-monitor-grid"><GroupMonitorCard v-for="group in groups" :key="group.group_id" :group="group" :bucket-seconds="bucketSeconds" @select="openGroup" /></div>
    <EmptyState v-else title="当前筛选没有分组聚合数据" />
		<Pagination v-if="total" :page="state.page" :total="total" :page-size="state.pageSize" :page-size-options="pageSizeOptions" @update:page="setPage" @update:page-size="changePageSize" />
    <GroupMonitorDetailDialog :show="Boolean(state.selectedGroupID)" :group-i-d="state.selectedGroupID" :range="state.range" :original-status="selectedGroup?.group_status" @close="closeGroup" @removed="handleRemoved" />
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { accountMonitorAPI, type GroupMonitorCard as GroupCard, type GroupMonitorQuality, type GroupPageSize } from '@/api/admin/accountMonitor'
import GroupMonitorFilters from './GroupMonitorFilters.vue'
import GroupMonitorCard from './GroupMonitorCard.vue'
import GroupMonitorDetailDialog from './GroupMonitorDetailDialog.vue'
import { useGroupMonitorFilters } from './useGroupMonitorFilters'
import { getConfiguredTablePageSizeOptions } from '@/utils/tablePreferences'
const groups = ref<GroupCard[]>([])
const total = ref(0)
const platforms = ref<string[]>([])
const dataAsOf = ref('')
const quality = ref<GroupMonitorQuality>({ data_as_of: null, collection_lag_seconds: null, usage_cursor: { cursor_time: null, cursor_id: 0, last_success_at: null }, error_cursor: { cursor_time: null, cursor_id: 0, last_success_at: null }, available_from: null, available_to: null, missing_group_requests: 0, exact_model_requests: 0, estimated_model_requests: 0 })
const loading = ref(false)
const error = ref('')
const bucketSeconds = ref(900)
const pageSizeOptions = getConfiguredTablePageSizeOptions()
const selectedGroup = ref<GroupCard | null>(null)
let requestID = 0
const { state, setFilters, resetFilters, setPage, setPageSize, selectGroup } = useGroupMonitorFilters(load)
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '分组监控加载失败'
async function load() { const id = ++requestID; loading.value = true; error.value = ''; try { const result = await accountMonitorAPI.listGroups({ page: state.page, pageSize: state.pageSize, range: state.range, query: state.query, platform: state.platform, groupStatus: state.groupStatus, callStatus: state.callStatus }); if (id !== requestID) return; groups.value = result.items; total.value = result.total; bucketSeconds.value = result.bucket_seconds || 900; platforms.value = result.platforms; dataAsOf.value = result.data_as_of || ''; quality.value = result.data_quality; if (state.selectedGroupID) selectedGroup.value = groups.value.find((item) => item.group_id === state.selectedGroupID) || selectedGroup.value } catch (value) { if (id === requestID) error.value = message(value) } finally { if (id === requestID) loading.value = false } }
async function openGroup(group: GroupCard) { selectedGroup.value = group; await selectGroup(group.group_id) }
async function closeGroup() { selectedGroup.value = null; await selectGroup(undefined) }
async function handleRemoved() { await closeGroup(); await load() }
async function changePageSize(value: number) { await setPageSize(value as GroupPageSize) }
onMounted(() => { void load() })
onBeforeUnmount(() => { accountMonitorAPI.dispose() })
</script>
