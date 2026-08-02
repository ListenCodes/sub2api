import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  checkUpdates,
  getCurrentRelease,
  type VersionInfo
} from '@/features/custom-release/api'
import { useCustomReleaseStore } from '@/features/custom-release/store'

vi.mock('@/features/custom-release/api', () => ({
  checkUpdates: vi.fn(),
  getCurrentRelease: vi.fn()
}))

function versionInfo(overrides: Partial<VersionInfo> = {}): VersionInfo {
  return {
    current_version: '0.1.170',
    latest_version: '0.1.171',
    release_id: 'release-current',
    current_official_version: 'v0.1.170',
    current_custom_version: 'v1.0.14',
    has_update: true,
    cached: false,
    build_type: 'release',
    update_kind: 'official',
    official_update: true,
    custom_update: false,
    docs_only: false,
    runtime_update: true,
    detection_complete: true,
    ...overrides
  }
}

describe('useCustomReleaseStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('hydrates the current version pair from the update check', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(versionInfo())

    const store = useCustomReleaseStore()
    await store.fetchVersion(true)

    expect(store.currentVersion).toBe('0.1.170')
    expect(store.currentOfficialVersion).toBe('v0.1.170')
    expect(store.currentCustomVersion).toBe('v1.0.14')
    expect(store.currentReleaseID).toBe('release-current')
    expect(getCurrentRelease).not.toHaveBeenCalled()
  })

  it('invalidates a stale full release identity when the release id changes', async () => {
    vi.mocked(getCurrentRelease).mockResolvedValue({
      release_id: 'release-previous',
      official_version: 'v0.1.169',
      official_commit: 'a'.repeat(40),
      custom_version: 'v1.0.13',
      custom_version_sequence: 13,
      custom_commit: 'b'.repeat(40),
      published_at: '2026-07-31T00:00:00Z'
    })
    vi.mocked(checkUpdates).mockResolvedValue(versionInfo())
    const store = useCustomReleaseStore()

    await store.fetchCurrentRelease()
    expect(store.currentRelease?.release_id).toBe('release-previous')

    await store.fetchVersion(true)
    expect(store.currentRelease).toBeNull()
    expect(store.currentReleaseID).toBe('release-current')
  })
})
