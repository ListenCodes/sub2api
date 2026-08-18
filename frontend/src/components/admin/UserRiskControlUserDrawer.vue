<template>
  <Teleport to="body">
    <div v-if="activeUser" class="fixed inset-0 z-[70] flex justify-end bg-gray-950/30" data-testid="risk-user-drawer" @click.self="emit('close')">
      <aside class="flex h-full w-full max-w-3xl flex-col overflow-hidden bg-white shadow-2xl dark:bg-dark-800" role="dialog" aria-modal="true">
        <header class="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div class="min-w-0"><button v-if="investigationStack.length > 1" type="button" class="mb-2 inline-flex items-center gap-1 text-xs font-medium text-primary-700 hover:underline dark:text-primary-300" data-testid="investigation-back" @click="goBack"><Icon name="arrowLeft" size="xs" /> 返回 {{ investigationStack[investigationStack.length - 2].username || investigationStack[investigationStack.length - 2].email || `账号 #${investigationStack[investigationStack.length - 2].id}` }}</button><p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.drawer.title') }}</p><h2 class="mt-1 break-all text-lg font-semibold text-gray-900 dark:text-white">{{ activeUser.email || activeUser.username || `#${activeUser.id}` }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400"><span v-if="activeUser.username">{{ activeUser.username }} · </span>#{{ activeUser.id }}</p></div>
          <button type="button" class="btn btn-ghost" :aria-label="t('common.close')" @click="emit('close')"><Icon name="x" size="sm" /></button>
        </header>
        <div class="flex-1 overflow-y-auto px-5">
          <UserRiskIdentityDetail :user-id="activeUser.id" @tab-change="identityTab = $event" @investigate="investigateUser" />
          <section v-if="activeUser.case_id && identityTab === 'summary'" class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="review-case-workspace">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.reviewCase') }} #{{ activeUser.case_id }}</h3>
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
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-risk-timeline"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.timeline') }}</summary><div v-if="legacyDetail.events.length" class="mt-3 space-y-3"><article v-for="event in legacyDetail.events" :key="event.id" class="border border-gray-200 p-3 text-sm dark:border-dark-700"><div class="flex items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2"><strong>{{ formatRiskType(event.risk_type || event.type) }}</strong><span v-if="event.identity_version === 'legacy_v1'" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">历史规则</span></div><time class="text-xs text-gray-500">{{ formatDate(event.occurred_at) }}</time></div><p class="mt-1 text-gray-600 dark:text-gray-300">{{ formatRiskReason(event.reason, { eventType: event.risk_type || event.type, identityVersion: event.identity_version, ruleCode: event.rule_codes?.[0], errorCode: event.error_code }) }}</p><p v-if="event.identity_version === 'legacy_v1'" class="mt-2 text-xs text-gray-500">这条记录来自已退役的历史规则，只用于解释过去的判断。</p><p v-if="event.risk_level" class="mt-2 text-xs text-gray-500">发生时风险：{{ formatRiskLevel(event.risk_level) }}</p><p v-if="event.ip" class="mt-2 text-xs text-gray-500">IP {{ event.ip }}</p><p v-if="event.rule_codes?.length" class="mt-2 text-xs text-gray-500">命中规则：{{ event.rule_codes.map(formatIdentitySignal).join('、') }}</p><details v-if="event.error_code || event.endpoint || event.model || event.device_id" class="mt-2 text-xs text-gray-400"><summary class="cursor-pointer">技术详情</summary><p class="mt-1">错误标识 {{ event.error_code || '-' }} · 接口 {{ event.endpoint || '-' }} · 模型 {{ event.model || '-' }} · 设备 {{ event.device_id || '-' }}</p></details></article></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noEvents') }}</p></details>
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-action-history"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.history') }}</summary><div v-if="legacyDetail.audit.length" class="mt-3 space-y-2"><div v-for="record in legacyDetail.audit" :key="record.id" class="flex items-start justify-between gap-3 border border-gray-200 p-3 text-sm dark:border-dark-700"><span><strong>{{ formatRiskAction(record.action) }}</strong> · {{ formatAuditResult(record.result) }} · {{ record.reason || '-' }}<span v-if="record.failure_reason" class="block text-xs text-red-600">失败原因：{{ record.failure_reason }}</span></span><time class="shrink-0 text-xs text-gray-500">{{ formatDate(record.created_at) }}</time></div></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noHistory') }}</p></details>
          </template>
        </div>
		<p v-if="accountActionWarning" class="border-t border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-300" data-testid="status-action-warning" role="status">部分完成：{{ accountActionWarning }}</p>
		<p v-if="accountActionError" class="border-t border-red-200 bg-red-50 px-5 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/20 dark:text-red-300" data-testid="status-action-error" role="alert">{{ accountActionError }}</p>
		<footer class="flex items-center justify-between gap-3 border-t border-gray-200 px-5 py-4 dark:border-dark-700"><span class="text-sm text-gray-500">{{ accountStateLabel }}</span><div v-if="accountActionable && currentStatus === 'active' && isNormalAccount" class="relative"><button type="button" class="btn btn-secondary" data-testid="secondary-account-actions" :aria-expanded="secondaryActionsOpen" @click="secondaryActionsOpen = !secondaryActionsOpen">次级操作 <Icon name="chevronDown" size="xs" /></button><div v-if="secondaryActionsOpen" class="absolute bottom-full right-0 z-10 mb-2 min-w-40 border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-700 dark:bg-dark-800"><button type="button" class="w-full px-3 py-2 text-left text-sm text-red-600 hover:bg-gray-50 dark:hover:bg-dark-700" data-testid="ban-user-secondary" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button></div></div><button v-else-if="accountActionable && currentStatus === 'active'" type="button" class="btn btn-danger" data-testid="ban-user" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button><button v-else-if="accountActionable && currentStatus === 'disabled'" type="button" class="btn btn-primary" data-testid="unban-user" @click="openConfirmation">{{ t('admin.userRiskControl.unban') }}</button><span v-else class="text-xs text-gray-500">{{ accountActionHint }}</span></footer>
      </aside>
    </div>
    <ConfirmDialog :show="confirming" :title="currentStatus === 'active' ? t('admin.userRiskControl.confirmBan') : t('admin.userRiskControl.confirmUnban')" :message="t('admin.userRiskControl.statusChangeMessage')" :danger="currentStatus === 'active'" :confirm-text="saving ? t('common.saving') : t('common.confirm')" :close-on-click-outside="true" :z-index="80" @confirm="submitStatus" @cancel="closeConfirmation"><div data-testid="status-confirmation"><TextArea v-model="reason" data-testid="status-reason" :label="t('admin.userRiskControl.reasonPlaceholder')" required :placeholder="t('admin.userRiskControl.reasonPlaceholder')" :error="validationError" @update:model-value="validationError = ''" /></div></ConfirmDialog>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import UserRiskIdentityDetail from '@/features/extensions/user-risk/UserRiskIdentityDetail.vue'
