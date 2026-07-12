import { riskControlClient } from '../riskControlClient'

export type RiskAction = 'allow' | 'observe' | 'review' | 'reject_candidate'

export interface RiskOverview {
  open_cases: number
  events_24h: number
  high_risk_subjects: number
  review_rate: number
  mode: string
  last_event_at?: string
}

export interface RiskEvent {
  id: number
  event_type: string
  subject_type: string
  subject_id: string
  evidence_family: string
  scenario_code: string
  action: RiskAction
  score: number
  reason: string
  created_at: string
}

export interface RiskSubject {
  subject_type: string
  subject_id: string
  event_count: number
  max_score: number
  last_seen: string
  last_action: RiskAction
}

export interface RiskCase {
  id: number
  subject_type: string
  subject_id: string
  status: 'open' | 'resolved'
  priority: 'low' | 'medium' | 'high'
  title: string
  summary: string
  created_at: string
  updated_at: string
}

export interface RiskScenario {
  id: number
  code: string
  name: string
  description: string
  enabled: boolean
  mode: 'shadow' | 'review' | 'enforce'
  config: Record<string, unknown>
  revision: number
  updated_at: string
}

export interface RiskListItem {
  id: number
  list_type: 'allow' | 'deny' | 'review'
  value_hash: string
  label: string
  expires_at?: string
  created_at: string
}

export interface RiskAuditItem {
  id: number
  actor_id?: number
  action: string
  target_type: string
  target_id: string
  created_at: string
}

export interface RiskSystemStatus {
  service: string
  mode: string
  decision_fail_mode: string
  scenario_revision: number
  features: Record<string, boolean>
}

const base = '/admin'

export async function getOverview(): Promise<RiskOverview> {
  const { data } = await riskControlClient.get<RiskOverview>(`${base}/overview`)
  return data
}

export async function listEvents(limit = 50): Promise<RiskEvent[]> {
  const { data } = await riskControlClient.get<{ items: RiskEvent[] }>(`${base}/events`, { params: { limit } })
  return data.items
}

export async function listSubjects(limit = 100): Promise<RiskSubject[]> {
  const { data } = await riskControlClient.get<{ items: RiskSubject[] }>(`${base}/subjects`, { params: { limit } })
  return data.items
}

export async function listCases(status = '', limit = 50): Promise<RiskCase[]> {
  const { data } = await riskControlClient.get<{ items: RiskCase[] }>(`${base}/cases`, { params: { status, limit } })
  return data.items
}

export async function resolveCase(id: number, resolution: string): Promise<void> {
  await riskControlClient.post(`${base}/cases/${id}/resolve`, { resolution })
}

export async function listScenarios(): Promise<RiskScenario[]> {
  const { data } = await riskControlClient.get<{ items: RiskScenario[] }>(`${base}/scenarios`)
  return data.items
}

export async function listRiskLists(type = ''): Promise<RiskListItem[]> {
  const { data } = await riskControlClient.get<{ items: RiskListItem[] }>(`${base}/lists`, { params: { type } })
  return data.items
}

export async function listAudit(limit = 50): Promise<RiskAuditItem[]> {
  const { data } = await riskControlClient.get<{ items: RiskAuditItem[] }>(`${base}/audit`, { params: { limit } })
  return data.items
}

export async function getSystemStatus(): Promise<RiskSystemStatus> {
  const { data } = await riskControlClient.get<RiskSystemStatus>(`${base}/system`)
  return data
}

export const userRiskControlAPI = {
  getOverview,
  listEvents,
  listSubjects,
  listCases,
  resolveCase,
  listScenarios,
  listRiskLists,
  listAudit,
  getSystemStatus,
}

export default userRiskControlAPI
