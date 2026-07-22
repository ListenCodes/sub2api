import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '@/components/common/VersionBadge.vue'

const mocks = vi.hoisted(() => ({
  authStore: { isAdmin: true },
  appStore: {
    versionLoading: false,
    currentVersion: '0.1.152',
    latestVersion: '0.1.153',
    hasUpdate: true,
    releaseInfo: undefined,
    buildType: 'source',
    fetchVersion: vi.fn(),
    clearVersionCache: vi.fn()
  },
  performUpdate: vi.fn(),
  prepareUpdate: vi.fn(),
  applyUpdate: vi.fn(),
  getUpdateStatus: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn(),
  restartService: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => mocks.authStore,
  useAppStore: () => mocks.appStore
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: mocks.performUpdate,
  prepareUpdate: mocks.prepareUpdate,
  applyUpdate: mocks.applyUpdate,
  getUpdateStatus: mocks.getUpdateStatus,
  getRollbackVersions: mocks.getRollbackVersions,
  rollback: mocks.rollback,
  restartService: mocks.restartService,
  isTerminalUpdateStatus: (status: string) =>
    status === 'success' || status === 'failed' || status === 'conflict' || status === 'expired' || status === 'drifted',
  isPollingSettledUpdateStatus: (status: string) =>
    status === 'success' || status === 'failed' || status === 'conflict' || status === 'prepared',
  updateNeedsRestart: (job: { need_restart: boolean }) => job.need_restart,
  updateWasPublished: (job: { published?: boolean }) => job.published === true
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: { value: false }, copyToClipboard: vi.fn() })
}))

