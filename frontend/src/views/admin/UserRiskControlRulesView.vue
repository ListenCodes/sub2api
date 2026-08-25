<template>
  <TablePageLayout :title="t('admin.userRiskControl.rulesTitle')" :description="t('admin.userRiskControl.rulesDescription')">
      <template #actions>
        <div class="space-y-4">
          <UserRiskControlTabs />
		  <div class="inline-flex max-w-full overflow-x-auto border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-800" role="tablist" aria-label="规则视图" data-testid="rule-views">
			<button v-for="option in ruleViews" :key="option.value" type="button" role="tab" class="min-h-9 whitespace-nowrap px-3 text-sm font-medium" :class="activeRuleView === option.value ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-selected="activeRuleView === option.value" :data-testid="`rule-view-${option.value}`" @click="setRuleView(option.value)">{{ option.label }}</button>
		  </div>
		  <div v-if="activeRuleView === 'identity'" class="flex max-w-full items-center gap-2 overflow-x-auto" aria-label="身份规则上下文工具">
			<span class="shrink-0 text-xs font-medium text-gray-500">上下文工具</span><button v-for="option in identityTools" :key="option.value" type="button" class="btn btn-sm whitespace-nowrap" :class="identityTool === option.value ? 'btn-primary' : 'btn-secondary'" :data-testid="identityToolTestID(option.value)" @click="setIdentityTool(option.value)">{{ option.label }}</button>
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
		  <section v-if="activeRuleView === 'identity' && identityTool === 'rules'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-v2-rules">
            <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">身份规则</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">邮箱、真实 IP、浏览器实例和综合关联独立计算</p>
              </div>
              <span :class="identityRulesOperating ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'" class="rounded-md px-2 py-1 text-xs font-medium">{{ identityRulesHeader }}</span>
            </div>
            <div v-if="identityHealth" class="mb-4 border-y border-gray-200 py-3 text-sm text-gray-600 dark:border-dark-700 dark:text-gray-300" data-testid="identity-shadow-status">
              <p class="font-medium text-gray-900 dark:text-white">{{ identityShadowMessage }}</p>
			  <p class="mt-1 text-xs">开始：{{ identityRulesOperating ? identityRulesStartedAt : '-' }} · {{ identityShadowWindow }} · 生效检测 {{ effectiveIdentityRuleCount }} 条 · {{ identityQualityLabel }}</p>
            </div>
            <p v-if="identityRulesError" class="mb-3 text-sm text-red-600 dark:text-red-300" data-testid="identity-rules-error">{{ identityRulesError }}</p>
            <div class="overflow-x-auto">
              <table class="w-full min-w-[760px] text-left text-sm">
                <thead class="text-xs text-gray-500 dark:text-gray-400">
				  <tr><th class="pb-2 pr-4 font-medium">规则</th><th class="pb-2 pr-4 font-medium">检测</th><th class="pb-2 pr-4 font-medium">判定</th><th class="pb-2 pr-4 font-medium">配置 / 实际动作</th><th class="pb-2 pr-4 font-medium">数据质量</th><th class="sticky right-0 bg-white pb-2 pl-2 font-medium dark:bg-dark-800">操作</th></tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="rule in identityRules" :key="rule.code" :data-testid="`identity-rule-${rule.code}`">
					<td class="py-2.5 pr-4"><div class="flex items-start justify-between gap-2"><div><p class="font-medium text-gray-900 dark:text-white">{{ identityRuleName(rule.code) }}</p><p class="mt-1 text-xs text-gray-400">{{ identityDomainLabel(rule.domain) }} · {{ identityRuleCondition(rule) }} · 风险分 {{ rule.score }}</p><p class="mt-1 text-xs text-gray-400">来源 {{ configSourceLabel(rule.config_source) }} · 第 {{ rule.revision }} 版 · {{ formatDate(rule.updated_at) }}</p></div><button type="button" class="btn btn-ghost btn-icon md:hidden" title="编辑规则" aria-label="编辑规则" :data-testid="`edit-identity-rule-mobile-${rule.code}`" @click="openIdentityEditor(rule)"><Icon name="edit" size="sm" /></button></div></td>
					<td class="py-2.5 pr-4">{{ detectionStateLabel(rule.detection_state || rule.state) }}<p class="mt-1 text-xs text-gray-400">{{ identityRuleStatus(rule) }}</p></td>
					<td class="py-2.5 pr-4">{{ identityDecisionMode(rule) === 'enforce' ? '自动处置' : '观察（不执行）' }}</td>
					<td class="py-2.5 pr-4">{{ identityActionLabel(identityConfiguredAction(rule)) }} / {{ identityActionLabel(identityEffectiveAction(rule)) }}<p v-if="rule.reason_codes?.length" class="mt-1 text-xs text-amber-600">{{ ruleReasonLabel(rule.reason_codes[0]) }}</p></td>
					<td class="py-2.5 pr-4">{{ detectionStateLabel(rule.data_quality || rule.state) }}</td>
					<td class="sticky right-0 border-l border-gray-100 bg-white py-2.5 pl-2 dark:border-dark-700 dark:bg-dark-800" :data-testid="`identity-rule-actions-${rule.code}`"><button type="button" class="btn btn-ghost btn-icon hidden md:inline-flex" title="编辑规则" aria-label="编辑规则" :data-testid="`edit-identity-rule-${rule.code}`" @click="openIdentityEditor(rule)"><Icon name="edit" size="sm" /></button></td>
                  </tr>
				  <tr v-if="!identityRules.length && !loading && !identityRulesError"><td colspan="6" class="py-4 text-center text-gray-500 dark:text-gray-400">暂无身份规则</td></tr>
                </tbody>
              </table>
            </div>
          </section>
		  <section v-else-if="activeRuleView === 'identity' && identityTool === 'effects'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rule-effects">
			<div class="mb-3 flex items-center justify-between gap-3"><h2 class="text-sm font-semibold text-gray-900 dark:text-white">运行效果</h2><button type="button" class="btn btn-secondary btn-sm" :disabled="auxLoading" @click="loadEffects"><Icon name="refresh" size="sm" />刷新</button></div>
			<p class="mb-3 text-xs text-gray-500">样本期：{{ identityRulesStartedAt }} 至当前；观察期截止：{{ identityHealth?.shadow_until ? formatDate(identityHealth.shadow_until) : '未配置' }}。有效命中与管理员反馈共同形成确认率和正常共享率。</p>
			<p v-if="auxError" class="mb-3 text-sm text-red-600 dark:text-red-300">{{ auxError }}</p>
			<div v-if="!auxLoading && !hasEffectiveSamples" class="border-y border-gray-200 py-8 text-center dark:border-dark-700"><p class="font-medium text-gray-900 dark:text-white">尚无有效样本</p><p class="mx-auto mt-2 max-w-xl text-sm text-gray-500">样本期从规则生效后开始；产生有效命中并由管理员反馈后形成确认率和正常共享率。</p></div>
			<div v-else class="overflow-x-auto"><table class="w-full min-w-[760px] text-left text-sm"><thead class="text-xs text-gray-500 dark:text-gray-400"><tr><th class="pb-2 pr-4">规则</th><th class="pb-2 pr-4">命中事件</th><th class="pb-2 pr-4">唯一主体</th><th class="pb-2 pr-4">人工确认率</th><th class="pb-2 pr-4">正常共享率</th><th class="pb-2">缺失信号率</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="effect in ruleEffects" :key="`${effect.rule_code}:${effect.revision}`"><td class="py-2.5 pr-4"><span class="font-medium">{{ identityRuleName(effect.rule_code) }}</span><p v-if="effect.sample_user_ids?.length" class="mt-1 text-xs text-gray-400">样本账号 {{ effect.sample_user_ids.length }} 个</p></td><td class="py-2.5 pr-4">{{ effect.hit_events }}</td><td class="py-2.5 pr-4">{{ effect.unique_subjects }}</td><td class="py-2.5 pr-4">{{ percentage(effect.confirmed_rate) }}</td><td class="py-2.5 pr-4">{{ percentage(effect.legitimate_shared_rate) }}</td><td class="py-2.5">{{ percentage(effect.missing_signal_rate) }}</td></tr></tbody></table></div>
		  </section>
		  <section v-else-if="activeRuleView === 'identity' && identityTool === 'versions'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rule-versions">
			<div class="flex flex-wrap gap-2"><button v-for="rule in identityRules" :key="rule.code" type="button" class="btn btn-sm" :class="selectedIdentityRule === rule.code ? 'btn-primary' : 'btn-secondary'" @click="selectIdentityRule(rule.code)">{{ identityRuleName(rule.code) }}</button></div>
			<div v-if="selectedRule" class="mt-4 flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700"><div><strong class="text-sm text-gray-900 dark:text-white">{{ identityRuleName(selectedRule.code) }}</strong><p class="mt-1 text-xs text-gray-500">当前第 {{ selectedRule.revision }} 版 · {{ identityRuleStatus(selectedRule) }}</p></div><button type="button" class="btn btn-secondary btn-sm" data-testid="edit-selected-identity-rule" @click="openIdentityEditor(selectedRule)"><Icon name="edit" size="sm" />编辑</button></div>
			<p v-if="auxError" class="mt-3 text-sm text-red-600 dark:text-red-300">{{ auxError }}</p>
			<div class="mt-3 space-y-2"><div v-for="(version, index) in ruleVersions" :key="version.revision" class="border-b border-gray-100 py-2 text-sm dark:border-dark-700"><div class="flex flex-wrap items-center justify-between gap-3"><span>第 {{ version.revision }} 版 · {{ versionDomainLabel(version) }}</span><span class="flex flex-wrap items-center gap-2"><span :class="version.enabled ? 'text-amber-700' : 'text-gray-500'">{{ versionDecisionLabel(version) }} · {{ formatDate(version.active_from) }}<template v-if="version.active_until"> 至 {{ formatDate(version.active_until) }}</template></span><button v-if="selectedRule && version.revision !== selectedRule.revision" type="button" class="btn btn-secondary btn-sm" @click="openRollback(version)">回滚到此版</button></span></div><p class="mt-1 break-words text-xs text-gray-500">{{ versionDiffLabel(version, ruleVersions[index + 1]) }}</p></div></div>
		  </section>
		  <section v-else-if="activeRuleView === 'identity' && identityTool === 'replay'" class="border-y border-gray-200 py-4 dark:border-dark-700" data-testid="identity-rebuild">
			<div class="flex flex-wrap items-start justify-between gap-3"><div class="max-w-2xl"><h2 class="text-sm font-semibold text-gray-900 dark:text-white">历史回放</h2><p class="mt-1 text-xs leading-5 text-gray-500">预检只读取当前有效身份规则和证据，展示影响范围及预计信号变化，不写入数据。写入仅在观察期已截止、同一管理员 30 分钟内完成合法预检、规则和证据范围未变化且数据质量正常时开放，并要求二次确认。</p><p v-if="rebuildResult" class="mt-2 text-xs text-gray-500" data-testid="rebuild-summary">预检范围已锁定 · 当前 {{ rebuildResult.current_signals }} 条 · 预计 {{ rebuildResult.v2_signals }} 条 · 变化主体 {{ rebuildResult.changed_subjects }} · {{ preflightStatusLabel }}</p><details v-if="rebuildResult" class="mt-2 text-xs text-gray-400" data-testid="rebuild-technical-details"><summary class="cursor-pointer">技术详情</summary><p class="mt-1">内部事件水位 {{ rebuildResult.evidence_high_water }} · 规则水位 {{ Object.keys(rebuildResult.rule_watermark || {}).length }} 项</p></details></div><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" data-testid="rebuild-dry-run" :disabled="auxLoading" @click="runPreflight">预检</button><button type="button" class="btn btn-primary btn-sm" data-testid="rebuild-apply" :disabled="auxLoading || !preflightValid" @click="openRebuildConfirmation">执行回放</button></div></div>
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
            :data="dailyRules"
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
			<template #cell-condition="{ row }"><span>{{ row.threshold }} 次 / {{ row.windowSeconds }} 秒</span><span class="mt-1 block text-xs text-gray-400">{{ countStrategyLabel(row.countStrategy) }}</span></template>
            <template #cell-risk="{ row }"><span class="font-semibold">{{ row.score }}</span><span class="ml-2">{{ formatRiskLevel(row.riskLevel) }}</span></template>
            <template #cell-action="{ row }">{{ formatRiskAction(row.action) }}</template>
            <template #cell-actions="{ row }">
			  <div class="flex justify-end gap-1">
				<button type="button" class="btn btn-ghost btn-icon" :data-testid="`test-rule-${row.id}`" title="测试规则" aria-label="测试规则" @click.stop="openRuleTest(row)"><Icon name="play" size="sm" /></button>
                <button type="button" class="btn btn-ghost btn-icon" :data-testid="`edit-rule-${row.id}`" title="编辑规则" aria-label="编辑规则" @click.stop="toggleEditor(row.id)">
                  <Icon name="edit" size="sm" />
                </button>
              </div>
            </template>
            <template #empty>
              <EmptyState title="暂无场景规则" description="新建第一条规则后，可在这里调整阈值并测试命中结果。" action-text="新建第一条规则" @action="openCreateForm" />
            </template>
          </DataTable>
          <details v-if="retiredRules.length" class="mt-4 border-y border-gray-200 py-3 dark:border-dark-700" data-testid="retired-event-rules"><summary class="cursor-pointer text-sm font-medium text-gray-600 dark:text-gray-300">历史 / 已停用规则（{{ retiredRules.length }}）</summary><ul class="mt-3 divide-y divide-gray-100 text-sm dark:divide-dark-700"><li v-for="rule in retiredRules" :key="rule.id" class="flex flex-wrap items-center justify-between gap-3 py-2"><span>{{ retiredRuleName(rule) }}</span><span class="flex items-center gap-2 text-xs text-gray-400">{{ rule.enabled ? '兼容规则仍启用' : '已停用' }}<button type="button" class="btn btn-ghost btn-icon" :data-testid="`edit-retired-rule-${rule.id}`" title="编辑历史规则" aria-label="编辑历史规则" @click.stop="toggleEditor(rule.id)"><Icon name="edit" size="sm" /></button></span></li></ul></details>
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
			<div><label class="input-label">计数口径 <span class="text-red-500">*</span></label><Select v-model="draft.countStrategy" data-testid="rule-count-strategy-create" :options="countStrategyOptions" /></div>
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
		<TextArea v-model="draft.reason" data-testid="create-rule-reason" label="创建备注（可选）" placeholder="留空时由系统记录创建操作" />
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
		  <div><label class="input-label">计数口径</label><Select v-model="editDraft.countStrategy" data-testid="rule-count-strategy-edit" :options="countStrategyOptions" /></div>
        </div>
		<TextArea v-model="editReason" data-testid="edit-rule-reason" label="修改备注（可选）" placeholder="留空时由系统记录修改操作" />
        <p v-if="editValidationError" class="text-sm text-red-600 dark:text-red-300" data-testid="edit-rule-error">{{ editValidationError }}</p>
      </form>
      <template #footer>
        <button v-if="editDraft && conflictRuleId === editDraft.id" type="button" class="btn btn-secondary" data-testid="reload-rule" @click="reloadRule(editDraft.id)">重新加载</button>
        <button type="button" class="btn btn-secondary" data-testid="close-rule-editor" @click="closeEditor">{{ t('common.cancel') }}</button>
        <button v-if="editDraft" type="button" class="btn btn-primary" data-testid="save-rule" :disabled="saving" @click="save(editDraft)">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </template>
	</BaseDialog>
	<BaseDialog :show="Boolean(testTarget)" title="规则测试" width="wide" :close-on-click-outside="true" @close="closeRuleTest">
		<div v-if="testTarget" class="space-y-4"><div><h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ testTarget.name || testTarget.code }}</h4><p class="mt-1 text-xs text-gray-500">{{ countStrategyLabel(testTarget.countStrategy) }} · 阈值 {{ testTarget.threshold }} · 窗口 {{ testTarget.windowSeconds }} 秒</p></div><div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"><div><label class="input-label">事件类型</label><Select v-model="testSample.event_type" :options="ruleEventTypeOptions" /></div><div><label class="input-label">包含本次事件的总计数</label><input v-model.number="testSample.observed_count" type="number" min="0" class="input w-full" data-testid="test-observed-count" /></div><div><label class="input-label">用户 ID</label><input v-model.number="testSample.user_id" type="number" min="0" class="input w-full" /></div><div><label class="input-label">账号标识</label><input v-model="testSample.subject_id" class="input w-full" placeholder="可留空以验证排除原因" /></div><div><label class="input-label">IP 样本标识</label><input v-model="testSample.ip_hash" class="input w-full" /></div><div><label class="input-label">设备样本标识</label><input v-model="testSample.device_hash" class="input w-full" /></div></div><p v-if="testError" class="text-sm text-red-600 dark:text-red-300" data-testid="rule-test-error" role="alert">{{ testError }}</p><div v-if="testResult && testedId === testTarget.id" class="border border-gray-200 bg-gray-50 p-4 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200" data-testid="rule-test-result">{{ testResult.matched ? '命中' : '未命中' }} · 风险分 {{ testResult.score }} · {{ formatRiskLevel(testResult.riskLevel) }} · 配置 {{ formatRiskAction(testResult.configuredAction) }} / 实际 {{ formatRiskAction(testResult.action) }}<span v-if="testResult.conditions.length" class="mt-1 block text-xs text-gray-500">命中条件：{{ testResult.conditions.join('、') }}</span><span v-if="testResult.excludedReasons.length" class="mt-1 block text-xs text-amber-600">排除原因：{{ testResult.excludedReasons.map(testExclusionLabel).join('、') }}</span><ol class="mt-2 space-y-1 text-xs text-gray-500"><li v-for="step in testResult.evaluation" :key="step.step">{{ step.passed ? '通过' : '未通过' }} · {{ testStepLabel(step.step) }} · {{ formatTestStepDetail(step.detail) }}</li></ol></div></div>
		<template #footer><button type="button" class="btn btn-secondary" @click="closeRuleTest">关闭</button><button v-if="testTarget" type="button" class="btn btn-primary" data-testid="run-rule-test" :disabled="testing" @click="test(testTarget)">{{ testing ? t('common.loading') : t('admin.userRiskControl.testRule') }}</button></template>
	</BaseDialog>
	<BaseDialog :show="Boolean(identityEditor)" :title="identityEditorMode === 'rollback' ? '回滚身份规则' : '编辑身份规则'" width="wide" :close-on-click-outside="true" @close="closeIdentityEditor">
		<form v-if="identityEditor" class="space-y-5" data-testid="identity-rule-editor" @submit.prevent>
			<div class="border-y border-gray-200 py-3 text-sm dark:border-dark-700"><p class="font-medium text-gray-900 dark:text-white">{{ identityRuleName(identityEditor.code) }}</p><p class="mt-1 text-xs text-gray-500">当前第 {{ identityEditor.revision }} 版 · {{ identityRuleStatus(identityEditor) }}<template v-if="identityRollbackRevision"> · 目标第 {{ identityRollbackRevision }} 版</template></p></div>
			<div v-if="identityEditorMode === 'edit'" class="flex items-center justify-between border-b border-gray-200 pb-4 dark:border-dark-700"><div><p class="text-sm font-medium text-gray-900 dark:text-white">启用规则</p><p class="mt-1 text-xs text-gray-500">保存后立即按当前运行模式生效</p></div><Toggle v-model="identityEnabled" data-testid="identity-rule-enabled" /></div>
			<p v-if="identityEditorMode === 'rollback'" class="text-xs text-gray-500">将原样恢复目标版本的配置和启用状态；历史版本内容不可编辑。</p>
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2"><div><label class="input-label">时间窗口（秒）</label><input v-model.number="identityDraft.window_seconds" type="number" min="1" class="input w-full" data-testid="identity-rule-window" :disabled="identityEditorMode === 'rollback'" /></div><div><label class="input-label">触发阈值</label><input v-model.number="identityDraft.threshold" type="number" min="1" class="input w-full" data-testid="identity-rule-threshold" :disabled="identityEditorMode === 'rollback'" /></div><div><label class="input-label">风险分</label><input v-model.number="identityDraft.score" type="number" min="0" max="100" class="input w-full" data-testid="identity-rule-score" :disabled="identityEditorMode === 'rollback'" /></div><div><label class="input-label">配置动作</label><Select v-model="identityDraft.configured_action" data-testid="identity-rule-action" :options="identityActionOptions" :disabled="identityEditorMode === 'rollback'" /></div></div>
			<TextArea v-model="identityDraft.reason" label="变更备注（可选）" placeholder="留空时由系统记录操作类型" data-testid="identity-rule-reason" />
			<p v-if="identityHasChanges" class="text-xs text-gray-500" data-testid="identity-rule-change-summary">{{ identityChangeSummary }}</p>
			<p v-if="identityEditorError" class="text-sm text-red-600 dark:text-red-300">{{ identityEditorError }}</p>
		</form>
		<template #footer><button type="button" class="btn btn-secondary" @click="closeIdentityEditor">取消</button><button v-if="identityEditorConflict" type="button" class="btn btn-secondary" data-testid="reload-identity-rule" :disabled="identityEditorSaving" @click="reloadIdentityRuleEditor">重新加载</button><button type="button" class="btn btn-primary" data-testid="publish-identity-rule" :disabled="identityEditorSaving || identityEditorConflict || !identityHasChanges" @click="publishIdentityRule">{{ identityEditorSaving ? '正在保存' : identitySubmitLabel }}</button></template>
	</BaseDialog>
	<BaseDialog :show="rebuildConfirmOpen" title="确认写入历史回放" width="narrow" :close-on-click-outside="true" :z-index="80" @close="closeRebuildConfirmation">
		<div data-testid="rebuild-confirm-dialog"><p class="text-sm text-gray-600 dark:text-gray-300">本次写入将使用刚完成并锁定的预检范围。系统会再次校验审批时间、规则状态、证据范围和数据质量；不满足条件时不会写入。</p><label class="mt-4 flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="rebuildConfirmed" type="checkbox" class="mt-0.5" data-testid="rebuild-confirm-ack" />我已核对预检范围和预计影响</label><p v-if="auxError" class="mt-3 text-sm text-red-600 dark:text-red-300" data-testid="rebuild-confirm-error" role="alert">{{ auxError }}</p></div>
		<template #footer><button type="button" class="btn btn-secondary" @click="closeRebuildConfirmation">取消</button><button type="button" class="btn btn-primary" data-testid="rebuild-confirm-apply" :disabled="!rebuildConfirmed || auxLoading || !preflightValid" @click="confirmRebuild">确认写入</button></template>
	</BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
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
import { userRiskControlV2API, type IdentityConfiguredAction, type IdentityHealth, type IdentityRebuildResult, type IdentityRule, type IdentityRuleDraft, type IdentityRuleEffect, type IdentityRuleVersion, type Rule, type RuleCreateInput } from '@/api/admin/userRiskControlV2'
import { formatRiskAction, formatRiskLevel, formatRiskType, riskActionOptions, riskLevelOptions, riskTypeOptions } from '@/utils/userRiskControlLabels'

