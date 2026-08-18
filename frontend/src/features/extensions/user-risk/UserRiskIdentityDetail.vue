<template>
  <div>
    <div v-if="health" class="flex flex-wrap items-center gap-2 border-b border-gray-200 pb-3 text-xs dark:border-dark-700">
      <span class="font-medium text-gray-700 dark:text-gray-200">身份关联</span>
      <span class="rounded bg-amber-100 px-2 py-1 font-medium text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">Shadow</span>
      <span v-for="domain in domainOrder" :key="domain" :class="healthClass(health.domains[domain])">{{ domainLabel(domain) }} · {{ stateLabel(health.domains[domain]) }}</span>
    </div>

    <div v-if="health && !health.admin_enabled" class="mt-4 border-y border-gray-200 py-5 text-sm text-gray-600 dark:border-dark-700 dark:text-gray-300">
      <p class="font-medium text-gray-900 dark:text-white">{{ t('admin.userRiskControl.identityDisabled') }}</p>
      <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.userRiskControl.identityDisabledHint') }}</p>
    </div>

    <template v-else>
      <nav class="mt-4 grid grid-cols-4 border-b border-gray-200 dark:border-dark-700" role="tablist" :aria-label="t('admin.userRiskControl.drawer.identityTabs')">
        <button v-for="tab in tabs" :key="tab.id" type="button" role="tab" :aria-selected="activeTab === tab.id" :class="['min-h-11 border-b-2 px-2 py-2 text-sm font-medium', activeTab === tab.id ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200']" @click="activate(tab.id)">
          {{ tab.label }}
        </button>
      </nav>

      <div v-if="activeState.loading" class="space-y-3 py-5" role="status" :aria-label="t('common.loading')"><div v-for="index in 4" :key="index" class="h-16 animate-pulse rounded bg-gray-100 dark:bg-dark-700" /></div>
      <div v-else-if="activeState.error" class="mt-4 border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/20 dark:text-red-300">
        <p>{{ activeState.error }}</p><button type="button" class="mt-2 text-sm font-medium underline" @click="reloadActive">{{ t('admin.userRiskControl.retry') }}</button>
      </div>

    <section v-else-if="activeTab === 'summary' && summary" class="py-5">
      <div data-testid="risk-conclusion" class="border-y border-gray-200 py-4 text-sm dark:border-dark-700">
        <p class="font-semibold text-gray-900 dark:text-white">{{ primarySignal ? signalExplanation(primarySignal.rule_code) : positiveIPOnlyAnomaly ? '当前未发现可处置风险，但检测到 IP-only 数据不一致' : '当前未发现需要处理的风险' }}</p>
        <p v-if="primarySignal" class="mt-1 text-gray-600 dark:text-gray-300">为何重要：该关联可能代表多账号共同使用同一身份环境，需要结合同期性和业务行为人工复核。</p>
        <p v-if="primarySignal" class="mt-1 text-gray-600 dark:text-gray-300">风险证据强度：{{ evidenceStrength(primarySignal) }}。规则已启用并计算，但只记录和人工复核，不会自动拒绝或封禁。</p>
        <p v-if="primarySignal" class="mt-1 text-gray-500">建议动作：核对同期证据、共享网络标签和账号业务行为后再决定处置。</p>
        <p v-else-if="positiveIPOnlyAnomaly" class="mt-1 text-amber-700 dark:text-amber-300">仅共享 IP 属于弱证据，不会直接评分或封禁；该异常正分已降级为数据诊断，请复核采集与规则数据。</p>
        <p v-else class="mt-1 text-gray-500">当前有效信号为 0，无需执行账号处置；历史记录仍可在下方技术详情中复核。</p>
      </div>
      <div class="mt-4 grid grid-cols-2 gap-3">
        <div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">当前有效风险</p><p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ summary.overall_score }}</p></div>
        <div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">历史最高风险</p><p class="mt-1 text-2xl font-semibold text-gray-700 dark:text-gray-200">{{ summary.historical_max_score || 0 }}</p></div>
      </div>
		<div v-if="primarySignal" data-testid="primary-risk-signal" class="mt-4 text-sm">
			<details class="border-y border-gray-200 py-3 text-xs text-gray-500 dark:border-dark-700">
				<summary class="cursor-pointer font-medium text-gray-700 dark:text-gray-200">技术详情</summary>
				<p class="mt-2">规则版本 {{ primarySignal.rule_revision || 1 }} · 决策标识 {{ primarySignal.decision_id || '-' }}</p>
			</details>
		</div>
      <div class="mt-4 divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <div v-for="domain in summary.domains" :key="domain.domain" class="py-3 text-sm">
          <div class="grid grid-cols-[1fr_auto_auto] items-center gap-4"><span class="font-medium text-gray-900 dark:text-white">{{ domainLabel(domain.domain) }}</span><span class="text-gray-500">{{ t('admin.userRiskControl.drawer.signals', { count: domain.signal_count }) }}</span><strong>{{ domain.score }}</strong></div>
			<ul v-if="domain.signals?.length" class="mt-2 space-y-1 text-xs text-gray-500"><li v-for="signal in domain.signals" :key="`${signal.rule_code}-${signal.rule_revision || 1}-${signal.occurred_at}`"><span>{{ signalExplanation(signal.rule_code) }}</span> · {{ t('admin.userRiskControl.identityEvidenceCount', { count: signal.evidence_count }) }} · {{ formatDate(signal.occurred_at) }}</li></ul>
			<p v-if="(domain.historical_max_score || 0) > domain.score" class="mt-2 text-xs text-gray-400">历史最高 {{ domain.historical_max_score }} · 历史信号 {{ domain.historical_signal_count || 0 }}</p>
        </div>
      </div>
      <details v-if="health" data-testid="identity-data-diagnostics" class="mt-4 border-y border-gray-200 py-3 text-xs text-gray-600 dark:border-dark-700 dark:text-gray-300">
        <summary class="flex cursor-pointer items-center justify-between gap-3"><strong>数据诊断</strong><span v-if="qualityIncomplete" class="font-medium text-red-600 dark:text-red-300">{{ t('admin.userRiskControl.identityQualityIncomplete') }}</span></summary>
        <p class="mt-3 font-medium text-gray-700 dark:text-gray-200">当前全局状态</p>
        <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3"><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityIPCoverage') }}</dt><dd>{{ coverage(health.quality_24h.valid_ip, health.quality_24h.events) }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityDeviceCoverage') }}</dt><dd>{{ coverage(health.quality_24h.valid_device, health.quality_24h.events) }}</dd></div><div><dt class="text-gray-400">当前地区来源</dt><dd>{{ health.geo_source || '-' }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityQueue') }}</dt><dd>{{ health.ingest_queue?.queued || 0 }}/{{ health.ingest_queue?.capacity || 0 }} · 成功 {{ health.ingest_queue?.succeeded || 0 }} · 失败 {{ health.ingest_queue?.failed || 0 }} · 丢弃 {{ health.ingest_queue?.dropped || 0 }} · {{ latency(health.ingest_queue?.average_latency_ms) }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityLinkedUsers') }}</dt><dd>{{ health.quality_24h.linked_users || 0 }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityMaxNetworkUsers') }}</dt><dd>{{ health.quality_24h.max_network_users || 0 }}</dd></div></dl>
        <p class="mt-3 text-gray-500">以上是当前全局采集状态；下方各条证据的可用性记录的是事件发生时状态，两者可能不同。</p>
      </details>
      <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ summary.legacy_notice }}</p>
    </section>

    <section v-else-if="activeTab === 'ip'" class="py-4">
      <p class="mb-3 border-y border-gray-200 py-3 text-xs text-gray-600 dark:border-dark-700 dark:text-gray-300">仅共享 IP 属于弱证据，不直接评分，也不会自动拒绝或封禁。完整 IP 只在当前受控详情中显示，不写入地址栏、浏览器存储或审计记录。</p>
      <form class="mb-4 flex items-center gap-2" role="search" @submit.prevent="searchIP">
        <input v-model="ipSearchInput" type="search" inputmode="text" autocomplete="off" class="input min-w-0 flex-1 font-mono" :aria-label="t('common.search')" placeholder="8.8.8.8">
        <button type="submit" class="btn btn-secondary btn-icon shrink-0" :aria-label="t('common.search')" :title="t('common.search')"><Icon name="search" size="sm" /></button>
        <button v-if="ipSearchInput || appliedIPQuery" type="button" class="btn btn-ghost btn-icon shrink-0" :aria-label="t('common.close')" :title="t('common.close')" @click="clearIPSearch"><Icon name="x" size="sm" /></button>
      </form>
      <div v-if="ipItems.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <article v-for="item in ipItems" :key="item.id" class="py-3">
			<div class="flex items-start justify-between gap-3"><div><p class="font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ item.ip }}</p><p class="mt-1 text-xs text-gray-500">{{ locationLabel(item) }}</p><p v-if="item.availability !== 'available'" class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ locationAvailability(item) }}</p></div><span class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.accounts', { count: item.associated_account_count }) }}</span></div>
			<p class="mt-2 text-xs text-gray-500">注册成功证据 {{ item.registration_success_count }} · ASN {{ item.asn || '-' }} · 最近出现 {{ formatDate(item.last_seen_at) }}</p>
			<details class="mt-2 text-xs text-gray-400"><summary class="cursor-pointer">采集详情</summary><p class="mt-1">发生时来源：{{ item.data_source || item.geo_source || '-' }} · 登录成功记录 {{ item.login_success_count }} · API 成功记录 {{ item.api_success_count }}。成功记录仅用于采集诊断，不作为主风险信号。</p></details>
        </article>
      </div><p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noIP') }}</p>
      <Pagination v-if="ipTotal" :page="states.ip.page" :total="ipTotal" :page-size="states.ip.pageSize" :show-page-size-selector="false" @update:page="changePage('ip', $event)" />
    </section>

    <section v-else-if="activeTab === 'device'" class="py-4">
      <div v-if="deviceItems.length" class="space-y-5">
        <section v-for="group in deviceGroups" :key="group.kind" v-show="group.items.length"><h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ group.label }}</h3><p class="mb-2 text-xs text-gray-500">{{ group.description }}</p><div class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
          <article v-for="item in group.items" :key="item.id" class="py-3"><div class="flex items-start justify-between gap-3"><div><p class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.display_code }}</p><p class="mt-1 text-xs text-gray-500">{{ [item.browser_family, item.os_family, item.device_class, item.language_family].filter(Boolean).join(' · ') || '-' }}</p></div><span class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.accounts', { count: item.associated_account_count }) }}</span></div><p class="mt-2 text-xs text-gray-500">身份识别可信度：{{ t(`admin.userRiskControl.identityConfidence.${item.confidence}`) }} · 风险证据强度：{{ deviceRiskEvidence(item.identity_kind) }} · {{ t('admin.userRiskControl.identityNetworks', { count: item.network_count }) }} · {{ t('admin.userRiskControl.drawer.lastSeen') }} {{ formatDate(item.last_seen_at) }}</p></article>
        </div></section>
      </div><p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noDevices') }}</p>
      <Pagination v-if="deviceTotal" :page="states.device.page" :total="deviceTotal" :page-size="states.device.pageSize" :show-page-size-selector="false" @update:page="changePage('device', $event)" />
    </section>

    <section v-else-if="activeTab === 'associated'" class="py-4">
        <div v-if="associatedItems.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
			<article v-for="item in associatedItems" :key="item.user_id" class="py-3"><div class="flex items-start justify-between gap-3"><div><button type="button" class="text-left text-sm font-semibold text-primary-700 hover:underline dark:text-primary-300" :data-testid="`associated-user-${item.user_id}`" @click="emit('investigate', item)">{{ item.account?.email || `账号 #${item.user_id}` }}</button><p class="mt-1 text-xs text-gray-500"><span v-if="item.account?.username">{{ item.account.username }} · </span>{{ accountAvailabilityLabel(item) }}</p></div><span class="text-xs font-medium text-gray-600 dark:text-gray-300">{{ relationLabel(item.relation) }} · 风险证据强度：{{ evidenceStrengthLabel(item.evidence_strength) }}</span></div><p class="mt-2 text-xs text-gray-500">关联依据：共享公网 IP {{ item.shared_network_count }} 次 · 共享浏览器实例 {{ item.shared_browser_instance_count || 0 }} 次 · API 客户端 {{ item.shared_api_client_count || 0 }} 次 · 同期综合证据 {{ item.cooccurring_evidence_count }} 次</p><p class="mt-1 text-xs text-gray-500">实际证据范围：{{ formatDate(item.overlap_start || '') }} 至 {{ formatDate(item.overlap_end || '') }} · 判定窗口 {{ formatEvidenceWindow(item.evidence_window_seconds) }}</p><p v-if="item.concurrent" class="mt-1 text-xs text-gray-500">起止范围存在真实重叠，可作为同期证据结合其他行为人工复核。</p><p v-else class="mt-1 text-xs text-amber-700 dark:text-amber-300">仅为历史关系，不能解释为同期中高证据，也不会直接触发账号处置。</p><p class="mt-1 text-xs text-gray-500">局限说明：{{ readableLimitations(item.limitations).join('；') || '需结合业务行为人工复核' }}</p><button type="button" class="mt-2 text-xs font-medium text-gray-500 underline" :data-testid="`associated-technical-${item.user_id}`" @click="toggleTechnical(item.user_id)">技术详情</button><div v-if="technicalExpanded.has(item.user_id)" class="mt-2 border-l-2 border-gray-200 pl-3 text-xs text-gray-400 dark:border-dark-700"><p>来源事件：{{ item.source_event_ids?.join(', ') || '-' }}</p><p v-if="item.limitations?.length" class="mt-1">内部限制已转换为上方中文说明。</p></div></article>
        </div><p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noAssociated') }}</p>
        <Pagination v-if="associatedTotal" :page="states.associated.page" :total="associatedTotal" :page-size="states.associated.pageSize" :show-page-size-selector="false" @update:page="changePage('associated', $event)" />
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { userRiskControlV2API, type AssociatedRiskUser, type DeviceIdentity, type IdentityDomain, type IdentityDomainState, type IdentityHealth, type IdentitySummary, type IPIdentity } from '@/api/admin/userRiskControlV2'

