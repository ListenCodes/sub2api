<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div>
          <div class="mb-2 flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
            <Icon name="shield" size="sm" class="text-primary-500" />
            <span>{{ t('admin.userRiskControl.sectionLabel') }}</span>
          </div>
          <h1 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('admin.userRiskControl.title') }}</h1>
          <p class="mt-1 max-w-2xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('admin.userRiskControl.refresh') }}
        </button>
      </header>

      <div v-if="error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>

      <div class="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <div v-for="metric in metricItems" :key="metric.label" class="card border-l-4 p-4" :class="metric.accent">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ metric.meta }}</p>
        </div>
      </div>

      <div class="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_minmax(320px,0.8fr)]">
        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-dark-700">
            <div>
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.openCases') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.openCasesHint') }}</p>
            </div>
            <router-link to="/admin/risk-control/cases" class="text-sm font-medium text-primary-600 hover:text-primary-500 dark:text-primary-400">{{ t('admin.userRiskControl.viewAll') }}</router-link>
          </div>
          <div v-if="cases.length" class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="item in cases.slice(0, 6)" :key="item.id" class="flex items-start justify-between gap-4 px-5 py-4">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span class="h-2 w-2 rounded-full" :class="priorityClass(item.priority)" />
                  <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.title }}</p>
                </div>
                <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ item.subject_type }} / {{ item.subject_id }}</p>
                <p class="mt-2 line-clamp-2 text-sm text-gray-600 dark:text-gray-300">{{ item.summary || t('admin.userRiskControl.noReason') }}</p>
              </div>
              <span class="shrink-0 text-xs text-gray-400">{{ formatDate(item.updated_at) }}</span>
            </div>
          </div>
          <div v-else class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.noOpenCases') }}</div>
        </section>

        <section class="card overflow-hidden">
          <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.serviceStatus') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.serviceStatusHint') }}</p>
          </div>
          <div class="space-y-4 p-5">
            <div class="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-3 dark:bg-dark-900/60">
              <span class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.mode') }}</span>
              <span class="inline-flex items-center gap-2 text-sm font-semibold text-amber-600 dark:text-amber-400"><span class="h-2 w-2 rounded-full bg-amber-500" />{{ system?.mode || 'shadow' }}</span>
            </div>
            <div class="flex items-center justify-between border-b border-gray-100 pb-3 text-sm dark:border-dark-700">
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.failureMode') }}</span><span class="font-medium text-gray-900 dark:text-white">{{ system?.decision_fail_mode || 'baseline_allow' }}</span>
            </div>
            <div class="flex items-center justify-between border-b border-gray-100 pb-3 text-sm dark:border-dark-700">
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.scenarioRevision') }}</span><span class="font-medium text-gray-900 dark:text-white">r{{ system?.scenario_revision || 1 }}</span>
            </div>
            <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-300">{{ t('admin.userRiskControl.shadowNotice') }}</div>
          </div>
        </section>
      </div>

      <section class="card overflow-hidden">
        <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.recentEvents') }}</h2><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.recentEventsHint') }}</p></div>
          <router-link to="/admin/risk-control/events" class="text-sm font-medium text-primary-600 hover:text-primary-500 dark:text-primary-400">{{ t('admin.userRiskControl.viewAll') }}</router-link>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
            <thead class="bg-gray-50/70 dark:bg-dark-900/40"><tr><th v-for="label in eventHeaders" :key="label" class="px-5 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in events.slice(0, 8)" :key="item.id" class="hover:bg-gray-50/70 dark:hover:bg-dark-700/30">
                <td class="whitespace-nowrap px-5 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatDate(item.created_at) }}</td>
                <td class="px-5 py-3 text-sm font-medium text-gray-900 dark:text-white">{{ item.scenario_code || item.event_type }}</td>
                <td class="px-5 py-3 text-sm text-gray-600 dark:text-gray-300">{{ item.subject_id }}</td>
                <td class="px-5 py-3"><span class="rounded-md px-2 py-1 text-xs font-medium" :class="actionClass(item.action)">{{ item.action }}</span></td>
                <td class="px-5 py-3 text-sm text-gray-600 dark:text-gray-300">{{ item.reason || '-' }}</td>
              </tr>
              <tr v-if="!events.length"><td colspan="5" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.noEvents') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI, type RiskCase, type RiskEvent, type RiskOverview, type RiskSystemStatus } from '@/api/admin'

const { t } = useI18n()
const loading = ref(true)
const error = ref('')
const overview = ref<RiskOverview | null>(null)
const cases = ref<RiskCase[]>([])
const events = ref<RiskEvent[]>([])
const system = ref<RiskSystemStatus | null>(null)

const metricItems = computed(() => [
  { label: t('admin.userRiskControl.metrics.openCases'), value: overview.value?.open_cases ?? 0, meta: t('admin.userRiskControl.metrics.openCasesMeta'), accent: 'border-amber-500' },
  { label: t('admin.userRiskControl.metrics.events24h'), value: overview.value?.events_24h ?? 0, meta: t('admin.userRiskControl.metrics.events24hMeta'), accent: 'border-sky-500' },
  { label: t('admin.userRiskControl.metrics.highRisk'), value: overview.value?.high_risk_subjects ?? 0, meta: t('admin.userRiskControl.metrics.highRiskMeta'), accent: 'border-rose-500' },
  { label: t('admin.userRiskControl.metrics.reviewRate'), value: `${Math.round((overview.value?.review_rate ?? 0) * 100)}%`, meta: t('admin.userRiskControl.metrics.reviewRateMeta'), accent: 'border-violet-500' },
])

const eventHeaders = computed(() => [
  t('admin.userRiskControl.table.time'), t('admin.userRiskControl.table.scenario'), t('admin.userRiskControl.table.subject'), t('admin.userRiskControl.table.action'), t('admin.userRiskControl.table.reason'),
])

async function load() {
  loading.value = true; error.value = ''
  try {
    const [nextOverview, nextCases, nextEvents, nextSystem] = await Promise.all([
      adminAPI.userRiskControl.getOverview(), adminAPI.userRiskControl.listCases('open', 20), adminAPI.userRiskControl.listEvents(20), adminAPI.userRiskControl.getSystemStatus(),
    ])
    overview.value = nextOverview; cases.value = nextCases; events.value = nextEvents; system.value = nextSystem
  } catch (err) { error.value = err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed')
  } finally { loading.value = false }
}
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function priorityClass(value: RiskCase['priority']) { return value === 'high' ? 'bg-rose-500' : value === 'medium' ? 'bg-amber-500' : 'bg-sky-500' }
function actionClass(value: RiskEvent['action']) { return value === 'reject_candidate' ? 'bg-rose-50 text-rose-700 dark:bg-rose-950/30 dark:text-rose-300' : value === 'review' ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300' : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300' }
onMounted(load)
</script>
