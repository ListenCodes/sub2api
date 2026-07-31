import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '@/features/custom-release/CustomReleaseBadge.vue'

const mocks = vi.hoisted(() => ({
  authStore: { isAdmin: true },
  appStore: {
    versionLoading: false,
    currentVersion: '0.1.152',
    latestVersion: '0.1.153',
    hasUpdate: true,
    noticeUnread: true,
    runtimeUpdate: true,
    officialUpdate: true,
    customUpdate: false,
    targetOfficialCommit: 'c'.repeat(40),
    targetCustomCommit: 'd'.repeat(40),
    updateFingerprint: 'f'.repeat(64),
    noticeWarning: '',
    releaseInfo: undefined,
    buildType: 'source',
    currentRelease: null as Record<string, unknown> | null,
    currentReleaseLoading: false,
    currentReleaseError: '',
    targetOfficialVersion: '',
    targetCustomVersion: '',
    fetchVersion: vi.fn(),
    fetchCurrentRelease: vi.fn(),
    markCurrentNoticeRead: vi.fn(),
    clearVersionCache: vi.fn()
  },
  prepareUpdate: vi.fn(),
  applyUpdate: vi.fn(),
  getUpdateStatus: vi.fn(),
  getRollbackReleases: vi.fn(),
  prepareRollback: vi.fn(),
  applyRollback: vi.fn(),
  rollback: vi.fn(),
  restartService: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => mocks.authStore
}))

vi.mock('@/features/custom-release/store', async () => {
  const { reactive } = await import('vue')
  mocks.appStore = reactive(mocks.appStore)
  return { useCustomReleaseStore: () => mocks.appStore }
})

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/features/custom-release/api', () => ({
  prepareUpdate: mocks.prepareUpdate,
  applyUpdate: mocks.applyUpdate,
  getUpdateStatus: mocks.getUpdateStatus,
  getRollbackReleases: mocks.getRollbackReleases,
  prepareRollback: mocks.prepareRollback,
  applyRollback: mocks.applyRollback,
  rollback: mocks.rollback,
  restartService: mocks.restartService,
  isTerminalUpdateStatus: (status: string) =>
    status === 'success' || status === 'failed' || status === 'conflict' || status === 'expired' || status === 'drifted' || status === 'failed_rolled_back' || status === 'rollback_failed',
  isPollingSettledUpdateStatus: (status: string) =>
    status === 'success' || status === 'failed' || status === 'conflict' || status === 'prepared',
  updateNeedsRestart: (job: { need_restart: boolean }) => job.need_restart,
  updateWasPublished: (job: { published?: boolean }) => job.published === true
}))

