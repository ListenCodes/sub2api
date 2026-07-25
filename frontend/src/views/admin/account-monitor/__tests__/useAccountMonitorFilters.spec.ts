import { describe, expect, it } from 'vitest'

import { parseAccountMonitorQuery, serializeAccountMonitorQuery } from '@/views/admin/account-monitor/useAccountMonitorFilters'

describe('account monitor URL state', () => {
  it('restores filters, risk range, sort, page, selection, and detail tab exactly', () => {
		window.__APP_CONFIG__ = { table_page_size_options: [20, 100, 1000] } as typeof window.__APP_CONFIG__
    const state = parseAccountMonitorQuery({
      range: 'custom',
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-08T00:00:00.000Z',
      platform: 'openai',
      query: 'owner@example.com',
      account_status: 'active',
      model: 'gpt-5',
      result: 'failure',
      rollup: 'parent',
      min_risk_score: '20',
      max_risk_score: '69',
      sort_by: 'risk_score',
      sort_order: 'asc',
      page: '3',
			page_size: '1000',
			group_id: 'ungrouped',
      account: '42',
      tab: 'errors',
    })

    expect(state).toMatchObject({
      range: 'custom',
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-08T00:00:00.000Z',
      platform: 'openai',
      query: 'owner@example.com',
      accountStatus: 'active',
      model: 'gpt-5',
      result: 'failure',
      rollup: 'parent',
      minRiskScore: 20,
      maxRiskScore: 69,
      sortBy: 'risk_score',
      sortOrder: 'asc',
      page: 3,
			pageSize: 1000,
			groupID: 'ungrouped',
      selectedAccountID: 42,
      detailTab: 'errors',
    })
    expect(serializeAccountMonitorQuery(state)).toEqual({
      range: 'custom',
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-08T00:00:00.000Z',
      platform: 'openai',
      query: 'owner@example.com',
      account_status: 'active',
      model: 'gpt-5',
      result: 'failure',
      rollup: 'parent',
      min_risk_score: '20',
      max_risk_score: '69',
      sort_by: 'risk_score',
      sort_order: 'asc',
      page: '3',
			page_size: '1000',
			group_id: 'ungrouped',
      account: '42',
      tab: 'errors',
    })
  })

  it('falls back safely for invalid pagination, risk, range, and tab values', () => {
    expect(parseAccountMonitorQuery({ range: 'forever', page: '-2', page_size: '12', min_risk_score: 'nan', max_risk_score: '500', tab: 'secrets' })).toMatchObject({
      range: '7d',
      page: 1,
      pageSize: 20,
      minRiskScore: undefined,
      maxRiskScore: 100,
      detailTab: 'models',
    })
  })
})
