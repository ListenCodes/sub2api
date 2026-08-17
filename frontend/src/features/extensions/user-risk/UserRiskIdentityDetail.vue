<template>
  <div>
    <div v-if="health" class="flex flex-wrap items-center gap-2 border-b border-gray-200 pb-3 text-xs dark:border-dark-700">
      <span class="font-medium text-gray-700 dark:text-gray-200">{{ t('admin.userRiskControl.drawer.identityV2') }}</span>
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
		<div class="grid grid-cols-2 gap-3">
        <div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.overallScore') }}</p><p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ summary.overall_score }}</p></div>
			<div class="border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500">历史最高风险</p><p class="mt-1 text-2xl font-semibold text-gray-700 dark:text-gray-200">{{ summary.historical_max_score || 0 }}</p></div>
      </div>
		<div v-if="primarySignal" class="mt-4 border-y border-gray-200 py-3 text-sm dark:border-dark-700">
			<p class="font-semibold text-gray-900 dark:text-white">{{ signalExplanation(primarySignal.rule_code) }}</p>
			<p class="mt-1 text-gray-600 dark:text-gray-300">证据强度：{{ evidenceStrength(primarySignal) }}。当前为 Shadow 人工复核，不会自动拒绝或封禁。</p>
			<p class="mt-1 text-gray-500">建议动作：核对同期证据、共享网络标签和账号业务行为后再决定处置。</p>
			<p class="mt-2 font-mono text-xs text-gray-400">{{ primarySignal.rule_code }}@{{ primarySignal.rule_revision || 1 }} · {{ primarySignal.decision_id || '-' }}</p>
		</div>
      <div class="mt-4 divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <div v-for="domain in summary.domains" :key="domain.domain" class="py-3 text-sm">
          <div class="grid grid-cols-[1fr_auto_auto] items-center gap-4"><span class="font-medium text-gray-900 dark:text-white">{{ domainLabel(domain.domain) }}</span><span class="text-gray-500">{{ t('admin.userRiskControl.drawer.signals', { count: domain.signal_count }) }}</span><strong>{{ domain.score }}</strong></div>
			<ul v-if="domain.signals?.length" class="mt-2 space-y-1 text-xs text-gray-500"><li v-for="signal in domain.signals" :key="`${signal.rule_code}-${signal.rule_revision || 1}-${signal.occurred_at}`"><span>{{ signalExplanation(signal.rule_code) }}</span> · {{ t('admin.userRiskControl.identityEvidenceCount', { count: signal.evidence_count }) }} · {{ formatDate(signal.occurred_at) }}<span class="block font-mono text-gray-400">{{ signal.rule_code }}@{{ signal.rule_revision || 1 }}</span></li></ul>
			<p v-if="(domain.historical_max_score || 0) > domain.score" class="mt-2 text-xs text-gray-400">历史最高 {{ domain.historical_max_score }} · 历史信号 {{ domain.historical_signal_count || 0 }}</p>
        </div>
      </div>
      <div v-if="health" class="mt-4 border-y border-gray-200 py-3 text-xs text-gray-600 dark:border-dark-700 dark:text-gray-300">
        <div class="flex items-center justify-between gap-3"><strong>{{ t('admin.userRiskControl.identityDataQuality') }}</strong><span v-if="qualityIncomplete" class="font-medium text-red-600 dark:text-red-300">{{ t('admin.userRiskControl.identityQualityIncomplete') }}</span></div>
        <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4"><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityIPCoverage') }}</dt><dd>{{ coverage(health.quality_24h.valid_ip, health.quality_24h.events) }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityDeviceCoverage') }}</dt><dd>{{ coverage(health.quality_24h.valid_device, health.quality_24h.events) }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityGeoSource') }}</dt><dd>{{ health.geo_source || '-' }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityQueue') }}</dt><dd>{{ health.ingest_queue?.queued || 0 }}/{{ health.ingest_queue?.capacity || 0 }} · {{ t('common.success') }} {{ health.ingest_queue?.succeeded || 0 }} · {{ t('common.error') }} {{ health.ingest_queue?.failed || 0 }} · {{ t('admin.userRiskControl.identityDropped', { count: health.ingest_queue?.dropped || 0 }) }} · {{ latency(health.ingest_queue?.average_latency_ms) }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityLinkedUsers') }}</dt><dd>{{ health.quality_24h.linked_users || 0 }}</dd></div><div><dt class="text-gray-400">{{ t('admin.userRiskControl.identityMaxNetworkUsers') }}</dt><dd>{{ health.quality_24h.max_network_users || 0 }}</dd></div></dl>
      </div>
      <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ summary.legacy_notice }}</p>
    </section>

    <section v-else-if="activeTab === 'ip'" class="py-4">
      <form class="mb-4 flex items-center gap-2" role="search" @submit.prevent="searchIP">
        <input v-model="ipSearchInput" type="search" inputmode="text" autocomplete="off" class="input min-w-0 flex-1 font-mono" :aria-label="t('common.search')" placeholder="8.8.8.8">
        <button type="submit" class="btn btn-secondary btn-icon shrink-0" :aria-label="t('common.search')" :title="t('common.search')"><Icon name="search" size="sm" /></button>
        <button v-if="ipSearchInput || appliedIPQuery" type="button" class="btn btn-ghost btn-icon shrink-0" :aria-label="t('common.close')" :title="t('common.close')" @click="clearIPSearch"><Icon name="x" size="sm" /></button>
      </form>
      <div v-if="ipItems.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <article v-for="item in ipItems" :key="item.id" class="py-3">
			<div class="flex items-start justify-between gap-3"><div><p class="font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ item.ip }}</p><p class="mt-1 text-xs text-gray-500">{{ locationLabel(item) }}</p><p v-if="item.availability !== 'available'" class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ locationAvailability(item) }}</p></div><span class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.accounts', { count: item.associated_account_count }) }}</span></div>
			<p class="mt-2 text-xs text-gray-500">{{ item.ip_source }} · {{ item.data_source || item.geo_source || '-' }} · ASN {{ item.asn || '-' }} · {{ t('admin.userRiskControl.identityEventsTotal', { count: item.registration_success_count + item.login_success_count + item.api_success_count }) }} · {{ formatDate(item.last_seen_at) }}</p>
        </article>
      </div><p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noIP') }}</p>
      <Pagination v-if="ipTotal" :page="states.ip.page" :total="ipTotal" :page-size="states.ip.pageSize" :show-page-size-selector="false" @update:page="changePage('ip', $event)" />
    </section>

    <section v-else-if="activeTab === 'device'" class="py-4">
      <div v-if="deviceItems.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <article v-for="item in deviceItems" :key="item.id" class="py-3"><div class="flex items-start justify-between gap-3"><div><p class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.display_code }} · {{ deviceKindLabel(item.identity_kind) }}</p><p class="mt-1 text-xs text-gray-500">{{ [item.browser_family, item.os_family, item.device_class, item.language_family].filter(Boolean).join(' · ') || '-' }}</p></div><span class="text-xs text-gray-500">{{ t('admin.userRiskControl.drawer.accounts', { count: item.associated_account_count }) }}</span></div><p class="mt-2 text-xs text-gray-500">{{ t(`admin.userRiskControl.identityConfidence.${item.confidence}`) }} · {{ t('admin.userRiskControl.identityNetworks', { count: item.network_count }) }} · {{ t('admin.userRiskControl.drawer.lastSeen') }} {{ formatDate(item.last_seen_at) }}</p></article>
      </div><p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.userRiskControl.drawer.noDevices') }}</p>
      <Pagination v-if="deviceTotal" :page="states.device.page" :total="deviceTotal" :page-size="states.device.pageSize" :show-page-size-selector="false" @update:page="changePage('device', $event)" />
    </section>

    <section v-else class="py-4">
        <div v-if="associatedItems.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
			<article v-for="item in associatedItems" :key="item.user_id" class="py-3"><div class="flex items-start justify-between gap-3"><div><a class="text-sm font-semibold text-primary-700 hover:underline dark:text-primary-300" :href="investigationURL(item.user_id)">{{ item.account?.email || `#${item.user_id}` }}</a><p class="mt-1 text-xs text-gray-500"><span v-if="item.account?.username">{{ item.account.username }} · </span>{{ accountAvailabilityLabel(item) }}</p></div><span class="text-xs font-medium text-gray-600 dark:text-gray-300">{{ relationLabel(item.relation) }} · {{ evidenceStrengthLabel(item.evidence_strength) }}</span></div><p class="mt-2 text-xs text-gray-500">IP {{ item.shared_network_count }} · 浏览器实例 {{ item.shared_browser_instance_count || 0 }} · API 客户端 {{ item.shared_api_client_count || 0 }} · 同期综合 {{ item.cooccurring_evidence_count }}</p><p v-if="item.concurrent" class="mt-1 text-xs text-gray-500">真实重叠：{{ formatDate(item.overlap_start || '') }} - {{ formatDate(item.overlap_end || '') }} · 窗口 {{ item.evidence_window_seconds }}s</p><p v-else class="mt-1 text-xs text-amber-700 dark:text-amber-300">仅为历史关系，不能解释为同期中高证据。</p><p v-if="item.source_event_ids?.length" class="mt-1 text-xs text-gray-400">来源事件：{{ item.source_event_ids.join(', ') }}</p><p v-if="item.limitations?.length" class="mt-1 text-xs text-gray-400">{{ item.limitations.join(' · ') }}</p></article>
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
const emit = defineEmits<{ (event: 'tab-change', tab: TabID): void }>()
const { t } = useI18n()
const activeTab = ref<TabID>('summary')
const summary = ref<IdentitySummary | null>(null), health = ref<IdentityHealth | null>(null), ipItems = ref<IPIdentity[]>([]), deviceItems = ref<DeviceIdentity[]>([]), associatedItems = ref<AssociatedRiskUser[]>([])
const ipTotal = ref(0), deviceTotal = ref(0), associatedTotal = ref(0)
const ipSearchInput = ref(''), appliedIPQuery = ref('')
const states = reactive<Record<TabID, { loading: boolean; loaded: boolean; error: string; page: number; pageSize: number; request: number }>>({ summary: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, ip: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, device: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 }, associated: { loading: false, loaded: false, error: '', page: 1, pageSize: 20, request: 0 } })
const tabs = computed(() => [{ id: 'summary' as const, label: t('admin.userRiskControl.drawer.summaryTab') }, { id: 'ip' as const, label: t('admin.userRiskControl.drawer.ipTab') }, { id: 'device' as const, label: t('admin.userRiskControl.drawer.deviceTab') }, { id: 'associated' as const, label: t('admin.userRiskControl.drawer.associatedTab') }])
const domainOrder: IdentityDomain[] = ['ip', 'device', 'composite']
const activeState = computed(() => states[activeTab.value])
const primarySignal = computed(() => summary.value?.domains.flatMap((domain) => domain.signals || []).filter((signal) => !signal.status || signal.status === 'active').sort((left, right) => right.score - left.score)[0] || null)
const qualityIncomplete = computed(() => health.value ? domainOrder.some((domain) => health.value?.domains[domain] !== 'healthy') || (health.value.ingest_queue?.dropped || 0) > 0 || (health.value.ingest_queue?.failed || 0) > 0 : false)

