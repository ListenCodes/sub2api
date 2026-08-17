<template>
  <div class="space-y-2" data-testid="identity-rollout-panel">
    <select
      v-model="selectedTransition"
      data-testid="identity-transition-select"
      :disabled="busy || blocked || restoring || Boolean(job)"
      :aria-label="t('transition')"
      class="w-full rounded-lg border border-gray-200 bg-white px-2.5 py-2 text-xs text-gray-700 outline-none transition-colors focus:border-primary-400 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200"
    >
      <option v-for="option in transitions" :key="option.value" :value="option.value">
        {{ t(option.label) }}
      </option>
    </select>

    <div
      v-if="job"
      class="rounded-lg border p-2.5 text-xs"
      :class="terminalFailure
        ? 'border-red-200 bg-red-50 text-red-700 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-300'
        : terminalSuccess
          ? 'border-green-200 bg-green-50 text-green-700 dark:border-green-800/50 dark:bg-green-900/20 dark:text-green-300'
          : 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-300'"
    >
      <p class="font-medium">{{ stateLabel }}</p>
      <p class="mt-1 break-words opacity-80">{{ job.message }}</p>
      <p v-if="job.status === 'prepared'" class="mt-1 tabular-nums opacity-80">
        {{ t('remaining', { seconds: remainingSeconds }) }}
      </p>
    </div>
    <div
      v-else-if="restoring"
      class="rounded-lg border border-blue-200 bg-blue-50 p-2.5 text-xs text-blue-700 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-300"
    >
      {{ t('restoring') }}
    </div>

    <p v-if="error" class="break-words text-xs text-red-600 dark:text-red-300">{{ error }}</p>

    <button
      v-if="!job && !restoring"
      data-testid="identity-prepare"
      @click="handlePrepare"
      :disabled="busy || blocked"
      class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
    >
      <Icon name="download" size="xs" :stroke-width="2" />
      {{ busy ? t('preparing') : t('prepare') }}
    </button>
    <button
      v-else-if="job && job.status === 'prepared' && remainingSeconds > 0"
      data-testid="identity-apply"
      @click="handleApply"
      :disabled="busy || blocked || remainingSeconds <= 0"
      class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
    >
      <Icon name="upload" size="xs" :stroke-width="2" />
      {{ busy ? t('applying') : t('apply') }}
    </button>
    <button
      v-else-if="job && (terminalSuccess || terminalFailure || job.status === 'prepared')"
      data-testid="identity-rollout-next"
      @click="resetOperation"
      class="flex w-full items-center justify-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-dark-300 dark:hover:bg-dark-700"
    >
      <Icon name="refresh" size="xs" :stroke-width="2" />
      {{ terminalSuccess ? t('next') : t('retry') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import {
  applyIdentityRollout,
  getUpdateStatus,
  isTerminalUpdateStatus,
  prepareIdentityRollout,
  type IdentityRolloutTransition,
  type UpdateJob
} from './api'

const STORAGE_KEY = 'sub2api-identity-rollout-job-id'
const POLL_INTERVAL_MS = 3000

const props = withDefaults(defineProps<{ blocked?: boolean }>(), { blocked: false })
const emit = defineEmits<{ activeChange: [active: boolean, jobID?: string] }>()

const messages = {
  en: {
    transition: 'Identity rollout transition',
    prepare: 'Prepare transition',
    preparing: 'Preparing...',
    apply: 'Confirm transition',
    applying: 'Applying...',
    next: 'Next transition',
    retry: 'Retry',
    restoring: 'Restoring the durable rollout job...',
    remaining: '{seconds}s remaining',
    failed: 'Identity rollout failed',
    transitionLabel: {
      reset: 'Stage 0 - safe reset', v2: 'Stage 1 - identity foundation', ip: 'Stage 1 - IP collection',
      device: 'Stage 1 - device collection', admin: 'Stage 2 - admin review',
      shadow: 'Stage 3 - Shadow window', rules: 'Stage 3 - Shadow rules', geo: 'Stage 4 - verified geo'
    },
    state: { running: 'Transition in progress', prepared: 'Prepared, confirmation required', success: 'Transition completed', failed: 'Transition failed' }
  },
  zh: {
    transition: '身份发布阶段',
    prepare: '准备阶段切换',
    preparing: '正在准备...',
    apply: '确认阶段切换',
    applying: '正在切换...',
    next: '继续下一阶段',
    retry: '重试',
    restoring: '正在恢复持久化发布任务...',
    remaining: '准备结果剩余 {seconds} 秒',
    failed: '身份能力发布失败',
    transitionLabel: {
      reset: 'Stage 0 - 安全归零', v2: 'Stage 1 - 身份基础', ip: 'Stage 1 - IP 采集',
      device: 'Stage 1 - 设备采集', admin: 'Stage 2 - 管理复核',
      shadow: 'Stage 3 - Shadow 窗口', rules: 'Stage 3 - Shadow 规则', geo: 'Stage 4 - 可信地区'
    },
    state: { running: '阶段切换进行中', prepared: '已准备，等待确认', success: '阶段切换完成', failed: '阶段切换失败' }
  }
}

const { t } = useI18n({ useScope: 'local', messages })
const transitions: Array<{ value: IdentityRolloutTransition; label: string }> = [
  { value: 'stage0-safe-reset', label: 'transitionLabel.reset' },
  { value: 'stage1-v2', label: 'transitionLabel.v2' },
  { value: 'stage1-ip', label: 'transitionLabel.ip' },
  { value: 'stage1-device', label: 'transitionLabel.device' },
  { value: 'stage2-admin', label: 'transitionLabel.admin' },
  { value: 'stage3-shadow-window', label: 'transitionLabel.shadow' },
  { value: 'stage3-rules', label: 'transitionLabel.rules' },
  { value: 'stage4-geo', label: 'transitionLabel.geo' }
]

const selectedTransition = ref<IdentityRolloutTransition>('stage0-safe-reset')
const job = ref<UpdateJob | null>(null)
const busy = ref(false)
const restoring = ref(false)
const error = ref('')
const now = ref(Date.now())
let pollTimer: ReturnType<typeof setInterval> | null = null
let clockTimer: ReturnType<typeof setInterval> | null = null
let pollInFlight = false

const terminalSuccess = computed(() => job.value?.status === 'success')
const terminalFailure = computed(() => Boolean(job.value && isTerminalUpdateStatus(job.value.status) && job.value.status !== 'success'))
const stateLabel = computed(() => {
  if (terminalSuccess.value) return t('state.success')
  if (terminalFailure.value) return t('state.failed')
  if (job.value?.status === 'prepared') return t('state.prepared')
  return t('state.running')
})
const remainingSeconds = computed(() => {
  const expiresAt = Date.parse(job.value?.expires_at || '')
  return Number.isFinite(expiresAt) ? Math.max(0, Math.ceil((expiresAt - now.value) / 1000)) : 0
})

function setJob(next: UpdateJob) {
  restoring.value = false
  job.value = next
  emit('activeChange', !isTerminalUpdateStatus(next.status), next.job_id)
  if (next.identity_transition) selectedTransition.value = next.identity_transition
  localStorage.setItem(STORAGE_KEY, next.job_id)
  if (next.status === 'prepared' || isTerminalUpdateStatus(next.status)) stopPolling()
  else startPolling(next.job_id)
}

function startPolling(jobID?: string) {
  stopPolling()
  pollTimer = setInterval(() => void refreshJob(jobID), POLL_INTERVAL_MS)
}

function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
}

async function refreshJob(jobID?: string) {
  if (pollInFlight || (jobID && job.value && job.value.job_id !== jobID)) return
  pollInFlight = true
  try {
    const status = await getUpdateStatus(jobID)
    if (status.update_kind === 'identity-config') {
      setJob(status)
    } else {
      finishMissingRestore(jobID)
    }
  } catch (cause: unknown) {
    if (isNotFound(cause)) finishMissingRestore(jobID)
  } finally {
    pollInFlight = false
  }
}

function finishMissingRestore(jobID?: string) {
  stopPolling()
  restoring.value = false
  if (!jobID || localStorage.getItem(STORAGE_KEY) === jobID) localStorage.removeItem(STORAGE_KEY)
  job.value = null
  emit('activeChange', false, jobID)
}

function isNotFound(cause: unknown): boolean {
  return (cause as { response?: { status?: number } })?.response?.status === 404
}

async function handlePrepare() {
  if (busy.value || props.blocked) return
  busy.value = true
  error.value = ''
  emit('activeChange', true)
  try {
    setJob(await prepareIdentityRollout(selectedTransition.value))
  } catch (cause: unknown) {
    const err = cause as { response?: { data?: { message?: string } }; message?: string }
    error.value = err.response?.data?.message || err.message || t('failed')
    if (err.response) emit('activeChange', false)
  } finally {
    busy.value = false
  }
}

async function handleApply() {
  if (busy.value || props.blocked || !job.value || remainingSeconds.value <= 0) return
  busy.value = true
  error.value = ''
  try {
    setJob(await applyIdentityRollout(job.value.job_id))
  } catch (cause: unknown) {
    const err = cause as { response?: { data?: { message?: string } }; message?: string }
    error.value = err.response?.data?.message || err.message || t('failed')
  } finally {
    busy.value = false
  }
}

function resetOperation() {
  const jobID = job.value?.job_id
  stopPolling()
  restoring.value = false
  localStorage.removeItem(STORAGE_KEY)
  job.value = null
  emit('activeChange', false, jobID)
  error.value = ''
}

onMounted(async () => {
  clockTimer = setInterval(() => { now.value = Date.now() }, 1000)
  const storedJobID = localStorage.getItem(STORAGE_KEY)
  try {
    const status = await getUpdateStatus(storedJobID || undefined)
    if (status.update_kind === 'identity-config' && (!isTerminalUpdateStatus(status.status) || status.job_id === storedJobID)) setJob(status)
  } catch (cause: unknown) {
    if (isNotFound(cause)) {
      finishMissingRestore(storedJobID || undefined)
      return
    }
    restoring.value = true
    emit('activeChange', true, storedJobID || undefined)
    startPolling(storedJobID || undefined)
  }
})

onBeforeUnmount(() => {
  stopPolling()
  if (clockTimer) clearInterval(clockTimer)
})
</script>
