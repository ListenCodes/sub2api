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

  it('sends an exact normalized IP in a POST body instead of a URL query', async () => {
    vi.mocked(mainAdminClient.post).mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20 } })
    await userRiskControlV2API.listUserIPIdentities(7, 1, 20, ' 8.8.8.8 ')
    expect(mainAdminClient.post).toHaveBeenCalledWith('/admin/users/7/ip-identities/search', { page: 1, limit: 20, query: '8.8.8.8' })
    expect(mainAdminClient.get).not.toHaveBeenCalled()
  })

  it('loads summary and health independently from detail lists', async () => {
    vi.mocked(mainAdminClient.get).mockImplementation(async (path) => ({ data: path === '/admin/identity-health' ? { enabled: true } : { user_id: 9, domains: [] } }))
    await userRiskControlV2API.getUserIdentitySummary(9)
    await userRiskControlV2API.getIdentityHealth()
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(1, '/admin/users/9/identity-summary')
    expect(mainAdminClient.get).toHaveBeenNthCalledWith(2, '/admin/identity-health')
  })

  it('adds a stable view-session header only when the caller provides one', async () => {
    vi.mocked(mainAdminClient.get).mockResolvedValue({ data: { user_id: 9, domains: [] } })
    vi.mocked(mainAdminClient.post).mockResolvedValue({ data: { items: [], total: 0 } })

    await userRiskControlV2API.getUserIdentitySummary(9, 'drawer-session-1')
    await userRiskControlV2API.listUserIPIdentities(9, 1, 20, '8.8.8.8', 'drawer-session-1')

    const config = { headers: { 'X-Risk-View-Session': 'drawer-session-1' } }
    expect(mainAdminClient.get).toHaveBeenCalledWith('/admin/users/9/identity-summary', config)
    expect(mainAdminClient.post).toHaveBeenCalledWith('/admin/users/9/ip-identities/search', { page: 1, limit: 20, query: '8.8.8.8' }, config)
  })

  it('uses the server-side risk queue with account completion and masked identity summary', async () => {
    vi.mocked(mainAdminClient.get).mockResolvedValueOnce({ data: { items: [{ id: 7, username: 'Alice', email: 'alice@example.com', status: 'active', identity: { user_id: 7, latest_ip: '203.0.113.0/24', country_code: 'US', region: 'CA', browser_instance_count: 2, api_client_count: 1, associated_account_count: 3, active_rule_count: 1, quality_state: 'healthy' } }], total: 1 } })

    const result = await userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })

    expect(mainAdminClient.get).toHaveBeenCalledTimes(1)
		expect(mainAdminClient.get).toHaveBeenCalledWith('/admin/user-risk/users', { params: { view: 'unassigned', page: 1, page_size: 20 } })
    expect(result.items[0].identity).toMatchObject({ latest_ip: '203.0.113.0/24', browser_instance_count: 2, api_client_count: 1 })
  })

  it('does not disguise server-side account completion failures as an empty queue', async () => {
    vi.mocked(mainAdminClient.get).mockRejectedValueOnce(new Error('account completion unavailable'))

    await expect(userRiskControlV2API.listUsers({ page: 1, pageSize: 20 })).rejects.toThrow('account completion unavailable')
  })

  it('keeps case feedback separate from account enforcement', async () => {
    vi.mocked(mainAdminClient.post).mockResolvedValue({ data: {} })

    await userRiskControlV2API.claimReviewCase(31)
    await userRiskControlV2API.submitReviewFeedback(31, 'legitimate_shared', 'Corporate NAT confirmed')

    expect(mainAdminClient.post).toHaveBeenNthCalledWith(1, '/admin/user-risk-control/review-cases/31/claim', {})
    expect(mainAdminClient.post).toHaveBeenNthCalledWith(2, '/admin/user-risk-control/review-cases/31/feedback', {
      feedback: 'legitimate_shared',
      reason: 'Corporate NAT confirmed',
    })
    expect(mainAdminClient.put).not.toHaveBeenCalled()
  })
})
