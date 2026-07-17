<template>
  <TablePageLayout :title="t('admin.userRiskControl.rulesTitle')" :description="t('admin.userRiskControl.rulesDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
              <div v-if="notice" class="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-700/40 dark:bg-emerald-900/20 dark:text-emerald-300" data-testid="save-notice">{{ notice }}</div>
            </div>
            <button type="button" class="btn btn-primary" data-testid="new-rule" @click="openCreateForm">
              <Icon name="plus" size="sm" />
              新建规则
            </button>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-end gap-3" data-testid="rule-filters">
          <div class="w-full sm:w-72">
            <label class="input-label">规则名称或编码</label>
            <SearchInput v-model="ruleSearch" data-testid="rule-search" placeholder="搜索规则名称、编码或说明" />
          </div>
          <div class="w-full sm:w-40">
            <label class="input-label">启用状态</label>
            <Select v-model="enabledFilter" data-testid="rule-enabled-filter" :options="enabledFilterOptions" />
          </div>
          <div class="w-full sm:w-40">
            <label class="input-label">风险等级</label>
            <Select v-model="levelFilter" data-testid="rule-level-filter" :options="levelFilterOptions" />
          </div>
        </div>
      </template>

      <template #table>
        <div data-testid="risk-rules-table">
          <DataTable
            :columns="columns"
            :data="filteredRules"
            :loading="loading"
            row-key="id"
          >
            <template #cell-rule="{ row }">
              <div class="min-w-0 text-left">
                <p class="font-medium text-gray-900 dark:text-white" :title="row.name || row.code">{{ row.name || row.code }}</p>
                <p class="mt-0.5 max-w-sm whitespace-normal text-xs text-gray-500 dark:text-gray-400" :title="row.description || row.code">
                  {{ row.code }} · 第 {{ row.revision }} 版<span v-if="row.description"> · {{ row.description }}</span>
                </p>
              </div>
            </template>
            <template #cell-eventTypes="{ row }">{{ row.eventTypes?.map(formatRiskType).join('、') || '-' }}</template>
            <template #cell-enabled="{ row }">
              <span class="rounded-md px-2 py-0.5 text-xs font-medium" :class="row.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ row.enabled ? '已启用' : '已停用' }}</span>
            </template>
            <template #cell-condition="{ row }">{{ row.threshold }} 次 / {{ row.windowSeconds }} 秒</template>
            <template #cell-risk="{ row }"><span class="font-semibold">{{ row.score }}</span><span class="ml-2">{{ formatRiskLevel(row.riskLevel) }}</span></template>
            <template #cell-action="{ row }">{{ formatRiskAction(row.action) }}</template>
            <template #cell-actions="{ row }">
              <div class="flex justify-end">
                <button type="button" class="btn btn-ghost btn-icon" :data-testid="`edit-rule-${row.id}`" title="编辑规则" aria-label="编辑规则" @click.stop="toggleEditor(row.id)">
                  <Icon name="edit" size="sm" />
                </button>
              </div>
            </template>
            <template #empty>
              <EmptyState title="暂无场景规则" description="新建第一条规则后，可在这里调整阈值并测试命中结果。" action-text="新建第一条规则" @action="openCreateForm" />
            </template>
          </DataTable>
        </div>
      </template>
    </TablePageLayout>

    <BaseDialog :show="createOpen" title="新建场景规则" width="wide" :close-on-click-outside="true" @close="closeCreateForm">
      <form id="create-risk-rule-form" data-testid="create-rule-form" class="space-y-5" @submit.prevent="submitCreate">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">选择规则模板</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">模板只会预填表单，保存前仍可调整全部参数。</p>
          <div class="mt-3 flex flex-wrap gap-2">
            <button
              v-for="template in ruleTemplates"
              :key="template.code"
              type="button"
              class="rounded-md border px-3 py-2 text-xs font-medium transition-colors"
              :class="selectedTemplateCode === template.code ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-950/30 dark:text-primary-300' : 'border-gray-200 text-gray-600 hover:border-primary-300 hover:text-primary-600 dark:border-dark-700 dark:text-gray-300'"
              :aria-pressed="selectedTemplateCode === template.code"
              :data-testid="`template-${template.code}`"
              @click="applyTemplate(template)"
            >
              {{ template.name }}
            </button>
          </div>
        </div>

        <section class="space-y-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">基础信息</h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">规则编码用于 API 协议和历史审计，创建后不可修改。</p>
          </div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div><label for="rule-code-input" class="input-label">规则编码 <span class="text-red-500">*</span></label><input id="rule-code-input" v-model="draft.code" class="input w-full" required data-testid="rule-code-input" placeholder="login_failure_burst" /></div>
            <div><label for="rule-name-input" class="input-label">规则名称 <span class="text-red-500">*</span></label><input id="rule-name-input" v-model="draft.name" class="input w-full" required data-testid="rule-name-input" /></div>
            <div class="sm:col-span-2"><label for="rule-description-input" class="input-label">规则说明</label><input id="rule-description-input" v-model="draft.description" class="input w-full" data-testid="rule-description-input" /></div>
            <div>
              <label class="input-label">事件类型 <span class="text-red-500">*</span></label>
              <Select v-model="draft.eventTypes[0]" data-testid="rule-event-type" :options="riskTypeOptions" />
            </div>
          </div>
        </section>

        <section class="space-y-3">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">触发与处置</h4>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
            <div><label for="rule-window" class="input-label">时间窗口（秒）</label><input id="rule-window" v-model.number="draft.windowSeconds" type="number" min="1" data-testid="rule-window" class="input w-full" /></div>
            <div><label for="rule-threshold-create" class="input-label">触发阈值（次）</label><input id="rule-threshold-create" v-model.number="draft.threshold" type="number" min="1" data-testid="rule-threshold-create" class="input w-full" /></div>
            <div><label for="rule-score-create" class="input-label">风险分</label><input id="rule-score-create" v-model.number="draft.score" type="number" min="0" max="100" data-testid="rule-score-create" class="input w-full" /></div>
            <div><label class="input-label">风险等级</label><Select v-model="draft.riskLevel" data-testid="rule-level-create" :options="riskLevelOptions" /></div>
            <div><label class="input-label">处置动作</label><Select v-model="draft.action" data-testid="rule-action-create" :options="ruleActionOptions" /></div>
          </div>
        </section>
        <p v-if="createValidationError" class="text-sm text-red-600 dark:text-red-300" data-testid="create-rule-error">{{ createValidationError }}</p>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeCreateForm">{{ t('common.cancel') }}</button>
        <button type="submit" form="create-risk-rule-form" class="btn btn-primary" data-testid="create-rule" :disabled="creating">{{ creating ? t('common.saving') : '创建规则' }}</button>
      </template>
    </BaseDialog>

    <BaseDialog :show="Boolean(editDraft)" title="编辑场景规则" width="wide" :close-on-click-outside="true" @close="closeEditor">
      <form v-if="editDraft" :data-testid="`rule-editor-${editDraft.id}`" class="space-y-5" @submit.prevent="save(editDraft)">
        <div class="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800">
          <p class="font-medium text-gray-900 dark:text-white">{{ editDraft.name || editDraft.code }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ editDraft.code }} · 第 {{ editDraft.revision }} 版</p>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div class="sm:col-span-2 lg:col-span-3">
            <label class="input-label">启用状态</label>
            <div class="flex items-center gap-3">
              <Toggle v-model="editDraft.enabled" />
              <span class="text-sm text-gray-600 dark:text-gray-300">{{ editDraft.enabled ? '已启用' : '已停用' }}</span>
            </div>
          </div>
          <div><label class="input-label">时间窗口（秒）</label><input v-model.number="editDraft.windowSeconds" type="number" min="1" class="input w-full" /></div>
          <div><label class="input-label">触发阈值（次）</label><input v-model.number="editDraft.threshold" type="number" min="1" class="input w-full" data-testid="rule-threshold" /></div>
          <div><label class="input-label">风险分</label><input v-model.number="editDraft.score" type="number" min="0" max="100" class="input w-full" /></div>
          <div><label class="input-label">风险等级</label><Select v-model="editDraft.riskLevel" :options="riskLevelOptions" /></div>
          <div><label class="input-label">处置动作</label><Select v-model="editDraft.action" data-testid="rule-action" :options="ruleActionOptions" /></div>
        </div>
        <div v-if="testResult && testedId === editDraft.id" class="rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200" data-testid="rule-test-result">
          {{ testResult.matched ? '命中' : '未命中' }} · 风险分 {{ testResult.score }} · {{ formatRiskLevel(testResult.riskLevel) }} · {{ formatRiskAction(testResult.action) }}
          <span v-if="testResult.conditions.length" class="mt-1 block text-xs text-gray-500">命中条件：{{ testResult.conditions.join('、') }}</span>
        </div>
        <p v-if="editValidationError" class="text-sm text-red-600 dark:text-red-300" data-testid="edit-rule-error">{{ editValidationError }}</p>
      </form>
      <template #footer>
        <button v-if="editDraft && conflictRuleId === editDraft.id" type="button" class="btn btn-secondary" data-testid="reload-rule" @click="reloadRule(editDraft.id)">重新加载</button>
        <button v-if="editDraft" type="button" class="btn btn-secondary" data-testid="test-rule" :disabled="testing" @click="test(editDraft)">{{ testing ? t('common.loading') : t('admin.userRiskControl.testRule') }}</button>
        <button type="button" class="btn btn-secondary" @click="closeEditor">{{ t('common.cancel') }}</button>
        <button v-if="editDraft" type="button" class="btn btn-primary" data-testid="save-rule" :disabled="saving" @click="save(editDraft)">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </template>
    </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import UserRiskControlTabs from '@/views/admin/extensions/UserRiskControlTabs.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { userRiskControlV2API, type Rule, type RuleCreateInput } from '@/api/admin/userRiskControlV2'
