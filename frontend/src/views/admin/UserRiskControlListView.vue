<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ title }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ description }}</p>
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          {{ t('admin.userRiskControl.refresh') }}
        </button>
      </header>

      <div v-if="error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <div v-if="notice" class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300">{{ notice }}</div>

      <section class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
            <thead class="bg-gray-50/70 dark:bg-dark-900/40">
              <tr>
                <th v-for="label in headers" :key="label" class="px-5 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</th>
                <th v-if="kind === 'cases'" class="px-5 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.table.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in rows" :key="getRiskRowKey(item)" class="hover:bg-gray-50/70 dark:hover:bg-dark-700/30">
                <td v-for="(cell, index) in getRiskRowCells(kind, item, formatDate)" :key="`${getRiskRowKey(item)}-${index}`" class="px-5 py-3 text-sm text-gray-600 dark:text-gray-300" :class="index === 0 ? 'font-medium text-gray-900 dark:text-white' : ''" :data-mono="kind === 'scenarios' && index === 1">
                  {{ cell }}
                </td>
                <td v-if="kind === 'cases'" class="whitespace-nowrap px-5 py-3 text-right">
                  <button v-if="isOpenRiskCase(kind, item)" type="button" class="btn btn-secondary btn-sm" :disabled="resolvingId === getRiskCaseId(item)" @click="resolve(item)">
                    {{ resolvingId === getRiskCaseId(item) ? t('admin.userRiskControl.resolving') : t('admin.userRiskControl.resolve') }}
                  </button>
                  <span v-else class="text-xs text-gray-400">{{ t('admin.userRiskControl.resolved') }}</span>
                </td>
              </tr>
              <tr v-if="!rows.length">
                <td :colspan="headers.length + (kind === 'cases' ? 1 : 0)" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.noData') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI, type RiskCase, type RiskEvent, type RiskScenario, type RiskSubject } from '@/api/admin'
import { getRiskCaseId, getRiskRowCells, getRiskRowKey, isOpenRiskCase, type RiskListKind, type RiskTableRow } from '@/utils/riskControlTable'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const resolvingId = ref<number | null>(null)
const error = ref('')
const notice = ref('')
const rows = ref<RiskTableRow[]>([])
const kind = computed(() => String(route.meta.riskListKind || 'events') as RiskListKind)
const title = computed(() => ({ cases: t('admin.userRiskControl.casesTitle'), scenarios: t('admin.userRiskControl.scenariosTitle'), subjects: t('admin.userRiskControl.subjectsTitle'), events: t('admin.userRiskControl.eventsTitle'), lists: t('admin.userRiskControl.listsTitle'), audit: t('admin.userRiskControl.auditTitle') }[kind.value]))
const description = computed(() => ({ cases: t('admin.userRiskControl.casesDescription'), scenarios: t('admin.userRiskControl.scenariosDescription'), subjects: t('admin.userRiskControl.subjectsDescription'), events: t('admin.userRiskControl.eventsDescription'), lists: t('admin.userRiskControl.listsDescription'), audit: t('admin.userRiskControl.auditDescription') }[kind.value]))
const headers = computed(() => {
  if (kind.value === 'cases') return [t('admin.userRiskControl.table.title'), t('admin.userRiskControl.table.subject'), t('admin.userRiskControl.table.priority'), t('admin.userRiskControl.table.status'), t('admin.userRiskControl.table.updated')]
  if (kind.value === 'scenarios') return [t('admin.userRiskControl.table.name'), t('admin.userRiskControl.table.code'), t('admin.userRiskControl.table.mode'), t('admin.userRiskControl.table.revision'), t('admin.userRiskControl.table.updated')]
  if (kind.value === 'subjects') return [t('admin.userRiskControl.table.type'), t('admin.userRiskControl.table.subject'), t('admin.userRiskControl.table.events'), t('admin.userRiskControl.table.score'), t('admin.userRiskControl.table.lastSeen'), t('admin.userRiskControl.table.action')]
  if (kind.value === 'lists') return [t('admin.userRiskControl.table.listType'), t('admin.userRiskControl.table.valueHash'), t('admin.userRiskControl.table.label'), t('admin.userRiskControl.table.updated')]
  if (kind.value === 'audit') return [t('admin.userRiskControl.table.time'), t('admin.userRiskControl.table.action'), t('admin.userRiskControl.table.type'), t('admin.userRiskControl.table.target'), t('admin.userRiskControl.table.actor')]
  return [t('admin.userRiskControl.table.time'), t('admin.userRiskControl.table.scenario'), t('admin.userRiskControl.table.subject'), t('admin.userRiskControl.table.action'), t('admin.userRiskControl.table.reason')]
})

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    if (kind.value === 'cases') rows.value = await adminAPI.userRiskControl.listCases('') as RiskCase[]
    else if (kind.value === 'scenarios') rows.value = await adminAPI.userRiskControl.listScenarios() as RiskScenario[]
    else if (kind.value === 'subjects') rows.value = await adminAPI.userRiskControl.listSubjects() as RiskSubject[]
    else if (kind.value === 'lists') rows.value = await adminAPI.userRiskControl.listRiskLists() as RiskTableRow[]
    else if (kind.value === 'audit') rows.value = await adminAPI.userRiskControl.listAudit() as RiskTableRow[]
    else rows.value = await adminAPI.userRiskControl.listEvents(100) as RiskEvent[]
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed')
  } finally {
    loading.value = false
  }
}

async function resolve(row: RiskTableRow) {
  const id = getRiskCaseId(row)
  const resolution = window.prompt(t('admin.userRiskControl.resolutionPrompt'), t('admin.userRiskControl.defaultResolution'))
  if (!resolution?.trim()) return
  resolvingId.value = id
  error.value = ''
  try {
    await adminAPI.userRiskControl.resolveCase(id, resolution.trim())
    notice.value = t('admin.userRiskControl.resolveSuccess')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed')
  } finally {
    resolvingId.value = null
  }
}

function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
onMounted(load)
watch(kind, load)
</script>
