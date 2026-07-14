<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p>
          <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.rulesTitle') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.rulesDescription') }}</p>
        </div>
        <button type="button" class="btn btn-primary" data-testid="new-rule" @click="openCreateForm"><Icon name="plus" size="sm" />新建规则</button>
      </header>

      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <div v-if="notice" class="rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700" data-testid="save-notice">{{ notice }}</div>

      <section v-if="createOpen" class="border-y border-gray-200 py-5 dark:border-dark-700" data-testid="create-rule-form">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
          <div><h2 class="font-semibold text-gray-900 dark:text-white">新建场景规则</h2><p class="mt-1 text-xs text-gray-500">编码用于 API 协议和历史审计，创建后不可修改。</p></div>
          <button type="button" class="btn btn-ghost btn-sm" @click="createOpen = false">{{ t('common.close') }}</button>
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <button v-for="template in ruleTemplates" :key="template.code" type="button" class="rounded-md border border-gray-200 px-3 py-2 text-xs text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-700 dark:text-gray-300" :data-testid="`template-${template.code}`" @click="applyTemplate(template)">{{ template.name }}</button>
        </div>
        <div class="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <label class="text-sm text-gray-600 dark:text-gray-300">规则编码<input v-model="draft.code" data-testid="rule-code-input" class="form-input mt-1 w-full" placeholder="login_failure_burst" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">规则名称<input v-model="draft.name" data-testid="rule-name-input" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300 md:col-span-2">规则说明<input v-model="draft.description" data-testid="rule-description-input" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">事件类型<select v-model="draft.eventTypes[0]" data-testid="rule-event-type" class="form-input mt-1 w-full"><option v-for="option in riskTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">时间窗口（秒）<input v-model.number="draft.windowSeconds" type="number" min="1" data-testid="rule-window" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">触发阈值<input v-model.number="draft.threshold" type="number" min="1" data-testid="rule-threshold-create" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">风险分<input v-model.number="draft.score" type="number" min="0" max="100" data-testid="rule-score-create" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">风险等级<select v-model="draft.riskLevel" data-testid="rule-level-create" class="form-input mt-1 w-full"><option v-for="option in riskLevelOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">处置动作<select v-model="draft.action" data-testid="rule-action-create" class="form-input mt-1 w-full"><option v-for="option in ruleActionOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
        </div>
        <p v-if="createValidationError" class="mt-3 text-sm text-red-600" data-testid="create-rule-error">{{ createValidationError }}</p>
        <div class="mt-5 flex justify-end gap-3"><button type="button" class="btn btn-secondary" @click="createOpen = false">{{ t('common.cancel') }}</button><button type="button" class="btn btn-primary" data-testid="create-rule" :disabled="creating" @click="submitCreate">{{ creating ? t('common.saving') : '创建规则' }}</button></div>
      </section>

      <div v-if="loading" class="card px-5 py-16 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="!rules.length" class="card px-5 py-16 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.empty') }}<button type="button" class="btn btn-secondary mt-4" @click="openCreateForm">新建第一条规则</button></div>
      <section v-else class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="w-full min-w-[1120px] table-fixed divide-y divide-gray-200 dark:divide-dark-700" data-testid="risk-rules-table">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="w-72 px-4 py-3 text-left text-xs font-medium text-gray-500">规则</th>
                <th class="w-36 px-4 py-3 text-left text-xs font-medium text-gray-500">事件类型</th>
                <th class="w-24 px-4 py-3 text-left text-xs font-medium text-gray-500">状态</th>
                <th class="w-40 px-4 py-3 text-left text-xs font-medium text-gray-500">触发条件</th>
                <th class="w-36 px-4 py-3 text-left text-xs font-medium text-gray-500">风险</th>
                <th class="w-32 px-4 py-3 text-left text-xs font-medium text-gray-500">处置动作</th>
                <th class="w-auto px-4 py-3 text-right text-xs font-medium text-gray-500">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <template v-for="rule in rules" :key="rule.id">
                <tr class="hover:bg-gray-50 dark:hover:bg-dark-800/60">
                  <td class="px-4 py-4"><p class="truncate font-medium text-gray-900 dark:text-white" :title="rule.name || rule.code">{{ rule.name || rule.code }}</p><p class="mt-0.5 truncate text-xs text-gray-500" :title="rule.description || rule.code">{{ rule.code }} · 第 {{ rule.revision }} 版<span v-if="rule.description"> · {{ rule.description }}</span></p></td>
                  <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ rule.eventTypes?.map(formatRiskType).join('、') || '-' }}</td>
                  <td class="px-4 py-4"><span class="rounded-full px-2 py-1 text-xs font-medium" :class="rule.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ rule.enabled ? '已启用' : '已停用' }}</span></td>
                  <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ rule.threshold }} 次 / {{ rule.windowSeconds }} 秒</td>
                  <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300"><span class="font-semibold text-gray-900 dark:text-white">{{ rule.score }}</span><span class="ml-2">{{ formatRiskLevel(rule.riskLevel) }}</span></td>
                  <td class="px-4 py-4 text-sm text-gray-600 dark:text-gray-300">{{ formatRiskAction(rule.action) }}</td>
                  <td class="px-4 py-4 text-right"><button type="button" class="btn-ghost btn-icon" :data-testid="`edit-rule-${rule.id}`" :title="expandedRuleId === rule.id ? '收起规则编辑' : '编辑规则'" :aria-label="expandedRuleId === rule.id ? '收起规则编辑' : '编辑规则'" @click="toggleEditor(rule.id)"><Icon :name="expandedRuleId === rule.id ? 'chevronUp' : 'edit'" size="sm" /></button></td>
                </tr>
                <tr v-if="expandedRuleId === rule.id" :data-testid="`rule-editor-${rule.id}`" class="bg-gray-50/80 dark:bg-dark-800/60">
                  <td colspan="7" class="px-4 py-4">
                    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-6">
                      <label class="text-sm text-gray-600 dark:text-gray-300">启用状态<span class="mt-1 flex h-10 items-center gap-2"><input v-model="rule.enabled" type="checkbox" class="rounded border-gray-300 text-primary-600" />{{ rule.enabled ? '已启用' : '已停用' }}</span></label>
                      <label class="text-sm text-gray-600 dark:text-gray-300">窗口（秒）<input v-model.number="rule.windowSeconds" type="number" min="1" class="form-input mt-1 w-full" /></label>
                      <label class="text-sm text-gray-600 dark:text-gray-300">阈值<input v-model.number="rule.threshold" type="number" min="1" class="form-input mt-1 w-full" data-testid="rule-threshold" /></label>
                      <label class="text-sm text-gray-600 dark:text-gray-300">风险分<input v-model.number="rule.score" type="number" min="0" max="100" class="form-input mt-1 w-full" /></label>
                      <label class="text-sm text-gray-600 dark:text-gray-300">风险等级<select v-model="rule.riskLevel" class="form-input mt-1 w-full"><option v-for="option in riskLevelOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
                      <label class="text-sm text-gray-600 dark:text-gray-300">处置动作<select v-model="rule.action" class="form-input mt-1 w-full" data-testid="rule-action"><option v-for="option in ruleActionOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
                    </div>
                    <div class="mt-4 flex flex-wrap items-center gap-3">
                      <button type="button" class="btn btn-primary" data-testid="save-rule" :disabled="saving" @click="save(rule)">{{ saving ? t('common.saving') : t('common.save') }}</button>
                      <button type="button" class="btn btn-secondary" data-testid="test-rule" :disabled="testing" @click="test(rule)">{{ testing ? t('common.loading') : t('admin.userRiskControl.testRule') }}</button>
                      <button v-if="conflictRuleId === rule.id" type="button" class="btn btn-secondary" data-testid="reload-rule" @click="reloadRule(rule.id)">重新加载</button>
                      <span v-if="testResult && testedId === rule.id" class="text-sm text-gray-600 dark:text-gray-300" data-testid="rule-test-result">{{ testResult.matched ? '命中' : '未命中' }} · 风险分 {{ testResult.score }} · {{ formatRiskLevel(testResult.riskLevel) }} · {{ formatRiskAction(testResult.action) }}<span v-if="testResult.conditions.length" class="block text-xs">命中条件：{{ testResult.conditions.join('、') }}</span></span>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
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
const createValidationError = ref('')
const conflictRuleId = ref<number | null>(null)
const testResult = ref<{ matched: boolean; score: number; riskLevel: string; action: string; conditions: string[] } | null>(null)
const testedId = ref<number | null>(null)
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
const draft = reactive<RuleCreateInput>({ ...ruleTemplates[0], eventTypes: [...(ruleTemplates[0].eventTypes || [])] })

