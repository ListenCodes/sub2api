import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import {
  applyUpdate,
  prepareIdentityRollout,
  applyIdentityRollout,
  prepareUpdate,
  prepareRollback,
  applyRollback,
  isTerminalUpdateStatus,
  updateNeedsRestart,
  updateWasPublished,
  markCustomReleaseRead,
  type UpdateJob,
  type UpdateJobStatus
} from '@/features/custom-release/api'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('upstream preparation jobs', () => {
  it('acknowledges the exact custom release fingerprint', async () => {
    const fingerprint = 'a'.repeat(64)
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      data: { fingerprint, persisted: true }
    })

    await expect(markCustomReleaseRead(fingerprint)).resolves.toEqual({
      fingerprint,
      persisted: true
    })
    expect(post).toHaveBeenCalledWith('/admin/system/custom-release/read', { fingerprint })
  })

	it('exposes proposed official and custom versions from detection', async () => {
		const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: {
			current_version: '0.1.164', latest_version: '0.1.165', has_update: true,
			build_type: 'release', update_kind: 'combined', official_update: true,
			custom_update: true, docs_only: false, runtime_update: true, detection_complete: true,
			target_official_version: 'v0.1.165', target_official_commit: 'a'.repeat(40),
			target_custom_version: 'v1.0.6', update_fingerprint: 'b'.repeat(64),
			notice_unread: true
		} })
		const { checkUpdates } = await import('@/features/custom-release/api')
		const result = await checkUpdates()
		expect(result.target_official_version).toBe('v0.1.165')
		expect(result.target_custom_version).toBe('v1.0.6')
		expect(result.target_official_commit).toBe('a'.repeat(40))
		expect(result.update_fingerprint).toBe('b'.repeat(64))
		expect(result.notice_unread).toBe(true)
		expect(get).toHaveBeenCalled()
	})
	it('uses separate prepare and apply endpoints with independent idempotency keys', async () => {
		const prepareJob: UpdateJob = {
			job_id: 'update-two-phase',
			action: 'prepare',
			status: 'checking_updates',
			message: 'queued',
			need_restart: false
		}
		const applyJob: UpdateJob = {
			...prepareJob,
			action: 'apply',
			status: 'apply_queued'
		}
		const post = vi.spyOn(apiClient, 'post')
			.mockResolvedValueOnce({ data: prepareJob })
			.mockResolvedValueOnce({ data: applyJob })

		await expect(prepareUpdate()).resolves.toEqual(prepareJob)
		await expect(applyUpdate('update-two-phase')).resolves.toEqual(applyJob)

		expect(post).toHaveBeenNthCalledWith(1, '/admin/system/update/prepare', undefined, {
			headers: { 'Idempotency-Key': expect.any(String) }
		})
		expect(post).toHaveBeenNthCalledWith(2, '/admin/system/update/apply', {
			job_id: 'update-two-phase'
		}, {
			headers: { 'Idempotency-Key': expect.any(String) }
		})
		expect(post.mock.calls[0][2]?.headers?.['Idempotency-Key']).not.toBe(
			post.mock.calls[1][2]?.headers?.['Idempotency-Key']
		)
	})

  it('uses the administrator identity rollout prepare and apply endpoints', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: { job_id: 'update-identity' } })
    await prepareIdentityRollout('stage0-safe-reset')
    await applyIdentityRollout('update-identity')
    expect(post).toHaveBeenNthCalledWith(1, '/admin/system/identity-rollout/prepare', { transition: 'stage0-safe-reset' }, { headers: { 'Idempotency-Key': expect.any(String) } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/system/identity-rollout/apply', { job_id: 'update-identity' }, { headers: { 'Idempotency-Key': expect.any(String) } })
    expect(post.mock.calls[0][2]?.headers?.['Idempotency-Key']).not.toBe(
      post.mock.calls[1][2]?.headers?.['Idempotency-Key']
    )
  })

  it('uses separate prepare and apply endpoints for complete snapshot rollback', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: { job_id: 'rollback-1' } })
    await prepareRollback('release-1')
    await applyRollback('rollback-1')
    expect(post).toHaveBeenNthCalledWith(1, '/admin/system/rollback/prepare', { release_id: 'release-1' }, { headers: { 'Idempotency-Key': expect.any(String) } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/system/rollback/apply', { job_id: 'rollback-1' }, { headers: { 'Idempotency-Key': expect.any(String) } })
  })

	it('treats every release phase as non-terminal until success, failure, or conflict', () => {
		const phases: UpdateJobStatus[] = [
			'checking_updates',
      'checking_release',
      'validating_tag',
      'merging_release',
      'waiting_actions',
			'waiting_images',
			'downloading_images',
			'preparing_compose',
      'promoting_release',
			'backing_up',
			'validating_backup',
			'prepared',
			'apply_queued',
      'deploying_extensions',
      'deploying_main',
      'health_checking',
      'rolling_back'
    ]

		for (const phase of phases) expect(isTerminalUpdateStatus(phase)).toBe(false)
		for (const phase of [
      'success',
      'failed',
      'conflict',
      'expired',
      'drifted',
      'failed_rolled_back',
      'rollback_failed'
    ] as const) {
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
