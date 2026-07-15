<template>
  <span
    class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium leading-none"
    :class="platformBadgeLightClass(normalizedPlatform)"
    :data-platform="normalizedPlatform"
  >
    <PlatformIcon :platform="iconPlatform" size="xs" />
    <span>{{ platformLabel(normalizedPlatform) }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GroupPlatform } from '@/types'
import PlatformIcon from './PlatformIcon.vue'
import { platformBadgeLightClass, platformLabel } from '@/utils/platformColors'

const props = defineProps<{
  platform?: string | null
}>()

const normalizedPlatform = computed(() => props.platform?.trim().toLowerCase() ?? '')
const iconPlatform = computed(() => normalizedPlatform.value as GroupPlatform)
</script>
