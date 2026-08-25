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
          <section v-if="identityTab === 'summary'" class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="review-case-workspace">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ activeUser.case_id ? `${t('admin.userRiskControl.reviewCase')} #${activeUser.case_id}` : '人工复核闭环' }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.reviewFeedbackHint') }}</p>
              </div>
              <span class="rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ caseStatusLabel(currentCaseStatus) }}</span>
            </div>
            <p v-if="caseMessage" class="mt-3 text-sm text-emerald-600 dark:text-emerald-400" role="status">{{ caseMessage }}</p>
            <p v-if="caseActionError" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ caseActionError }}</p>
			<div v-if="!activeUser.case_id" class="mt-4 space-y-3"><TextArea v-model="manualCaseReason" data-testid="manual-case-reason" label="建案原因" required :error="manualCaseError" @update:model-value="manualCaseError = ''" /><div class="grid gap-3 sm:grid-cols-2"><label class="block text-xs font-medium text-gray-600 dark:text-gray-300">复查时间<input v-model="observationDueAt" type="datetime-local" class="input mt-1 w-full" data-testid="observation-due-at" /></label><TextArea v-model="observationGoal" data-testid="observation-goal" label="观察目标" placeholder="转观察时必填" /></div><div class="flex flex-wrap gap-2"><button type="button" class="btn btn-primary" data-testid="create-review-case" :disabled="caseSaving" @click="createCase('pending')">人工建案</button><button type="button" class="btn btn-secondary" data-testid="create-observing-case" :disabled="caseSaving" @click="createCase('observing')">转入观察</button></div></div>
			<div v-else-if="currentCaseStatus === 'pending'" class="mt-4"><button type="button" class="btn btn-primary" data-testid="claim-review-case" :disabled="caseSaving" @click="claimCase">{{ caseSaving ? t('admin.userRiskControl.claimingCase') : t('admin.userRiskControl.claimCase') }}</button></div>
			<div v-else-if="currentCaseStatus === 'observing'" class="mt-4 border-l-2 border-gray-300 pl-3 text-sm dark:border-dark-600"><p><strong>复查时间：</strong>{{ formatDate(activeUser.review_due_at || '') }}</p><p class="mt-1 break-words"><strong>观察目标：</strong>{{ activeUser.observation_goal || '历史案件未记录目标' }}</p><button type="button" class="btn btn-primary mt-3" data-testid="claim-review-case" :disabled="caseSaving" @click="claimCase">开始复查</button></div>
			<div v-if="currentCaseStatus === 'in_review' || resolutionRecovery" class="mt-4 grid gap-3 sm:grid-cols-[minmax(0,14rem)_1fr]">
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.feedbackType') }}</label>
			  <Select v-model="feedback" data-testid="review-feedback-type" :options="feedbackOptions" :aria-label="t('admin.userRiskControl.feedbackType')" :disabled="resolutionLocked" />
			  </div>
			  <TextArea v-model="feedbackReason" data-testid="review-feedback-reason" :label="t('admin.userRiskControl.feedbackReason')" required :placeholder="t('admin.userRiskControl.feedbackReasonPlaceholder')" :error="feedbackError" :disabled="resolutionLocked" @update:model-value="feedbackError = ''" />
			  <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">账号动作</label><Select v-model="accountAction" data-testid="review-account-action" :options="accountActionOptions" :disabled="resolutionLocked" /></div>
			  <div class="grid gap-3 sm:grid-cols-2"><label class="block text-xs font-medium text-gray-600 dark:text-gray-300">复查时间<input v-model="observationDueAt" type="datetime-local" class="input mt-1 w-full" :disabled="resolutionLocked" /></label><TextArea v-model="observationGoal" label="观察目标" :disabled="resolutionLocked" /></div>
              <div class="sm:col-start-2">
                <button type="button" class="btn btn-primary" data-testid="submit-review-feedback" :disabled="caseSaving || resolutionLocked" @click="submitFeedback">
                  {{ caseSaving ? t('common.saving') : t('admin.userRiskControl.submitFeedback') }}
                </button>
				<button type="button" class="btn btn-secondary ml-2" :disabled="caseSaving || resolutionLocked" data-testid="observe-review-case" @click="observeCase">转入观察</button>
			</div>
			<div v-if="resolutionResult || resolutionRecovery" class="mt-4 border border-gray-200 p-3 text-sm dark:border-dark-700" data-testid="resolution-step-results"><template v-if="resolutionResult"><p class="font-medium">完成结果：{{ resolutionResult.result === 'success' ? '全部完成' : '部分完成' }}</p><p class="mt-1">账号：{{ resolutionStepLabel(resolutionResult.account.result) }}<span v-if="resolutionResult.account.failure_reason"> · {{ resolutionResult.account.failure_reason }}</span></p><p class="mt-1">案件：{{ resolutionStepLabel(resolutionResult.case.result || (resolutionResult.result === 'success' ? 'resolved' : 'failed')) }}<span v-if="resolutionResult.case.failure_reason"> · {{ resolutionResult.case.failure_reason }}</span></p></template><p v-else class="font-medium">结案请求结果待确认</p><p v-if="resolutionRecovery && !resolutionResult" class="mt-1 text-gray-600 dark:text-gray-300">已保留原请求，可安全重试确认案件与账号动作。</p><button v-if="resolutionRecovery || resolutionResult?.retryable" type="button" class="btn btn-secondary mt-3" :disabled="caseSaving" data-testid="retry-resolve-case" @click="submitFeedback">重试未完成步骤</button><button v-else-if="resolutionResult?.result === 'partial'" type="button" class="btn btn-secondary mt-3" :disabled="caseSaving" data-testid="reload-review-case" @click="reloadPartialCase">重新加载案件</button></div>
            </div>
          </section>
          <div v-if="identityTab === 'summary' && legacyLoading" class="space-y-3 py-5"><div v-for="index in 2" :key="index" class="h-14 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div>
		  <div v-else-if="identityTab === 'summary' && legacyError" class="border-t border-red-200 py-5 text-sm text-red-700 dark:border-red-800 dark:text-red-300" role="alert" data-testid="legacy-detail-error"><p>{{ legacyError }}</p><button type="button" class="btn btn-secondary mt-3" data-testid="retry-legacy-detail" @click="loadLegacy">重试加载</button></div>
          <template v-else-if="identityTab === 'summary' && legacyDetail">
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-risk-timeline"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.timeline') }}</summary><div v-if="legacyDetail.events.length" class="mt-3 space-y-3"><article v-for="event in legacyDetail.events" :key="event.id" class="border border-gray-200 p-3 text-sm dark:border-dark-700"><div class="flex items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2"><strong>{{ formatRiskType(event.risk_type || event.type) }}</strong><span v-if="event.identity_version === 'legacy_v1'" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">历史规则</span></div><time class="text-xs text-gray-500">{{ formatDate(event.occurred_at) }}</time></div><p class="mt-1 text-gray-600 dark:text-gray-300">{{ formatRiskReason(event.reason, { eventType: event.risk_type || event.type, identityVersion: event.identity_version, ruleCode: event.rule_codes?.[0], errorCode: event.error_code }) }}</p><p v-if="event.identity_version === 'legacy_v1'" class="mt-2 text-xs text-gray-500">这条记录来自已退役的历史规则，只用于解释过去的判断。</p><p v-if="event.risk_level" class="mt-2 text-xs text-gray-500">发生时风险：{{ formatRiskLevel(event.risk_level) }}</p><p v-if="event.ip" class="mt-2 text-xs text-gray-500">IP {{ event.ip }}</p><p v-if="event.rule_codes?.length" class="mt-2 text-xs text-gray-500">命中规则：{{ event.rule_codes.map(formatIdentitySignal).join('、') }}</p><details v-if="event.error_code || event.endpoint || event.model || event.device_id" class="mt-2 text-xs text-gray-400"><summary class="cursor-pointer">技术详情</summary><p class="mt-1">错误标识 {{ event.error_code || '-' }} · 接口 {{ event.endpoint || '-' }} · 模型 {{ event.model || '-' }} · 设备 {{ event.device_id || '-' }}</p></details></article></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noEvents') }}</p></details>
            <details class="border-t border-gray-200 py-5 dark:border-dark-700" data-testid="legacy-action-history"><summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.history') }}</summary><div v-if="legacyDetail.audit.length" class="mt-3 space-y-2"><div v-for="record in legacyDetail.audit" :key="record.id" class="flex items-start justify-between gap-3 border border-gray-200 p-3 text-sm dark:border-dark-700"><span><strong>{{ formatRiskAction(record.action) }}</strong> · {{ formatAuditResult(record.result) }} · {{ record.reason || '-' }}<span v-if="record.failure_reason" class="block text-xs text-red-600">失败原因：{{ record.failure_reason }}</span></span><time class="shrink-0 text-xs text-gray-500">{{ formatDate(record.created_at) }}</time></div></div><p v-else class="mt-3 text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noHistory') }}</p></details>
          </template>
        </div>
		<div v-if="accountActionWarning" class="flex flex-wrap items-center justify-between gap-3 border-t border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-300" data-testid="status-action-warning" role="status"><span>{{ accountActionWarning }}</span><button v-if="accountRecovery" type="button" class="btn btn-secondary" :disabled="saving" data-testid="retry-session-revocation" @click="retryAccountCleanup">{{ accountRecovery.pendingStep === 'session_revocation' ? '重试会话清理' : accountRecovery.pendingStep === 'status_confirmation' ? '重试状态确认' : '重试审计确认' }}</button></div>
		<p v-if="accountActionError && !confirming" class="border-t border-red-200 bg-red-50 px-5 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/20 dark:text-red-300" data-testid="status-action-error" role="alert">{{ accountActionError }}</p>
		<footer class="flex items-center justify-between gap-3 border-t border-gray-200 px-5 py-4 dark:border-dark-700"><span class="text-sm text-gray-500">{{ accountStateLabel }}</span><div v-if="accountActionable && currentStatus === 'active' && isNormalAccount" class="relative"><button type="button" class="btn btn-secondary" data-testid="secondary-account-actions" :aria-expanded="secondaryActionsOpen" @click="secondaryActionsOpen = !secondaryActionsOpen">次级操作 <Icon name="chevronDown" size="xs" /></button><div v-if="secondaryActionsOpen" class="absolute bottom-full right-0 z-10 mb-2 min-w-40 border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-700 dark:bg-dark-800"><button type="button" class="w-full px-3 py-2 text-left text-sm text-red-600 hover:bg-gray-50 dark:hover:bg-dark-700" data-testid="ban-user-secondary" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button></div></div><button v-else-if="accountActionable && currentStatus === 'active'" type="button" class="btn btn-danger" data-testid="ban-user" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button><button v-else-if="accountActionable && currentStatus === 'disabled'" type="button" class="btn btn-primary" data-testid="unban-user" @click="openConfirmation">{{ t('admin.userRiskControl.unban') }}</button><span v-else class="text-xs text-gray-500">{{ accountActionHint }}</span></footer>
      </aside>
    </div>
    <ConfirmDialog :show="confirming" :title="currentStatus === 'active' ? t('admin.userRiskControl.confirmBan') : t('admin.userRiskControl.confirmUnban')" :message="t('admin.userRiskControl.statusChangeMessage')" :danger="currentStatus === 'active'" :confirm-text="saving ? t('common.saving') : t('common.confirm')" :close-on-click-outside="true" :z-index="80" @confirm="submitStatus" @cancel="closeConfirmation"><div class="space-y-3" data-testid="status-confirmation"><p class="break-all text-sm text-gray-600 dark:text-gray-300">{{ activeUser.email || activeUser.username || `账号 #${activeUser.id}` }} · {{ formatAccountStatus(currentStatus) }} → {{ formatAccountStatus(currentStatus === 'active' ? 'disabled' : 'active') }}</p><TextArea v-model="reason" data-testid="status-reason" :label="t('admin.userRiskControl.reasonPlaceholder')" required :placeholder="t('admin.userRiskControl.reasonPlaceholder')" :error="validationError" @update:model-value="validationError = ''" /><p v-if="accountActionError" class="text-sm text-red-600 dark:text-red-400" data-testid="status-confirmation-error" role="alert">{{ accountActionError }}</p></div></ConfirmDialog>
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
import { userRiskControlV2API, type AccountStatus, type AssociatedRiskUser, type ResolveRiskCaseResult, type RiskCaseStatus, type RiskFeedback, type RiskUserDetail, type RiskUserRow } from '@/api/admin/userRiskControlV2'
import { formatAccountStatus, formatAuditResult, formatIdentitySignal, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType } from '@/utils/userRiskControlLabels'

