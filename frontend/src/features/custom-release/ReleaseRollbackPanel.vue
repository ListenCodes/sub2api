<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ReleaseIdentity, UpdateJob } from './api'
import { remainingSeconds } from './releaseOperation'

const { t } = useI18n({
  useScope: 'local',
  messages: {
    en: {
      current: 'Current',
      prepare: 'Prepare rollback',
      confirm: 'Confirm rollback',
      secondsRemaining: '{seconds}s remaining'
    },
    zh: {
      current: '当前版本',
      prepare: '准备回退',
      confirm: '确认回退',
      secondsRemaining: '剩余 {seconds} 秒'
    }
  }
})

const props = defineProps<{
  current: ReleaseIdentity
  releases: ReleaseIdentity[]
  operation?: UpdateJob | null
  loading?: boolean
  error?: string
}>()
const emit = defineEmits<{ prepare: [releaseID: string]; apply: [jobID: string] }>()
const selected = defineModel<string>('selected', { default: '' })
const selectedRelease = computed(() => props.releases.find((item) => item.release_id === selected.value))
const prepared = computed(() => props.operation?.status === 'prepared')
const now = ref(Date.now())
const preparedRemainingSeconds = computed(() => remainingSeconds(props.operation?.expires_at, now.value))
let countdownTimer: ReturnType<typeof setInterval> | undefined

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
  return Number.isNaN(timestamp) ? '' : new Date(timestamp).toLocaleString()
}
</script>

<template>
  <section class="space-y-3" data-testid="rollback-panel">
    <div class="text-sm font-semibold" data-testid="rollback-current-pair">
      {{ t('current') }}: Official {{ current.official_version }} / Custom {{ current.custom_version }}
    </div>
    <p v-if="error" class="text-xs text-red-600">{{ error }}</p>
    <button
      v-for="release in releases"
      :key="release.release_id"
      type="button"
      class="block w-full rounded border p-2 text-left"
      data-testid="rollback-target-pair"
      @click="selected = release.release_id"
    >
      <span>Official {{ release.official_version }} / Custom {{ release.custom_version }}</span>
      <small class="ml-2">{{ release.custom_commit.slice(0, 8) }}</small>
      <small class="block">{{ formatPublishedAt(release.published_at) }}</small>
    </button>
    <button v-if="selectedRelease && !prepared" type="button" data-testid="prepare-rollback" :disabled="loading" @click="emit('prepare', selected)">{{ t('prepare') }}</button>
    <button v-if="prepared && operation?.job_id" type="button" data-testid="confirm-rollback" :disabled="loading || preparedRemainingSeconds === 0" @click="emit('apply', operation.job_id)">{{ t('confirm') }}</button>
    <span v-if="prepared" data-testid="rollback-countdown">{{ t('secondsRemaining', { seconds: preparedRemainingSeconds }) }}</span>
  </section>
</template>
