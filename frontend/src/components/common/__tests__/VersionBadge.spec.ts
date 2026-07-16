import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
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
  getUpdateStatus: mocks.getUpdateStatus,
  getRollbackVersions: mocks.getRollbackVersions,
  rollback: mocks.rollback,
  restartService: mocks.restartService,
  updateNeedsRestart: (job: { need_restart: boolean }) => job.need_restart,
  updateWasPublished: (job: { published?: boolean }) => job.published === true
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: { value: false }, copyToClipboard: vi.fn() })
}))

describe('VersionBadge conflict reporting', () => {
  it('shows conflicted files and production safety state after a failed update', async () => {
    mocks.performUpdate.mockResolvedValue({ job_id: 'update-conflict' })
    mocks.getUpdateStatus.mockResolvedValue({
      job_id: 'update-conflict',
      status: 'failed',
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
})