type TabID = 'summary' | 'ip' | 'device' | 'associated'
const props = defineProps<{ userId: number }>()
const emit = defineEmits<{ (event: 'tab-change', tab: TabID): void; (event: 'investigate', user: AssociatedRiskUser): void }>()
const { t } = useI18n()
const activeTab = ref<TabID>('summary')
const summary = ref<IdentitySummary | null>(null), health = ref<IdentityHealth | null>(null), ipItems = ref<IPIdentity[]>([]), deviceItems = ref<DeviceIdentity[]>([]), associatedItems = ref<AssociatedRiskUser[]>([])
const ipTotal = ref(0), deviceTotal = ref(0), associatedTotal = ref(0)
const ipSearchInput = ref(''), appliedIPQuery = ref('')
const technicalExpanded = ref(new Set<number>())
const states = reactive<Record<TabID, { loading: boolean; loaded: boolean; error: string; page: number; pageSize: number; request: number }>>({ summary: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, ip: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, device: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, associated: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 } })
const tabs = computed(() => [{ id: 'summary' as const, label: t('admin.userRiskControl.drawer.summaryTab') }, { id: 'ip' as const, label: t('admin.userRiskControl.drawer.ipTab') }, { id: 'device' as const, label: t('admin.userRiskControl.drawer.deviceTab') }, { id: 'associated' as const, label: t('admin.userRiskControl.drawer.associatedTab') }])
const domainOrder: IdentityDomain[] = ['ip', 'device', 'composite']
const activeState = computed(() => states[activeTab.value])
const primarySignal = computed(() => {
  if (!summary.value || summary.value.overall_score <= 0) return null
  return summary.value.domains.flatMap((domain) => domain.signals || []).filter((signal) => signal.score > 0 && (!signal.status || signal.status === 'active') && isActionableIdentitySignal(signal.rule_code)).sort((left, right) => right.score - left.score)[0] || null
})
const positiveIPOnlyAnomaly = computed(() => Boolean(summary.value && summary.value.overall_score > 0 && !primarySignal.value && summary.value.domains.flatMap((domain) => domain.signals || []).some((signal) => signal.score > 0 && signal.rule_code === 'v2_registration_ip_accounts')))
const qualityIncomplete = computed(() => health.value ? domainOrder.some((domain) => health.value?.domains[domain] !== 'healthy') || (health.value.ingest_queue?.dropped || 0) > 0 || (health.value.ingest_queue?.failed || 0) > 0 : false)
const deviceGroups = computed(() => [
  { kind: 'browser_instance', label: '浏览器实例', description: '可稳定识别的浏览器实例；身份识别可信度与风险证据强度分别评估。', items: deviceItems.value.filter((item) => item.identity_kind === 'browser_instance') },
  { kind: 'browser_profile', label: '浏览器特征', description: '较弱的浏览器特征组合，只作为辅助线索。', items: deviceItems.value.filter((item) => item.identity_kind === 'browser_profile') },
  { kind: 'api_client', label: 'API 客户端', description: '仅作 0 分观察，不参与自动处置。', items: deviceItems.value.filter((item) => item.identity_kind === 'api_client') },
])

