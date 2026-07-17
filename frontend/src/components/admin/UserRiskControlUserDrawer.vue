<template>
  <Teleport to="body">
    <div v-if="user" class="fixed inset-0 z-[70] flex justify-end bg-gray-950/30" data-testid="risk-user-drawer" @click.self="emit('close')">
      <aside class="flex h-full w-full max-w-xl flex-col overflow-hidden bg-white shadow-2xl dark:bg-dark-800" role="dialog" aria-modal="true">
        <header class="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div>
            <p class="text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.drawer.title') }}</p>
            <h2 class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ primaryIdentity }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400"><span v-if="secondaryIdentity">{{ secondaryIdentity }} · </span>#{{ user.id }}</p>
          </div>
          <button type="button" class="btn btn-ghost" :aria-label="t('common.close')" @click="emit('close')"><Icon name="x" size="sm" /></button>
        </header>

        <div class="flex-1 overflow-y-auto p-5">
          <div v-if="loading" class="space-y-4" data-testid="risk-user-detail-skeleton" role="status" :aria-label="t('common.loading')"><div class="grid grid-cols-2 gap-3"><span v-for="index in 4" :key="index" class="h-20 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" /></div><div v-for="index in 4" :key="`row-${index}`" class="h-14 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" /></div>
          <div v-else-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
          <template v-else-if="detail">
            <section class="grid grid-cols-2 gap-3">
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.riskScore') }}</p><RiskScoreBadge class="mt-1" :score="detail.summary?.score ?? user.risk_score" :available="(detail.summary?.score ?? user.risk_score) !== null && (detail.summary?.score ?? user.risk_score) !== undefined && Boolean(detail.summary?.level || user.risk_level)" :explicit-level="detail.summary?.level || user.risk_level" /></div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.riskLevel') }}</p><p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatRiskLevel(detail.summary?.level ?? user.risk_level) }}</p></div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.ipAssociations') }}</p><p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ detail.associations?.ip_count ?? 0 }}</p></div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.deviceAssociations') }}</p><p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ detail.associations?.device_count ?? 0 }}</p></div>
            </section>
            <p v-if="detail.summary?.reason || user.risk_reason" class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300">{{ formatRiskReason(detail.summary?.reason || user.risk_reason) }}</p>

            <section class="mt-6">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.timeline') }}</h3>
              <div v-if="detail.events.length" class="mt-3 space-y-3 border-l border-gray-200 pl-4 dark:border-dark-700">
                <article v-for="event in detail.events" :key="event.id" class="relative rounded-lg border border-gray-200 p-3 dark:border-dark-700">
                  <span class="absolute -left-[1.35rem] top-4 h-2 w-2 rounded-full bg-primary-500" />
                  <div class="flex flex-wrap items-center justify-between gap-3"><strong class="text-sm text-gray-900 dark:text-white">{{ formatRiskType(event.risk_type || event.type) }}</strong><time class="text-xs text-gray-500">{{ formatDate(event.occurred_at) }}</time></div>
                  <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskReason(event.reason, { eventType: event.risk_type || event.type, ruleCode: event.rule_codes?.[0], errorCode: event.error_code }) }}</p>
                  <p class="mt-2 text-xs text-gray-500">{{ event.error_code || '无错误代码' }} · {{ event.endpoint || '无接口信息' }} · {{ event.model || '无模型信息' }}<span v-if="event.risk_level"> · {{ formatRiskLevel(event.risk_level) }}</span><span v-if="event.score !== undefined"> · 风险分 {{ event.score }}</span></p>
                  <div v-if="event.ip || event.device_id" class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
                    <span v-if="event.ip">IP：<span class="font-medium text-gray-700 dark:text-gray-300">{{ event.ip }}</span></span>
                    <span v-if="event.device_id">设备：<span class="font-medium text-gray-700 dark:text-gray-300">{{ event.device_id }}</span></span>
                  </div>
                  <p v-if="event.rule_codes?.length" class="mt-2 text-xs text-gray-500">命中规则：{{ event.rule_codes.join('、') }}</p>
                  <details v-if="event.evidence && Object.keys(event.evidence).length" class="mt-2 text-xs text-gray-500"><summary class="cursor-pointer">查看原始证据</summary><pre class="mt-2 max-w-full overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(event.evidence, null, 2) }}</pre></details>
                </article>
              </div>
              <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.drawer.noEvents') }}</p>
            </section>
            <section class="mt-6">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.drawer.history') }}</h3>
              <div v-if="detail.audit.length" class="mt-3 space-y-2">
                <div v-for="record in detail.audit" :key="record.id" class="flex items-start justify-between gap-3 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700"><span><strong>{{ formatRiskAction(record.action) }}</strong> · {{ formatAuditResult(record.result) }} · {{ record.reason || '无操作原因' }}<span v-if="record.failure_reason" class="block text-xs text-red-600">失败原因：{{ record.failure_reason }}</span></span><span class="shrink-0 text-xs text-gray-500">{{ formatDate(record.created_at) }}</span></div>
              </div>
              <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.drawer.noHistory') }}</p>
            </section>
          </template>
        </div>

        <footer v-if="detail" class="flex items-center justify-between border-t border-gray-200 px-5 py-4 dark:border-dark-700">
          <span class="text-sm text-gray-500">{{ formatAccountStatus(detail.user?.status) }}</span>
          <button v-if="detail.user?.status === 'active'" type="button" class="btn btn-danger" data-testid="ban-user" @click="openConfirmation">{{ t('admin.userRiskControl.ban') }}</button>
          <button v-else type="button" class="btn btn-primary" data-testid="unban-user" @click="openConfirmation">{{ t('admin.userRiskControl.unban') }}</button>
        </footer>
      </aside>
    </div>
    <ConfirmDialog
      :show="confirming"
      :title="detail?.user?.status === 'active' ? t('admin.userRiskControl.confirmBan') : t('admin.userRiskControl.confirmUnban')"
      :message="t('admin.userRiskControl.statusChangeMessage')"
      :danger="detail?.user?.status === 'active'"
      :confirm-text="saving ? t('common.saving') : t('common.confirm')"
      :close-on-click-outside="true"
      :z-index="80"
      @confirm="submitStatus"
      @cancel="closeConfirmation"
    >
      <div data-testid="status-confirmation">
        <TextArea v-model="reason" data-testid="status-reason" label="操作原因" required :placeholder="t('admin.userRiskControl.reasonPlaceholder')" :error="validationError" @update:model-value="validationError = ''" />
      </div>
    </ConfirmDialog>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TextArea from '@/components/common/TextArea.vue'
