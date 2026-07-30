<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { ReleaseIdentity, UpdateJob, UpdateJobStatus } from './api'
import { remainingSeconds } from './releaseOperation'

const { t } = useI18n({
  useScope: 'local',
  messages: {
    en: {
      current: 'Current release',
      empty: 'No complete rollback snapshots are available',
      retry: 'Retry',
      prepare: 'Prepare rollback',
      confirm: 'Confirm rollback',
      prepared: 'Rollback prepared',
      secondsRemaining: '{seconds}s remaining'
    },
    zh: {
      current: '当前发布',
      empty: '暂无完整的可回退快照',
      retry: '重试',
      prepare: '准备回退',
      confirm: '确认回退',
      prepared: '回退已准备',
      secondsRemaining: '剩余 {seconds} 秒'
    }
  }
})

const props = defineProps<{
  current?: ReleaseIdentity | null
  releases: ReleaseIdentity[]
  operation?: UpdateJob | null
  currentLoading?: boolean
  historyLoading?: boolean
  error?: string
}>()

const emit = defineEmits<{
  retry: []
  prepare: [releaseID: string]
  apply: [jobID: string]
  expired: [jobID: string]
}>()

const selectedReleaseID = ref('')
const now = ref(Date.now())
const expiredJobID = ref('')
let countdownTimer: ReturnType<typeof setInterval> | undefined

const prepared = computed(() => props.operation?.status === 'prepared')
const effectiveSelectedID = computed(() =>
  prepared.value ? props.operation?.target_release_id || '' : selectedReleaseID.value
)
const selectedRelease = computed(() =>
  props.releases.find((release) => release.release_id === effectiveSelectedID.value)
)
const preparedTarget = computed(() =>
  prepared.value
    ? props.releases.find((release) => release.release_id === props.operation?.target_release_id)
    : undefined
)
const preparedRemainingSeconds = computed(() =>
  remainingSeconds(props.operation?.expires_at, now.value)
)
const loading = computed(() => props.currentLoading || props.historyLoading)
const terminalStatuses = new Set<UpdateJobStatus>([
  'failed',
  'conflict',
  'expired',
  'drifted',
  'failed_rolled_back',
  'rollback_failed'
])
const operationError = computed(() =>
  props.operation && terminalStatuses.has(props.operation.status) ? props.operation.message : ''
)
const displayError = computed(() => props.error || operationError.value)

watch(
  [() => props.operation?.job_id, preparedRemainingSeconds],
  ([jobID, seconds]) => {
    if (prepared.value && jobID && seconds === 0 && expiredJobID.value !== jobID) {
      expiredJobID.value = jobID
      emit('expired', jobID)
    }
  },
  { immediate: true }
)

onMounted(() => {
  countdownTimer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
})

onBeforeUnmount(() => {
  if (countdownTimer) clearInterval(countdownTimer)
})

function formatPublishedAt(value: string): string {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? value : new Date(timestamp).toLocaleString()
}

function selectRelease(releaseID: string): void {
  if (!prepared.value) selectedReleaseID.value = releaseID
}
</script>