async function load(tab: TabID, force = false) { const state = states[tab]; if ((state.loaded && !force) || state.loading) return; const api = userRiskControlV2API as Partial<typeof userRiskControlV2API>; if (tab === 'summary' && (!api.getUserIdentitySummary || !api.getIdentityHealth)) { state.loaded = true; return } const request = ++state.request; state.loading = true; state.error = ''; try { if (tab === 'summary') { const healthData = await userRiskControlV2API.getIdentityHealth(); if (request !== state.request) return; health.value = healthData; if (!healthData.admin_enabled) { state.loaded = true; return } const summaryData = await userRiskControlV2API.getUserIdentitySummary(props.userId); if (request !== state.request) return; summary.value = summaryData } else if (health.value && !health.value.admin_enabled) { state.loaded = true; return } else if (tab === 'ip') { const data = await userRiskControlV2API.listUserIPIdentities(props.userId, state.page, state.pageSize, appliedIPQuery.value); if (request !== state.request) return; ipItems.value = data.items; ipTotal.value = data.total } else if (tab === 'device') { const data = await userRiskControlV2API.listUserDeviceIdentities(props.userId, state.page, state.pageSize); if (request !== state.request) return; deviceItems.value = data.items; deviceTotal.value = data.total } else { const data = await userRiskControlV2API.listAssociatedUsers(props.userId, state.page, state.pageSize); if (request !== state.request) return; associatedItems.value = data.items; associatedTotal.value = data.total } state.loaded = true } catch (error) { if (request === state.request) state.error = error instanceof Error ? error.message : t('admin.userRiskControl.loadFailed') } finally { if (request === state.request) state.loading = false } }
function activate(tab: TabID) { activeTab.value = tab; emit('tab-change', tab); void load(tab) }
function reloadActive() { states[activeTab.value].loaded = false; void load(activeTab.value, true) }
function changePage(tab: Exclude<TabID, 'summary'>, page: number) { states[tab].page = page; states[tab].loaded = false; void load(tab, true) }
function searchIP() { const query = ipSearchInput.value.trim(); ipSearchInput.value = query; if (query === appliedIPQuery.value && states.ip.loaded) return; appliedIPQuery.value = query; states.ip.page = 1; states.ip.loaded = false; void load('ip', true) }
function clearIPSearch() { ipSearchInput.value = ''; if (!appliedIPQuery.value) return; appliedIPQuery.value = ''; states.ip.page = 1; states.ip.loaded = false; void load('ip', true) }
function domainLabel(domain: IdentityDomain) { return t(`admin.userRiskControl.drawer.domain.${domain}`) }
function stateLabel(state: IdentityDomainState) { return t(`admin.userRiskControl.drawer.state.${state}`) }
function healthClass(state: IdentityDomainState) { return ['rounded px-2 py-1', state === 'healthy' ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300' : state === 'disabled' ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300' : state === 'not_evaluable' || state === 'paused' ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'] }
function relationLabel(value: AssociatedRiskUser['relation']) { return ({ ip: '共享 IP', browser_instance: '共享浏览器实例', api_client: '共享 API 客户端', multi_domain: '多域历史关系', composite: '同期 IP+浏览器' } as Record<string, string>)[value] || value }
function locationLabel(item: IPIdentity) { return [item.country_code, item.region, item.city].filter(Boolean).join(' · ') || t('admin.userRiskControl.drawer.unknownLocation') }
function locationAvailability(item: IPIdentity) { const reason = ({ non_public_address: '发生时为非公网地址，无法评估地区', geo_source_not_configured: '事件发生时未配置地区数据源', geo_lookup_unverified: '事件发生时地区查询未得到可验证结果' } as Record<string, string>)[item.unavailable_reason || ''] || '事件发生时地区数据不可用'; const impact = item.unavailable_impact === 'location_not_used_for_risk' ? '不参与风险判断' : '缺少地区上下文'; return `${reason}；影响：${impact}；发生时来源：${item.data_source || 'none'}。当前全局地区来源健康不代表历史记录可补全。` }
function accountAvailabilityLabel(item: AssociatedRiskUser) { if (item.account?.availability === 'deleted') return t('admin.userRiskControl.drawer.deletedAccount'); if (item.account?.availability === 'unavailable') return '账号补全暂不可用'; if (item.account?.availability === 'not_evaluable') return '账号状态不可评估'; return ({ active: '正常', disabled: '已停用', pending: '待激活' } as Record<string, string>)[item.account?.status || ''] || item.account?.status || '-' }
function evidenceStrength(signal: NonNullable<typeof primarySignal.value>) { if (signal.rule_code.includes('composite')) return '高'; if (signal.signal_family?.includes('browser') || signal.rule_code.includes('device')) return '中高'; if (signal.signal_family?.includes('ip') || signal.rule_code.includes('_ip_')) return '弱'; return '仅观察' }
function evidenceStrengthLabel(value: AssociatedRiskUser['evidence_strength']) { return ({ observation: '仅观察', weak: '弱', medium_high: '中高', high: '高' } as Record<string, string>)[value] || '未评估' }
function signalExplanation(code: string) { return ({ v2_registration_ip_accounts: '同一公网 IP 出现多个成功注册账号', v2_registration_device_accounts: '同一签名浏览器实例出现多个成功注册账号', v2_registration_composite_accounts: '同一 IP 与浏览器实例在真实重叠窗口共同出现', v2_api_client_accounts: '同一 API 客户端关联多个账号（仅观察）', v2_registration_email_retries: '同邮箱注册重试异常（零分观察）' } as Record<string, string>)[code] || '身份关联信号' }
function isActionableIdentitySignal(code: string) { return !['v2_registration_ip_accounts', 'v2_api_client_accounts', 'v2_registration_email_retries'].includes(code) }
function deviceRiskEvidence(kind: DeviceIdentity['identity_kind']) { return kind === 'browser_instance' ? '中高' : kind === 'api_client' ? '仅观察（0 分）' : '弱' }
function readableLimitations(values: string[] = []) { const labels: Record<string, string> = { same_ip_and_browser_instance_within_window: '同一公网 IP 和浏览器实例在判定窗口内共同出现', shared_context_requires_manual_review: '共享环境仍需结合业务行为人工复核', historical_relationship_not_proof_of_concurrency: '历史关联不能证明同期使用', browser_instance_can_be_shared: '浏览器实例可能被多人共享', api_client_is_observation_only: 'API 客户端仅作 0 分观察', daily_aggregate_has_no_event_ids: '日聚合记录没有逐事件标识', ip_only: '仅共享 IP 属于弱证据', ip_only_is_weak_evidence: '仅共享 IP 属于弱证据', shared_network_possible: '共享网络可能由多个无关账号共同使用', known_shared_network_label: '该网络已标记为已知共享网络' }; return [...new Set(values.map((value) => labels[value] || '存在需人工解释的技术限制'))] }
function toggleTechnical(userID: number) { const next = new Set(technicalExpanded.value); if (next.has(userID)) next.delete(userID); else next.add(userID); technicalExpanded.value = next }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function formatEvidenceWindow(seconds: number) { if (seconds >= 86400 && seconds % 86400 === 0) return `${seconds / 86400} 天`; if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`; if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`; return `${seconds || 0} 秒` }
function coverage(valid = 0, total = 0) { return total > 0 ? `${Math.round(valid * 100 / total)}% (${valid}/${total})` : '-' }
function latency(value?: number) { return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)} ms` : '-' }
watch(() => props.userId, () => { for (const state of Object.values(states)) { state.request += 1; state.loaded = false; state.loading = false; state.error = ''; state.page = 1 } activeTab.value = 'summary'; emit('tab-change', 'summary'); summary.value = null; health.value = null; ipSearchInput.value = ''; appliedIPQuery.value = ''; technicalExpanded.value = new Set(); void load('summary') }, { immediate: true })
</script>