async function load(tab: TabID, force = false) { const state = states[tab]; if ((state.loaded && !force) || state.loading) return; const api = userRiskControlV2API as Partial<typeof userRiskControlV2API>; if (tab === 'summary' && (!api.getUserIdentitySummary || !api.getIdentityHealth)) { state.loaded = true; return } const request = ++state.request; state.loading = true; state.error = ''; try { if (tab === 'summary') { const healthData = await userRiskControlV2API.getIdentityHealth(); if (request !== state.request) return; health.value = healthData; if (!healthData.admin_enabled) { state.loaded = true; return } const summaryData = await userRiskControlV2API.getUserIdentitySummary(props.userId); if (request !== state.request) return; summary.value = summaryData } else if (health.value && !health.value.admin_enabled) { state.loaded = true; return } else if (tab === 'ip') { const data = await userRiskControlV2API.listUserIPIdentities(props.userId, state.page, state.pageSize, appliedIPQuery.value); if (request !== state.request) return; ipItems.value = data.items; ipTotal.value = data.total } else if (tab === 'device') { const data = await userRiskControlV2API.listUserDeviceIdentities(props.userId, state.page, state.pageSize); if (request !== state.request) return; deviceItems.value = data.items; deviceTotal.value = data.total } else { const data = await userRiskControlV2API.listAssociatedUsers(props.userId, state.page, state.pageSize); if (request !== state.request) return; associatedItems.value = data.items; associatedTotal.value = data.total } state.loaded = true } catch (error) { if (request === state.request) state.error = error instanceof Error ? error.message : t('admin.userRiskControl.loadFailed') } finally { if (request === state.request) state.loading = false } }
function activate(tab: TabID) { activeTab.value = tab; emit('tab-change', tab); void load(tab) }
function reloadActive() { states[activeTab.value].loaded = false; void load(activeTab.value, true) }
function changePage(tab: Exclude<TabID, 'summary'>, page: number) { states[tab].page = page; states[tab].loaded = false; void load(tab, true) }
function searchIP() { const query = ipSearchInput.value.trim(); ipSearchInput.value = query; if (query === appliedIPQuery.value && states.ip.loaded) return; appliedIPQuery.value = query; states.ip.page = 1; states.ip.loaded = false; void load('ip', true) }
function clearIPSearch() { ipSearchInput.value = ''; if (!appliedIPQuery.value) return; appliedIPQuery.value = ''; states.ip.page = 1; states.ip.loaded = false; void load('ip', true) }
function domainLabel(domain: IdentityDomain) { return t(`admin.userRiskControl.drawer.domain.${domain}`) }
function stateLabel(state: IdentityDomainState) { return t(`admin.userRiskControl.drawer.state.${state}`) }
function healthClass(state: IdentityDomainState) { return ['rounded px-2 py-1', state === 'healthy' ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300' : state === 'disabled' ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300' : state === 'not_evaluable' || state === 'paused' ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'] }
function deviceKindLabel(kind: DeviceIdentity['identity_kind']) { return t(`admin.userRiskControl.drawer.deviceKind.${kind}`) }
function relationLabel(value: AssociatedRiskUser['relation']) { return ({ ip: '共享 IP', browser_instance: '共享浏览器实例', api_client: '共享 API 客户端', multi_domain: '多域历史关系', composite: '同期 IP+浏览器' } as Record<string, string>)[value] || value }
function locationLabel(item: IPIdentity) { return [item.country_code, item.region, item.city].filter(Boolean).join(' · ') || t('admin.userRiskControl.drawer.unknownLocation') }
function locationAvailability(item: IPIdentity) { const reason = ({ non_public_address: '非公网地址不可评估地区', geo_source_not_configured: '未配置地区数据源', geo_lookup_unverified: '地区查询未得到可验证结果' } as Record<string, string>)[item.unavailable_reason || ''] || '地区数据不可用'; const impact = item.unavailable_impact === 'location_not_used_for_risk' ? '不参与风险判断' : '缺少地区上下文'; return `${reason}；影响：${impact}；来源：${item.data_source || 'none'}` }
function accountAvailabilityLabel(item: AssociatedRiskUser) { if (item.account?.availability === 'deleted') return t('admin.userRiskControl.drawer.deletedAccount'); if (item.account?.availability === 'unavailable') return '账号补全暂不可用'; if (item.account?.availability === 'not_evaluable') return '账号状态不可评估'; return ({ active: '正常', disabled: '已停用', pending: '待激活' } as Record<string, string>)[item.account?.status || ''] || item.account?.status || '-' }
function evidenceStrength(signal: NonNullable<typeof primarySignal.value>) { if (signal.rule_code.includes('composite')) return '高'; if (signal.signal_family?.includes('browser') || signal.rule_code.includes('device')) return '中高'; if (signal.signal_family?.includes('ip') || signal.rule_code.includes('_ip_')) return '弱'; return '仅观察' }
function evidenceStrengthLabel(value: AssociatedRiskUser['evidence_strength']) { return ({ observation: '仅观察', weak: '弱', medium_high: '中高', high: '高' } as Record<string, string>)[value] || '未评估' }
function signalExplanation(code: string) { return ({ v2_registration_ip_accounts: '同一公网 IP 出现多个成功注册账号', v2_registration_device_accounts: '同一签名浏览器实例出现多个成功注册账号', v2_registration_composite_accounts: '同一 IP 与浏览器实例在真实重叠窗口共同出现', v2_api_client_accounts: '同一 API 客户端关联多个账号（仅观察）', v2_registration_email_retries: '同邮箱注册重试异常（零分观察）' } as Record<string, string>)[code] || '身份关联信号' }
function investigationURL(userID: number) { return `/admin/extensions/user-risk/users?view=all&search=${encodeURIComponent(String(userID))}` }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function coverage(valid = 0, total = 0) { return total > 0 ? `${Math.round(valid * 100 / total)}% (${valid}/${total})` : '-' }
function latency(value?: number) { return typeof value === 'number' && Number.isFinite(value) ? `${value.toFixed(1)} ms` : '-' }
watch(() => props.userId, () => { for (const state of Object.values(states)) { state.request += 1; state.loaded = false; state.loading = false; state.error = ''; state.page = 1 } activeTab.value = 'summary'; emit('tab-change', 'summary'); summary.value = null; health.value = null; ipSearchInput.value = ''; appliedIPQuery.value = ''; void load('summary') }, { immediate: true })
</script>