<template>
  <section class="space-y-3" data-testid="rollback-panel">
    <div
      v-if="currentLoading"
      class="flex items-center justify-center py-6 text-primary-500"
      data-testid="rollback-loading"
    >
      <Icon name="refresh" size="md" :stroke-width="2" class="animate-spin" />
    </div>

    <template v-else>
      <div
        v-if="current"
        class="border-b border-gray-100 pb-3 dark:border-dark-700"
        data-testid="rollback-current-pair"
      >
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('current') }}</p>
        <p class="mt-1 text-sm font-semibold text-gray-800 dark:text-dark-100">
          Official {{ current.official_version }} / Custom {{ current.custom_version }}
        </p>
        <p class="mt-1 font-mono text-[11px] text-gray-400 dark:text-dark-500">
          {{ current.custom_commit.slice(0, 8) }}
        </p>
      </div>

      <div
        v-if="displayError"
        class="space-y-2 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800/50 dark:bg-red-900/20"
        data-testid="rollback-error"
      >
        <div class="flex items-start gap-2">
          <Icon name="x" size="sm" :stroke-width="2" class="mt-0.5 text-red-600 dark:text-red-400" />
          <p class="min-w-0 text-xs text-red-700 dark:text-red-300">{{ displayError }}</p>
        </div>
        <button
          type="button"
          class="flex w-full items-center justify-center rounded-lg bg-red-500 px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-400 disabled:cursor-not-allowed disabled:opacity-50"
          data-testid="rollback-retry"
          :disabled="loading"
          @click="emit('retry')"
        >
          {{ t('retry') }}
        </button>
      </div>

      <div
        v-if="preparedTarget"
        class="rounded-lg border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-800/50 dark:bg-emerald-900/20"
        data-testid="prepared-rollback-target"
      >
        <p class="text-xs font-medium text-emerald-700 dark:text-emerald-300">{{ t('prepared') }}</p>
        <p class="mt-1 text-sm font-semibold text-emerald-800 dark:text-emerald-200">
          Official {{ preparedTarget.official_version }} / Custom {{ preparedTarget.custom_version }}
        </p>
        <p class="mt-1 font-mono text-[11px] text-emerald-600/70 dark:text-emerald-400/70">
          {{ preparedTarget.custom_commit.slice(0, 8) }}
        </p>
      </div>

      <div
        v-if="historyLoading"
        class="flex items-center justify-center py-5 text-primary-500"
        data-testid="rollback-history-loading"
      >
        <Icon name="refresh" size="md" :stroke-width="2" class="animate-spin" />
      </div>

      <p
        v-else-if="releases.length === 0 && !displayError"
        class="py-5 text-center text-xs text-gray-500 dark:text-dark-400"
        data-testid="rollback-empty"
      >
        {{ t('empty') }}
      </p>

      <div v-else class="space-y-2">
        <button
          v-for="release in releases"
          :key="release.release_id"
          type="button"
          class="flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-400 disabled:cursor-not-allowed"
          :class="
            effectiveSelectedID === release.release_id
              ? 'border-amber-400 bg-amber-50 dark:border-amber-600 dark:bg-amber-900/20'
              : 'border-gray-200 bg-white hover:border-gray-300 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500 dark:hover:bg-dark-700'
          "
          data-testid="rollback-target-pair"
          :disabled="prepared"
          @click="selectRelease(release.release_id)"
        >
          <span
            class="mt-0.5 flex h-4 w-4 flex-none items-center justify-center rounded-full border"
            :class="
              effectiveSelectedID === release.release_id
                ? 'border-amber-500 bg-amber-500'
                : 'border-gray-300 bg-white dark:border-dark-500 dark:bg-dark-700'
            "
            data-testid="rollback-radio"
          >
            <span
              v-if="effectiveSelectedID === release.release_id"
              class="h-1.5 w-1.5 rounded-full bg-white"
            />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block text-xs font-medium text-gray-800 dark:text-dark-100">
              Official {{ release.official_version }} / Custom {{ release.custom_version }}
            </span>
            <span class="mt-1 flex items-center justify-between gap-2 text-[11px] text-gray-400 dark:text-dark-500">
              <span class="font-mono">{{ release.custom_commit.slice(0, 8) }}</span>
              <span>{{ formatPublishedAt(release.published_at) }}</span>
            </span>
          </span>
        </button>
      </div>

      <button
        v-if="selectedRelease && !prepared && !displayError"
        type="button"
        class="flex w-full items-center justify-center rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-amber-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-400 disabled:cursor-not-allowed disabled:opacity-50"
        data-testid="prepare-rollback"
        :disabled="loading"
        @click="emit('prepare', selectedRelease.release_id)"
      >
        {{ t('prepare') }}
      </button>

      <div v-if="prepared && operation?.job_id" class="space-y-2">
        <p
          class="text-center text-xs tabular-nums text-amber-700 dark:text-amber-300"
          data-testid="rollback-countdown"
        >
          {{ t('secondsRemaining', { seconds: preparedRemainingSeconds }) }}
        </p>
        <button
          type="button"
          class="flex w-full items-center justify-center rounded-lg bg-amber-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-amber-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-400 disabled:cursor-not-allowed disabled:opacity-50"
          data-testid="confirm-rollback"
          :disabled="loading || preparedRemainingSeconds === 0"
          @click="emit('apply', operation.job_id)"
        >
          {{ t('confirm') }}
        </button>
      </div>
    </template>
  </section>
</template>
