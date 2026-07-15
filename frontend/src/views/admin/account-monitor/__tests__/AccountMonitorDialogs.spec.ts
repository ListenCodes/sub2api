import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { accountMonitorAPI } from '@/api/admin/accountMonitor'
import AccountMonitorThresholdDialog from '@/views/admin/account-monitor/AccountMonitorThresholdDialog.vue'
import AccountMonitorRebuildDialog from '@/views/admin/account-monitor/AccountMonitorRebuildDialog.vue'

vi.mock('@/api/admin/accountMonitor', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api/admin/accountMonitor')>()
  return { ...original, accountMonitorAPI: { getThreshold: vi.fn(), updateThreshold: vi.fn(), startRebuild: vi.fn(), getRebuildJob: vi.fn() } }
})

afterEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('account monitor dialogs', () => {
  it('retains the threshold input when saving fails', async () => {
    vi.mocked(accountMonitorAPI.getThreshold).mockResolvedValue({ scope: 'global', scope_id: 0, success_rate: 0.9 })
    vi.mocked(accountMonitorAPI.updateThreshold).mockRejectedValue(new Error('保存失败'))
    mount(AccountMonitorThresholdDialog, { props: { show: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const input = document.querySelector('[data-testid="threshold-success-rate"]') as HTMLInputElement
    input.value = '92'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    ;(document.querySelector('[data-testid="threshold-save"]') as HTMLElement).click()
    await flushPromises()

    expect(input.value).toBe('92')
    expect(document.body.textContent).toContain('保存失败')
  })

  it('shows rebuild job ID, processed rows, and a terminal error', async () => {
    vi.useFakeTimers()
    vi.mocked(accountMonitorAPI.startRebuild).mockResolvedValue({ id: 17, from: '2026-07-01T00:00:00Z', to: '2026-07-02T00:00:00Z', status: 'pending', processed_rows: 0 })
    vi.mocked(accountMonitorAPI.getRebuildJob).mockResolvedValue({ id: 17, from: '2026-07-01T00:00:00Z', to: '2026-07-02T00:00:00Z', status: 'failed', processed_rows: 321, error: '聚合写入失败' })
    mount(AccountMonitorRebuildDialog, { props: { show: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    const from = document.querySelector('[data-testid="rebuild-from"]') as HTMLInputElement
    const to = document.querySelector('[data-testid="rebuild-to"]') as HTMLInputElement
    from.value = '2026-07-01T00:00'
    from.dispatchEvent(new Event('input', { bubbles: true }))
    to.value = '2026-07-02T00:00'
    to.dispatchEvent(new Event('input', { bubbles: true }))
    ;(document.querySelector('[data-testid="rebuild-start"]') as HTMLElement).click()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(document.body.textContent).toContain('17')
    expect(document.body.textContent).toContain('321')
    expect(document.body.textContent).toContain('聚合写入失败')
  })
})
