import type { RiskAuditItem, RiskCase, RiskEvent, RiskListItem, RiskScenario, RiskSubject } from '@/api/admin'

export type RiskListKind = 'events' | 'cases' | 'scenarios' | 'subjects' | 'lists' | 'audit'
export type RiskTableRow = RiskEvent | RiskCase | RiskScenario | RiskSubject | RiskListItem | RiskAuditItem

export function getRiskRowCells(kind: RiskListKind, row: RiskTableRow, formatDate: (value: string) => string): string[] {
  switch (kind) {
    case 'cases': {
      const item = row as RiskCase
      return [item.title, item.subject_id, item.priority, item.status, formatDate(item.updated_at)]
    }
    case 'scenarios': {
      const item = row as RiskScenario
      return [item.name, item.code, item.mode, `r${item.revision}`, formatDate(item.updated_at)]
    }
    case 'subjects': {
      const item = row as RiskSubject
      return [item.subject_type, item.subject_id, String(item.event_count), String(item.max_score), formatDate(item.last_seen), item.last_action]
    }
    case 'lists': {
      const item = row as RiskListItem
      return [item.list_type, item.value_hash, item.label || '-', formatDate(item.expires_at || '')]
    }
    case 'audit': {
      const item = row as RiskAuditItem
      return [formatDate(item.created_at), item.action, item.target_type, item.target_id, item.actor_id ? String(item.actor_id) : '-']
    }
    case 'events':
    default: {
      const item = row as RiskEvent
      return [formatDate(item.created_at), item.scenario_code || item.event_type, item.subject_id, item.action, item.reason || '-']
    }
  }
}

export function getRiskRowKey(row: RiskTableRow): string | number {
  return 'id' in row ? row.id : `${row.subject_type}:${row.subject_id}`
}

export function isOpenRiskCase(kind: RiskListKind, row: RiskTableRow): boolean {
  return kind === 'cases' && (row as RiskCase).status === 'open'
}

export function getRiskCaseId(row: RiskTableRow): number {
  return (row as RiskCase).id
}