describe('VersionBadge conflict reporting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mocks.appStore.hasUpdate = true
    mocks.appStore.noticeUnread = true
    mocks.appStore.runtimeUpdate = true
    mocks.appStore.officialUpdate = true
    mocks.appStore.customUpdate = false
    mocks.appStore.updateFingerprint = 'f'.repeat(64)
    mocks.appStore.noticeWarning = ''
    mocks.appStore.buildType = 'release'
    mocks.appStore.updateKind = 'official'
    mocks.appStore.detectionComplete = true
    mocks.appStore.updateWarning = ''
    mocks.appStore.targetCustomShortSHA = ''
    mocks.appStore.targetOfficialVersion = 'v0.1.169'
    mocks.appStore.targetOfficialCommit = 'c'.repeat(40)
    mocks.appStore.targetCustomVersion = ''
    mocks.appStore.targetCustomCommit = 'd'.repeat(40)
    mocks.appStore.currentRelease = {
      release_id: 'release-current',
      official_version: 'v0.1.164',
      official_commit: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      custom_version: 'v1.0.5',
      custom_version_sequence: 5,
      custom_commit: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      main_digest: `sha256:${'1'.repeat(64)}`,
      extensions_digest: `sha256:${'2'.repeat(64)}`,
      published_at: '2026-07-24T00:00:00Z'
    }
    mocks.appStore.currentReleaseLoading = false
    mocks.appStore.currentReleaseError = ''
    mocks.appStore.markCurrentNoticeRead.mockImplementation(async () => {
      mocks.appStore.noticeUnread = false
    })
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockRejectedValue({ response: { status: 404 } })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps runtime updates amber independently from advisory unread state', async () => {
    mocks.appStore.noticeUnread = false
    const acknowledged = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    const acknowledgedBadge = acknowledged.get('[data-testid="custom-release-badge"]')
    expect(acknowledgedBadge.classes()).toContain('bg-amber-100')
    expect(acknowledged.find('[data-testid="release-notice-indicator"]').exists()).toBe(true)
    await acknowledgedBadge.trigger('click')
    expect(acknowledged.text()).toContain('version.latestVersion')
    expect(mocks.appStore.markCurrentNoticeRead).not.toHaveBeenCalled()
    acknowledged.unmount()

    mocks.appStore.noticeUnread = true
    const unread = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    const unreadBadge = unread.get('[data-testid="custom-release-badge"]')
    expect(unreadBadge.classes()).toContain('bg-amber-100')
    expect(unread.get('[data-testid="release-notice-indicator"]').exists()).toBe(true)
    expect(unread.get('[data-testid="release-notice-ping"]').classes()).toContain('animate-ping')
    unread.unmount()
  })

  it('retries a reconciled unread fingerprint and permits a new target', async () => {
    mocks.appStore.updateKind = 'docs-only'
    mocks.appStore.runtimeUpdate = false
    mocks.appStore.officialUpdate = false
    mocks.appStore.customUpdate = true
    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    const badge = wrapper.get('[data-testid="custom-release-badge"]')

    await badge.trigger('click')
    await flushPromises()
    await badge.trigger('click')
    await badge.trigger('click')
    await flushPromises()
    expect(mocks.appStore.markCurrentNoticeRead).toHaveBeenCalledTimes(1)

    await badge.trigger('click')
    mocks.appStore.noticeUnread = true
    await nextTick()
    await badge.trigger('click')
    await flushPromises()
    expect(mocks.appStore.markCurrentNoticeRead).toHaveBeenCalledTimes(2)

    await badge.trigger('click')
    mocks.appStore.updateFingerprint = 'e'.repeat(64)
    mocks.appStore.noticeUnread = true
    await nextTick()
    await badge.trigger('click')
    await flushPromises()
    expect(mocks.appStore.markCurrentNoticeRead).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('starts update preparation without waiting for advisory acknowledgement', async () => {
    mocks.prepareUpdate.mockResolvedValue({
      job_id: 'update-order',
      status: 'resolving_target',
      message: 'queued',
      need_restart: false
    })
    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="custom-release-badge"]').trigger('click')
    await flushPromises()
    mocks.appStore.updateFingerprint = 'd'.repeat(64)
    mocks.appStore.noticeUnread = true
    await nextTick()

    const updateButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.updateNow'))
    await updateButton!.trigger('click')
    await flushPromises()

    expect(mocks.appStore.markCurrentNoticeRead).not.toHaveBeenCalled()
    expect(mocks.prepareUpdate).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders current release errors and retries identity plus history', async () => {
    mocks.appStore.currentRelease = null
    mocks.appStore.currentReleaseError = 'current release unavailable'
    mocks.getRollbackReleases.mockRejectedValue(new Error('history unavailable'))
    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="custom-release-badge"]').trigger('click')
    await wrapper.get('[data-testid="rollback-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="rollback-error"]').text()).toContain('current release unavailable')
    await wrapper.get('[data-testid="rollback-retry"]').trigger('click')
    await flushPromises()
    expect(mocks.appStore.fetchCurrentRelease).toHaveBeenCalledTimes(2)
    expect(mocks.getRollbackReleases).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('shows conflicted files and production safety state after a failed update', async () => {
    mocks.prepareUpdate.mockResolvedValue({ job_id: 'update-conflict' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-conflict',
      status: 'conflict',
      message: 'upstream merge conflict',
      need_restart: false,
      published: false,
      conflict_files: ['backend/internal/server/routes/gateway.go', 'deploy/README.md'],
      base_commit: '4920db770b7a8d17287e2d43ad5ecf7eb00815d1',
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

    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')
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
    expect(wrapper.text()).toContain('4920db770b7a')
    expect(wrapper.text()).toContain('v0.1.158@26abd19a2812')
    expect(wrapper.text()).not.toContain('version.updatePublished')

    wrapper.unmount()
  })

  it('shows the stable Release tag and short commit after publishing', async () => {
    mocks.prepareUpdate.mockResolvedValue({ job_id: 'update-published' })
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
    expect(mocks.appStore.fetchCurrentRelease).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('does not show the previous terminal result when the update dialog is reopened', async () => {
    mocks.prepareUpdate.mockResolvedValue({ job_id: 'update-published' })
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

  it('settles automatic restoration terminal states instead of polling to timeout', async () => {
    mocks.prepareUpdate.mockResolvedValue({ job_id: 'update-auto-restored' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-auto-restored',
      operation_kind: 'update',
      action: 'apply',
      status: 'failed_rolled_back',
      message: 'deployment failed; previous release restored',
      need_restart: false,
      rollback: { attempted: true, succeeded: true, message: 'automatic restoration succeeded' }
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')
    const updateButton = wrapper.findAll('button').find((button) => button.text().includes('version.updateNow'))
    await updateButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('deployment failed; previous release restored')
    expect(wrapper.text()).toContain('automatic restoration succeeded')
    expect(localStorage.getItem('sub2api-release-job-id')).toBe('update-auto-restored')
    wrapper.unmount()
  })

  it('shows the proposed official and custom version pair', async () => {
    mocks.appStore.targetOfficialVersion = 'v0.1.165'
    mocks.appStore.targetCustomVersion = 'v1.0.6'
    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')

    expect(wrapper.get('[data-testid="target-version-pair"]').text()).toContain('v0.1.165')
    expect(wrapper.get('[data-testid="target-version-pair"]').text()).toContain('v1.0.6')
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

  it('restores a matching server-side failure across close and reopen', async () => {
    mocks.appStore.updateKind = 'combined'
    mocks.appStore.officialUpdate = true
    mocks.appStore.customUpdate = true
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-server-failure',
      operation_kind: 'update',
      action: 'prepare',
      status: 'failed',
      message: 'required check deployment concluded failure',
      stable_release_tag: 'v0.1.169',
      stable_release_commit: 'c'.repeat(40),
      target_custom_commit: 'd'.repeat(40),
      failed_check: 'deployment',
      check_url: 'https://github.com/ListenCodes/sub2api/actions/runs/1/job/2',
      conclusion: 'failure',
      error_code: 'ACTIONS_REQUIRED_CHECK_FAILED',
      production_changed: false,
      need_restart: false,
      updated_at: '2026-07-31T10:00:00Z'
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.168' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()

    expect(mocks.getUpdateStatus).toHaveBeenCalledWith(undefined)
    expect(localStorage.getItem('sub2api-release-job-id')).toBe('update-server-failure')
    const badge = wrapper.get('[data-testid="custom-release-badge"]')
    await badge.trigger('click')
    expect(wrapper.text()).toContain('required check deployment concluded failure')
    expect(wrapper.text()).toContain('deployment')
    expect(wrapper.text()).toContain('ACTIONS_REQUIRED_CHECK_FAILED')
    expect(wrapper.text()).toContain('failure')
    expect(wrapper.text()).toContain('production_changed=false')
    const actionsLink = wrapper.get(
      'a[href="https://github.com/ListenCodes/sub2api/actions/runs/1/job/2"]'
    )
    expect(actionsLink.attributes('target')).toBe('_blank')
    expect(actionsLink.attributes('rel')).toContain('noopener')

    await badge.trigger('click')
    await badge.trigger('click')
    expect(wrapper.text()).toContain('required check deployment concluded failure')
    expect(wrapper.text()).toContain('version.retryPreparation')
    wrapper.unmount()
  })

  it('prefers a newer server failure over an older local job and replaces it on retry', async () => {
    localStorage.setItem('sub2api-release-job-id', 'update-local-old')
    mocks.appStore.updateKind = 'official'
    mocks.appStore.officialUpdate = true
    mocks.appStore.customUpdate = false
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus
      .mockResolvedValueOnce({
        job_id: 'update-local-old',
        operation_kind: 'update',
        action: 'prepare',
        status: 'failed',
        message: 'old failure',
        stable_release_tag: 'v0.1.168',
        stable_release_commit: 'b'.repeat(40),
        production_changed: false,
        need_restart: false,
        updated_at: '2026-07-31T09:00:00Z'
      })
      .mockResolvedValueOnce({
        job_id: 'update-server-new',
        operation_kind: 'update',
        action: 'prepare',
        status: 'failed',
        message: 'new target failure',
        stable_release_tag: 'v0.1.169',
        stable_release_commit: 'c'.repeat(40),
        production_changed: false,
        need_restart: false,
        updated_at: '2026-07-31T10:00:00Z'
      })
    mocks.prepareUpdate.mockResolvedValue({
      job_id: 'update-retry-new',
      operation_kind: 'update',
      action: 'prepare',
      status: 'resolving_target',
      message: 'retry queued',
      need_restart: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.168' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="custom-release-badge"]').trigger('click')

    expect(wrapper.text()).toContain('new target failure')
    expect(wrapper.text()).not.toContain('old failure')
    expect(localStorage.getItem('sub2api-release-job-id')).toBe('update-server-new')
    const retry = wrapper
      .findAll('button')
      .find((button) => button.text().includes('version.retryPreparation'))
    expect(retry).toBeDefined()
    await retry!.trigger('click')
    await flushPromises()

    expect(mocks.prepareUpdate).toHaveBeenCalled()
    expect(localStorage.getItem('sub2api-release-job-id')).toBe('update-retry-new')
    wrapper.unmount()
  })

  it('does not restore a terminal failure for a replaced target', async () => {
    localStorage.setItem('sub2api-release-job-id', 'update-stale-target')
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-stale-target',
      operation_kind: 'update',
      action: 'prepare',
      status: 'failed',
      message: 'old target failure',
      stable_release_tag: 'v0.1.168',
      stable_release_commit: 'b'.repeat(40),
      production_changed: false,
      need_restart: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.168' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="custom-release-badge"]').trigger('click')

    expect(wrapper.text()).not.toContain('old target failure')
    expect(localStorage.getItem('sub2api-release-job-id')).toBeNull()
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
    mocks.appStore.updateFingerprint = 'c'.repeat(64)
    mocks.appStore.noticeUnread = true
    await nextTick()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(mocks.applyUpdate).toHaveBeenCalledWith('update-prepared')
    expect(mocks.appStore.markCurrentNoticeRead).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows docs-only detection without a production update action', async () => {
    mocks.appStore.updateKind = 'docs-only'
    mocks.appStore.hasUpdate = true
    mocks.appStore.runtimeUpdate = false
    mocks.appStore.officialUpdate = false
    mocks.appStore.customUpdate = true

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

  it('acknowledges a docs-only notice without hiding its content', async () => {
    mocks.appStore.updateKind = 'docs-only'
    mocks.appStore.hasUpdate = true
    mocks.appStore.noticeUnread = true
    mocks.appStore.runtimeUpdate = false
    mocks.appStore.officialUpdate = false
    mocks.appStore.customUpdate = true

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.158' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    const badge = wrapper.get('[data-testid="custom-release-badge"]')
    expect(badge.classes()).toContain('bg-amber-100')

    await badge.trigger('click')
    await flushPromises()

    expect(mocks.appStore.markCurrentNoticeRead).toHaveBeenCalledTimes(1)
    expect(mocks.appStore.noticeUnread).toBe(false)
    expect(mocks.appStore.runtimeUpdate).toBe(false)
    await nextTick()
    expect(badge.classes()).not.toContain('bg-amber-100')
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
    mocks.prepareUpdate.mockResolvedValue({
      job_id: 'update-custom',
      status: 'checking_release',
      message: 'Release job queued',
      need_restart: false
    })
    mocks.getUpdateStatus
      .mockRejectedValueOnce({ response: { status: 404 } })
      .mockResolvedValue({
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

  it('uses the paired release panel and prepares the selected rollback snapshot', async () => {
    const target = {
      release_id: 'release-target',
      official_version: 'v0.1.162',
      official_commit: 'cccccccccccccccccccccccccccccccccccccccc',
      custom_version: 'v1.0.3',
      custom_version_sequence: 3,
      custom_commit: 'dddddddddddddddddddddddddddddddddddddddd',
      main_digest: `sha256:${'3'.repeat(64)}`,
      extensions_digest: `sha256:${'4'.repeat(64)}`,
      published_at: '2026-07-22T00:00:00Z'
    }
    mocks.getRollbackReleases.mockResolvedValue([target])
    mocks.prepareRollback.mockResolvedValue({
      job_id: 'rollback-prepare',
      operation_kind: 'rollback',
      action: 'prepare',
      status: 'resolving_snapshot',
      message: 'queued',
      need_restart: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')
    await wrapper.find('[data-testid="rollback-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="rollback-panel"]').exists()).toBe(true)
    await wrapper.find('[data-testid="rollback-panel"] button').trigger('click')
    mocks.appStore.updateFingerprint = 'b'.repeat(64)
    mocks.appStore.noticeUnread = true
    await nextTick()
    await wrapper.find('[data-testid="prepare-rollback"]').trigger('click')
    await flushPromises()

    expect(mocks.prepareRollback).toHaveBeenCalledWith('release-target')
    expect(mocks.appStore.markCurrentNoticeRead).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="confirm-rollback"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('restores a server-side prepared rollback and applies only after confirmation', async () => {
    const target = {
      release_id: 'release-target',
      official_version: 'v0.1.162',
      official_commit: 'c'.repeat(40),
      custom_version: 'v1.0.3',
      custom_version_sequence: 3,
      custom_commit: 'd'.repeat(40),
      published_at: '2026-07-22T00:00:00Z'
    }
    mocks.getRollbackReleases.mockResolvedValue([target])
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'rollback-prepared',
      operation_kind: 'rollback',
      action: 'prepare',
      status: 'prepared',
      message: 'prepared',
      need_restart: false,
      expires_at: new Date(Date.now() + 15 * 60 * 1000).toISOString()
    })
    mocks.applyRollback.mockResolvedValue({
      job_id: 'rollback-prepared',
      operation_kind: 'rollback',
      action: 'apply',
      status: 'apply_queued',
      message: 'queued',
      need_restart: false
    })

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')
    await wrapper.find('[data-testid="rollback-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="confirm-rollback"]').exists()).toBe(true)
    expect(mocks.applyUpdate).not.toHaveBeenCalled()
    mocks.appStore.updateFingerprint = 'a'.repeat(64)
    mocks.appStore.noticeUnread = true
    await nextTick()
    await wrapper.find('[data-testid="confirm-rollback"]').trigger('click')
    await flushPromises()

    expect(mocks.applyRollback).toHaveBeenCalledWith('rollback-prepared')
    expect(mocks.appStore.markCurrentNoticeRead).not.toHaveBeenCalled()
    expect(mocks.applyUpdate).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('settles local rollback expiry through the server refusal path and reloads identity', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-30T00:00:00Z'))
    const target = {
      release_id: 'release-target',
      official_version: 'v0.1.162',
      official_commit: 'c'.repeat(40),
      custom_version: 'v1.0.3',
      custom_version_sequence: 3,
      custom_commit: 'd'.repeat(40),
      published_at: '2026-07-22T00:00:00Z'
    }
    const prepared = {
      job_id: 'rollback-expiring',
      operation_kind: 'rollback',
      action: 'prepare',
      status: 'prepared',
      message: 'prepared',
      need_restart: false,
      target_release_id: target.release_id,
      expires_at: '2026-07-30T00:00:02Z'
    }
    mocks.getRollbackReleases.mockResolvedValue([target])
    mocks.getUpdateStatus.mockReset()
    mocks.getUpdateStatus
      .mockResolvedValueOnce(prepared)
      .mockResolvedValue({
        ...prepared,
        status: 'expired',
        message: 'prepared rollback expired'
      })
    mocks.applyRollback.mockRejectedValue(new Error('prepared rollback expired'))

    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.164' },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()
    await wrapper.find('button[title="version.updateAvailable"]').trigger('click')
    await wrapper.find('[data-testid="rollback-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="confirm-rollback"]').exists()).toBe(true)

    await vi.advanceTimersByTimeAsync(2_500)
    await flushPromises()

    expect(mocks.applyRollback).toHaveBeenCalledWith('rollback-expiring')
    expect(mocks.getUpdateStatus.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(mocks.appStore.fetchCurrentRelease).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="confirm-rollback"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="rollback-target-pair"]')).toHaveLength(1)
    wrapper.unmount()
  })
})
