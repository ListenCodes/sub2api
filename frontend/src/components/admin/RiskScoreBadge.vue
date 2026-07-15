<template>
  <span
    class="inline-flex min-h-7 items-center gap-1.5 whitespace-nowrap rounded-md border px-2 py-1 text-xs font-semibold"
    :class="presentation.classes"
    :aria-label="accessibleLabel"
    data-testid="risk-score-badge"
  >
    <template v-if="available">
      <span class="tabular-nums">{{ normalizedScore }}</span>
      <span>{{ presentation.label }}</span>
    </template>
    <template v-else>暂无评分</template>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  score?: number | null
  available?: boolean
  explicitLevel?: string | null
}>(), {
  score: null,
  available: true,
  explicitLevel: null,
})

const normalizedScore = computed(() => Math.min(100, Math.max(0, Math.round(Number(props.score) || 0))))

const scorePresentations = [
  { minimum: 70, label: '严重', classes: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300' },
  { minimum: 40, label: '异常', classes: 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900/60 dark:bg-orange-950/30 dark:text-orange-300' },
  { minimum: 20, label: '关注', classes: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300' },
  { minimum: 0, label: '正常', classes: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300' },
] as const

const explicitPresentations: Record<string, { label: string; classes: string }> = {
  none: { label: '无风险记录', classes: 'border-gray-200 bg-gray-50 text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300' },
  low: { label: '低风险', classes: scorePresentations[3].classes },
  medium: { label: '中风险', classes: scorePresentations[2].classes },
  high: { label: '高风险', classes: scorePresentations[1].classes },
  critical: { label: '严重风险', classes: scorePresentations[0].classes },
}

const presentation = computed(() => {
  if (!props.available) return explicitPresentations.none
  const explicit = props.explicitLevel ? explicitPresentations[props.explicitLevel] : undefined
  return explicit || scorePresentations.find((item) => normalizedScore.value >= item.minimum) || scorePresentations[3]
})

const accessibleLabel = computed(() => props.available
  ? `风险分 ${normalizedScore.value}，${presentation.value.label}`
  : '暂无评分')
</script>
