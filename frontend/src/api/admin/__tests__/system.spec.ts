import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import {
  performUpdate,
  rollback,
  isTerminalUpdateStatus,
  updateNeedsRestart,
  updateWasPublished,
  type UpdateJob,
  type UpdateJobStatus
} from '@/api/admin/system'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('upstream preparation jobs', () => {
  it('starts a durable update with an idempotency key instead of a long browser timeout', async () => {
    const job: UpdateJob = {
      job_id: 'update-async',
      status: 'checking_release',
      message: 'queued',
      need_restart: false
    }
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: job })

    await expect(performUpdate()).resolves.toEqual(job)
    expect(post).toHaveBeenCalledWith('/admin/system/update', undefined, {
      headers: { 'Idempotency-Key': expect.any(String) }
    })
  })

  it('keeps the official long timeout for versioned rollback downloads', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      data: { message: 'rolled back', need_restart: true }
    })

    await rollback('0.1.161')
    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      { version: '0.1.161' },
      {
        headers: { 'Idempotency-Key': expect.any(String) },
        timeout: 15 * 60 * 1000
      }
    )
  })

  it('treats every release phase as non-terminal until success, failure, or conflict', () => {
    const phases: UpdateJobStatus[] = [
      'checking_release',
      'validating_tag',
      'merging_release',
      'waiting_actions',
      'waiting_images',
      'promoting_release',
      'backing_up',
      'deploying_extensions',
      'deploying_main',
      'health_checking',
      'rolling_back'
    ]

    for (const phase of phases) expect(isTerminalUpdateStatus(phase)).toBe(false)
    for (const phase of ['success', 'failed', 'conflict'] as const) {
      expect(isTerminalUpdateStatus(phase)).toBe(true)
    }
  })

  it('does not request a restart when the job only prepares an integration branch', () => {
    const job: UpdateJob = {
      job_id: 'update-1',
      status: 'success',
      message: 'branch ready',
      need_restart: false,
      integration_branch: 'integration/upstream-20260713'
    }

    expect(updateNeedsRestart(job)).toBe(false)
    expect(updateWasPublished(job)).toBe(false)
  })

  it('recognizes a conflict-free job that completed production publishing', () => {
    const job: UpdateJob = {
      job_id: 'update-2',
      status: 'success',
      message: 'PUBLISH OK: commit=abc123',
      need_restart: false,
      integration_branch: 'integration/upstream-20260713',
      release_tag: 'v0.1.158',
      release_commit: '26abd19a2812edba02bbef93c3e2a620141cc257',
      release_published_at: '2026-07-16T12:37:06Z',
      published: true,
      published_commit: 'abc123'
    }

    expect(updateWasPublished(job)).toBe(true)
    expect(updateNeedsRestart(job)).toBe(false)
    expect(job.release_tag).toBe('v0.1.158')
    expect(job.release_commit).toBe('26abd19a2812edba02bbef93c3e2a620141cc257')
  })

  it('carries actionable conflict metadata without treating the job as published', () => {
    const job: UpdateJob = {
      job_id: 'update-conflict',
      status: 'failed',
      message: 'upstream merge conflict',
      need_restart: false,
      conflict_files: ['backend/internal/server/routes/gateway.go', 'deploy/README.md'],
      conflict_base: 'custom123',
      conflict_upstream: 'upstream456',
      conflict_release: 'v0.1.158@26abd19a2812edba02bbef93c3e2a620141cc257',
      conflict_log:
        '/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/update-conflict/metadata.json',
      resolution_hint: 'Resolve conflicts and retry.'
    }

    expect(updateWasPublished(job)).toBe(false)
    expect(job.conflict_files).toEqual([
      'backend/internal/server/routes/gateway.go',
      'deploy/README.md'
    ])
    expect(job.resolution_hint).toBe('Resolve conflicts and retry.')
    expect(job.conflict_release).toContain('v0.1.158')
  })
})
