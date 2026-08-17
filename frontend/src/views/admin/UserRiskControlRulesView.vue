<template>
  <TablePageLayout :title="t('admin.userRiskControl.rulesTitle')" :description="t('admin.userRiskControl.rulesDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
		  <div class="inline-flex max-w-full overflow-x-auto border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-800" role="tablist" aria-label="规则视图" data-testid="rule-views">
			<button v-for="option in ruleViews" :key="option.value" type="button" role="tab" class="min-h-9 whitespace-nowrap px-3 text-sm font-medium" :class="activeRuleView === option.value ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-selected="activeRuleView === option.value" :data-testid="`rule-view-${option.value}`" @click="setRuleView(option.value)">{{ option.label }}</button>
		  </div>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-700/40 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
              <div v-if="notice" class="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-700/40 dark:bg-emerald-900/20 dark:text-emerald-300" data-testid="save-notice">{{ notice }}</div>
            </div>
			<button v-if="activeRuleView === 'event'" type="button" class="btn btn-primary" data-testid="new-rule" @click="openCreateForm">
              <Icon name="plus" size="sm" />
              新建规则
            </button>
          </div>
		  <section v-if="activeRuleView === 'identity'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-v2-rules">
            <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">V2 身份规则</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">邮箱、真实 IP、浏览器实例和综合关联独立计算</p>
              </div>
              <span :class="identityRulesActive ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'" class="rounded-md px-2 py-1 text-xs font-medium">{{ identityRulesHeader }}</span>
            </div>
            <p v-if="identityRulesError" class="mb-3 text-sm text-red-600 dark:text-red-300" data-testid="identity-rules-error">{{ identityRulesError }}</p>
            <div class="overflow-x-auto">
              <table class="w-full min-w-[760px] text-left text-sm">
                <thead class="text-xs text-gray-500 dark:text-gray-400">
                  <tr><th class="pb-2 pr-4 font-medium">规则</th><th class="pb-2 pr-4 font-medium">独立域</th><th class="pb-2 pr-4 font-medium">触发条件</th><th class="pb-2 pr-4 font-medium">风险分</th><th class="pb-2 font-medium">状态</th></tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="rule in identityRules" :key="rule.code" :data-testid="`identity-rule-${rule.code}`">
                    <td class="py-2.5 pr-4"><p class="font-medium text-gray-900 dark:text-white">{{ identityRuleName(rule.code) }}</p><p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ rule.code }}</p></td>
                    <td class="py-2.5 pr-4">{{ identityDomainLabel(rule.domain) }}</td>
                    <td class="py-2.5 pr-4">{{ identityRuleCondition(rule) }}</td>
                    <td class="py-2.5 pr-4">{{ rule.score ? rule.score : '0（仅观察）' }}</td>
                    <td class="py-2.5"><span :class="rule.enabled ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-gray-400'">{{ identityRuleStatus(rule) }}</span></td>
                  </tr>
                  <tr v-if="!identityRules.length && !loading && !identityRulesError"><td colspan="5" class="py-4 text-center text-gray-500 dark:text-gray-400">暂无 V2 身份规则</td></tr>
                </tbody>
              </table>
            </div>
          </section>
		  <section v-else-if="activeRuleView === 'shadow'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rule-effects">
			<div class="mb-3 flex items-center justify-between gap-3"><h2 class="text-sm font-semibold text-gray-900 dark:text-white">Shadow 效果</h2><button type="button" class="btn btn-secondary btn-sm" :disabled="auxLoading" @click="loadEffects"><Icon name="refresh" size="sm" />刷新</button></div>
			<p v-if="auxError" class="mb-3 text-sm text-red-600 dark:text-red-300">{{ auxError }}</p>
			<div class="overflow-x-auto"><table class="w-full min-w-[760px] text-left text-sm"><thead class="text-xs text-gray-500 dark:text-gray-400"><tr><th class="pb-2 pr-4">规则</th><th class="pb-2 pr-4">命中事件</th><th class="pb-2 pr-4">唯一主体</th><th class="pb-2 pr-4">人工确认率</th><th class="pb-2 pr-4">正常共享率</th><th class="pb-2">缺失信号率</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="effect in ruleEffects" :key="`${effect.rule_code}:${effect.revision}`"><td class="py-2.5 pr-4"><span class="font-medium">{{ identityRuleName(effect.rule_code) }}</span><span class="ml-2 text-xs text-gray-400">r{{ effect.revision }}</span><p v-if="effect.sample_user_ids?.length" class="mt-1 text-xs text-gray-400">样本 {{ effect.sample_user_ids.map((id) => `#${id}`).join('、') }}</p></td><td class="py-2.5 pr-4">{{ effect.hit_events }}</td><td class="py-2.5 pr-4">{{ effect.unique_subjects }}</td><td class="py-2.5 pr-4">{{ percentage(effect.confirmed_rate) }}</td><td class="py-2.5 pr-4">{{ percentage(effect.legitimate_shared_rate) }}</td><td class="py-2.5">{{ percentage(effect.missing_signal_rate) }}</td></tr><tr v-if="!ruleEffects.length && !auxLoading"><td colspan="6" class="py-4 text-center text-gray-500">暂无效果样本</td></tr></tbody></table></div>
		  </section>
		  <section v-else-if="activeRuleView === 'versions'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rule-versions">
			<div class="flex flex-wrap gap-2"><button v-for="rule in identityRules" :key="rule.code" type="button" class="btn btn-sm" :class="selectedIdentityRule === rule.code ? 'btn-primary' : 'btn-secondary'" @click="selectIdentityRule(rule.code)">{{ identityRuleName(rule.code) }}</button></div>
			<div v-if="selectedRule" class="mt-4 flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700"><div><strong class="text-sm text-gray-900 dark:text-white">{{ identityRuleName(selectedRule.code) }}</strong><p class="mt-1 text-xs text-gray-500">{{ selectedRule.code }} · r{{ selectedRule.revision }} · {{ identityRuleStatus(selectedRule) }}</p></div><button v-if="selectedRule.configured_enabled" type="button" class="btn btn-danger btn-sm" data-testid="disable-identity-rule" @click="openDisableRule(selectedRule)">停用</button></div>
			<p v-if="auxError" class="mt-3 text-sm text-red-600 dark:text-red-300">{{ auxError }}</p>
			<div class="mt-3 space-y-2"><div v-for="version in ruleVersions" :key="version.revision" class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 py-2 text-sm dark:border-dark-700"><span>r{{ version.revision }} · {{ version.signal_family }} · {{ version.domain }}</span><span :class="version.enabled ? 'text-amber-700' : 'text-gray-500'">{{ version.enabled ? 'Shadow' : '已停用' }} · {{ formatDate(version.active_from) }}<template v-if="version.active_until"> 至 {{ formatDate(version.active_until) }}</template></span></div></div>
		  </section>
		  <section v-else-if="activeRuleView === 'replay'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rebuild">
			<div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="text-sm font-semibold text-gray-900 dark:text-white">历史回放</h2><p v-if="rebuildResult" class="mt-1 text-xs text-gray-500">水位 {{ rebuildResult.evidence_high_water }} · 当前 {{ rebuildResult.current_signals }} 条 · 回放 {{ rebuildResult.v2_signals }} 条 · 变化主体 {{ rebuildResult.changed_subjects }}</p></div><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" data-testid="rebuild-dry-run" :disabled="auxLoading" @click="runRebuild(true)">Dry Run</button><button type="button" class="btn btn-primary btn-sm" data-testid="rebuild-apply" :disabled="auxLoading || !rebuildResult?.dry_run" @click="runRebuild(false)">执行回放</button></div></div>
			<p v-if="auxError" class="mt-3 text-sm text-red-600 dark:text-red-300">{{ auxError }}</p>
			<div v-if="rebuildResult" class="mt-3 overflow-x-auto"><table class="w-full min-w-[560px] text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="pb-2 pr-4">规则</th><th class="pb-2">命中主体</th></tr></thead><tbody><tr v-for="(count, code) in rebuildResult.rule_hits" :key="code"><td class="py-2 pr-4">{{ identityRuleName(code) }}</td><td class="py-2">{{ count }}</td></tr></tbody></table></div>
		  </section>
        </div>
      </template>

      <template #filters>
		<div v-if="activeRuleView === 'event'" class="flex flex-wrap items-end gap-3" data-testid="rule-filters">
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
		<div v-if="activeRuleView === 'event'" data-testid="risk-rules-table">
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
              <Select v-model="draft.eventTypes[0]" data-testid="rule-event-type" :options="ruleEventTypeOptions" />
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

	<BaseDialog :show="Boolean(disableTarget)" title="停用身份规则" width="narrow" :close-on-click-outside="true" @close="closeDisableRule">
		<p class="text-sm text-gray-600 dark:text-gray-300">{{ disableTarget ? identityRuleName(disableTarget.code) : '' }}</p>
		<TextArea v-model="disableReason" class="mt-4" data-testid="disable-rule-reason" label="停用原因" required :error="disableValidationError" @update:model-value="disableValidationError = ''" />
		<template #footer><button type="button" class="btn btn-secondary" @click="closeDisableRule">取消</button><button type="button" class="btn btn-danger" data-testid="confirm-disable-rule" :disabled="auxLoading" @click="disableSelectedRule">停用</button></template>
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
import TextArea from '@/components/common/TextArea.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { userRiskControlV2API, type IdentityRebuildResult, type IdentityRule, type IdentityRuleEffect, type IdentityRuleVersion, type Rule, type RuleCreateInput } from '@/api/admin/userRiskControlV2'
import { formatRiskAction, formatRiskLevel, formatRiskType, riskActionOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const rules = ref<Rule[]>([])
const identityRules = ref<IdentityRule[]>([])
const identityRulesError = ref('')
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
type RuleView = 'identity' | 'event' | 'replay' | 'shadow' | 'versions'
const activeRuleView = ref<RuleView>('event')
const ruleViews: Array<{ value: RuleView; label: string }> = [{ value: 'identity', label: '身份规则' }, { value: 'event', label: '事件规则' }, { value: 'replay', label: '历史回放' }, { value: 'shadow', label: 'Shadow 效果' }, { value: 'versions', label: '版本与停用' }]
const ruleEffects = ref<IdentityRuleEffect[]>([])
const ruleVersions = ref<IdentityRuleVersion[]>([])
const selectedIdentityRule = ref('')
const rebuildResult = ref<IdentityRebuildResult | null>(null)
const auxLoading = ref(false)
const auxError = ref('')
const disableTarget = ref<IdentityRule | null>(null)
const disableReason = ref('')
const disableValidationError = ref('')
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
const reliabilityEventTypes = new Set(['api_request', 'api_error', 'upstream_error'])
const ruleEventTypeOptions = riskTypeOptions.filter((option) => !reliabilityEventTypes.has(option.value))
const filteredRules = computed(() => {
  const search = ruleSearch.value.trim().toLocaleLowerCase()
  return rules.value
    .filter((rule) => !search || [rule.name, rule.code, rule.description, ...(rule.eventTypes || [])].some((value) => String(value || '').toLocaleLowerCase().includes(search)))
    .filter((rule) => enabledFilter.value === 'enabled' ? rule.enabled : enabledFilter.value === 'disabled' ? !rule.enabled : true)
    .filter((rule) => !levelFilter.value || rule.riskLevel === levelFilter.value)
    .map((rule) => ({ ...rule, rule: rule.name || rule.code, condition: rule.threshold, risk: rule.score }))
})
const identityRulesActive = computed(() => identityRules.value.some((rule) => rule.enabled))
const selectedRule = computed(() => identityRules.value.find((rule) => rule.code === selectedIdentityRule.value) || null)
const identityRulesHeader = computed(() => identityRulesActive.value ? 'Shadow 观察期' : identityRules.value.some((rule) => ['paused', 'degraded', 'not_evaluable'].includes(rule.state)) ? '数据质量保护中' : '尚未启用')
const ruleActionOptions = riskActionOptions.filter((option) => ['observe', 'review', 'ban', 'reject_candidate', 'auto_ban'].includes(option.value))
const ruleTemplates: RuleCreateInput[] = [
  { code: 'login_failure_burst', name: '登录失败爆发', description: '同一账号连续登录失败', eventTypes: ['login_failure'], enabled: true, windowSeconds: 600, threshold: 5, score: 70, riskLevel: 'high', action: 'review' },
  { code: 'content_risk', name: '内容风险', description: '命中内容安全策略', eventTypes: ['content_risk'], enabled: true, windowSeconds: 86400, threshold: 1, score: 85, riskLevel: 'high', action: 'review' },
  { code: 'quota_abuse', name: '配额滥用', description: '持续触发配额或计费限制', eventTypes: ['quota_exceeded'], enabled: true, windowSeconds: 3600, threshold: 5, score: 55, riskLevel: 'medium', action: 'review' },
]
const draft = reactive<RuleCreateInput>({ ...ruleTemplates[0], eventTypes: [...ruleTemplates[0].eventTypes] })

function errorMessage(err: unknown, fallback = t('admin.userRiskControl.loadFailed')) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
function identityRuleName(code: string) { return ({ v2_registration_email_retries: '同邮箱重复注册尝试', v2_registration_ip_accounts: '同真实 IP 多账号注册', v2_registration_device_accounts: '同浏览器实例多账号注册', v2_registration_composite_accounts: '同 IP + 浏览器实例多账号注册', v2_api_client_accounts: 'API 客户端观察' } as Record<string, string>)[code] || code }
function identityDomainLabel(domain: IdentityRule['domain']) { return ({ account: '邮箱', ip: 'IP', device: '设备', composite: '综合关联' } as Record<IdentityRule['domain'], string>)[domain] }
function identityRuleCondition(rule: IdentityRule) { const unit = rule.domain === 'account' ? '次注册尝试' : '个成功账号'; return `${Math.round(rule.window_seconds / 60)} 分钟内 ${rule.threshold} ${unit}` }
function identityRuleStatus(rule: IdentityRule) { return rule.enabled ? '已启用 · Shadow' : rule.state === 'paused' ? '数据质量异常 · 已暂停' : rule.state === 'degraded' ? '样本不足 · 暂不计算' : rule.state === 'not_evaluable' ? '数据缺口 · 不可计算' : rule.configured_enabled ? '配置已开 · 尚未生效' : '已停用' }
function percentage(value: number) { return `${(Number(value || 0) * 100).toFixed(1)}%` }
function formatDate(value?: string) { return value ? new Date(value).toLocaleString() : '-' }
async function setRuleView(view: RuleView) { activeRuleView.value = view; auxError.value = ''; if (view === 'shadow' && !ruleEffects.value.length) await loadEffects(); if (view === 'versions' && identityRules.value.length && !selectedIdentityRule.value) await selectIdentityRule(identityRules.value[0].code) }
async function loadEffects() { auxLoading.value = true; auxError.value = ''; try { ruleEffects.value = await userRiskControlV2API.listIdentityRuleEffects() } catch (err) { auxError.value = errorMessage(err, 'Shadow 效果暂时不可用') } finally { auxLoading.value = false } }
async function selectIdentityRule(code: string) { selectedIdentityRule.value = code; auxLoading.value = true; auxError.value = ''; try { ruleVersions.value = await userRiskControlV2API.listIdentityRuleVersions(code) } catch (err) { auxError.value = errorMessage(err, '规则版本暂时不可用') } finally { auxLoading.value = false } }
async function runRebuild(dryRun: boolean) { auxLoading.value = true; auxError.value = ''; try { rebuildResult.value = dryRun ? await userRiskControlV2API.dryRunIdentityRebuild() : await userRiskControlV2API.applyIdentityRebuild(); notice.value = dryRun ? 'Dry Run 已完成' : '历史回放已完成' } catch (err) { auxError.value = errorMessage(err, dryRun ? 'Dry Run 失败' : '历史回放失败') } finally { auxLoading.value = false } }
function openDisableRule(rule: IdentityRule) { disableTarget.value = rule; disableReason.value = ''; disableValidationError.value = '' }
function closeDisableRule() { if (!auxLoading.value) disableTarget.value = null }
async function disableSelectedRule() { if (!disableTarget.value) return; if (!disableReason.value.trim()) { disableValidationError.value = '停用原因不能为空。'; return } auxLoading.value = true; try { const code = disableTarget.value.code; await userRiskControlV2API.disableIdentityRule(code, disableReason.value); disableTarget.value = null; notice.value = '身份规则已停用'; await load(); await selectIdentityRule(code) } catch (err) { disableValidationError.value = errorMessage(err, '身份规则停用失败') } finally { auxLoading.value = false } }
async function load() {
  loading.value = true
  error.value = ''
  identityRulesError.value = ''
  const [genericResult, identityResult] = await Promise.allSettled([userRiskControlV2API.listRules(), userRiskControlV2API.listIdentityRules()])
  if (genericResult.status === 'fulfilled') rules.value = genericResult.value
  else error.value = errorMessage(genericResult.reason)
  if (identityResult.status === 'fulfilled') identityRules.value = identityResult.value
  else identityRulesError.value = errorMessage(identityResult.reason, 'V2 身份规则暂时不可用')
  loading.value = false
}
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
  try { const created = await userRiskControlV2API.createRule({ ...draft, code: draft.code.trim(), name: draft.name?.trim(), eventTypes: draft.eventTypes.filter(Boolean) }); rules.value = [created, ...rules.value]; createOpen.value = false; notice.value = '规则已创建' } catch (err) { createValidationError.value = errorMessage(err, '规则创建失败') } finally { creating.value = false }
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
