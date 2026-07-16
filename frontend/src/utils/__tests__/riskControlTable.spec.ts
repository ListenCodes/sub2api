import { describe, expect, it } from 'vitest'
import { getRiskRowCells } from '@/utils/riskControlTable'

describe('risk control table rows', () => {
  it('formats subject rows for the profile view', () => {
    expect(
      getRiskRowCells(
        'subjects',
        {
          subject_type: 'user',
          subject_id: '42',
          event_count: 3,
          max_score: 50,
          last_seen: '2026-07-11T12:00:00Z',
          last_action: 'review',
        },
        () => 'formatted',
      ),
    ).toEqual(['user', '42', '3', '50', 'formatted', 'review'])
  })
})
