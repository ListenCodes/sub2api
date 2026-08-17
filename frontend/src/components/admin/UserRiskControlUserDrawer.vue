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
          <section v-if="user.case_id && identityTab === 'summary'" class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="review-case-workspace">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.reviewCase') }} #{{ user.case_id }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.reviewFeedbackHint') }}</p>
              </div>
              <span class="rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ caseStatusLabel(currentCaseStatus) }}</span>
            </div>
            <p v-if="caseMessage" class="mt-3 text-sm text-emerald-600 dark:text-emerald-400" role="status">{{ caseMessage }}</p>
            <p v-if="caseActionError" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ caseActionError }}</p>
            <button v-if="currentCaseStatus === 'pending'" type="button" class="btn btn-secondary mt-4" data-testid="claim-review-case" :disabled="caseSaving" @click="claimCase">
              {{ caseSaving ? t('admin.userRiskControl.claimingCase') : t('admin.userRiskControl.claimCase') }}
            </button>
            <div v-if="currentCaseStatus === 'in_review'" class="mt-4 grid gap-3 sm:grid-cols-[minmax(0,14rem)_1fr]">
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.feedbackType') }}</label>
                <Select v-model="feedback" data-testid="review-feedback-type" :options="feedbackOptions" :aria-label="t('admin.userRiskControl.feedbackType')" />
              </div>
              <TextArea v-model="feedbackReason" data-testid="review-feedback-reason" :label="t('admin.userRiskControl.feedbackReason')" required :placeholder="t('admin.userRiskControl.feedbackReasonPlaceholder')" :error="feedbackError" @update:model-value="feedbackError = ''" />
              <div class="sm:col-start-2">
                <button type="button" class="btn btn-primary" data-testid="submit-review-feedback" :disabled="caseSaving" @click="submitFeedback">
                  {{ caseSaving ? t('common.saving') : t('admin.userRiskControl.submitFeedback') }}
                </button>
              </div>
            </div>
          </section>
          <div v-if="identityTab === 'summary' && legacyLoading" class="space-y-3 py-5"><div v-for="index in 2" :key="index" class="h-14 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div>
          <template v-else-if="identityTab === 'summary' && legacyDetail">
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-risk-timeline"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.timeline') }}</summary><div v-if="legacyDetail.events.length" class="mt-3 space-y-3"><article v-for="event in legacyDetail.events" :key="event.id" class="border border-gray-200 p-3 text-sm dark:border-dark-700"><div class="flex items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2"><strong>{{ formatRiskType(event.risk_type || event.type) }}</strong><span v-if="event.identity_version === 'legacy_v1'" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('admin.userRiskControl.drawer.legacyV1') }}</span></div><time class="text-xs text-gray-500">{{ formatDate(event.occurred_at) }}</time></div><p class="mt-1 text-gray-600 dark:text-gray-300">{{ formatRiskReason(event.reason, { eventType: event.risk_type || event.type, identityVersion: event.identity_version, ruleCode: event.rule_codes?.[0], errorCode: event.error_code }) }}</p><p v-if="event.identity_version === 'legacy_v1'" class="mt-2 text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.legacyV1Hint') }}</p><p class="mt-2 text-xs text-gray-500">{{ event.error_code || '-' }} · {{ event.endpoint || '-' }} · {{ event.model || '-' }}<span v-if="event.risk_level"> · {{ formatRiskLevel(event.risk_level) }}</span></p><p v-if="event.ip || event.device_id" class="mt-2 text-xs text-gray-500"><span v-if="event.ip">IP {{ event.ip }}</span><span v-if="event.ip && event.device_id"> · </span><span v-if="event.device_id">设备 {{ event.device_id }}</span></p><p v-if="event.rule_codes?.length" class="mt-2 text-xs text-gray-500">命中规则：{{ event.rule_codes.join('、') }}</p></article></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noEvents') }}</p></details>
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-action-history"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.history') }}</summary><div v-if="legacyDetail.audit.length" class="mt-3 space-y-2"><div v-for="record in legacyDetail.audit" :key="record.id" class="flex items-start justify-between gap-3 border border-gray-200 p-3 text-sm dark:border-dark-700"><span><strong>{{ formatRiskAction(record.action) }}</strong> · {{ formatAuditResult(record.result) }} · {{ record.reason || '-' }}<span v-if="record.failure_reason" class="block text-xs text-red-600">失败原因：{{ record.failure_reason }}</span></span><time class="shrink-0 text-xs text-gray-500">{{ formatDate(record.created_at) }}</time></div></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noHistory') }}</p></details>
          </template>
        </div>
        <footer class="flex items-center justify-between border-t border-gray-200 px-5 py-4 dark:border-dark-700"><span class="text-sm text-gray-500">{{ formatAccountStatus(currentStatus) }}</span><button v-if="currentStatus === 'active'" type="button" class="btn btn-danger" data-testid="ban-user" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button><button v-else-if="currentStatus === 'disabled'" type="button" class="btn btn-primary" data-testid="unban-user" @click="openConfirmation">{{ t('admin.userRiskControl.unban') }}</button><span v-else class="text-xs text-gray-500">{{ t('admin.userRiskControl.pendingAccountNoStatusAction') }}</span></footer>
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
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import UserRiskIdentityDetail from '@/features/extensions/user-risk/UserRiskIdentityDetail.vue'
import { userRiskControlV2API, type AccountStatus, type RiskCaseStatus, type RiskFeedback, type RiskUserDetail, type RiskUserRow } from '@/api/admin/userRiskControlV2'
import { formatAccountStatus, formatAuditResult, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType } from '@/utils/userRiskControlLabels'

