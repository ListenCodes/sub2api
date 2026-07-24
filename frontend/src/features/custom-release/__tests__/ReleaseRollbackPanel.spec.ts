import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ReleaseRollbackPanel from '@/features/custom-release/ReleaseRollbackPanel.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, values?: { seconds?: number }) =>
      key === 'secondsRemaining' ? `${values?.seconds}s remaining` : key
  })
}))

const current = {
  release_id: 'release-current',
  official_version: 'v0.1.164',
  official_commit: 'a'.repeat(40),
  custom_version: 'v1.0.5',
  custom_version_sequence: 5,
  custom_commit: 'b'.repeat(40),
  published_at: '2026-07-24T00:00:00Z'
}

const target = {
  release_id: 'release-target',
  official_version: 'v0.1.162',
  official_commit: 'c'.repeat(40),
  custom_version: 'v1.0.3',
  custom_version_sequence: 3,
  custom_commit: '12345678'.padEnd(40, 'd'),
  published_at: '2026-07-22T00:00:00Z'
}

describe('ReleaseRollbackPanel', () => {
  afterEach(() => vi.useRealTimers())

  it('shows paired releases and emits prepare without exposing confirm', async () => {
    const wrapper = mount(ReleaseRollbackPanel, {
      props: { current, releases: [target], selected: '' }
    })

    expect(wrapper.get('[data-testid="rollback-current-pair"]').text()).toContain('v0.1.164')
    expect(wrapper.get('[data-testid="rollback-current-pair"]').text()).toContain('v1.0.5')
    expect(wrapper.get('[data-testid="rollback-target-pair"]').text()).toContain('v0.1.162')
    expect(wrapper.get('[data-testid="rollback-target-pair"]').text()).toContain('v1.0.3')
    expect(wrapper.text()).toContain('12345678')
    expect(wrapper.text()).toContain('2026')

    await wrapper.get('[data-testid="rollback-target-pair"]').trigger('click')
    await wrapper.get('[data-testid="prepare-rollback"]').trigger('click')

    expect(wrapper.emitted('prepare')).toEqual([['release-target']])
    expect(wrapper.find('[data-testid="confirm-rollback"]').exists()).toBe(false)
  })

  it('only enables confirmation for an unexpired prepared operation', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-24T00:00:00Z'))
    const operation = {
      job_id: 'rollback-prepared',
      operation_kind: 'rollback' as const,
      action: 'prepare' as const,
      status: 'prepared' as const,
      message: 'prepared',
      need_restart: false,
      target_release_id: target.release_id,
      expires_at: '2026-07-24T00:01:00Z'
    }
    const wrapper = mount(ReleaseRollbackPanel, {
      props: { current, releases: [target], selected: target.release_id, operation }
    })

    const confirm = wrapper.get('[data-testid="confirm-rollback"]')
    expect(confirm.attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="rollback-countdown"]').text()).toContain('60')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('[data-testid="rollback-countdown"]').text()).toContain('59')
    await confirm.trigger('click')
    expect(wrapper.emitted('apply')).toEqual([['rollback-prepared']])

    await wrapper.setProps({ operation: { ...operation, expires_at: '2026-07-23T23:59:59Z' } })
    expect(wrapper.get('[data-testid="confirm-rollback"]').attributes('disabled')).toBeDefined()
  })

  it('locks the prepared target even if another release is clicked', async () => {
    const other = { ...target, release_id: 'release-other', official_version: 'v0.1.161' }
    const wrapper = mount(ReleaseRollbackPanel, {
      props: {
        current,
        releases: [target, other],
        selected: target.release_id,
        operation: {
          job_id: 'rollback-prepared',
          operation_kind: 'rollback',
          action: 'prepare',
          status: 'prepared',
          message: 'prepared',
          need_restart: false,
          target_release_id: target.release_id,
          expires_at: new Date(Date.now() + 60_000).toISOString()
        }
      }
    })

    expect(wrapper.get('[data-testid="prepared-rollback-target"]').text()).toContain('v0.1.162')
    const targets = wrapper.findAll('[data-testid="rollback-target-pair"]')
    await targets[1].trigger('click')
    expect(wrapper.emitted('update:selected')).toBeUndefined()
  })
})