const props = defineProps<{ user: RiskUserRow }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'updated', user: RiskUserRow): void; (event: 'case-claimed', user: RiskUserRow): void; (event: 'status-partial', user: RiskUserRow, failureReason: string): void; (event: 'status-recovery', userID: number, pending: boolean): void }>()
const { t } = useI18n()
const investigationStack = ref<RiskUserRow[]>([{ ...props.user }])
const completionStack = ref<boolean[]>([true])
const activeUser = computed(() => investigationStack.value[investigationStack.value.length - 1])
const activeUserCompleted = computed(() => completionStack.value[completionStack.value.length - 1] === true)
const currentStatus = ref<AccountStatus>(props.user.status), saving = ref(false), confirming = ref(false), reason = ref(''), validationError = ref('')
const currentCaseStatus = ref<RiskCaseStatus | undefined>(props.user.case_status)
const caseSaving = ref(false), feedback = ref<RiskFeedback>('insufficient_evidence'), feedbackReason = ref(''), feedbackError = ref(''), caseMessage = ref(''), caseActionError = ref('')
const manualCaseReason = ref(''), manualCaseError = ref('')
const observationDueAt = ref(defaultObservationDueAt()), observationGoal = ref('')
const accountAction = ref<'none' | 'disable' | 'restore'>('none')
const resolutionResult = ref<ResolveRiskCaseResult | null>(null)
const resolutionRequestID = ref('')
type ResolutionRecovery = { caseId: number; userId: number; feedback: RiskFeedback; reason: string; accountAction: 'none' | 'disable' | 'restore'; expectedRevision: number; requestId: string }
const resolutionRecovery = ref<ResolutionRecovery | null>(null)
const resolutionLocked = computed(() => Boolean(resolutionRecovery.value || resolutionResult.value?.result === 'partial'))
const feedbackOptions: Array<{ value: RiskFeedback; label: string }> = [
  { value: 'confirmed_abuse', label: t('admin.userRiskControl.feedbackConfirmedAbuse') },
  { value: 'legitimate_shared', label: t('admin.userRiskControl.feedbackLegitimateShared') },
  { value: 'insufficient_evidence', label: t('admin.userRiskControl.feedbackInsufficientEvidence') },
  { value: 'data_error', label: t('admin.userRiskControl.feedbackDataError') },
  { value: 'business_violation', label: t('admin.userRiskControl.feedbackBusinessViolation') },
]
const accountActionOptions = [
	{ value: 'none', label: '不修改账号状态' },
	{ value: 'disable', label: '禁用账号并撤销会话' },
	{ value: 'restore', label: '恢复账号' },
]
const identityTab = ref('summary')
const legacyDetail = ref<RiskUserDetail | null>(null), legacyLoading = ref(true), legacyError = ref('')
const secondaryActionsOpen = ref(false)
const accountActionWarning = ref('')
const accountActionError = ref('')
type AccountRecovery = { reason: string; requestId: string; status: AccountStatus; pendingStep: string; batchId?: string }
const accountRecovery = ref<AccountRecovery | null>(null)
const accountActionable = computed(() => !accountRecovery.value && activeUserCompleted.value && (!activeUser.value.account_availability || activeUser.value.account_availability === 'available') && (currentStatus.value === 'active' || currentStatus.value === 'disabled'))
const accountActionHint = computed(() => accountRecovery.value ? '请先完成未完成步骤' : activeUserCompleted.value && (!activeUser.value.account_availability || activeUser.value.account_availability === 'available') ? t('admin.userRiskControl.pendingAccountNoStatusAction') : '账号状态不可操作')
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
	const next: AccountStatus = currentStatus.value === 'active' ? 'disabled' : 'active'
	const operationReason = reason.value.trim()
	const operationID = `risk-status-${activeUser.value.id}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
	accountRecovery.value = { reason: operationReason, requestId: operationID, status: next, pendingStep: 'status_confirmation' }
	if (!persistAccountRecovery()) {
		accountRecovery.value = null
		accountActionError.value = '无法保存恢复信息，请检查浏览器会话存储后重试'
		saving.value = false
		return
	}
	try {
		const outcome = await userRiskControlV2API.setUserStatus(activeUser.value.id, next, operationReason, undefined, operationID)
		const nextUser = { ...activeUser.value, ...outcome.user, status: outcome.user.status || next }
		investigationStack.value[investigationStack.value.length - 1] = nextUser
		currentStatus.value = nextUser.status
		confirming.value = false
		reason.value = ''
		secondaryActionsOpen.value = false
		if (outcome.result === 'partial') {
			accountActionWarning.value = `部分完成：${outcome.failureReason || '账号状态已更新，但关联清理未全部完成'}`
			if (outcome.retryable && outcome.pendingStep) {
				accountRecovery.value = { reason: operationReason, requestId: outcome.requestId || operationID, status: next, pendingStep: outcome.pendingStep, batchId: outcome.batchId }
				persistAccountRecovery()
			} else clearAccountRecovery()
			emit('status-partial', nextUser, accountActionWarning.value)
		} else { clearAccountRecovery(); emit('updated', nextUser) }
	} catch (error) {
		const responseStatus = statusFromError(error)
		const definitiveFailure = responseStatus >= 400 && responseStatus < 500 && responseStatus !== 408 && responseStatus !== 425 && responseStatus !== 429
		if (definitiveFailure) clearAccountRecovery()
		else {
			accountActionWarning.value = '请求已发送但结果未知，可使用原请求重试确认'
			confirming.value = false
			reason.value = ''
			secondaryActionsOpen.value = false
			persistAccountRecovery()
		}
		accountActionError.value = errorMessage(error, '账号状态修改失败')
	} finally { saving.value = false }
}
async function retryAccountCleanup() {
	if (!accountRecovery.value || saving.value) return
	saving.value = true
	accountActionError.value = ''
	try {
		const recovery = accountRecovery.value
		const outcome = recovery.pendingStep === 'session_revocation'
			? await userRiskControlV2API.retryUserSessionRevocation(activeUser.value.id, recovery.reason, recovery.requestId, recovery.batchId)
			: await userRiskControlV2API.setUserStatus(activeUser.value.id, recovery.status, recovery.reason, recovery.batchId, recovery.requestId)
		if (outcome.user?.status) {
			const nextUser = { ...activeUser.value, ...outcome.user }
			investigationStack.value[investigationStack.value.length - 1] = nextUser
			currentStatus.value = nextUser.status
		}
		if (outcome.result === 'success') {
			accountActionWarning.value = ''
			clearAccountRecovery()
			emit('updated', activeUser.value)
		} else {
			accountActionWarning.value = `部分完成：${outcome.failureReason || '关联清理仍未完成'}`
			if (!outcome.retryable) clearAccountRecovery()
			else {
				accountRecovery.value = { ...recovery, pendingStep: outcome.pendingStep || recovery.pendingStep }
				persistAccountRecovery()
			}
		}
	} catch (error) {
		if (isDefinitiveStatusFailure(error)) {
			clearAccountRecovery()
			accountActionWarning.value = ''
		}
		accountActionError.value = errorMessage(error, '会话清理重试失败')
	} finally { saving.value = false }
}
async function claimCase() {
  if (!activeUser.value.case_id || caseSaving.value) return
  caseSaving.value = true
  caseMessage.value = ''
  caseActionError.value = ''
  try {
		const item = await userRiskControlV2API.claimReviewCase(activeUser.value.case_id)
    currentCaseStatus.value = 'in_review'
    caseMessage.value = t('admin.userRiskControl.claimCaseSuccess')
		const updated = { ...activeUser.value, case_status: 'in_review' as const, processing_status: 'in_review', case_revision: item?.revision ?? activeUser.value.case_revision }
    investigationStack.value[investigationStack.value.length - 1] = updated
    emit('case-claimed', updated)
  } catch (error) {
    caseActionError.value = error instanceof Error ? error.message : t('admin.userRiskControl.caseActionFailed')
  } finally {
    caseSaving.value = false
  }
}
async function createCase(status: 'pending' | 'observing') {
	if (activeUser.value.case_id || caseSaving.value) return
	if (!manualCaseReason.value.trim()) { manualCaseError.value = '建案原因不能为空'; return }
	caseSaving.value = true; caseMessage.value = ''; caseActionError.value = ''
	try {
		let observation: { reviewDueAt: string; goal: string } | undefined
		if (status === 'observing') {
			const value = observationInput()
			if (!value) return
			observation = value
		}
		const item = await userRiskControlV2API.createReviewCase(activeUser.value.id, manualCaseReason.value, status, 'manual_review', observation)
		currentCaseStatus.value = item.status
		const updated = { ...activeUser.value, case_id: item.id, case_status: item.status, processing_status: item.status, review_due_at: item.review_due_at, observation_goal: item.observation_goal, case_revision: item.revision }
		investigationStack.value[investigationStack.value.length - 1] = updated
		manualCaseReason.value = ''
		caseMessage.value = status === 'observing' ? '已转入观察' : '人工案件已建立'
		emit('updated', updated)
	} catch (error) { caseActionError.value = error instanceof Error ? error.message : '人工建案失败' } finally { caseSaving.value = false }
}
async function observeCase() {
	if (!activeUser.value.case_id || caseSaving.value || resolutionLocked.value) return
	const observeReason = feedbackReason.value.trim() || manualCaseReason.value.trim()
	if (!observeReason) { feedbackError.value = '请先填写转观察原因'; return }
	const observation = observationInput()
	if (!observation) return
	caseSaving.value = true; caseMessage.value = ''; caseActionError.value = ''
	try { const item = await userRiskControlV2API.observeReviewCase(activeUser.value.case_id, observeReason, observation.reviewDueAt, observation.goal, activeUser.value.case_revision || 0); currentCaseStatus.value = 'observing'; const updated = { ...activeUser.value, case_status: 'observing' as const, processing_status: 'observing', review_due_at: item.review_due_at, observation_goal: item.observation_goal, case_revision: item.revision }; investigationStack.value[investigationStack.value.length - 1] = updated; feedbackReason.value = ''; caseMessage.value = '已转入观察'; emit('updated', updated) } catch (error) { caseActionError.value = error instanceof Error ? error.message : '转入观察失败' } finally { caseSaving.value = false }
}
async function submitFeedback() {
	if (!activeUser.value.case_id || caseSaving.value || (currentCaseStatus.value !== 'in_review' && !resolutionRecovery.value)) return
	const trimmedReason = resolutionRecovery.value?.reason || feedbackReason.value.trim()
  if (!trimmedReason) {
    feedbackError.value = t('admin.userRiskControl.feedbackReasonRequired')
    return
  }
	caseSaving.value = true
	caseMessage.value = ''
	caseActionError.value = ''
	if (!resolutionRecovery.value) {
		if (!resolutionRequestID.value) resolutionRequestID.value = `review-${activeUser.value.case_id}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
		resolutionRecovery.value = { caseId: activeUser.value.case_id, userId: activeUser.value.id, feedback: feedback.value, reason: trimmedReason, accountAction: accountAction.value, expectedRevision: activeUser.value.case_revision || 0, requestId: resolutionRequestID.value }
		if (!persistResolutionRecovery()) {
			resolutionRecovery.value = null
			resolutionRequestID.value = ''
			caseActionError.value = '无法保存结案恢复信息，请检查浏览器会话存储后重试'
			caseSaving.value = false
			return
		}
	}
	try {
	const intent = resolutionRecovery.value!
	const result = await userRiskControlV2API.resolveReviewCase(intent.caseId, intent.userId, intent.feedback, intent.reason, intent.accountAction, intent.expectedRevision, intent.requestId)
	resolutionResult.value = result
	if (result.account.after_status === 'active' || result.account.after_status === 'disabled') {
		const afterStatus = result.account.after_status as AccountStatus
		currentStatus.value = afterStatus
		const partiallyUpdated: RiskUserRow = { ...activeUser.value, status: afterStatus }
		investigationStack.value[investigationStack.value.length - 1] = partiallyUpdated
		emit('updated', partiallyUpdated)
	}
	if (result.result === 'partial') {
		let recoveryStorageError = ''
		let canClearResolutionRecovery = result.case.result === 'resolved'
		if (result.case.result === 'resolved') {
			currentCaseStatus.value = 'resolved'
			const caseUpdated: RiskUserRow = { ...activeUser.value, case_status: 'resolved', processing_status: 'resolved', pending: false, resolution_reason: trimmedReason }
			investigationStack.value[investigationStack.value.length - 1] = caseUpdated
			emit('updated', caseUpdated)
		}
		if (intent.accountAction !== 'none' && result.account.retryable && result.account.pending_step) {
			accountRecovery.value = { reason: intent.reason, requestId: result.request_id, status: intent.accountAction === 'disable' ? 'disabled' : 'active', pendingStep: result.account.pending_step }
			accountActionWarning.value = `部分完成：${result.account.failure_reason || '案件已结案，账号步骤仍需恢复'}`
			if (!persistAccountRecovery()) {
				canClearResolutionRecovery = false
				recoveryStorageError = '案件已结案，但无法保存账号恢复信息；请勿关闭抽屉并重试未完成步骤。'
			}
		}
		if (canClearResolutionRecovery) clearResolutionRecovery()
		else if (!result.retryable) clearResolutionRecovery()
		caseActionError.value = recoveryStorageError || (result.retryable ? result.case.result === 'resolved' ? '案件已结案，账号步骤未完成，可在下方直接重试。' : '部分步骤未完成，可直接重试未完成步骤。' : '案件状态已变化，请重新加载后继续。')
		return
	}
	clearResolutionRecovery()
	currentCaseStatus.value = 'resolved'
	feedbackReason.value = ''
	caseMessage.value = t('admin.userRiskControl.submitFeedbackSuccess')
	const updated = { ...activeUser.value, status: (result.account.after_status || activeUser.value.status) as AccountStatus, case_status: 'resolved' as const, processing_status: 'resolved', pending: false, resolution_reason: trimmedReason }
	investigationStack.value[investigationStack.value.length - 1] = updated
	emit('updated', updated)
	} catch (error) {
		if (isDefinitiveStatusFailure(error)) {
			clearResolutionRecovery()
			resolutionResult.value = null
			caseActionError.value = errorMessage(error, t('admin.userRiskControl.caseActionFailed'))
		} else {
			caseActionError.value = `结案请求结果未知，已保留原请求：${errorMessage(error, t('admin.userRiskControl.caseActionFailed'))}`
		}
  } finally {
    caseSaving.value = false
  }
}
async function reloadPartialCase() {
	if (!activeUser.value.case_id || caseSaving.value) return
	caseSaving.value = true
	caseActionError.value = ''
	try {
		const [reviewCase, detail] = await Promise.all([userRiskControlV2API.getReviewCase(activeUser.value.case_id), userRiskControlV2API.getUserDetail(activeUser.value.id).catch(() => null)])
		if (reviewCase.user_id !== activeUser.value.id) throw new Error('案件与当前账号不匹配')
		const refreshed = { ...activeUser.value, ...(detail?.user || {}), case_id: reviewCase.id, case_status: reviewCase.status, processing_status: reviewCase.status, case_revision: reviewCase.revision, review_due_at: reviewCase.review_due_at, observation_goal: reviewCase.observation_goal, resolution_reason: reviewCase.resolution_reason }
		investigationStack.value[investigationStack.value.length - 1] = refreshed
		currentStatus.value = refreshed.status
		currentCaseStatus.value = reviewCase.status
		resolutionResult.value = null
		resolutionRequestID.value = ''
		caseMessage.value = '案件已重新加载'
		emit('updated', refreshed)
	} catch (error) {
		caseActionError.value = error instanceof Error && error.message.trim() ? error.message : '案件重新加载失败'
	} finally { caseSaving.value = false }
}
function openConfirmation() { if (!accountActionable.value || (currentStatus.value !== 'active' && currentStatus.value !== 'disabled')) return; confirming.value = true; secondaryActionsOpen.value = false; validationError.value = ''; accountActionError.value = ''; reason.value = '' }
function closeConfirmation() { if (!saving.value) confirming.value = false }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function defaultObservationDueAt() { const due = new Date(Date.now() + 24 * 60 * 60 * 1000); const local = new Date(due.getTime() - due.getTimezoneOffset() * 60000); return local.toISOString().slice(0, 16) }
function observationInput() {
	const due = new Date(observationDueAt.value)
	const goal = observationGoal.value.trim()
	if (!goal || Number.isNaN(due.getTime()) || due.getTime() <= Date.now()) { caseActionError.value = '转入观察必须填写未来复查时间和观察目标'; return null }
	return { reviewDueAt: due.toISOString(), goal }
}
function caseStatusLabel(value?: RiskCaseStatus) { return ({ pending: t('admin.userRiskControl.viewPending'), in_review: t('admin.userRiskControl.viewMine'), observing: t('admin.userRiskControl.viewObserving'), resolved: t('admin.userRiskControl.viewResolved') } as Record<string, string>)[value || ''] || t('admin.userRiskControl.noCases') }
function resolutionStepLabel(value?: string) { return ({ success: '已完成', failed: '失败', skipped: '无需执行', not_executed: '未执行', resolved: '已结案', partial: '部分完成' } as Record<string, string>)[value || ''] || '状态未知' }
let legacyRequest = 0
async function loadLegacy() {
  const request = ++legacyRequest
  const requiresCompletion = !activeUserCompleted.value
	legacyError.value = ''
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
	catch (error) { if (request === legacyRequest) legacyError.value = error instanceof Error && error.message.trim() ? `历史详情加载失败：${error.message}` : '历史详情加载失败' }
  finally { if (request === legacyRequest) legacyLoading.value = false }
}
function statusFromError(error: unknown) { if (typeof error !== 'object' || error === null) return 0; const direct = Number((error as { status?: unknown }).status); if (Number.isFinite(direct) && direct > 0) return direct; const nested = Number((error as { response?: { status?: unknown } }).response?.status); return Number.isFinite(nested) && nested > 0 ? nested : 0 }
function isDefinitiveStatusFailure(error: unknown) { const status = statusFromError(error); return status >= 400 && status < 500 && status !== 408 && status !== 425 && status !== 429 }
function errorMessage(error: unknown, fallback: string) { if (typeof error === 'object' && error !== null && typeof (error as { message?: unknown }).message === 'string' && String((error as { message: string }).message).trim()) return String((error as { message: string }).message); return error instanceof Error && error.message.trim() ? error.message : fallback }
function accountRecoveryKey() { return `sub2api:risk-account-recovery:${activeUser.value.id}` }
function persistAccountRecovery() { try { if (accountRecovery.value) sessionStorage.setItem(accountRecoveryKey(), JSON.stringify(accountRecovery.value)) } catch { return false } emit('status-recovery', activeUser.value.id, Boolean(accountRecovery.value)); return true }
function clearAccountRecovery() { try { sessionStorage.removeItem(accountRecoveryKey()) } catch (error) { void error } accountRecovery.value = null; emit('status-recovery', activeUser.value.id, false) }
function restoreAccountRecovery() {
	try {
		const raw = sessionStorage.getItem(accountRecoveryKey())
		const value = raw ? JSON.parse(raw) as Partial<AccountRecovery> : null
		accountRecovery.value = value?.reason && value.requestId && value.status && value.pendingStep ? value as AccountRecovery : null
	} catch { accountRecovery.value = null }
	emit('status-recovery', activeUser.value.id, Boolean(accountRecovery.value))
	accountActionWarning.value = accountRecovery.value ? accountRecovery.value.pendingStep === 'status_confirmation' ? '上次请求结果未知，请使用原请求确认' : '账号状态已更新，仍有步骤需要恢复' : ''
}
function resolutionRecoveryKey() { return `sub2api:risk-case-resolution-recovery:${activeUser.value.case_id || 0}` }
function persistResolutionRecovery() { try { if (resolutionRecovery.value) sessionStorage.setItem(resolutionRecoveryKey(), JSON.stringify(resolutionRecovery.value)); return true } catch { return false } }
function clearResolutionRecovery() { try { sessionStorage.removeItem(resolutionRecoveryKey()) } catch (error) { void error } resolutionRecovery.value = null; resolutionRequestID.value = '' }
function restoreResolutionRecovery() {
	try {
		const raw = sessionStorage.getItem(resolutionRecoveryKey())
		const value = raw ? JSON.parse(raw) as Partial<ResolutionRecovery> : null
		const valid = value && value.caseId === activeUser.value.case_id && value.userId === activeUser.value.id && value.reason && value.requestId && value.feedback && value.accountAction && value.expectedRevision
		resolutionRecovery.value = valid ? value as ResolutionRecovery : null
	} catch { resolutionRecovery.value = null }
	const recovery = resolutionRecovery.value
	if (recovery) {
		feedback.value = recovery.feedback
		feedbackReason.value = recovery.reason
		accountAction.value = recovery.accountAction
		resolutionRequestID.value = recovery.requestId
		caseActionError.value = '上次结案请求结果未知，已保留原请求，可直接重试确认。'
	}
}
function resetActiveState() { currentStatus.value = activeUser.value.status; currentCaseStatus.value = activeUser.value.case_status; identityTab.value = 'summary'; secondaryActionsOpen.value = false; confirming.value = false; manualCaseReason.value = ''; manualCaseError.value = ''; observationDueAt.value = activeUser.value.review_due_at ? new Date(new Date(activeUser.value.review_due_at).getTime() - new Date(activeUser.value.review_due_at).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : defaultObservationDueAt(); observationGoal.value = activeUser.value.observation_goal || ''; resolutionResult.value = null; resolutionRequestID.value = ''; resolutionRecovery.value = null; accountActionWarning.value = ''; accountActionError.value = ''; restoreResolutionRecovery(); restoreAccountRecovery(); legacyDetail.value = null; legacyError.value = ''; void loadLegacy() }
function investigateUser(item: AssociatedRiskUser) {
  const account = item.account
  const status: AccountStatus = account?.status === 'active' || account?.status === 'disabled' || account?.status === 'pending' ? account.status : 'pending'
  investigationStack.value = [...investigationStack.value, { id: item.user_id, email: account?.email || '', username: account?.username || '', status, risk_score: null, risk_level: null, risk_type: null, account_availability: account?.availability || 'unavailable' }]
  completionStack.value = [...completionStack.value, false]
  resetActiveState()
}
function goBack() { if (investigationStack.value.length < 2) return; investigationStack.value = investigationStack.value.slice(0, -1); completionStack.value = completionStack.value.slice(0, -1); resetActiveState() }
restoreResolutionRecovery()
restoreAccountRecovery()
void loadLegacy()
</script>