import { userRiskControlV2API, type AccountStatus, type AssociatedRiskUser, type RiskCaseStatus, type RiskFeedback, type RiskUserDetail, type RiskUserRow } from '@/api/admin/userRiskControlV2'
import { formatAccountStatus, formatAuditResult, formatIdentitySignal, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType } from '@/utils/userRiskControlLabels'

const props = defineProps<{ user: RiskUserRow }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'updated', user: RiskUserRow): void; (event: 'case-claimed', user: RiskUserRow): void; (event: 'status-partial', user: RiskUserRow, failureReason: string): void }>()
const { t } = useI18n()
const investigationStack = ref<RiskUserRow[]>([{ ...props.user }])
const completionStack = ref<boolean[]>([true])
const activeUser = computed(() => investigationStack.value[investigationStack.value.length - 1])
const activeUserCompleted = computed(() => completionStack.value[completionStack.value.length - 1] === true)
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
const secondaryActionsOpen = ref(false)
const accountActionWarning = ref('')
const accountActionError = ref('')
const accountActionable = computed(() => activeUserCompleted.value && (!activeUser.value.account_availability || activeUser.value.account_availability === 'available') && (currentStatus.value === 'active' || currentStatus.value === 'disabled'))
const accountActionHint = computed(() => activeUserCompleted.value && (!activeUser.value.account_availability || activeUser.value.account_availability === 'available') ? t('admin.userRiskControl.pendingAccountNoStatusAction') : '账号状态不可操作')
const accountStateLabel = computed(() => {
	if (!activeUserCompleted.value) return legacyLoading.value ? '正在补全账号详情' : '账号补全暂不可用'
  if (activeUser.value.account_availability === 'deleted') return '账号已删除'
  if (activeUser.value.account_availability === 'unavailable') return '账号补全暂不可用'
  if (activeUser.value.account_availability === 'not_evaluable') return '账号状态不可评估'
  return formatAccountStatus(currentStatus.value)
})
const isNormalAccount = computed(() => !activeUser.value.case_id && (!activeUser.value.risk_level || activeUser.value.risk_level === 'none') && Number(activeUser.value.risk_score || 0) <= 0)
watch(() => props.user, (value) => { investigationStack.value = [{ ...value }]; completionStack.value = [true]; resetActiveState() }, { deep: true })
async function submitStatus() {
	if (!accountActionable.value || saving.value || (currentStatus.value !== 'active' && currentStatus.value !== 'disabled')) return
	if (!reason.value.trim()) { validationError.value = t('admin.userRiskControl.reasonRequired'); return }
	saving.value = true
	accountActionWarning.value = ''
	accountActionError.value = ''
	try {
		const next: AccountStatus = currentStatus.value === 'active' ? 'disabled' : 'active'
		const outcome = await userRiskControlV2API.setUserStatus(activeUser.value.id, next, reason.value.trim())
		const nextUser = { ...activeUser.value, ...outcome.user, status: outcome.user.status || next }
		investigationStack.value[investigationStack.value.length - 1] = nextUser
		currentStatus.value = nextUser.status
		confirming.value = false
		reason.value = ''
		secondaryActionsOpen.value = false
		if (outcome.result === 'partial') {
			accountActionWarning.value = outcome.failureReason || '账号状态已更新，但关联清理未全部完成'
			emit('status-partial', nextUser, accountActionWarning.value)
		} else emit('updated', nextUser)
	} catch (error) {
		accountActionError.value = error instanceof Error && error.message.trim() ? error.message : '账号状态修改失败'
	} finally { saving.value = false }
}
async function claimCase() {
  if (!activeUser.value.case_id || caseSaving.value) return
  caseSaving.value = true
  caseMessage.value = ''
  caseActionError.value = ''
  try {
    await userRiskControlV2API.claimReviewCase(activeUser.value.case_id)
    currentCaseStatus.value = 'in_review'
    caseMessage.value = t('admin.userRiskControl.claimCaseSuccess')
    const updated = { ...activeUser.value, case_status: 'in_review' as const, processing_status: 'in_review' }
    investigationStack.value[investigationStack.value.length - 1] = updated
    emit('case-claimed', updated)
  } catch (error) {
    caseActionError.value = error instanceof Error ? error.message : t('admin.userRiskControl.caseActionFailed')
  } finally {
    caseSaving.value = false
  }
}
async function submitFeedback() {
  if (!activeUser.value.case_id || caseSaving.value || currentCaseStatus.value !== 'in_review') return
  const trimmedReason = feedbackReason.value.trim()
  if (!trimmedReason) {
    feedbackError.value = t('admin.userRiskControl.feedbackReasonRequired')
    return
  }
  caseSaving.value = true
  caseMessage.value = ''
  caseActionError.value = ''
  try {
    await userRiskControlV2API.submitReviewFeedback(activeUser.value.case_id, feedback.value, trimmedReason)
    currentCaseStatus.value = 'resolved'
    feedbackReason.value = ''
    caseMessage.value = t('admin.userRiskControl.submitFeedbackSuccess')
    const updated = { ...activeUser.value, case_status: 'resolved' as const, processing_status: 'resolved', pending: false }
    investigationStack.value[investigationStack.value.length - 1] = updated
    emit('updated', updated)
  } catch (error) {
    caseActionError.value = error instanceof Error ? error.message : t('admin.userRiskControl.caseActionFailed')
  } finally {
    caseSaving.value = false
  }
}
function openConfirmation() { if (!accountActionable.value || (currentStatus.value !== 'active' && currentStatus.value !== 'disabled')) return; confirming.value = true; secondaryActionsOpen.value = false; validationError.value = ''; accountActionError.value = ''; reason.value = '' }
function closeConfirmation() { if (!saving.value) confirming.value = false }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function caseStatusLabel(value?: RiskCaseStatus) { return ({ pending: t('admin.userRiskControl.viewPending'), in_review: t('admin.userRiskControl.viewMine'), observing: t('admin.userRiskControl.viewObserving'), resolved: t('admin.userRiskControl.viewResolved') } as Record<string, string>)[value || ''] || t('admin.userRiskControl.noCases') }
let legacyRequest = 0
async function loadLegacy() {
  const request = ++legacyRequest
  const requiresCompletion = !activeUserCompleted.value
  legacyDetail.value = null
  if (activeUser.value.account_availability && activeUser.value.account_availability !== 'available') { legacyLoading.value = false; return }
  legacyLoading.value = true
  try {
	const detail = await userRiskControlV2API.getUserDetail(activeUser.value.id)
	if (request === legacyRequest) {
		legacyDetail.value = detail
		const index = investigationStack.value.length - 1
		if (requiresCompletion && investigationStack.value[index]?.id === detail.user.id) {
			const completed = { ...investigationStack.value[index], ...detail.user, account_availability: investigationStack.value[index].account_availability }
			investigationStack.value[index] = completed
			completionStack.value[index] = true
			currentStatus.value = completed.status
			currentCaseStatus.value = completed.case_status
		}
	}
  }
  catch { if (request === legacyRequest) legacyDetail.value = null }
  finally { if (request === legacyRequest) legacyLoading.value = false }
}
function resetActiveState() { currentStatus.value = activeUser.value.status; currentCaseStatus.value = activeUser.value.case_status; identityTab.value = 'summary'; secondaryActionsOpen.value = false; confirming.value = false; accountActionWarning.value = ''; accountActionError.value = ''; void loadLegacy() }
function investigateUser(item: AssociatedRiskUser) {
  const account = item.account
  const status: AccountStatus = account?.status === 'active' || account?.status === 'disabled' || account?.status === 'pending' ? account.status : 'pending'
  investigationStack.value = [...investigationStack.value, { id: item.user_id, email: account?.email || '', username: account?.username || '', status, risk_score: null, risk_level: null, risk_type: null, account_availability: account?.availability || 'unavailable' }]
  completionStack.value = [...completionStack.value, false]
  resetActiveState()
}
function goBack() { if (investigationStack.value.length < 2) return; investigationStack.value = investigationStack.value.slice(0, -1); completionStack.value = completionStack.value.slice(0, -1); resetActiveState() }
void loadLegacy()
</script>
