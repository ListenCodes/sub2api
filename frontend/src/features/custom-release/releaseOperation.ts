import type { ReleaseOperationKind, UpdateJob, UpdateJobStatus } from './api'

export function isTerminalStatus(status: UpdateJobStatus): boolean {
  return ['success', 'failed', 'conflict', 'expired', 'drifted', 'failed_rolled_back', 'rollback_failed'].includes(status)
}
export function isSettledStatus(status: UpdateJobStatus): boolean {
  return isTerminalStatus(status) || status === 'prepared'
}
export function confirmLabel(kind: ReleaseOperationKind): string {
  return kind === 'rollback' ? '确认回退' : '确认更新'
}
export function remainingSeconds(expiresAt: string | undefined, now = Date.now()): number {
  if (!expiresAt) return 0
  return Math.max(0, Math.ceil((Date.parse(expiresAt) - now) / 1000))
}
export function operationKind(job: Pick<UpdateJob, 'operation_kind'>): ReleaseOperationKind {
  return job.operation_kind === 'rollback' ? 'rollback' : 'update'
}