function errorMessage(err: unknown, fallback = t('admin.userRiskControl.loadFailed')) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
async function load() { loading.value = true; try { rules.value = await userRiskControlV2API.listRules() } catch (err) { error.value = errorMessage(err) } finally { loading.value = false } }
function openCreateForm() { createOpen.value = true; createValidationError.value = ''; notice.value = '' }
function toggleEditor(id: number) { expandedRuleId.value = expandedRuleId.value === id ? null : id; notice.value = ''; testResult.value = null; testedId.value = null }
function applyTemplate(template: RuleCreateInput) { Object.assign(draft, template, { eventTypes: [...(template.eventTypes || [])] }); createValidationError.value = '' }
function validateDraft() {
  if (!/^[a-z0-9][a-z0-9_-]{1,79}$/.test(draft.code.trim())) return '规则编码只能使用小写字母、数字、下划线和短横线。'
  if (!draft.name?.trim()) return '规则名称不能为空。'
  if (!draft.eventTypes?.[0]) return '请选择事件类型。'
  if (!Number.isFinite(draft.windowSeconds) || draft.windowSeconds <= 0) return '时间窗口必须大于 0。'
  if (!Number.isFinite(draft.threshold) || draft.threshold <= 0) return '阈值必须大于 0。'
  if (!Number.isFinite(draft.score) || draft.score < 0 || draft.score > 100) return '风险分必须在 0 到 100 之间。'
  if (!draft.riskLevel || !draft.action) return '风险等级和处置动作不能为空。'
  return ''
}
async function submitCreate() {
  const validation = validateDraft()
  if (validation) { createValidationError.value = validation; return }
  creating.value = true; error.value = ''; notice.value = ''
  try { const created = await userRiskControlV2API.createRule({ ...draft, code: draft.code.trim(), name: draft.name?.trim(), eventTypes: [draft.eventTypes?.[0] || ''] }); rules.value = [created, ...rules.value]; createOpen.value = false; notice.value = '规则已创建' } catch (err) { createValidationError.value = errorMessage(err, '规则创建失败') } finally { creating.value = false }
}
async function save(rule: Rule) { saving.value = true; error.value = ''; notice.value = ''; conflictRuleId.value = null; try { const result = await userRiskControlV2API.updateRule(rule.id, rule); rule.revision = result.revision; notice.value = '规则已保存' } catch (err) { conflictRuleId.value = rule.id; error.value = errorMessage(err, t('admin.userRiskControl.saveFailed')) } finally { saving.value = false } }
async function reloadRule(_id: number) { try { const latest = await userRiskControlV2API.listRules(); rules.value = latest; conflictRuleId.value = null } catch (err) { error.value = errorMessage(err) } }
async function test(rule: Rule) { testing.value = true; error.value = ''; try { const result = await userRiskControlV2API.testRule(rule, { event_type: rule.eventTypes?.[0] || rule.code, count: rule.threshold }); testResult.value = { matched: result.matched, score: result.score, riskLevel: result.riskLevel || rule.riskLevel, action: result.action || rule.action, conditions: result.conditions || [] }; testedId.value = rule.id } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.testFailed')) } finally { testing.value = false } }
onMounted(load)
</script>
