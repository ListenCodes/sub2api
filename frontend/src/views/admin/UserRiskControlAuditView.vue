<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p>
        <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.auditPageTitle') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.auditPageDescription') }}</p>
      </header>
      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">{{ error }}</div>
      <section class="card p-4">
        <div class="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          <input v-model="draft.actor" class="form-input" data-testid="audit-actor-filter" placeholder="管理员 ID" />
          <input v-model="draft.target" class="form-input" data-testid="audit-target-filter" :placeholder="t('admin.userRiskControl.targetUserPlaceholder')" />
          <select v-model="draft.action" class="form-input" data-testid="audit-action-filter"><option value="">{{ t('admin.userRiskControl.allActions') }}</option><option v-for="option in auditActionOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
          <select v-model="draft.result" class="form-input" data-testid="audit-result-filter"><option value="">{{ t('admin.userRiskControl.allResults') }}</option><option v-for="option in auditResultOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
          <label class="text-sm text-gray-600 dark:text-gray-300">开始时间<input v-model="draft.from" type="date" data-testid="audit-from-filter" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">结束时间<input v-model="draft.to" type="date" data-testid="audit-to-filter" class="form-input mt-1 w-full" /></label>
        </div>
        <button type="button" class="btn btn-primary mt-4" data-testid="apply-audit-filters" @click="loadAudit(true)">{{ t('common.apply') }}</button>
      </section>
      <section class="card overflow-hidden">
        <div v-if="loading" class="p-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
        <div v-else-if="!records.length" class="p-12 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.empty') }}</div>
        <div v-else class="overflow-x-auto">
          <table class="min-w-[1180px] divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800"><tr>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-time" :aria-sort="sortAria('created_at')" @click="toggleSort('created_at')">{{ t('admin.userRiskControl.table.time') }} {{ sortIndicator('created_at') }}</button></th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.actor') }}</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.action') }}</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-target" :aria-sort="sortAria('target')" @click="toggleSort('target')">{{ t('admin.userRiskControl.table.target') }} {{ sortIndicator('target') }}</button></th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.statusChange') }}</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500"><button type="button" data-testid="audit-sort-result" :aria-sort="sortAria('result')" @click="toggleSort('result')">{{ t('admin.userRiskControl.table.result') }} {{ sortIndicator('result') }}</button></th>
              <th class="min-w-80 px-4 py-3 text-left text-xs font-medium text-gray-500">{{ t('admin.userRiskControl.table.reason') }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="record in records" :key="record.id">
                <td class="whitespace-nowrap px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatDate(record.created_at) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ record.actor || '管理员 ID 未知' }}</td>
                <td class="px-4 py-4 text-sm font-medium text-gray-900 dark:text-white">{{ formatRiskAction(record.action) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ targetLabel(record) }}</td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatAccountStatus(record.before_status) }} → {{ formatAccountStatus(record.after_status) }}</td>
                <td class="px-4 py-4"><span :class="record.result === 'success' ? 'text-emerald-600' : record.result === 'partial' ? 'text-amber-600' : 'text-red-600'">{{ formatAuditResult(record.result) }}</span></td>
                <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300"><p>{{ record.reason || '无操作原因' }}</p><p v-if="record.failure_reason" class="mt-1 text-red-600">失败原因：{{ record.failure_reason }}</p><p v-if="record.batch_id || record.request_id" class="mt-1 text-xs text-gray-400">批次：{{ record.batch_id || '-' }} · 请求：{{ record.request_id || '-' }}</p></td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer v-if="total" class="flex flex-col gap-3 border-t border-gray-200 px-5 py-3 text-sm text-gray-500 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between"><span>{{ t('admin.userRiskControl.total', { total }) }}</span><div class="flex items-center gap-2"><button type="button" class="btn btn-secondary btn-sm" data-testid="audit-previous-page" :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('common.previous') }}</button><span>{{ page }} / {{ totalPages }}</span><button type="button" class="btn btn-secondary btn-sm" data-testid="audit-next-page" :disabled="page >= totalPages || loading" @click="changePage(page + 1)">{{ t('common.next') }}</button></div></footer>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { userRiskControlV2API, type AuditFilters, type RiskAuditRecord } from '@/api/admin/userRiskControlV2'
import { auditResultOptions, formatAccountStatus, formatAuditResult, formatRiskAction, riskActionOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const records = ref<RiskAuditRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const error = ref('')
const sortBy = ref<AuditFilters['sortBy']>()
const sortOrder = ref<'asc' | 'desc'>('desc')
const draft = reactive<AuditFilters>({ action: '', targetUserId: undefined, target: '', actor: '', result: '', from: '', to: '' })
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const auditActionOptions = riskActionOptions.filter((option) => ['ban', 'unban', 'auto_ban', 'create_rule', 'update_rule', 'rule_test', 'mark_processed'].includes(option.value))

function errorMessage(err: unknown) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed') }
function requestFilters(): AuditFilters {
  const result: AuditFilters = { action: draft.action, result: draft.result, page: page.value, pageSize }
  if (draft.targetUserId) result.targetUserId = draft.targetUserId
  if (draft.target?.trim()) result.target = draft.target.trim()
  if (draft.actor?.trim()) result.actor = draft.actor.trim()
  if (draft.from) result.from = draft.from
  if (draft.to) result.to = draft.to
  if (sortBy.value) { result.sortBy = sortBy.value; result.sortOrder = sortOrder.value }
  return result
}
async function loadAudit(resetPage = false) { if (resetPage) page.value = 1; loading.value = true; error.value = ''; try { const response = await userRiskControlV2API.listAudit(requestFilters()); records.value = response.items; total.value = response.total; page.value = response.page || page.value } catch (err) { error.value = errorMessage(err) } finally { loading.value = false } }
async function changePage(next: number) { page.value = next; await loadAudit() }
function toggleSort(next: NonNullable<AuditFilters['sortBy']>) {
  if (sortBy.value === next) sortOrder.value = sortOrder.value === 'desc' ? 'asc' : 'desc'
  else { sortBy.value = next; sortOrder.value = 'desc' }
  page.value = 1
  void loadAudit()
}
function sortIndicator(value: NonNullable<AuditFilters['sortBy']>) { return sortBy.value === value ? sortOrder.value === 'desc' ? '↓' : '↑' : '↕' }
function sortAria(value: NonNullable<AuditFilters['sortBy']>) { return sortBy.value === value ? sortOrder.value === 'desc' ? 'descending' : 'ascending' : 'none' }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function targetLabel(record: RiskAuditRecord) { return record.target_type === 'user' ? `用户 #${record.target_id || record.target_user_id}` : `${record.target_type || '未知目标'}：${record.target_id || '-'}` }
onMounted(loadAudit)
</script>