import { formatRiskAction, formatRiskLevel, formatRiskType, riskActionOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const rules = ref<Rule[]>([])
const loading = ref(true)
const saving = ref(false)
const creating = ref(false)
const testing = ref(false)
const error = ref('')
const notice = ref('')
const createOpen = ref(false)
const expandedRuleId = ref<number | null>(null)
const editDraft = ref<Rule | null>(null)
const selectedTemplateCode = ref('')
const createValidationError = ref('')
const editValidationError = ref('')
const conflictRuleId = ref<number | null>(null)
const testResult = ref<{ matched: boolean; score: number; riskLevel: string; action: string; conditions: string[] } | null>(null)
const testedId = ref<number | null>(null)
const columns: Column[] = [
  { key: 'rule', label: '规则', sortable: true },
  { key: 'eventTypes', label: '事件类型' },
  { key: 'enabled', label: '状态', sortable: true },
  { key: 'condition', label: '触发条件', sortable: true },
  { key: 'risk', label: '风险', sortable: true },
  { key: 'action', label: '处置动作', sortable: true },
  { key: 'actions', label: '操作', class: 'text-right' },
]
const ruleSearch = ref('')
const enabledFilter = ref('')
const levelFilter = ref('')
const enabledFilterOptions = [{ value: '', label: '全部启用状态' }, { value: 'enabled', label: '已启用' }, { value: 'disabled', label: '已停用' }]
const levelFilterOptions = [{ value: '', label: '全部风险等级' }, ...riskLevelOptions]
const filteredRules = computed(() => {
  const search = ruleSearch.value.trim().toLocaleLowerCase()
  return rules.value
    .filter((rule) => !search || [rule.name, rule.code, rule.description, ...(rule.eventTypes || [])].some((value) => String(value || '').toLocaleLowerCase().includes(search)))
    .filter((rule) => enabledFilter.value === 'enabled' ? rule.enabled : enabledFilter.value === 'disabled' ? !rule.enabled : true)
    .filter((rule) => !levelFilter.value || rule.riskLevel === levelFilter.value)
    .map((rule) => ({ ...rule, rule: rule.name || rule.code, condition: rule.threshold, risk: rule.score }))
})
const ruleActionOptions = riskActionOptions.filter((option) => ['observe', 'review', 'ban', 'reject_candidate', 'auto_ban'].includes(option.value))
const ruleTemplates: RuleCreateInput[] = [
  { code: 'registration_abuse', name: '注册滥用', description: '短时间内重复注册或命中注册风险信号', eventTypes: ['registration_attempt'], enabled: true, windowSeconds: 600, threshold: 3, score: 80, riskLevel: 'critical', action: 'reject_candidate' },
  { code: 'login_failure_burst', name: '登录失败爆发', description: '同一账号连续登录失败', eventTypes: ['login_failure'], enabled: true, windowSeconds: 600, threshold: 5, score: 70, riskLevel: 'high', action: 'review' },
  { code: 'api_error_burst', name: 'API 错误爆发', description: '同一用户短时间内出现大量 API 错误', eventTypes: ['api_error'], enabled: true, windowSeconds: 300, threshold: 10, score: 35, riskLevel: 'medium', action: 'observe' },
  { code: 'content_risk', name: '内容风险', description: '命中内容安全策略', eventTypes: ['content_risk'], enabled: true, windowSeconds: 86400, threshold: 1, score: 85, riskLevel: 'high', action: 'review' },
  { code: 'quota_abuse', name: '配额滥用', description: '持续触发配额或计费限制', eventTypes: ['quota_exceeded'], enabled: true, windowSeconds: 3600, threshold: 5, score: 55, riskLevel: 'medium', action: 'review' },
  { code: 'upstream_error', name: '上游错误', description: '持续触发上游错误', eventTypes: ['upstream_error'], enabled: true, windowSeconds: 600, threshold: 8, score: 25, riskLevel: 'low', action: 'observe' },
  { code: 'api_request_observation', name: 'API 请求观察', description: '保留正常请求基线', eventTypes: ['api_request'], enabled: true, windowSeconds: 86400, threshold: 1, score: 0, riskLevel: 'low', action: 'observe' },
]
const draft = reactive<RuleCreateInput>({ ...ruleTemplates[0], eventTypes: [...ruleTemplates[0].eventTypes] })

function errorMessage(err: unknown, fallback = t('admin.userRiskControl.loadFailed')) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
async function load() { loading.value = true; try { rules.value = await userRiskControlV2API.listRules() } catch (err) { error.value = errorMessage(err) } finally { loading.value = false } }
function openCreateForm() { applyTemplate(ruleTemplates[0]); createOpen.value = true; notice.value = '' }
function closeCreateForm() { if (!creating.value) createOpen.value = false }
function closeEditor() { if (saving.value || testing.value) return; expandedRuleId.value = null; editDraft.value = null; testResult.value = null; testedId.value = null }
function toggleEditor(id: number) {
  const rule = rules.value.find((item) => item.id === id)
  if (!rule) return
  expandedRuleId.value = id
  editDraft.value = { ...rule, eventTypes: [...(rule.eventTypes || [])] }
  editValidationError.value = ''
  conflictRuleId.value = null
  notice.value = ''
  testResult.value = null
  testedId.value = null
}
function applyTemplate(template: RuleCreateInput) { Object.assign(draft, template, { eventTypes: [...template.eventTypes] }); selectedTemplateCode.value = template.code; createValidationError.value = '' }
function validateRuleFields(rule: Pick<RuleCreateInput, 'windowSeconds' | 'threshold' | 'score' | 'riskLevel' | 'action'>) {
  if (!Number.isFinite(rule.windowSeconds) || rule.windowSeconds <= 0) return '时间窗口必须大于 0。'
  if (!Number.isFinite(rule.threshold) || rule.threshold <= 0) return '阈值必须大于 0。'
  if (!Number.isFinite(rule.score) || rule.score < 0 || rule.score > 100) return '风险分必须在 0 到 100 之间。'
  if (!rule.riskLevel || !rule.action) return '风险等级和处置动作不能为空。'
  return ''
}
function validateDraft() {
  if (!/^[a-z0-9][a-z0-9_-]{1,79}$/.test(draft.code.trim())) return '规则编码只能使用小写字母、数字、下划线和短横线。'
  if (!draft.name?.trim()) return '规则名称不能为空。'
  if (!draft.eventTypes?.[0]) return '请选择事件类型。'
  return validateRuleFields(draft)
}
async function submitCreate() {
  const validation = validateDraft()
  if (validation) { createValidationError.value = validation; return }
  creating.value = true; error.value = ''; notice.value = ''
  try { const created = await userRiskControlV2API.createRule({ ...draft, code: draft.code.trim(), name: draft.name?.trim(), eventTypes: [draft.eventTypes[0]] }); rules.value = [created, ...rules.value]; createOpen.value = false; notice.value = '规则已创建' } catch (err) { createValidationError.value = errorMessage(err, '规则创建失败') } finally { creating.value = false }
}
async function save(rule: Rule) {
  const validation = validateRuleFields(rule)
  if (validation) { editValidationError.value = validation; return }
  saving.value = true; error.value = ''; notice.value = ''; conflictRuleId.value = null
  try {
    const result = await userRiskControlV2API.updateRule(rule.id, rule)
    const index = rules.value.findIndex((item) => item.id === rule.id)
    const updated = { ...rule, revision: result.revision }
    if (index >= 0) rules.value[index] = updated
    editDraft.value = updated
    notice.value = '规则已保存'
  } catch (err) {
    if (typeof err === 'object' && err !== null && 'status' in err && err.status === 409) conflictRuleId.value = rule.id
    error.value = errorMessage(err, t('admin.userRiskControl.saveFailed'))
  } finally { saving.value = false }
}
async function reloadRule(id: number) {
  try {
    const latest = await userRiskControlV2API.listRules()
    rules.value = latest
    conflictRuleId.value = null
    editValidationError.value = ''
    const rule = latest.find((item) => item.id === id)
    editDraft.value = rule ? { ...rule, eventTypes: [...(rule.eventTypes || [])] } : null
  } catch (err) { error.value = errorMessage(err) }
}
async function test(rule: Rule) {
  testing.value = true; error.value = ''
  try { const result = await userRiskControlV2API.testRule(rule, { event_type: rule.eventTypes?.[0] || rule.code, count: rule.threshold }); testResult.value = { matched: result.matched, score: result.score, riskLevel: result.riskLevel || rule.riskLevel, action: result.action || rule.action, conditions: result.conditions || [] }; testedId.value = rule.id } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.testFailed')) } finally { testing.value = false }
}
onMounted(load)
</script>