const props = defineProps<{ user: RiskUserRow }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'updated', user: RiskUserRow): void; (event: 'case-claimed', user: RiskUserRow): void }>()
const { t } = useI18n()
const currentStatus = ref<AccountStatus>(props.user.status), saving = ref(false), confirming = ref(false), reason = ref(''), validationError = ref('')
const currentCaseStatus = ref<RiskCaseStatus | undefined>(props.user.case_status)
const caseSaving = ref(false), feedback = ref<RiskFeedback>('insufficient_evidence'), feedbackReason = ref(''), feedbackError = ref(''), caseMessage = ref(''), caseActionError = ref('')
const feedbackOptions: Array<{ value: RiskFeedback; label: string }> = [
  { value: 'confirmed_abuse', label: t('admin.userRiskControl.feedbackConfirmedAbuse') },
  { value: 'legitimate_shared', label: t('admin.userRiskControl.feedbackLegitimateShared') },
  { value: 'insufficient_evidence', label: t('admin.userRiskControl.feedbackInsufficientEvidence') },
  { value: 'data_error', label: t('admin.userRiskControl.feedbackDataError') },
  { value: 'business_violation', label: t('admin.userRiskControl.feedbackBusinessViolation') },
]
const identityTab = ref('summary')
const legacyDetail = ref<RiskUserDetail | null>(null), legacyLoading = ref(true)
watch(() => props.user.status, (value) => { currentStatus.value = value })
watch(() => props.user.case_status, (value) => { currentCaseStatus.value = value })
async function submitStatus() { if (saving.value || (currentStatus.value !== 'active' && currentStatus.value !== 'disabled')) return; if (!reason.value.trim()) { validationError.value = t('admin.userRiskControl.reasonRequired'); return } saving.value = true; try { const next: AccountStatus = currentStatus.value === 'active' ? 'disabled' : 'active'; const updated = await userRiskControlV2API.setUserStatus(props.user.id, next, reason.value.trim()); const nextUser = updated || { ...props.user, status: next }; currentStatus.value = nextUser.status; confirming.value = false; reason.value = ''; emit('updated', nextUser) } finally { saving.value = false } }
async function claimCase() {
  if (!props.user.case_id || caseSaving.value) return
  caseSaving.value = true
  caseMessage.value = ''
  caseActionError.value = ''
  try {
    await userRiskControlV2API.claimReviewCase(props.user.case_id)
    currentCaseStatus.value = 'in_review'
    caseMessage.value = t('admin.userRiskControl.claimCaseSuccess')
    emit('case-claimed', { ...props.user, case_status: 'in_review', processing_status: 'in_review' })
  } catch (error) {
    caseActionError.value = error instanceof Error ? error.message : t('admin.userRiskControl.caseActionFailed')
  } finally {
    caseSaving.value = false
  }
}
async function submitFeedback() {
  if (!props.user.case_id || caseSaving.value || currentCaseStatus.value !== 'in_review') return
  const trimmedReason = feedbackReason.value.trim()
  if (!trimmedReason) {
    feedbackError.value = t('admin.userRiskControl.feedbackReasonRequired')
    return
  }
  caseSaving.value = true
  caseMessage.value = ''
  caseActionError.value = ''
  try {
    await userRiskControlV2API.submitReviewFeedback(props.user.case_id, feedback.value, trimmedReason)
    currentCaseStatus.value = 'resolved'
    feedbackReason.value = ''
    caseMessage.value = t('admin.userRiskControl.submitFeedbackSuccess')
    emit('updated', { ...props.user, case_status: 'resolved', processing_status: 'resolved', pending: false })
  } catch (error) {
    caseActionError.value = error instanceof Error ? error.message : t('admin.userRiskControl.caseActionFailed')
  } finally {
    caseSaving.value = false
  }
}
function openConfirmation() { if (currentStatus.value !== 'active' && currentStatus.value !== 'disabled') return; confirming.value = true; validationError.value = ''; reason.value = '' }
function closeConfirmation() { if (!saving.value) confirming.value = false }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function caseStatusLabel(value?: RiskCaseStatus) { return ({ pending: t('admin.userRiskControl.viewPending'), in_review: t('admin.userRiskControl.viewMine'), observing: t('admin.userRiskControl.viewObserving'), resolved: t('admin.userRiskControl.viewResolved') } as Record<string, string>)[value || ''] || t('admin.userRiskControl.noCases') }
async function loadLegacy() { legacyLoading.value = true; try { legacyDetail.value = await userRiskControlV2API.getUserDetail(props.user.id) } finally { legacyLoading.value = false } }
onMounted(loadLegacy)
</script>
