<template>
  <BaseDialog :show="show" title="历史重建" width="normal" :close-on-click-outside="false" @close="$emit('close')">
    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div><label for="rebuild-from" class="input-label">开始时间</label><input id="rebuild-from" v-model="from" data-testid="rebuild-from" type="datetime-local" class="input w-full" /></div>
      <div><label for="rebuild-to" class="input-label">结束时间</label><input id="rebuild-to" v-model="to" data-testid="rebuild-to" type="datetime-local" class="input w-full" /></div>
    </div>
    <div v-if="job" class="mt-4 rounded-lg border border-gray-200 px-4 py-3 text-sm dark:border-dark-700 dark:bg-dark-800/50" data-testid="rebuild-status">
      <p class="font-medium">任务 {{ job.id }} · {{ statusLabel(job.status) }}</p>
      <p class="mt-1 text-gray-500 dark:text-gray-400">已处理 {{ job.processed_rows }} 行</p>
      <p v-if="job.error" class="mt-1 text-red-600 dark:text-red-400">{{ job.error }}</p>
    </div>
    <p v-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <template #footer><button type="button" class="btn btn-secondary" @click="$emit('close')">关闭</button><button type="button" class="btn btn-primary" data-testid="rebuild-start" :disabled="starting || polling" @click="start">{{ starting ? '创建中' : '开始重建' }}</button></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { accountMonitorAPI, type RebuildJob } from '@/api/admin/accountMonitor'
const props = defineProps<{ show: boolean }>()
defineEmits<{ close: [] }>()
const from = ref('')
const to = ref('')
const starting = ref(false)
const polling = ref(false)
const job = ref<RebuildJob | null>(null)
const error = ref('')
let timer: number | undefined
const message = (value: unknown) => value instanceof Error ? value.message : typeof value === 'object' && value && 'message' in value ? String(value.message) : '重建失败'
const localValue = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
function initialize() { if (!props.show) return; const end = new Date(); const start = new Date(end.getTime() - 86400000); from.value = localValue(start); to.value = localValue(end); job.value = null; error.value = '' }
function schedule(id: number) { timer = window.setTimeout(() => void poll(id), 1500) }
async function poll(id: number) { polling.value = true; try { job.value = await accountMonitorAPI.getRebuildJob(id); if (job.value.status === 'pending' || job.value.status === 'running') schedule(id) } catch (value) { error.value = message(value) } finally { polling.value = false } }
async function start() {
  const startAt = new Date(from.value); const endAt = new Date(to.value)
  if (Number.isNaN(startAt.getTime()) || Number.isNaN(endAt.getTime()) || startAt >= endAt || endAt.getTime() - startAt.getTime() > 31 * 86400000) { error.value = '重建范围必须有效且不超过 31 天'; return }
  starting.value = true; error.value = ''
  try { job.value = await accountMonitorAPI.startRebuild({ from: startAt.toISOString(), to: endAt.toISOString() }); schedule(job.value.id) } catch (value) { error.value = message(value) } finally { starting.value = false }
}
const statusLabel = (value: string) => ({ pending: '等待中', running: '运行中', completed: '已完成', failed: '失败' }[value] || value)
watch(() => props.show, initialize, { immediate: true })
onBeforeUnmount(() => { if (timer !== undefined) window.clearTimeout(timer) })
</script>
