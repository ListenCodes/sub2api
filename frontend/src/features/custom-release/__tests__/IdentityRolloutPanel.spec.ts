import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IdentityRolloutPanel from '@/features/custom-release/IdentityRolloutPanel.vue'
import { applyIdentityRollout, getUpdateStatus, prepareIdentityRollout } from '@/features/custom-release/api'

vi.mock('@/features/custom-release/api', () => ({
  applyIdentityRollout: vi.fn(),
  getUpdateStatus: vi.fn(),
  prepareIdentityRollout: vi.fn(),
  isTerminalUpdateStatus: (status: string) =>
    ['success', 'failed', 'conflict', 'expired', 'drifted', 'failed_rolled_back', 'rollback_failed'].includes(status)
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('IdentityRolloutPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(getUpdateStatus).mockRejectedValue({ response: { status: 404 } })
    vi.mocked(prepareIdentityRollout).mockReset()
    vi.mocked(applyIdentityRollout).mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('requires a separate prepare and explicit apply confirmation', async () => {
    const expiresAt = new Date(Date.now() + 60_000).toISOString()
    vi.mocked(prepareIdentityRollout).mockResolvedValue({
      job_id: 'update-identity-reset', update_kind: 'identity-config', identity_transition: 'stage0-safe-reset',
      action: 'prepare', status: 'prepared', message: 'prepared', expires_at: expiresAt, need_restart: false
    })
    vi.mocked(applyIdentityRollout).mockResolvedValue({
      job_id: 'update-identity-reset', update_kind: 'identity-config', identity_transition: 'stage0-safe-reset',
      action: 'apply', status: 'apply_queued', message: 'queued', need_restart: false
    })
    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()

    await wrapper.get('[data-testid="identity-prepare"]').trigger('click')
    await flushPromises()
    expect(prepareIdentityRollout).toHaveBeenCalledWith('stage0-safe-reset')
    expect(applyIdentityRollout).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="identity-apply"]').trigger('click')
    await flushPromises()
    expect(applyIdentityRollout).toHaveBeenCalledWith('update-identity-reset')
    wrapper.unmount()
  })

  it('restores only an identity configuration job', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValue({
      job_id: 'update-identity-stage1', update_kind: 'identity-config', identity_transition: 'stage1-v2',
      action: 'prepare', status: 'prepared', message: 'prepared', expires_at: new Date(Date.now() + 60_000).toISOString(), need_restart: false
    })
    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect((wrapper.get('[data-testid="identity-transition-select"]').element as HTMLSelectElement).value).toBe('stage1-v2')
    expect(wrapper.find('[data-testid="identity-apply"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps and retries a durable job after a transient restore failure', async () => {
    vi.useFakeTimers()
    localStorage.setItem('sub2api-identity-rollout-job-id', 'update-identity-durable')
    vi.mocked(getUpdateStatus)
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockResolvedValue({
        job_id: 'update-identity-durable', update_kind: 'identity-config', identity_transition: 'stage1-v2',
        action: 'prepare', status: 'prepared', message: 'prepared',
        expires_at: new Date(Date.now() + 60_000).toISOString(), need_restart: false
      })

    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(localStorage.getItem('sub2api-identity-rollout-job-id')).toBe('update-identity-durable')
    expect(wrapper.find('[data-testid="identity-prepare"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(getUpdateStatus).toHaveBeenLastCalledWith('update-identity-durable')
    expect(wrapper.find('[data-testid="identity-apply"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('retries discovery without a local pointer and unlocks after a confirmed 404', async () => {
    vi.useFakeTimers()
    vi.mocked(getUpdateStatus)
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockRejectedValue({ response: { status: 404 } })

    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('restoring')
    expect(wrapper.find('[data-testid="identity-prepare"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(getUpdateStatus).toHaveBeenLastCalledWith(undefined)
    expect(wrapper.find('[data-testid="identity-prepare"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('clears a stale local pointer after a confirmed 404', async () => {
    localStorage.setItem('sub2api-identity-rollout-job-id', 'update-identity-missing')
    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(localStorage.getItem('sub2api-identity-rollout-job-id')).toBeNull()
    expect(wrapper.find('[data-testid="identity-prepare"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('allows only one status poll in flight', async () => {
    vi.useFakeTimers()
    let resolvePoll!: (value: Awaited<ReturnType<typeof getUpdateStatus>>) => void
    const pendingPoll = new Promise<Awaited<ReturnType<typeof getUpdateStatus>>>((resolve) => { resolvePoll = resolve })
    vi.mocked(getUpdateStatus)
      .mockResolvedValueOnce({
        job_id: 'update-identity-running', update_kind: 'identity-config', identity_transition: 'stage1-ip',
        action: 'apply', status: 'apply_queued', message: 'queued', need_restart: false
      })
      .mockReturnValueOnce(pendingPoll)

    const wrapper = mount(IdentityRolloutPanel, { global: { stubs: { Icon: true } } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(getUpdateStatus).toHaveBeenCalledTimes(2)

    resolvePoll({
      job_id: 'update-identity-running', update_kind: 'identity-config', identity_transition: 'stage1-ip',
      action: 'apply', status: 'success', message: 'complete', need_restart: false
    })
    await flushPromises()
    expect(wrapper.text()).toContain('state.success')
    wrapper.unmount()
  })
})
