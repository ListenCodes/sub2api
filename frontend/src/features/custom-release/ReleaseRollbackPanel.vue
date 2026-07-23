<script setup lang="ts">
import { computed } from 'vue'
import type { ReleaseIdentity, UpdateJob } from './api'
import { remainingSeconds } from './releaseOperation'

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
</script>

<template>
  <section class="space-y-3" data-testid="rollback-panel">
    <div class="text-sm font-semibold">Official {{ current.official_version }} / Custom {{ current.custom_version }}</div>
    <p v-if="error" class="text-xs text-red-600">{{ error }}</p>
    <button v-for="release in releases" :key="release.release_id" type="button" class="block w-full border p-2 text-left" @click="selected = release.release_id">
      <span>{{ release.official_version }} / {{ release.custom_version }}</span>
      <small class="ml-2">{{ release.custom_commit.slice(0, 8) }}</small>
    </button>
    <button v-if="selectedRelease && !prepared" type="button" data-testid="prepare-rollback" :disabled="loading" @click="emit('prepare', selected)">准备回退</button>
    <button v-if="prepared && operation?.job_id" type="button" data-testid="confirm-rollback" :disabled="loading || remainingSeconds(operation.expires_at) === 0" @click="emit('apply', operation.job_id)">确认回退</button>
    <span v-if="prepared">{{ remainingSeconds(operation?.expires_at) }}s</span>
  </section>
</template>
