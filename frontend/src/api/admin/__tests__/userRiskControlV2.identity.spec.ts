import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mainAdminClient, userRiskControlV2API } from '../userRiskControlV2'

vi.mock('@/api/client', () => ({ default: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

describe('user risk identity API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses same-origin explicit identity endpoints and bounded page parameters', async () => {
    vi.mocked(mainAdminClient.get).mockResolvedValue({ data: { items: [], total: 0, page: 2, page_size: 20 } })
    await userRiskControlV2API.listUserIPIdentities(7, 2, 20)
    await userRiskControlV2API.listUserDeviceIdentities(7, 2, 20)
    await userRiskControlV2API.listAssociatedUsers(7, 2, 20)
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(1, '/admin/users/7/ip-identities', { params: { page: 2, limit: 20 } })
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(2, '/admin/users/7/device-identities', { params: { page: 2, limit: 20 } })
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(3, '/admin/users/7/associated-users', { params: { page: 2, limit: 20 } })
  })

  it('sends only an exact normalized IP query to the identity endpoint', async () => {
    vi.mocked(mainAdminClient.get).mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    await userRiskControlV2API.listUserIPIdentities(7, 1, 20, ' 8.8.8.8 ')
    expect(mainAdminClient.get).toHaveBeenCalledWith('/admin/users/7/ip-identities', { params: { page: 1, limit: 20, q: '8.8.8.8' } })
  })

  it('loads summary and health independently from detail lists', async () => {
    vi.mocked(mainAdminClient.get).mockImplementation(async (path) => ({ data: path === '/admin/identity-health' ? { enabled: true } : { user_id: 9, domains: [] } }))
    await userRiskControlV2API.getUserIdentitySummary(9)
    await userRiskControlV2API.getIdentityHealth()
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(1, '/admin/users/9/identity-summary')
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(2, '/admin/identity-health')
  })

  it('loads one bounded masked identity summary batch for the visible users', async () => {
    vi.mocked(mainAdminClient.get)
      .mockResolvedValueOnce({ data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1 } })
      .mockResolvedValueOnce({ data: { items: [], total: 0 } })
      .mockResolvedValueOnce({ data: { items: [{ user_id: 7, latest_ip: '203.0.113.0/24', country_code: 'US', region: 'CA', browser_instance_count: 2, api_client_count: 1, associated_account_count: 3, active_rule_count: 1, quality_state: 'healthy' }] } })

    const result = await userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })

    expect(mainAdminClient.get).toHaveBeenNthCalledWith(3, '/admin/identity-summaries', { params: { user_ids: '7' } })
    expect(result.items[0].identity).toMatchObject({ latest_ip: '203.0.113.0/24', browser_instance_count: 2, api_client_count: 1 })
  })

  it('keeps the account list available when identity summary loading fails', async () => {
    vi.mocked(mainAdminClient.get)
      .mockResolvedValueOnce({ data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active' }], total: 1 } })
      .mockResolvedValueOnce({ data: { items: [], total: 0 } })
      .mockRejectedValueOnce(new Error('identity service unavailable'))

    await expect(userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })).resolves.toMatchObject({ items: [{ id: 7 }] })
  })
})
