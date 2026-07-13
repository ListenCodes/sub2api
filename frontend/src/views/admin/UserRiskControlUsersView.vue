<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between"><div><p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p><h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.usersTitle') }}</h1><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.usersDescription') }}</p></div><button type="button" class="btn btn-secondary" :disabled="loading" @click="loadUsers">{{ t('admin.userRiskControl.refresh') }}</button></header>
      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <section class="card p-4"><div class="grid gap-3 md:grid-cols-2 lg:grid-cols-6"><input v-model="draft.search" class="form-input lg:col-span-2" :placeholder="t('admin.userRiskControl.searchPlaceholder')" @keyup.enter="applyFilters" /><select v-model="draft.status" class="form-input"><option value="">{{ t('admin.userRiskControl.allStatuses') }}</option><option value="active">{{ t('admin.userRiskControl.active') }}</option><option value="disabled">{{ t('admin.userRiskControl.disabled') }}</option></select><select v-model="draft.riskType" class="form-input" data-testid="risk-type-filter"><option value="">{{ t('admin.userRiskControl.allRiskTypes') }}</option><option v-for="riskType in riskTypes" :key="riskType" :value="riskType">{{ riskType }}</option></select><select v-model="draft.riskLevel" class="form-input"><option value="">{{ t('admin.userRiskControl.allRiskLevels') }}</option><option value="low">low</option><option value="medium">medium</option><option value="high">high</option><option value="critical">critical</option></select><label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300"><input v-model="draft.pendingOnly" type="checkbox" class="rounded border-gray-300 text-primary-600" />{{ t('admin.userRiskControl.pendingOnly') }}</label></div><button type="button" class="btn btn-primary mt-4" data-testid="apply-filters" @click="applyFilters">{{ t('common.apply') }}</button></section>
      <section class="card overflow-hidden"><div v-if="loading" class="px-5 py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</div><div v-else-if="!users.length" class="px-5 py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.empty') }}</div><div v-else class="overflow-x-auto"><table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700"><thead class="bg-gray-50 dark:bg-dark-800"><tr><th v-for="label in headers" :key="label" class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wide text-gray-500">{{ label }}</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="user in users" :key="user.id" class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800/60" :data-testid="`user-row-${user.id}`" @click="selectedUser = user"><td class="px-5 py-4"><p class="font-medium text-gray-900 dark:text-white">{{ user.username || '-' }}</p><p class="text-xs text-gray-500">{{ user.email }}</p></td><td class="px-5 py-4 text-sm text-gray-600 dark:text-gray-300">#{{ user.id }}</td><td class="px-5 py-4"><span class="rounded-full px-2 py-1 text-xs font-medium" :class="user.status === 'disabled' ? 'bg-red-100 text-red-700 dark:bg-red-950/30 dark:text-red-300' : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300'">{{ user.status }}</span></td><td class="px-5 py-4 text-sm text-gray-600 dark:text-gray-300">{{ user.risk_type || '-' }}</td><td class="px-5 py-4 text-sm font-semibold text-gray-900 dark:text-white">{{ user.risk_score ?? 0 }} <span class="ml-1 text-xs font-normal text-gray-500">{{ user.risk_level || '-' }}</span></td><td class="px-5 py-4 text-sm text-gray-600 dark:text-gray-300">{{ user.risk_reason || '-' }}</td><td class="px-5 py-4 text-right"><span v-if="user.pending" class="rounded-full bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/30 dark:text-amber-300">{{ t('admin.userRiskControl.pending') }}</span></td></tr></tbody></table></div><footer v-if="total" class="flex items-center justify-between border-t border-gray-200 px-5 py-3 text-sm text-gray-500 dark:border-dark-700"><span>{{ t('admin.userRiskControl.total', { total }) }}</span><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1" @click="changePage(page - 1)">{{ t('common.previous') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="page * pageSize >= total" @click="changePage(page + 1)">{{ t('common.next') }}</button></div></footer></section>
    </div>
    <UserRiskControlUserDrawer v-if="selectedUser" :user="selectedUser" @close="selectedUser = null" @updated="handleUpdated" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserRiskControlUserDrawer from '@/components/admin/UserRiskControlUserDrawer.vue'
import { userRiskControlV2API, type RiskUserRow, type UserRiskFilters } from '@/api/admin/userRiskControlV2'

const { t } = useI18n()
const users = ref<RiskUserRow[]>([])
const selectedUser = ref<RiskUserRow | null>(null)
const loading = ref(true)
const error = ref('')
const page = ref(1)
const pageSize = 20
const riskTypes = ['registration_attempt', 'registration_success', 'login_attempt', 'login_failure', 'api_error', 'content_risk', 'quota_exceeded', 'upstream_error', 'api_request']
const total = ref(0)
const draft = reactive<UserRiskFilters>({ search: '', status: '', riskType: '', riskLevel: '', pendingOnly: false })
const activeFilters = reactive<UserRiskFilters>({ ...draft })
const headers = computed(() => [t('admin.userRiskControl.table.account'), t('admin.userRiskControl.table.id'), t('admin.userRiskControl.table.status'), t('admin.userRiskControl.table.riskType'), t('admin.userRiskControl.table.score'), t('admin.userRiskControl.table.reason'), t('admin.userRiskControl.table.pending')])

function errorMessage(err: unknown) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : t('admin.userRiskControl.loadFailed') }
async function loadUsers() { loading.value = true; error.value = ''; try { const response = await userRiskControlV2API.listUsers({ ...activeFilters, page: page.value, pageSize }); users.value = response.items; total.value = response.total } catch (err) { error.value = errorMessage(err) } finally { loading.value = false } }
async function applyFilters() { Object.assign(activeFilters, draft); page.value = 1; await loadUsers() }
async function changePage(next: number) { page.value = next; await loadUsers() }
function handleUpdated(updated: RiskUserRow) { const index = users.value.findIndex((user) => user.id === updated.id); if (index >= 0) users.value[index] = { ...users.value[index], ...updated }; selectedUser.value = null }
onMounted(loadUsers)
</script>
