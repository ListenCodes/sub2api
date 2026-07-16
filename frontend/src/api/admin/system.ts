/**
 * System API endpoints for admin operations
 */

import { apiClient } from '../client'

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
}

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
  const { data } = await apiClient.get<VersionInfo>('/admin/system/check-updates', {
    params: force ? { force: 'true' } : undefined
  })
  return data
}

export type UpdateJobStatus =
  | 'checking_release'
  | 'validating_tag'
  | 'merging_release'
  | 'waiting_actions'
  | 'waiting_images'
  | 'promoting_release'
  | 'backing_up'
  | 'deploying_extensions'
  | 'deploying_main'
  | 'health_checking'
  | 'rolling_back'
  | 'success'
  | 'failed'
  | 'conflict'

export interface UpdateRollback {
  attempted: boolean
  succeeded: boolean
  message: string
}

export interface UpdateJob {
  job_id: string
  status: UpdateJobStatus
  message: string
  integration_branch?: string
  base_commit?: string
  target_commit?: string
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
  rollback?: UpdateRollback
  updated_at?: string
  started_at?: string | null
  finished_at?: string | null
}

export function isTerminalUpdateStatus(status: UpdateJobStatus): boolean {
  return status === 'success' || status === 'failed' || status === 'conflict'
}

export function updateNeedsRestart(job: Pick<UpdateJob, 'need_restart'>): boolean {
  return job.need_restart === true
}

export function updateWasPublished(job: Pick<UpdateJob, 'published'>): boolean {
  return job.published === true
}

function newUpdateIdempotencyKey(): string {
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
  const { data } = await apiClient.get<{ versions: RollbackVersionInfo[] }>(
    '/admin/system/rollback-versions'
  )
  return data
}

/**
 * Perform system update
 * Downloads and applies the latest version
 */
export async function performUpdate(): Promise<UpdateJob> {
  const { data } = await apiClient.post<UpdateJob>('/admin/system/update', undefined, {
    headers: { 'Idempotency-Key': newUpdateIdempotencyKey() }
  })
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

export interface UpdateResult {
  message: string
  need_restart: boolean
}

/**
 * Rollback to a previous version
 * @param version - Target version (e.g. "0.1.146"); omit to restore the local backup binary
 */
export async function rollback(version?: string): Promise<UpdateResult> {
  const { data } = await apiClient.post<UpdateResult>(
    '/admin/system/rollback',
    version ? { version } : undefined
  )
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
  getUpdateStatus,
  getRollbackVersions,
  rollback,
  restartService
}

export default systemAPI