const { t } = useI18n()
const rules = ref<Rule[]>([])
const identityRules = ref<IdentityRule[]>([])
const identityHealth = ref<IdentityHealth | null>(null)
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
const testResult = ref<{ matched: boolean; score: number; riskLevel: string; action: string; configuredAction: string; conditions: string[]; excludedReasons: string[]; evaluation: Array<{ step: string; passed: boolean; detail: unknown }> } | null>(null)
const testedId = ref<number | null>(null)
const testTarget = ref<Rule | null>(null)
const testError = ref('')
type RuleView = 'identity' | 'event' | 'replay' | 'shadow' | 'versions'
const activeRuleView = ref<RuleView>('identity')
const ruleViews: Array<{ value: RuleView; label: string }> = [{ value: 'identity', label: '身份规则' }, { value: 'event', label: '事件规则' }]
type IdentityTool = 'rules' | 'effects' | 'replay' | 'versions'
const identityTool = ref<IdentityTool>('rules')
const identityTools: Array<{ value: IdentityTool; label: string }> = [{ value: 'rules', label: '规则配置' }, { value: 'effects', label: '效果样本' }, { value: 'versions', label: '版本与回滚' }, { value: 'replay', label: '历史回放' }]
const ruleEffects = ref<IdentityRuleEffect[]>([])
const ruleVersions = ref<IdentityRuleVersion[]>([])
const selectedIdentityRule = ref('')
const rebuildResult = ref<IdentityRebuildResult | null>(null)
const preflightFingerprint = ref('')
const preflightNow = ref(Date.now())
const rebuildConfirmOpen = ref(false)
const rebuildConfirmed = ref(false)
const auxLoading = ref(false)
const auxError = ref('')
const editReason = ref('')
let identityVersionRequest = 0
const testSample = reactive({ event_type: 'login_failure', observed_count: 0, user_id: 0, subject_id: '', ip_hash: '', device_hash: '' })
const identityEditor = ref<IdentityRule | null>(null)
const identityEditorMode = ref<'edit' | 'rollback'>('edit')
const identityRollbackRevision = ref<number | undefined>()
const identityDraft = reactive<IdentityRuleDraft>({ rule_code: '', base_revision: 1, window_seconds: 600, threshold: 1, score: 0, configured_action: 'observe', reason: '' })
const identityEnabled = ref(false)
const identityEditorSaving = ref(false), identityEditorError = ref('')
const identityEditorConflict = ref(false)
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
const retiredRuleCodes = new Set(['registration_abuse', 'registration_identity_abuse', 'registration_ip_multi_account', 'api_request_observation'])
const retiredReliabilityRuleCodes = new Set(['api_error_burst', 'upstream_error'])
function isRetiredRule(rule: Rule) { return retiredRuleCodes.has(rule.code) || (!rule.enabled && retiredReliabilityRuleCodes.has(rule.code)) }
const dailyRules = computed(() => filteredRules.value.filter((rule) => !isRetiredRule(rule)))
const retiredRules = computed(() => filteredRules.value.filter(isRetiredRule))
const identityRulesActive = computed(() => identityRules.value.some((rule) => rule.enabled))
const effectiveIdentityRuleCount = computed(() => {
	const enabled = identityRules.value.filter((rule) => rule.enabled).length
	const reported = identityHealth.value?.effective_rule_count
	return typeof reported === 'number' ? Math.min(enabled, Math.max(reported, 0)) : enabled
})
const identityRulesOperating = computed(() => Boolean(identityHealth.value?.enabled && identityHealth.value?.admin_enabled && identityRulesActive.value && effectiveIdentityRuleCount.value > 0))
const compositeEnforcementActive = computed(() => Boolean(identityHealth.value?.mode === 'enforce' && identityHealth.value?.features?.composite_enforcement))
const selectedRule = computed(() => identityRules.value.find((rule) => rule.code === selectedIdentityRule.value) || null)
const identityHasChanges = computed(() => {
	const rule = identityEditor.value
	if (!rule) return false
	return identityDraft.window_seconds !== rule.window_seconds || identityDraft.threshold !== rule.threshold || identityDraft.score !== rule.score || identityDraft.configured_action !== identityConfiguredAction(rule) || identityEnabled.value !== rule.configured_enabled
})
const identityChangeSummary = computed(() => {
	const rule = identityEditor.value
	if (!rule) return ''
	const changes: string[] = []
	if (identityEnabled.value !== rule.configured_enabled) changes.push(identityEnabled.value ? '启用规则' : '停用规则')
	if (identityDraft.window_seconds !== rule.window_seconds) changes.push(`窗口 ${rule.window_seconds} → ${identityDraft.window_seconds} 秒`)
	if (identityDraft.threshold !== rule.threshold) changes.push(`阈值 ${rule.threshold} → ${identityDraft.threshold}`)
	if (identityDraft.score !== rule.score) changes.push(`风险分 ${rule.score} → ${identityDraft.score}`)
	if (identityDraft.configured_action !== identityConfiguredAction(rule)) changes.push(`动作 ${identityActionLabel(identityConfiguredAction(rule))} → ${identityActionLabel(identityDraft.configured_action)}`)
	return `本次修改：${changes.join('；')}`
})
const identitySubmitLabel = computed(() => identityEditorMode.value === 'rollback' ? `回滚到第 ${identityRollbackRevision.value} 版` : !identityEditor.value?.configured_enabled && identityEnabled.value ? '保存并启用' : identityEditor.value?.configured_enabled && !identityEnabled.value ? '保存并停用' : '保存并生效')
const identityRulesHeader = computed(() => compositeEnforcementActive.value ? '综合注册候选处置已开启' : identityRulesOperating.value ? '身份检测运行中' : identityRules.value.some((rule) => ['paused', 'degraded', 'not_evaluable'].includes(rule.state)) ? '数据质量保护中' : '尚未启用')
const hasEffectiveSamples = computed(() => ruleEffects.value.some((effect) => effect.hit_events > 0 || effect.unique_subjects > 0 || effect.sample_user_ids?.length))
const identityRulesStartedAt = computed(() => {
  const values = identityRules.value.filter((rule) => rule.enabled).map((rule) => rule.active_from || rule.updated_at).filter(Boolean).sort()
  return values.length ? formatDate(values[0]) : '-'
})
const identityQualityDomains = computed(() => identityHealth.value?.quality_domains || identityHealth.value?.domains)
const rebuildQualityReady = computed(() => { const values = Object.values(identityQualityDomains.value || {}); return values.length > 0 && values.every((state) => state === 'healthy') })
const identityQualityLabel = computed(() => rebuildQualityReady.value ? '数据质量正常' : '数据质量需关注')
const identityShadowMessage = computed(() => compositeEnforcementActive.value ? '综合注册规则会在满足同现阈值时拒绝新的候选账号；其他规则仅记录或进入人工复核，不会自动封禁现有账号。' : identityRulesOperating.value ? '身份检测已启用；当前实际动作以规则行展示为准。' : '当前未启用有效检测，不会自动拒绝或封禁。')
const identityShadowWindow = computed(() => identityRulesOperating.value ? identityHealth.value?.shadow_until ? `${compositeEnforcementActive.value ? '其他规则观察截止' : '截止'}：${formatDateOnly(identityHealth.value.shadow_until)}` : '截止：未配置' : '观察窗口：未生效')
function currentPreflightFingerprint() {
	const rules = identityRules.value.map((rule) => `${rule.code}:${rule.revision}:${rule.enabled}:${rule.state}`).sort().join('|')
	const quality = identityQualityDomains.value ? Object.entries(identityQualityDomains.value).sort().map(([domain, state]) => `${domain}:${state}`).join('|') : ''
	return `${identityHealth.value?.enabled}:${identityHealth.value?.admin_enabled}:${identityHealth.value?.shadow_until || ''}:${effectiveIdentityRuleCount.value}:${rules}:${quality}`
}
function preflightEvaluation(now: number) {
	const result = rebuildResult.value
	if (!result?.dry_run || result.status !== 'completed') return { valid: false, label: '预检未完成' }
	const completed = new Date(result.completed_at || result.started_at).getTime()
	const age = now - completed
	if (!Number.isFinite(completed) || age < 0 || age > 30 * 60 * 1000) return { valid: false, label: '预检已过期，请重新执行' }
	if (!identityRulesOperating.value) return { valid: false, label: '身份规则当前未启用' }
	const deadline = new Date(identityHealth.value?.shadow_until || '').getTime()
	if (!Number.isFinite(deadline) || now < deadline) return { valid: false, label: '观察期尚未截止' }
	if (!rebuildQualityReady.value) return { valid: false, label: '数据质量不满足写入条件' }
	if (!preflightFingerprint.value || preflightFingerprint.value !== currentPreflightFingerprint()) return { valid: false, label: '规则或数据质量状态已变化，请重新预检' }
	return { valid: true, label: '预检有效，可二次确认' }
}
const preflightState = computed(() => preflightEvaluation(preflightNow.value))
const preflightValid = computed(() => preflightState.value.valid)
const preflightStatusLabel = computed(() => preflightState.value.label)
const ruleActionOptions = riskActionOptions.filter((option) => ['observe', 'review', 'ban', 'reject_candidate', 'auto_ban'].includes(option.value))
const countStrategyOptions = [
	{ value: 'user_events', label: '同一用户事件数' }, { value: 'email_subject_events', label: '同一账号标识事件数' }, { value: 'ip_distinct_success_users', label: '同 IP 成功账号数' }, { value: 'browser_instance_distinct_success_users', label: '同浏览器实例成功账号数' }, { value: 'api_client_distinct_users', label: '同 API 客户端账号数' }, { value: 'ip_browser_cooccurrence', label: '同 IP + 浏览器同期账号数' },
]
const identityActionOptions: Array<{ value: IdentityConfiguredAction; label: string }> = [{ value: 'observe', label: '仅观察' }, { value: 'review', label: '人工复核' }, { value: 'reject_candidate', label: '拒绝新候选账号' }, { value: 'auto_ban', label: '自动封禁（当前安全降级为人工复核）' }]
const ruleTemplates: RuleCreateInput[] = [
  { code: 'login_failure_burst', name: '登录失败爆发', description: '同一账号连续登录失败', eventTypes: ['login_failure'], countStrategy: 'user_events', enabled: true, windowSeconds: 600, threshold: 5, score: 70, riskLevel: 'high', action: 'review', reason: '' },
  { code: 'content_risk', name: '内容风险', description: '命中内容安全策略', eventTypes: ['content_risk'], countStrategy: 'user_events', enabled: true, windowSeconds: 86400, threshold: 1, score: 85, riskLevel: 'high', action: 'review', reason: '' },
  { code: 'quota_abuse', name: '配额滥用', description: '持续触发配额或计费限制', eventTypes: ['quota_exceeded'], countStrategy: 'user_events', enabled: true, windowSeconds: 3600, threshold: 5, score: 55, riskLevel: 'medium', action: 'review', reason: '' },
]
const draft = reactive<RuleCreateInput>({ ...ruleTemplates[0], eventTypes: [...ruleTemplates[0].eventTypes] })

