<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('admin.userRiskControl.sectionLabel') }}</p>
        <h1 class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.userRiskControl.rulesTitle') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.rulesDescription') }}</p>
      </header>
      <div v-if="error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">{{ error }}</div>
      <div v-if="notice" class="rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700" data-testid="save-notice">{{ notice }}</div>
      <div v-if="loading" class="card p-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="!rules.length" class="card p-12 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.empty') }}</div>
      <section v-for="rule in rules" v-else :key="rule.id" class="card p-5">
        <div class="flex flex-col gap-3 border-b border-gray-200 pb-4 md:flex-row md:items-center md:justify-between dark:border-dark-700">
          <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ rule.name || rule.code }}</h2><p class="mt-1 text-xs text-gray-500">{{ rule.code }} · r{{ rule.revision }}</p></div>
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300"><input v-model="rule.enabled" type="checkbox" class="rounded border-gray-300 text-primary-600" />{{ t('admin.userRiskControl.enabled') }}</label>
        </div>
        <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
          <label class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.window') }}<input v-model.number="rule.windowSeconds" type="number" min="1" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.threshold') }}<input v-model.number="rule.threshold" type="number" min="1" class="form-input mt-1 w-full" data-testid="rule-threshold" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.score') }}<input v-model.number="rule.score" type="number" min="0" max="100" class="form-input mt-1 w-full" /></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.level') }}<select v-model="rule.riskLevel" class="form-input mt-1 w-full"><option v-for="level in levels" :key="level" :value="level">{{ level }}</option></select></label>
          <label class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.action') }}<select v-model="rule.action" class="form-input mt-1 w-full" data-testid="rule-action"><option value="observe">observe</option><option value="review">review</option><option value="ban">ban</option><option value="reject_candidate">reject_candidate</option></select></label>
        </div>
        <div class="mt-5 flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-primary" data-testid="save-rule" :disabled="saving" @click="save(rule)">{{ saving ? t('common.saving') : t('common.save') }}</button>
          <button type="button" class="btn btn-secondary" data-testid="test-rule" :disabled="testing" @click="test(rule)">{{ testing ? t('common.loading') : t('admin.userRiskControl.testRule') }}</button>
          <span v-if="testResult && testedId === rule.id" class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.userRiskControl.testResult') }}: {{ testResult.score }} / {{ testResult.matched ? t('admin.userRiskControl.matched') : t('admin.userRiskControl.notMatched') }}</span>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { userRiskControlV2API, type Rule } from '@/api/admin/userRiskControlV2'
const { t } = useI18n(); const rules = ref<Rule[]>([]); const loading = ref(true); const saving = ref(false); const testing = ref(false); const error = ref(''); const notice = ref(''); const testResult = ref<{ matched: boolean; score: number } | null>(null); const testedId = ref<number | null>(null); const levels = ['low', 'medium', 'high', 'critical'] as const
function errorMessage(err: unknown, fallback: string) { return typeof err === 'object' && err !== null && 'message' in err && typeof err.message === 'string' && err.message.trim() ? err.message : err instanceof Error ? err.message : fallback }
async function load() { try { rules.value = await userRiskControlV2API.listRules() } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.loadFailed')) } finally { loading.value = false } }
async function save(rule: Rule) { saving.value = true; error.value = ''; notice.value = ''; try { const result = await userRiskControlV2API.updateRule(rule.id, rule); rule.revision = result.revision; notice.value = t('admin.userRiskControl.saved') } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.saveFailed')) } finally { saving.value = false } }
async function test(rule: Rule) { testing.value = true; error.value = ''; try { testResult.value = await userRiskControlV2API.testRule(rule, { event_type: rule.code, count: rule.threshold }); testedId.value = rule.id } catch (err) { error.value = errorMessage(err, t('admin.userRiskControl.testFailed')) } finally { testing.value = false } }
onMounted(load)
</script>
