import { beforeEach, describe, expect, it, vi } from 'vitest'

import apiClient from '@/api/client'
import { accountMonitorAPI } from '@/api/admin/accountMonitor'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}))

describe('accountMonitorAPI', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    accountMonitorAPI.dispose()
  })

  it('serializes account filters using the authenticated admin proxy contract', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 0, page: 2, page_size: 50 } })

    await accountMonitorAPI.listAccounts({
      from: '2026-07-01T00:00:00.000Z',
      to: '2026-07-08T00:00:00.000Z',
      page: 2,
      pageSize: 50,
      sortBy: 'risk_score',
      sortOrder: 'asc',
      platform: 'openai',
      query: 'owner@example.com',
      accountStatus: 'active',
      minRiskScore: 20,
      maxRiskScore: 69,
      rollup: 'physical',
    })

    expect(apiClient.get).toHaveBeenCalledWith('/admin/extensions-self/account-monitor/accounts', {
      params: {
        from: '2026-07-01T00:00:00.000Z',
        to: '2026-07-08T00:00:00.000Z',
        page: 2,
        page_size: 50,
        sort_by: 'risk_score',
        sort_order: 'asc',
        platform: 'openai',
        query: 'owner@example.com',
        account_status: 'active',
        min_risk_score: 20,
        max_risk_score: 69,
        rollup: 'physical',
      },
      signal: expect.any(AbortSignal),
    })
  })

  it('preserves page size 1000 and concrete or ungrouped account group filters', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 895, page: 1, page_size: 1000 } })

    await accountMonitorAPI.listAccounts({ page: 1, pageSize: 1000, groupID: 'ungrouped' })
    expect(apiClient.get).toHaveBeenLastCalledWith('/admin/extensions-self/account-monitor/accounts', expect.objectContaining({
      params: expect.objectContaining({ page_size: 1000, group_id: 'ungrouped' })
    }))

    await accountMonitorAPI.listAccounts({ page: 1, pageSize: 1000, groupID: 11 })
    expect(apiClient.get).toHaveBeenLastCalledWith('/admin/extensions-self/account-monitor/accounts', expect.objectContaining({
      params: expect.objectContaining({ page_size: 1000, group_id: 11 })
    }))
  })

  it.each([12, 24, 48])('accepts group page size %i', async (pageSize) => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: pageSize } })
    await accountMonitorAPI.listGroups({ page: 1, pageSize, range: '6h' })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/extensions-self/account-monitor/group-monitor/groups', expect.objectContaining({ params: expect.objectContaining({ page_size: pageSize, range: '6h' }) }))
  })

	it.each(['7d', '30d'] as const)('accepts %s group range with page size 1000', async (range) => {
		vi.mocked(apiClient.get).mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 1000, bucket_seconds: range === '7d' ? 3600 : 21600 } })
		await accountMonitorAPI.listGroups({ page: 1, pageSize: 1000, range })
		expect(apiClient.get).toHaveBeenCalledWith('/admin/extensions-self/account-monitor/group-monitor/groups', expect.objectContaining({ params: expect.objectContaining({ page_size: 1000, range }) }))
	})

  it('rejects invalid page sizes and ranges before issuing a request', async () => {
		await expect(accountMonitorAPI.listAccounts({ page: 1, pageSize: 4 })).rejects.toThrow('page size')
		await expect(accountMonitorAPI.listAccounts({ page: 1, pageSize: 1001 })).rejects.toThrow('page size')
    await expect(accountMonitorAPI.listGroups({ page: 1, pageSize: 12, range: '2h' as '6h' })).rejects.toThrow('group range')
    expect(apiClient.get).not.toHaveBeenCalled()
  })

  it('cancels a superseded request in the same family', async () => {
    vi.mocked(apiClient.get).mockReturnValue(new Promise(() => {}))

    void accountMonitorAPI.getOverview({ from: '2026-07-01T00:00:00.000Z', to: '2026-07-02T00:00:00.000Z' })
    const firstSignal = vi.mocked(apiClient.get).mock.calls[0][1]?.signal
    void accountMonitorAPI.getOverview({ from: '2026-07-02T00:00:00.000Z', to: '2026-07-03T00:00:00.000Z' })

    expect(firstSignal?.aborted).toBe(true)
  })

  it.each([401, 423])('propagates structured auth error %i unchanged', async (status) => {
    const failure = { status, code: status === 423 ? 'ADMIN_COMPLIANCE_ACK_REQUIRED' : 'UNAUTHORIZED', message: 'blocked' }
    vi.mocked(apiClient.get).mockRejectedValue(failure)

    await expect(accountMonitorAPI.getDataQuality({})).rejects.toBe(failure)
  })

  it('sends thresholds and rebuild jobs with exact native payloads', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ data: { scope: 'global', scope_id: 0, success_rate: 0.92 } })
    vi.mocked(apiClient.post).mockResolvedValue({ data: { id: 9, status: 'pending' } })

    await accountMonitorAPI.updateThreshold({ scope: 'global', scope_id: 0, success_rate: 0.92 })
    await accountMonitorAPI.startRebuild({ from: '2026-07-01T00:00:00.000Z', to: '2026-07-08T00:00:00.000Z' })

    expect(apiClient.put).toHaveBeenCalledWith('/admin/extensions-self/account-monitor/thresholds', { scope: 'global', scope_id: 0, success_rate: 0.92 }, { signal: expect.any(AbortSignal) })
    expect(apiClient.post).toHaveBeenCalledWith('/admin/extensions-self/account-monitor/rebuild-jobs', { from: '2026-07-01T00:00:00.000Z', to: '2026-07-08T00:00:00.000Z' }, { signal: expect.any(AbortSignal) })
  })
})