describe('VersionBadge conflict reporting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mocks.appStore.hasUpdate = true
    mocks.appStore.buildType = 'release'
    mocks.appStore.updateKind = 'official'
    mocks.appStore.detectionComplete = true
    mocks.appStore.updateWarning = ''
    mocks.appStore.targetCustomShortSHA = ''
    mocks.prepareUpdate.mockImplementation(mocks.performUpdate)
    mocks.getUpdateStatus.mockRejectedValueOnce({ response: { status: 404 } })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows conflicted files and production safety state after a failed update', async () => {
    mocks.performUpdate.mockResolvedValue({ job_id: 'update-conflict' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-conflict',
      status: 'conflict',
      message: 'upstream merge conflict',
      need_restart: false,
      published: false,
      conflict_files: ['backend/internal/server/routes/gateway.go', 'deploy/README.md'],
      conflict_base: 'custom123',
      conflict_upstream: 'upstream456',
      release_tag: 'v0.1.158',
      release_commit: '26abd19a2812edba02bbef93c3e2a620141cc257',
      release_published_at: '2026-07-16T12:37:06Z',
      conflict_release: 'v0.1.158@26abd19a2812edba02bbef93c3e2a620141cc257',
      conflict_log:
        '/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/update-conflict/metadata.json',
      resolution_hint: 'Resolve conflicts and retry.'
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.152' },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()

    await wrapper.find('button').trigger('click')
    const updateButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.updateNow'))
    expect(updateButton).toBeDefined()
    await updateButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('version.updateConflict')
    expect(wrapper.text()).toContain('backend/internal/server/routes/gateway.go')
    expect(wrapper.text()).toContain('deploy/README.md')
    expect(wrapper.text()).toContain('version.updateConflictNoProductionChange')
    expect(wrapper.text()).toContain('v0.1.158@26abd19a2812')
    expect(wrapper.text()).not.toContain('version.updatePublished')

    wrapper.unmount()
  })

  it('shows the stable Release tag and short commit after publishing', async () => {
    mocks.performUpdate.mockResolvedValue({ job_id: 'update-published' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-published',
      status: 'success',
      message: 'PUBLISH OK',
      need_restart: false,
      published: true,
      published_commit: 'customrelease1234567890',
      release_tag: 'v0.1.158',
      release_commit: '26abd19a2812edba02bbef93c3e2a620141cc257',
      release_published_at: '2026-07-16T12:37:06Z'
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.152' },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()

    await wrapper.find('button').trigger('click')
    const updateButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.updateNow'))
    await updateButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('version.updatePublished')
    expect(wrapper.text()).toContain('v0.1.158')
    expect(wrapper.text()).toContain('commit 26abd19a2812')

    wrapper.unmount()
  })

  it('does not show the previous terminal result when the update dialog is reopened', async () => {
    mocks.performUpdate.mockResolvedValue({ job_id: 'update-published' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-published',
      status: 'success',
      message: 'PUBLISH OK',
      need_restart: false,
      published: true,
      published_commit: 'customrelease1234567890'
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.152' },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()
    const badge = wrapper.find('button')
    await badge.trigger('click')
    const updateButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.updateNow'))
    await updateButton!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(wrapper.text()).toContain('version.updatePublished')

    await badge.trigger('click')
    await badge.trigger('click')

    expect(wrapper.text()).not.toContain('version.updatePublished')
    expect(wrapper.text()).not.toContain('PUBLISH OK')

    wrapper.unmount()
  })

  it('resumes a persisted non-terminal release job after refresh', async () => {
    localStorage.setItem('sub2api-release-job-id', 'update-resume')
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-resume',
      status: 'waiting_actions',
      message: 'Waiting for GitHub Actions',
      need_restart: false,
      published: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button').trigger('click')

    expect(mocks.getUpdateStatus).toHaveBeenCalledWith('update-resume')
    expect(wrapper.text()).toContain('version.releaseState.waiting_actions')
    expect(wrapper.text()).toContain('Waiting for GitHub Actions')

    wrapper.unmount()
  })

  it('restores a prepared job and applies it only after explicit confirmation', async () => {
    const expiresAt = new Date(Date.now() + 15 * 60 * 1000).toISOString()
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-prepared',
      action: 'prepare',
      status: 'prepared',
      message: 'Prepared',
      expires_at: expiresAt,
      need_restart: false
    })
    mocks.applyUpdate.mockResolvedValue({
      job_id: 'update-prepared',
      action: 'apply',
      status: 'apply_queued',
      message: 'Apply queued',
      need_restart: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button').trigger('click')

    expect(wrapper.text()).toContain('version.prepared')
    expect(mocks.applyUpdate).not.toHaveBeenCalled()
    const confirmButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.confirmUpdate'))
    expect(confirmButton).toBeDefined()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(mocks.applyUpdate).toHaveBeenCalledWith('update-prepared')
    wrapper.unmount()
  })

  it('shows docs-only detection without a production update action', async () => {
    mocks.appStore.updateKind = 'docs-only'
    mocks.appStore.hasUpdate = true

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button').trigger('click')

    expect(wrapper.text()).toContain('version.docsOnlyUpdate')
    expect(wrapper.findAll('button').some((button) => button.text().includes('version.updateNow'))).toBe(false)
    wrapper.unmount()
  })

  it('shows an incomplete detection warning without a production action', async () => {
    mocks.appStore.detectionComplete = false
    mocks.appStore.updateWarning = 'custom branch probe unavailable'
    mocks.appStore.hasUpdate = false
    mocks.appStore.updateKind = 'none'

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button').trigger('click')

    expect(wrapper.text()).toContain('version.updateDetectionIncomplete')
    expect(wrapper.text()).toContain('custom branch probe unavailable')
    expect(wrapper.findAll('button').some((button) => button.text().includes('version.updateNow'))).toBe(false)
    wrapper.unmount()
  })

  it('allows a release check with no newer upstream version and keeps polling past 15 minutes', async () => {
    vi.useFakeTimers()
    mocks.appStore.hasUpdate = false
    mocks.appStore.updateKind = 'custom'
    mocks.performUpdate.mockResolvedValue({
      job_id: 'update-custom',
      status: 'checking_release',
      message: 'Release job queued',
      need_restart: false
    })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-custom',
      status: 'waiting_images',
      message: 'Waiting for paired images',
      need_restart: false,
      published: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button').trigger('click')
    const updateButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.updateNow'))
    expect(updateButton).toBeDefined()

    await updateButton!.trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(16 * 60 * 1000)

    expect(wrapper.text()).toContain('version.releaseState.waiting_images')
    expect(wrapper.text()).not.toContain('status polling timed out')
    expect(mocks.getUpdateStatus).toHaveBeenCalled()

    wrapper.unmount()
  })
})
