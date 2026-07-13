import { describe, expect, it } from 'vitest'
import { updateNeedsRestart, type UpdateJob } from '@/api/admin/system'

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
  })
})