function errorMessage(err: unknown, fallback = t('admin.userRiskControl.loadFailed')) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
function identityRuleName(code: string) { return ({ v2_registration_email_retries: '同邮箱重复注册尝试', v2_registration_ip_accounts: '同真实 IP 多账号注册', v2_registration_device_accounts: '同浏览器实例多账号注册', v2_registration_composite_accounts: '同 IP + 浏览器实例多账号注册', v2_api_client_accounts: 'API 客户端观察' } as Record<string, string>)[code] || '身份关联规则' }
function retiredRuleName(rule: Rule) { return ({ registration_abuse: '历史注册频率规则', registration_identity_abuse: '历史账号与设备注册规则', registration_ip_multi_account: '历史共享 IP 注册规则', api_request_observation: '历史正常 API 流量记录', api_error_burst: '已迁移的 API 可靠性规则', upstream_error: '已迁移的上游可靠性规则' } as Record<string, string>)[rule.code] || '历史事件规则' }
function identityDomainLabel(domain: IdentityRule['domain']) { return ({ account: '邮箱', ip: 'IP', device: '设备', composite: '综合关联' } as Record<IdentityRule['domain'], string>)[domain] }
function identityRuleCondition(rule: IdentityRule) { const unit = rule.domain === 'account' ? '次注册尝试' : '个成功账号'; return `${Math.round(rule.window_seconds / 60)} 分钟内 ${rule.threshold} ${unit}` }
function identityRuleStatus(rule: IdentityRule) { const effective = identityEffectiveAction(rule); return rule.enabled ? effective === 'reject_candidate' ? '已启用 · 自动拒绝候选' : `已启用 · ${identityActionLabel(effective)}` : rule.state === 'paused' ? '数据质量异常 · 已暂停' : rule.state === 'degraded' ? '样本不足 · 暂不计算' : rule.state === 'not_evaluable' ? '数据缺口 · 不可计算' : rule.configured_enabled ? '配置已开 · 尚未生效' : '已停用' }
function identityConfiguredAction(rule: IdentityRule): IdentityConfiguredAction { if (rule.configured_action) return rule.configured_action; if (rule.code === 'v2_registration_composite_accounts') return 'reject_candidate'; if (rule.score > 0) return 'review'; return 'observe' }
function identityEffectiveAction(rule: IdentityRule) { if (rule.effective_action) return rule.effective_action; const configured = identityConfiguredAction(rule); if (!rule.enabled) return 'none'; if (configured === 'reject_candidate' && !(compositeEnforcementActive.value && rule.code === 'v2_registration_composite_accounts')) return 'review'; if (configured === 'auto_ban') return 'review'; return configured }
function identityDecisionMode(rule: IdentityRule) { return rule.decision_mode || (compositeEnforcementActive.value && rule.code === 'v2_registration_composite_accounts' && identityEffectiveAction(rule) === 'reject_candidate' ? 'enforce' : rule.mode) }
function identityToolTestID(tool: IdentityTool) { return ({ effects: 'rule-view-shadow', replay: 'rule-view-replay', versions: 'rule-view-versions', rules: 'identity-rule-config' } as Record<IdentityTool, string>)[tool] }
function detectionStateLabel(value: string) { return ({ healthy: '检测正常', disabled: '已停用', paused: '质量保护暂停', degraded: '数据不足', not_evaluable: '不可评估' } as Record<string, string>)[value] || value }
function identityActionLabel(value?: string) { return ({ none: '无动作', observe: '仅观察', review: '人工复核', reject_candidate: '拒绝新候选', auto_ban: '自动封禁' } as Record<string, string>)[value || ''] || value || '-' }
function configSourceLabel(value?: string) { return ({ database: '数据库配置', environment: '环境配置', default: '内置默认', migration: '迁移继承' } as Record<string, string>)[value || ''] || value || '未声明' }
function countStrategyLabel(value?: Rule['countStrategy']) { return ({ user_events: '按用户事件计数', email_subject_events: '按邮箱主体事件计数', ip_distinct_success_users: 'IP 去重成功用户', browser_instance_distinct_success_users: '浏览器实例去重成功用户', api_client_distinct_users: 'API 客户端去重用户', ip_browser_cooccurrence: 'IP 与浏览器同现用户' } as Record<string, string>)[value || 'user_events'] }
function ruleReasonLabel(value: string) { return ({ rule_disabled: '规则已停用', auto_ban_not_supported_safe_default: '安全降级为人工复核', candidate_rejection_not_eligible: '当前不具备拒绝条件', data_quality_paused: '数据质量异常', data_quality_degraded: '数据不足', data_quality_not_evaluable: '不可评估' } as Record<string, string>)[value] || value }
function versionDecisionLabel(version: IdentityRuleVersion) { const action = String(version.rule_snapshot?.configured_action || 'observe'); return `${version.enabled ? '已启用' : '已停用'} · ${identityActionLabel(action)}` }
function versionFieldValue(key: string, value: unknown) { if (key === 'enabled') return value === true ? '已启用' : '已停用'; if (key === 'configured_action') return identityActionLabel(String(value || 'observe')); return String(value ?? '-') }
function versionDiffLabel(version: IdentityRuleVersion, previous?: IdentityRuleVersion) { if (!previous) return '初始版本'; const keys = ['window_seconds', 'threshold', 'score', 'configured_action', 'enabled']; const value = (item: IdentityRuleVersion, key: string) => key === 'enabled' ? item.enabled : item.rule_snapshot?.[key]; const changes = keys.filter((key) => String(value(version, key)) !== String(value(previous, key))).map((key) => `${({ window_seconds: '窗口', threshold: '阈值', score: '风险分', configured_action: '配置动作', enabled: '启用状态' } as Record<string, string>)[key]}：${versionFieldValue(key, value(previous, key))} → ${versionFieldValue(key, value(version, key))}`); return changes.join('；') || '配置无变化' }
function testExclusionLabel(value: string) { return ({ missing_user_id: '缺少用户 ID', missing_subject_id: '缺少账号标识', missing_ip_hash: '缺少 IP 样本标识', missing_device_hash: '缺少设备样本标识', missing_event_type: '缺少事件类型', event_type_not_configured: '事件类型不在规则范围', threshold_not_reached: '计数未达到阈值', invalid_observed_count: '样本计数无效' } as Record<string, string>)[value] || value }
function testStepLabel(value: string) { return ({ event_type: '事件类型匹配', evidence: '计数口径所需字段', threshold: '阈值判断', action: '当前模式下实际动作' } as Record<string, string>)[value] || value }
function formatTestStepDetail(value: unknown) { if (value == null || value === '') return '-'; if (typeof value === 'string') return value; try { return JSON.stringify(value) } catch { return String(value) } }
function percentage(value: number) { return `${(Number(value || 0) * 100).toFixed(1)}%` }
function formatDate(value?: string) { return value ? new Date(value).toLocaleString() : '-' }
function formatDateOnly(value?: string) { return value ? value.slice(0, 10) : '-' }
function versionDomainLabel(version: IdentityRuleVersion) { const domain = version.domain || selectedRule.value?.domain || 'account'; return ({ account: '邮箱', ip: 'IP', device: '设备', composite: '综合关联' } as Record<string, string>)[domain] || '身份关联' }
async function setRuleView(view: RuleView) { activeRuleView.value = view; auxError.value = ''; notice.value = '' }
async function setIdentityTool(tool: IdentityTool) { identityTool.value = tool; auxError.value = ''; if (tool === 'effects' && !ruleEffects.value.length) await loadEffects(); if (tool === 'versions' && identityRules.value.length && !selectedIdentityRule.value) await selectIdentityRule(identityRules.value[0].code) }
async function loadEffects() { auxLoading.value = true; auxError.value = ''; try { ruleEffects.value = await userRiskControlV2API.listIdentityRuleEffects() } catch (err) { auxError.value = errorMessage(err, '运行效果暂时不可用') } finally { auxLoading.value = false } }
async function selectIdentityRule(code: string) { const request = ++identityVersionRequest; selectedIdentityRule.value = code; ruleVersions.value = []; auxLoading.value = true; auxError.value = ''; try { const versions = await userRiskControlV2API.listIdentityRuleVersions(code); if (request === identityVersionRequest && selectedIdentityRule.value === code) ruleVersions.value = versions } catch (err) { if (request === identityVersionRequest) auxError.value = errorMessage(err, '规则版本暂时不可用') } finally { if (request === identityVersionRequest) auxLoading.value = false } }
function assignIdentityDraft(rule: IdentityRule, values?: Partial<IdentityRuleDraft>) { Object.assign(identityDraft, { rule_code: rule.code, base_revision: rule.revision, window_seconds: rule.window_seconds, threshold: rule.threshold, score: rule.score, configured_action: identityConfiguredAction(rule), reason: '', ...values }); identityEnabled.value = rule.configured_enabled; identityEditorError.value = ''; identityEditorConflict.value = false }
function openIdentityEditor(rule: IdentityRule) { identityEditor.value = rule; identityEditorMode.value = 'edit'; identityRollbackRevision.value = undefined; notice.value = ''; assignIdentityDraft(rule) }
function closeIdentityEditor() { if (!identityEditorSaving.value) identityEditor.value = null }
function openRollback(version: IdentityRuleVersion) { if (!selectedRule.value || !ruleVersions.value.some((item) => item.revision === version.revision)) return; const snapshot = version.rule_snapshot || {}; identityEditor.value = selectedRule.value; identityEditorMode.value = 'rollback'; identityRollbackRevision.value = version.revision; assignIdentityDraft(selectedRule.value, { window_seconds: Number(snapshot.window_seconds || selectedRule.value.window_seconds), threshold: Number(snapshot.threshold || selectedRule.value.threshold), score: Number(snapshot.score ?? selectedRule.value.score), configured_action: String(snapshot.configured_action || 'observe') as IdentityConfiguredAction }); identityEnabled.value = version.enabled }
function validateIdentityEditor() { if (identityDraft.window_seconds <= 0 || identityDraft.threshold <= 0 || identityDraft.score < 0 || identityDraft.score > 100) return '窗口、阈值或风险分无效。'; if (identityDraft.configured_action === 'reject_candidate' && identityEditor.value?.code !== 'v2_registration_composite_accounts') return '只有综合注册规则可配置拒绝候选。'; return '' }
async function publishIdentityRule() {
	if (!identityEditor.value || !identityHasChanges.value) return
	const validation = validateIdentityEditor()
	if (validation) { identityEditorError.value = validation; return }
	identityEditorSaving.value = true
	identityEditorError.value = ''
	const operation = identityEditorMode.value === 'rollback' ? 'rollback' : 'publish'
	try {
		await userRiskControlV2API.identityRuleLifecycle(identityEditor.value.code, operation, operation === 'rollback'
			? { reason: identityDraft.reason, targetRevision: identityRollbackRevision.value }
			: { reason: identityDraft.reason, baseRevision: identityDraft.base_revision, windowSeconds: identityDraft.window_seconds, threshold: identityDraft.threshold, score: identityDraft.score, configuredAction: identityDraft.configured_action, enabled: identityEnabled.value })
		notice.value = operation === 'rollback' ? '身份规则已回滚' : identityEnabled.value ? '身份规则已保存并生效' : '身份规则已保存并停用'
		const code = identityEditor.value.code
		identityEditor.value = null
		rebuildResult.value = null
		preflightFingerprint.value = ''
		await load()
		if (identityTool.value === 'versions') await selectIdentityRule(code)
	} catch (err) {
		const message = errorMessage(err, '身份规则保存失败')
		if (message.includes('revision conflict')) {
			identityEditorConflict.value = true
			identityEditorError.value = '规则已由其他管理员更新，请重新加载后再修改。'
		} else {
			identityEditorError.value = message.includes('configuration is unchanged') ? '规则配置没有变化。' : message
		}
	} finally { identityEditorSaving.value = false }
}
async function reloadIdentityRuleEditor() {
	if (!identityEditor.value) return
	const code = identityEditor.value.code
	identityEditorSaving.value = true
	identityEditorError.value = ''
	try {
		const latestRules = await userRiskControlV2API.listIdentityRules()
		const latest = latestRules.find((rule) => rule.code === code)
		if (!latest) throw new Error('规则当前不可用')
		identityRules.value = latestRules
		identityEditor.value = latest
		identityEditorMode.value = 'edit'
		identityRollbackRevision.value = undefined
		assignIdentityDraft(latest)
	} catch (err) {
		identityEditorError.value = errorMessage(err, '重新加载身份规则失败')
	} finally { identityEditorSaving.value = false }
}
async function runPreflight() { rebuildResult.value = null; preflightFingerprint.value = ''; rebuildConfirmOpen.value = false; rebuildConfirmed.value = false; auxLoading.value = true; auxError.value = ''; try { rebuildResult.value = await userRiskControlV2API.dryRunIdentityRebuild(); preflightFingerprint.value = currentPreflightFingerprint(); preflightNow.value = Date.now(); notice.value = rebuildResult.value.status === 'completed' ? '预检已完成' : '预检尚未完成' } catch (err) { auxError.value = errorMessage(err, '预检失败') } finally { auxLoading.value = false } }
function openRebuildConfirmation() { preflightNow.value = Date.now(); const state = preflightEvaluation(preflightNow.value); if (!state.valid) { auxError.value = state.label; return } rebuildConfirmed.value = false; rebuildConfirmOpen.value = true }
function closeRebuildConfirmation() { if (!auxLoading.value) { rebuildConfirmOpen.value = false; rebuildConfirmed.value = false } }
async function confirmRebuild() { preflightNow.value = Date.now(); const state = preflightEvaluation(preflightNow.value); const approvedDryRunID = rebuildResult.value?.id || 0; if (!rebuildConfirmed.value || !state.valid || auxLoading.value) { if (!state.valid) auxError.value = state.label; return } auxLoading.value = true; auxError.value = ''; try { const result = await userRiskControlV2API.applyIdentityRebuild(approvedDryRunID); if (result.status !== 'completed' || result.dry_run || result.approved_dry_run_id !== approvedDryRunID) throw new Error('写入结果与本次预检不一致，请停止后续操作并重新核验'); rebuildResult.value = result; preflightFingerprint.value = ''; rebuildConfirmOpen.value = false; rebuildConfirmed.value = false; notice.value = '历史回放已完成' } catch (err) { auxError.value = errorMessage(err, '历史回放失败') } finally { auxLoading.value = false } }
async function load() {
  loading.value = true
  error.value = ''
  identityRulesError.value = ''
  const [genericResult, identityResult, healthResult] = await Promise.allSettled([userRiskControlV2API.listRules(), userRiskControlV2API.listIdentityRules(), userRiskControlV2API.getIdentityHealth()])
  if (genericResult.status === 'fulfilled') rules.value = genericResult.value
  else error.value = errorMessage(genericResult.reason)
  if (identityResult.status === 'fulfilled') identityRules.value = identityResult.value
  else identityRulesError.value = errorMessage(identityResult.reason, '身份规则暂时不可用')
	if (healthResult.status === 'fulfilled') identityHealth.value = healthResult.value
	else { identityHealth.value = null; identityRulesError.value = identityRulesError.value || errorMessage(healthResult.reason, '身份健康状态暂时不可用') }
  loading.value = false
}
function openCreateForm() { applyTemplate(ruleTemplates[0]); createOpen.value = true; notice.value = '' }
function closeCreateForm() { if (!creating.value) createOpen.value = false }
function closeEditor() { if (saving.value || testing.value) return; expandedRuleId.value = null; editDraft.value = null; testResult.value = null; testedId.value = null }
function toggleEditor(id: number) {
  const rule = rules.value.find((item) => item.id === id)
  if (!rule) return
  expandedRuleId.value = id
  editDraft.value = { ...rule, countStrategy: rule.countStrategy || 'user_events', eventTypes: [...(rule.eventTypes || [])] }
  editValidationError.value = ''
  conflictRuleId.value = null
  notice.value = ''
  testResult.value = null
  testedId.value = null
	testError.value = ''
	editReason.value = ''
	testSample.event_type = rule.eventTypes?.[0] || ''
	testSample.observed_count = rule.threshold
	testSample.user_id = 1
	testSample.subject_id = ''
	testSample.ip_hash = ''
	testSample.device_hash = ''
}
function openRuleTest(rule: Rule) { testTarget.value = rule; testResult.value = null; testedId.value = null; testError.value = ''; testSample.event_type = rule.eventTypes?.[0] || ''; testSample.observed_count = rule.threshold; testSample.user_id = 1; testSample.subject_id = ''; testSample.ip_hash = ''; testSample.device_hash = '' }
function closeRuleTest() { if (!testing.value) { testTarget.value = null; testError.value = '' } }
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
	if (!draft.countStrategy) return '请选择计数口径。'
	return validateRuleFields(draft)
}
async function submitCreate() {
  const validation = validateDraft()
  if (validation) { createValidationError.value = validation; return }
  creating.value = true; error.value = ''; notice.value = ''
	try { const created = await userRiskControlV2API.createRule({ ...draft, reason: draft.reason?.trim() || '管理员直接创建规则', code: draft.code.trim(), name: draft.name?.trim(), eventTypes: draft.eventTypes.filter(Boolean) }); rules.value = [created, ...rules.value]; createOpen.value = false; notice.value = '规则已创建' } catch (err) { createValidationError.value = errorMessage(err, '规则创建失败') } finally { creating.value = false }
}
async function save(rule: Rule) {
	const validation = validateRuleFields(rule)
	if (validation) { editValidationError.value = validation; return }
	if (!rule.countStrategy) { editValidationError.value = '请选择计数口径。'; return }
	 saving.value = true; editValidationError.value = ''; notice.value = ''; conflictRuleId.value = null
	 try {
		const result = await userRiskControlV2API.updateRule(rule.id, { ...rule, reason: editReason.value.trim() || '管理员直接修改规则' })
    const index = rules.value.findIndex((item) => item.id === rule.id)
    const updated = { ...rule, revision: result.revision }
    if (index >= 0) rules.value[index] = updated
		editDraft.value = updated
		editReason.value = ''
    notice.value = '规则已保存'
  } catch (err) {
	const message = errorMessage(err, t('admin.userRiskControl.saveFailed'))
	if ((typeof err === 'object' && err !== null && 'status' in err && err.status === 409) || message.toLowerCase().includes('revision conflict')) { conflictRuleId.value = rule.id; editValidationError.value = '规则已被其他管理员修改，请重新加载。' }
	else editValidationError.value = message
  } finally { saving.value = false }
}
async function reloadRule(id: number) {
  try {
    const latest = await userRiskControlV2API.listRules()
    rules.value = latest
    conflictRuleId.value = null
    editValidationError.value = ''
    const rule = latest.find((item) => item.id === id)
    editDraft.value = rule ? { ...rule, countStrategy: rule.countStrategy || 'user_events', eventTypes: [...(rule.eventTypes || [])] } : null
  } catch (err) { editValidationError.value = errorMessage(err, '重新加载规则失败') }
}
async function test(rule: Rule) {
  testing.value = true; testError.value = ''
	try { const result = await userRiskControlV2API.testRule(rule, { sample: { ...testSample } }); testResult.value = { matched: result.matched, score: result.score, riskLevel: result.riskLevel || rule.riskLevel, action: result.action || rule.action, configuredAction: result.configuredAction || rule.action, conditions: result.conditions || [], excludedReasons: result.excludedReasons || [], evaluation: result.evaluation || [] }; testedId.value = rule.id } catch (err) { testError.value = errorMessage(err, t('admin.userRiskControl.testFailed')) } finally { testing.value = false }
}
let preflightTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(); preflightTimer = setInterval(() => { preflightNow.value = Date.now() }, 15_000) })
onBeforeUnmount(() => { if (preflightTimer) clearInterval(preflightTimer) })
</script>
