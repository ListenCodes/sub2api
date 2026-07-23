/**
 * System API endpoints for admin operations
 */

import { apiClient } from '@/api/client'

export interface ReleaseInfo {
  name: string
  body: string
  published_at: string
  html_url: string
}

export interface VersionInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: ReleaseInfo
  cached: boolean
  warning?: string
  build_type: string // "source" for manual builds, "release" for CI builds
  update_kind: UpdateKind
  official_update: boolean
  custom_update: boolean
  docs_only: boolean
  runtime_update: boolean
  detection_complete: boolean
  production_commit?: string
  production_stable_tag?: string
  production_stable_commit?: string
  target_custom_commit?: string
  target_custom_short_sha?: string
  release_tag?: string
  custom_scope_error?: string
}

export type UpdateKind = 'none' | 'official' | 'custom' | 'combined' | 'docs-only'
export interface ReleaseIdentity {
  release_id: string
  official_version: string
  official_commit: string
  custom_version: string
  custom_version_sequence: number
  custom_commit: string
  published_at: string
}
export type ReleaseOperationKind = 'update' | 'rollback'
export type ReleaseOperationAction = 'prepare' | 'apply'

/**
 * Get current version
 */
export async function getVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/admin/system/version')
  return data
}

/**
 * Check for updates
 * @param force - Force refresh from GitHub API
 */
export async function checkUpdates(force = false): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/custom-release/check', {
    params: force ? { force: 'true' } : undefined
  })
  return data
}

export type UpdateJobStatus =
  | 'checking_updates'
  | 'checking_release'
  | 'validating_tag'
  | 'merging_release'
  | 'waiting_actions'
  | 'waiting_images'
  | 'downloading_images'
  | 'preparing_compose'
  | 'promoting_release'
  | 'backing_up'
  | 'validating_backup'
  | 'prepared'
  | 'apply_queued'
  | 'deploying_extensions'
  | 'deploying_main'
  | 'health_checking'
  | 'rolling_back'
  | 'success'
  | 'failed'
  | 'conflict'
  | 'expired'
  | 'drifted'
  | 'failed_rolled_back'
  | 'rollback_failed'

export type UpdateAction = 'prepare' | 'apply'

export interface UpdateRollback {
  attempted: boolean
  succeeded: boolean
  message: string
}

export interface UpdateJob {
  job_id: string
  operation_kind?: ReleaseOperationKind
  action?: UpdateAction
  status: UpdateJobStatus
  message: string
  integration_branch?: string
  base_commit?: string
  target_commit?: string
  target_custom_commit?: string
  update_kind?: UpdateKind
  production_commit?: string
  stable_release_tag?: string
  stable_release_commit?: string
  release_tag?: string
  release_commit?: string
  release_published_at?: string
  workflow_url?: string
  main_digest?: string
  extensions_digest?: string
  conflict_files?: string[]
  conflict_base?: string
  conflict_upstream?: string
  conflict_release?: string
  conflict_log?: string
  resolution_hint?: string
  need_restart: boolean
  published?: boolean
  published_commit?: string
  production_changed?: boolean
  error_code?: string
  artifact_path?: string
  prepared_manifest?: string
  prepared_manifest_sha256?: string
  prepared_at?: string
  expires_at?: string
  rollback?: UpdateRollback
  updated_at?: string
  started_at?: string | null
  finished_at?: string | null
}

export function isTerminalUpdateStatus(status: UpdateJobStatus): boolean {
  return (
    status === 'success' ||
    status === 'failed' ||
    status === 'conflict' ||
    status === 'expired' ||
    status === 'drifted'
  )
}

export function isPollingSettledUpdateStatus(status: UpdateJobStatus): boolean {
  return isTerminalUpdateStatus(status) || status === 'prepared'
}

export function updateNeedsRestart(job: Pick<UpdateJob, 'need_restart'>): boolean {
  return job.need_restart === true
}

export function updateWasPublished(job: Pick<UpdateJob, 'published'>): boolean {
  return job.published === true
}

function newSystemOperationIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `update-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export interface RollbackVersionInfo {
  version: string
  published_at: string
  html_url: string
}

/**
 * Get versions available for rollback (up to 3 versions older than current)
 */
export async function getRollbackVersions(): Promise<{ versions: RollbackVersionInfo[] }> {
  const releases = await getRollbackReleases()
  return { versions: releases.map((release) => ({ version: release.release_id, published_at: release.published_at, html_url: '' })) }
}

/**
 * In-place update/rollback downloads a full release binary from GitHub, which
 * can take several minutes on slow links. The global 30s axios timeout would
 * abort the request mid-download (#4504), so these calls wait as long as the
 * backend allows (15 minutes server-side).
 */
/**
 * Perform system update
 * Downloads and applies the latest version
 */
export async function performUpdate(): Promise<UpdateJob> {
  return prepareUpdate('/admin/system/update')
}

export async function prepareUpdate(endpoint = '/admin/system/update/prepare'): Promise<UpdateJob> {
  const { data } = await apiClient.post<UpdateJob>(endpoint, undefined, {
    headers: { 'Idempotency-Key': newSystemOperationIdempotencyKey() }
  })
  return data
}

export async function applyUpdate(jobID: string): Promise<UpdateJob> {
  const { data } = await apiClient.post<UpdateJob>(
    '/admin/system/update/apply',
    { job_id: jobID },
    { headers: { 'Idempotency-Key': newSystemOperationIdempotencyKey() } }
  )
  return data
}

/**
 * Get the current status of an asynchronous system update.
 */
export async function getUpdateStatus(jobID?: string): Promise<UpdateJob> {
  const { data } = await apiClient.get<UpdateJob>('/admin/system/update/status', {
    params: jobID ? { job_id: jobID } : undefined
  })
  return data
}

export async function prepareRollback(releaseID: string): Promise<UpdateJob> {
  const { data } = await apiClient.post<UpdateJob>('/admin/system/rollback/prepare', { release_id: releaseID }, {
    headers: { 'Idempotency-Key': newSystemOperationIdempotencyKey() }
  })
  return data
}

export async function applyRollback(jobID: string): Promise<UpdateJob> {
  const { data } = await apiClient.post<UpdateJob>('/admin/system/rollback/apply', { job_id: jobID }, {
    headers: { 'Idempotency-Key': newSystemOperationIdempotencyKey() }
  })
  return data
}

export async function getCurrentRelease(): Promise<ReleaseIdentity> {
  const { data } = await apiClient.get<ReleaseIdentity>('/admin/system/release')
  return data
}

export async function getRollbackReleases(): Promise<ReleaseIdentity[]> {
  const { data } = await apiClient.get<ReleaseIdentity[]>('/admin/system/releases/rollback')
  return data
}

/**
 * Restart the service
 */
export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart')
  return data
}

export const systemAPI = {
  getVersion,
  checkUpdates,
  performUpdate,
  prepareUpdate,
  applyUpdate,
  getUpdateStatus,
  getRollbackVersions,
  prepareRollback,
  applyRollback,
  getCurrentRelease,
  getRollbackReleases,
  restartService
}

export default systemAPI
