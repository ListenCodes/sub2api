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

const releases = [
  {
    release_id: 'release-3',
    official_version: 'v0.1.163',
    official_commit: 'c'.repeat(40),
    custom_version: 'v1.0.4',
    custom_version_sequence: 4,
    custom_commit: '33333333'.padEnd(40, 'c'),
    published_at: '2026-07-23T00:00:00Z'
  },
  {
    release_id: 'release-2',
    official_version: 'v0.1.162',
    official_commit: 'd'.repeat(40),
    custom_version: 'v1.0.3',
    custom_version_sequence: 3,
    custom_commit: '22222222'.padEnd(40, 'd'),
    published_at: '2026-07-22T00:00:00Z'
  },
  {
    release_id: 'release-1',
    official_version: 'v0.1.161',
    official_commit: 'e'.repeat(40),
    custom_version: 'v1.0.2',
    custom_version_sequence: 2,
    custom_commit: '11111111'.padEnd(40, 'e'),
    published_at: '2026-07-21T00:00:00Z'
  }
]

function mountPanel(props: Record<string, unknown>) {
  return mount(ReleaseRollbackPanel, {
    props: { releases: [], ...props },
    global: { stubs: { Icon: true } }
  })
}

describe('ReleaseRollbackPanel', () => {
  afterEach(() => vi.useRealTimers())

  it('renders loading, error with retry, and empty states', async () => {
    const loading = mountPanel({ current: null, currentLoading: true })
    expect(loading.get('[data-testid="rollback-loading"]').exists()).toBe(true)
    loading.unmount()

    const failed = mountPanel({ current: null, error: 'current release unavailable' })
    expect(failed.get('[data-testid="rollback-error"]').text()).toContain('current release unavailable')
    await failed.get('[data-testid="rollback-retry"]').trigger('click')
    expect(failed.emitted('retry')).toHaveLength(1)
    failed.unmount()

    const empty = mountPanel({ current, releases: [] })
    expect(empty.get('[data-testid="rollback-empty"]').text()).toContain('empty')
    empty.unmount()
  })

  it('shows current identity and three complete target identities', () => {
    const wrapper = mountPanel({ current, releases })
    expect(wrapper.get('[data-testid="rollback-current-pair"]').text()).toContain('v0.1.164')
    expect(wrapper.get('[data-testid="rollback-current-pair"]').text()).toContain('v1.0.5')

    const targets = wrapper.findAll('[data-testid="rollback-target-pair"]')
    expect(targets).toHaveLength(3)
    for (let index = 0; index < targets.length; index++) {
      expect(targets[index].text()).toContain(`Official ${releases[index].official_version}`)
      expect(targets[index].text()).toContain(`Custom ${releases[index].custom_version}`)
      expect(targets[index].text()).toContain(releases[index].custom_commit.slice(0, 8))
      expect(targets[index].text()).toContain('2026')
    }
  })

  it('selects an amber radio target and prepares that release', async () => {
    const wrapper = mountPanel({ current, releases })
    const second = wrapper.findAll('[data-testid="rollback-target-pair"]')[1]
    await second.trigger('click')

    expect(second.classes()).toContain('border-amber-400')
    expect(second.get('[data-testid="rollback-radio"]').classes()).toContain('border-amber-500')
    await wrapper.get('[data-testid="prepare-rollback"]').trigger('click')
    expect(wrapper.emitted('prepare')).toEqual([['release-2']])
  })

  it('locks the prepared target and confirms only its job', async () => {
    const operation = {
      job_id: 'rollback-prepared',
      operation_kind: 'rollback' as const,
      action: 'prepare' as const,
      status: 'prepared' as const,
      message: 'prepared',
      need_restart: false,
      target_release_id: 'release-2',
      expires_at: new Date(Date.now() + 60_000).toISOString()
    }
    const wrapper = mountPanel({ current, releases, operation })
    expect(wrapper.get('[data-testid="prepared-rollback-target"]').text()).toContain('v0.1.162')

    await wrapper.findAll('[data-testid="rollback-target-pair"]')[0].trigger('click')
    expect(wrapper.findAll('[data-testid="rollback-target-pair"]')[1].classes()).toContain('border-amber-400')
    expect(wrapper.find('[data-testid="prepare-rollback"]').exists()).toBe(false)
    await wrapper.get('[data-testid="confirm-rollback"]').trigger('click')
    expect(wrapper.emitted('apply')).toEqual([['rollback-prepared']])
  })

  it('expires once, disables confirmation, and keeps the target recoverable', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-24T00:00:00Z'))
    const operation = {
      job_id: 'rollback-expiring',
      operation_kind: 'rollback' as const,
      action: 'prepare' as const,
      status: 'prepared' as const,
      message: 'prepared',
      need_restart: false,
      target_release_id: 'release-3',
      expires_at: '2026-07-24T00:00:02Z'
    }
    const wrapper = mountPanel({ current, releases, operation })
    expect(wrapper.get('[data-testid="rollback-countdown"]').text()).toContain('2')

    await vi.advanceTimersByTimeAsync(2_500)
    expect(wrapper.get('[data-testid="confirm-rollback"]').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('expired')).toEqual([['rollback-expiring']])
    await vi.advanceTimersByTimeAsync(3_000)
    expect(wrapper.emitted('expired')).toEqual([['rollback-expiring']])
    expect(wrapper.findAll('[data-testid="rollback-target-pair"]')).toHaveLength(3)
  })

  it('shows a terminal operation failure without hiding targets and retries once', async () => {
    const wrapper = mountPanel({
      current,
      releases,
      operation: {
        job_id: 'rollback-failed',
        operation_kind: 'rollback',
        action: 'prepare',
        status: 'failed',
        message: 'snapshot verification failed',
        need_restart: false,
        target_release_id: 'release-2'
      }
    })
    expect(wrapper.get('[data-testid="rollback-error"]').text()).toContain('snapshot verification failed')
    expect(wrapper.findAll('[data-testid="rollback-target-pair"]')).toHaveLength(3)
    await wrapper.get('[data-testid="rollback-retry"]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
