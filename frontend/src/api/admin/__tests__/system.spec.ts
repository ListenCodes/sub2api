import { describe, expect, it } from 'vitest'
import { updateNeedsRestart, updateWasPublished, type UpdateJob } from '@/api/admin/system'

describe('upstream preparation jobs', () => {
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
      published: true,
      published_commit: 'abc123'
    }

    expect(updateWasPublished(job)).toBe(true)
    expect(updateNeedsRestart(job)).toBe(false)
  })
})