import { userRiskControlV2API, type RiskUserDetail, type RiskUserRow } from '@/api/admin/userRiskControlV2'
import RiskScoreBadge from '@/components/admin/RiskScoreBadge.vue'
import { formatAccountStatus, formatAuditResult, formatRiskAction, formatRiskLevel, formatRiskReason, formatRiskType } from '@/utils/userRiskControlLabels'

const props = defineProps<{ user: RiskUserRow }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'updated', user: RiskUserRow): void }>()
const { t } = useI18n()
const detail = ref<RiskUserDetail | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const confirming = ref(false)
const reason = ref('')
const validationError = ref('')
const currentUser = computed(() => detail.value?.user || props.user)
const primaryIdentity = computed(() => currentUser.value.email || currentUser.value.username || `用户 #${props.user.id}`)
const secondaryIdentity = computed(() => currentUser.value.username && currentUser.value.username !== primaryIdentity.value ? currentUser.value.username : '')

async function load() {
  try { detail.value = await userRiskControlV2API.getUserDetail(props.user.id) } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.loadFailed')) } finally { loading.value = false }
}
async function submitStatus() {
  if (!detail.value || saving.value) return
  if (!reason.value.trim()) {
    validationError.value = t('admin.userRiskControl.reasonRequired')
    return
  }
  saving.value = true
  try { const next = detail.value.user.status === 'active' ? 'disabled' : 'active'; const updated = await userRiskControlV2API.setUserStatus(props.user.id, next, reason.value.trim() || t('admin.userRiskControl.defaultReason')); const nextUser = updated || { ...detail.value.user, status: next }; detail.value.user = nextUser; confirming.value = false; reason.value = ''; emit('updated', nextUser) } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.statusFailed')) } finally { saving.value = false }
}
function openConfirmation() {
  confirming.value = true
  validationError.value = ''
  reason.value = ''
}
function closeConfirmation() { if (!saving.value) confirming.value = false }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function errorMessage(err: unknown, fallback: string) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
onMounted(load)
</script>
