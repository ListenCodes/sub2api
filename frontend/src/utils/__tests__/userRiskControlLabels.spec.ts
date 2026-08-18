import { describe, expect, it } from 'vitest'
import {
  formatAccountStatus,
  formatAuditResult,
  formatRiskAction,
  formatRiskLevel,
  formatRiskReason,
  formatRiskType,
} from '@/utils/userRiskControlLabels'

describe('user risk-control labels', () => {
  it('translates the one-time V1 cleanup audit action', () => {
    expect(formatRiskAction('purge_legacy_v1')).toBe('清理 V1 历史数据')
  })

  it('formats protocol enums as Chinese administrator labels', () => {
    expect(formatRiskType('login_failure')).toBe('登录失败')
    expect(formatRiskLevel('critical')).toBe('严重风险')
    expect(formatRiskAction('reject_candidate')).toBe('拒绝注册')
    expect(formatRiskAction('review_case')).toBe('人工复核案件')
    expect(formatAccountStatus('disabled')).toBe('已封禁')
    expect(formatAuditResult('partial')).toBe('部分成功')
  })

  it('preserves unknown values instead of rendering blank text', () => {
    expect(formatRiskType('new_signal')).toBe('未知类型（new_signal）')
    expect(formatRiskLevel('extreme')).toBe('未知等级（extreme）')
    expect(formatRiskAction('freeze')).toBe('未知动作（freeze）')
    expect(formatAuditResult('queued')).toBe('未知结果（queued）')
  })

  it('builds an understandable fallback reason from rule evidence', () => {
    expect(formatRiskReason('', {
      eventType: 'login_failure',
      ruleName: '登录失败爆发',
      count: 5,
      windowSeconds: 300,
    })).toBe('命中规则：登录失败爆发（5 分钟内失败 5 次）')
  })

  it('translates legacy rule-hit reasons into direct administrator language', () => {
    expect(formatRiskReason('规则 api_request_observation 命中')).toBe('V1 历史正常 API 流量记录：该规则已停用，不再计入账号风险摘要。')
    expect(formatRiskReason('命中规则：API 请求观察（24 小时内1 次事件）')).toBe('V1 历史正常 API 流量记录：该规则已停用，不再计入账号风险摘要。')
    expect(formatRiskReason('gateway request completed', { eventType: 'api_request', identityVersion: 'legacy_v1' })).toBe('V1 历史正常 API 流量记录：该规则已停用，不再计入账号风险摘要。')
    expect(formatRiskReason('规则 registration_identity_abuse 命中')).toBe('同邮箱或设备重复注册：同一邮箱或设备在短时间内重复提交注册。')
    expect(formatRiskReason('规则 registration_ip_multi_account 命中')).toBe('同 IP 多账号注册：同一真实客户端 IP 在短时间内注册多个不同账号。')
  })

  it('translates structured legacy reasons into readable rule evidence', () => {
    expect(formatRiskReason('rule=login_failure_burst count=9 window=300')).toBe('命中规则：登录失败爆发（5 分钟内失败 9 次）')
  })
})
