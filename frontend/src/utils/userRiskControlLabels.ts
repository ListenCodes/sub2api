export type RiskLabelOption = { value: string; label: string }

const riskTypes: Record<string, string> = {
  registration_attempt: '注册尝试',
  registration_success: '注册成功',
  login_attempt: '登录尝试',
  login_failure: '登录失败',
  api_error: 'API 错误',
  content_risk: '内容风险',
  quota_exceeded: '配额超限',
  upstream_error: '上游错误',
  api_request: 'API 请求',
}

const riskLevels: Record<string, string> = {
  none: '无风险记录',
  low: '低风险',
  medium: '中风险',
  high: '高风险',
  critical: '严重风险',
}

const riskActions: Record<string, string> = {
  observe: '仅记录',
  review: '人工复核',
  ban: '封禁账号',
  unban: '解封账号',
  reject_candidate: '拒绝注册',
  auto_ban: '自动封禁',
  create_rule: '新建规则',
  update_rule: '修改规则',
  rule_test: '规则测试',
  mark_processed: '标记已处理',
}

const accountStatuses: Record<string, string> = {
  active: '正常',
  disabled: '已封禁',
  pending: '待审核',
}

const auditResults: Record<string, string> = {
  success: '成功',
  partial: '部分成功',
  failed: '失败',
  pending: '待处理',
}

const processingStatuses: Record<string, string> = {
  pending: '待处理',
  observed: '已观察',
  reviewed: '已复核',
  banned: '已封禁',
  unbanned: '已解封',
}

export const riskTypeOptions: RiskLabelOption[] = Object.entries(riskTypes).map(([value, label]) => ({ value, label }))
export const riskLevelOptions: RiskLabelOption[] = Object.entries(riskLevels).filter(([value]) => value !== 'none').map(([value, label]) => ({ value, label }))
export const riskActionOptions: RiskLabelOption[] = Object.entries(riskActions).map(([value, label]) => ({ value, label }))
export const accountStatusOptions: RiskLabelOption[] = Object.entries(accountStatuses).map(([value, label]) => ({ value, label }))
export const auditResultOptions: RiskLabelOption[] = Object.entries(auditResults).map(([value, label]) => ({ value, label }))
export const processingStatusOptions: RiskLabelOption[] = Object.entries(processingStatuses).map(([value, label]) => ({ value, label }))

function formatMappedValue(value: unknown, mapping: Record<string, string>, category: string, emptyLabel: string): string {
  const raw = String(value ?? '').trim()
  if (!raw) return emptyLabel
  return mapping[raw] || `未知${category}（${raw}）`
}

export function formatRiskType(value: unknown): string {
  return formatMappedValue(value, riskTypes, '类型', '暂无风险记录')
}

export function formatRiskLevel(value: unknown): string {
  return formatMappedValue(value, riskLevels, '等级', '暂无风险记录')
}

export function formatRiskAction(value: unknown): string {
  return formatMappedValue(value, riskActions, '动作', '暂无处置动作')
}

export function formatAccountStatus(value: unknown): string {
  return formatMappedValue(value, accountStatuses, '状态', '未知状态')
}

export function formatAuditResult(value: unknown): string {
  return formatMappedValue(value, auditResults, '结果', '未知结果')
}

export function formatProcessingStatus(value: unknown): string {
  return formatMappedValue(value, processingStatuses, '处理状态', '未知处理状态')
}

export type RiskReasonEvidence = {
  eventType?: string
  ruleName?: string
  ruleCode?: string
  count?: number
  threshold?: number
  windowSeconds?: number
  errorCode?: string
}

function formatWindow(seconds: number): string {
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function reasonVerb(eventType: string): string {
  if (eventType === 'login_failure') return '失败'
  if (eventType === 'api_error') return '次错误'
  return '次事件'
}

export function formatRiskReason(rawReason: unknown, evidence: RiskReasonEvidence = {}): string {
  const reason = String(rawReason ?? '').trim()
  if (reason) return reason
  const rule = String(evidence.ruleName || evidence.ruleCode || '').trim()
  const count = Number(evidence.count || evidence.threshold || 0)
  const windowSeconds = Number(evidence.windowSeconds || 0)
  if (rule && count > 0 && windowSeconds > 0) {
    const verb = reasonVerb(String(evidence.eventType || ''))
    const countText = verb === '失败' ? `${verb} ${count} 次` : `${count} ${verb}`
    return `命中规则：${rule}（${formatWindow(windowSeconds)}内${countText}）`
  }
  if (rule) return `命中规则：${rule}`
  if (evidence.errorCode) return `记录错误：${evidence.errorCode}`
  return '暂无风险原因'
}
