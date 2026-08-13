<template>
  <Teleport to="body">
    <div v-if="user" class="fixed inset-0 z-[70] flex justify-end bg-gray-950/30" data-testid="risk-user-drawer" @click.self="emit('close')">
      <aside class="flex h-full w-full max-w-3xl flex-col overflow-hidden bg-white shadow-2xl dark:bg-dark-800" role="dialog" aria-modal="true">
        <header class="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div><p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.drawer.title') }}</p><h2 class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ user.email || user.username || `#${user.id}` }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400"><span v-if="user.username">{{ user.username }} · </span>#{{ user.id }}</p></div>
          <button type="button" class="btn btn-ghost" :aria-label="t('common.close')" @click="emit('close')"><Icon name="x" size="sm" /></button>
        </header>
        <div class="flex-1 overflow-y-auto px-5">
          <UserRiskIdentityDetail :user-id="user.id" @tab-change="identityTab = $event" />
          <div v-if="identityTab === 'summary' && legacyLoading" class="space-y-3 py-5"><div v-for="index in 2" :key="index" class="h-14 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div>
          <template v-else-if="identityTab === 'summary' && legacyDetail">
            <section class="border-t border-gray-200 py-5 dark:border-dark-700"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.timeline') }}</h3><div v-if="legacyDetail.events.length" class="mt-3 space-y-3"><article v-for="event in legacyDetail.events" :key="event.id" class="border border-gray-200 p-3 text-sm dark:border-dark-700"><div class="flex items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2"><strong>{{ formatRiskType(event.risk_type || event.type) }}</strong><span v-if="event.identity_version === 'legacy_v1'" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('admin.userRiskControl.drawer.legacyV1') }}</span></div><time class="text-xs text-gray-500">{{ formatDate(event.occurred_at) }}</time></div><p class="mt-1 text-gray-600 dark:text-gray-300">{{ formatRiskReason(event.reason, { eventType: event.risk_type || event.type, identityVersion: event.identity_version, ruleCode: event.rule_codes?.[0], errorCode: event.error_code }) }}</p><p v-if="event.identity_version === 'legacy_v1'" class="mt-2 text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.legacyV1Hint') }}</p><p class="mt-2 text-xs text-gray-500">{{ event.error_code || '-' }} · {{ event.endpoint || '-' }} · {{ event.model || '-' }}<span v-if="event.risk_level"> · {{ formatRiskLevel(event.risk_level) }}</span></p><p v-if="event.ip || event.device_id" class="mt-2 text-xs text-gray-500"><span v-if="event.ip">IP {{ event.ip }}</span><span v-if="event.ip && event.device_id"> · </span><span v-if="event.device_id">设备 {{ event.device_id }}</span></p><p v-if="event.rule_codes?.length" class="mt-2 text-xs text-gray-500">命中规则：{{ event.rule_codes.join('、') }}</p></article></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noEvents') }}</p></section>
            <section class="border-t border-gray-200 py-5 dark:border-dark-700"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.history') }}</h3><div v-if="legacyDetail.audit.length" class="mt-3 space-y-2"><div v-for="record in legacyDetail.audit" :key="record.id" class="flex items-start justify-between gap-3 border border-gray-200 p-3 text-sm dark:border-dark-700"><span><strong>{{ formatRiskAction(record.action) }}</strong> · {{ formatAuditResult(record.result) }} · {{ record.reason || '-' }}<span v-if="record.failure_reason" class="block text-xs text-red-600">失败原因：{{ record.failure_reason }}</span></span><time class="shrink-0 text-xs text-gray-500">{{ formatDate(record.created_at) }}</time></div></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noHistory') }}</p></section>
          </template>
        </div>
        <footer class="flex items-center justify-between border-t border-gray-200 px-5 py-4 dark:border-dark-700"><span class="text-sm text-gray-500">{{ formatAccountStatus(currentStatus) }}</span><button v-if="currentStatus === 'active'" type="button" class="btn btn-danger" data-testid="ban-user" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button><button v-else type="button" class="btn btn-primary" data-testid="unban-user" @click="openConfirmation">{{ t('admin.userRiskControl.unban') }}</button></footer>
      </aside>
    </div>
    <ConfirmDialog :show="confirming" :title="currentStatus === 'active' ? t('admin.userRiskControl.confirmBan') : t('admin.userRiskControl.confirmUnban')" :message="t('admin.userRiskControl.statusChangeMessage')" :danger="currentStatus === 'active'" :confirm-text="saving ? t('common.saving') : t('common.confirm')" :close-on-click-outside="true" :z-index="80" @confirm="submitStatus" @cancel="closeConfirmation"><div data-testid="status-confirmation"><TextArea v-model="reason" data-testid="status-reason" :label="t('admin.userRiskControl.reasonPlaceholder')" required :placeholder="t('admin.userRiskControl.reasonPlaceholder')" :error="validationError" @update:model-value="validationError = ''" /></div></ConfirmDialog>
  </Teleport>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TextArea from '@/components/common/TextArea.vue'
import UserRiskIdentityDetail from '@/features/extensions/user-risk/UserRiskIdentityDetail.vue'
import { userRiskControlV2API, type AccountStatus, type RiskUserDetail, type RiskUserRow } from '@/api/admin/userRiskControlV2'
import { formatAccountStatus, formatAuditResult, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType } from '@/utils/userRiskControlLabels'

const props = defineProps<{ user: RiskUserRow }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'updated', user: RiskUserRow): void }>()
const { t } = useI18n()
const currentStatus = ref<AccountStatus>(props.user.status), saving = ref(false), confirming = ref(false), reason = ref(''), validationError = ref('')
const identityTab = ref('summary')
const legacyDetail = ref<RiskUserDetail | null>(null), legacyLoading = ref(true)
watch(() => props.user.status, (value) => { currentStatus.value = value })
async function submitStatus() { if (saving.value) return; if (!reason.value.trim()) { validationError.value = t('admin.userRiskControl.reasonRequired'); return } saving.value = true; try { const next: AccountStatus = currentStatus.value === 'active' ? 'disabled' : 'active'; const updated = await userRiskControlV2API.setUserStatus(props.user.id, next, reason.value.trim()); const nextUser = updated || { ...props.user, status: next }; currentStatus.value = nextUser.status; confirming.value = false; reason.value = ''; emit('updated', nextUser) } finally { saving.value = false } }
function openConfirmation() { confirming.value = true; validationError.value = ''; reason.value = '' }
function closeConfirmation() { if (!saving.value) confirming.value = false }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
async function loadLegacy() { legacyLoading.value = true; try { legacyDetail.value = await userRiskControlV2API.getUserDetail(props.user.id) } finally { legacyLoading.value = false } }
onMounted(loadLegacy)
</script>
