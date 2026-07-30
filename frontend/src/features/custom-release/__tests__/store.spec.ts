import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useCustomReleaseStore } from '@/features/custom-release/store'

const mocks = vi.hoisted(() => ({
  checkUpdates: vi.fn(),
  getCurrentRelease: vi.fn(),
  markCustomReleaseRead: vi.fn()
}))

vi.mock('@/features/custom-release/api', () => ({
  checkUpdates: mocks.checkUpdates,
  getCurrentRelease: mocks.getCurrentRelease,
  markCustomReleaseRead: mocks.markCustomReleaseRead
}))

function versionFixture(fingerprint: string, noticeUnread: boolean) {
  return {
    current_version: '0.1.168',
    latest_version: '0.1.169',
    has_update: true,
    cached: false,
    build_type: 'release',
    update_kind: 'combined' as const,
    official_update: true,
    custom_update: true,
    docs_only: false,
    runtime_update: true,
    detection_complete: true,
    production_commit: 'a'.repeat(40),
    production_stable_tag: 'v0.1.168',
    production_stable_commit: 'b'.repeat(40),
    target_official_version: 'v0.1.169',
    target_official_commit: 'c'.repeat(40),
    target_custom_version: 'v1.0.7',
    target_custom_commit: 'd'.repeat(40),
    target_custom_short_sha: 'dddddddd',
    update_fingerprint: fingerprint,
    notice_unread: noticeUnread
  }
}

describe('custom release store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('keeps update availability independent from the acknowledged notice', async () => {
    const firstFingerprint = '1'.repeat(64)
    const nextFingerprint = '2'.repeat(64)
    mocks.checkUpdates
      .mockResolvedValueOnce(versionFixture(firstFingerprint, false))
      .mockResolvedValueOnce(versionFixture(nextFingerprint, true))
    const store = useCustomReleaseStore()

    await store.fetchVersion(true)
    expect(store.hasUpdate).toBe(true)
    expect(store.noticeUnread).toBe(false)
    expect(store.updateFingerprint).toBe(firstFingerprint)

    await store.fetchVersion(true)
    expect(store.hasUpdate).toBe(true)
    expect(store.noticeUnread).toBe(true)
    expect(store.updateFingerprint).toBe(nextFingerprint)
  })

  it('acknowledges optimistically and reconciles a persistence failure', async () => {
    const fingerprint = '3'.repeat(64)
    mocks.checkUpdates.mockResolvedValue(versionFixture(fingerprint, true))
    mocks.markCustomReleaseRead.mockRejectedValue(new Error('notice state unavailable'))
    const store = useCustomReleaseStore()
    await store.fetchVersion(true)

    await expect(store.markCurrentNoticeRead()).resolves.toBeUndefined()
    expect(store.noticeUnread).toBe(false)
    expect(store.hasUpdate).toBe(true)
    expect(mocks.markCustomReleaseRead).toHaveBeenCalledWith(fingerprint)

    await store.fetchVersion(true)
    expect(store.noticeUnread).toBe(true)
    expect(store.hasUpdate).toBe(true)
  })

  it('exposes current release failure and clears it after a successful retry', async () => {
    const identity = {
      release_id: 'release-current',
      official_version: 'v0.1.168',
      official_commit: 'a'.repeat(40),
      custom_version: 'v1.0.6',
      custom_version_sequence: 6,
      custom_commit: 'b'.repeat(40),
      published_at: '2026-07-30T00:00:00Z'
    }
    mocks.getCurrentRelease
      .mockRejectedValueOnce(new Error('current release unavailable'))
      .mockResolvedValueOnce(identity)
    const store = useCustomReleaseStore()

    await expect(store.fetchCurrentRelease()).resolves.toBeNull()
    expect(store.currentRelease).toBeNull()
    expect(store.currentReleaseLoading).toBe(false)
    expect(store.currentReleaseError).toContain('current release')

    await expect(store.fetchCurrentRelease()).resolves.toEqual(identity)
    expect(store.currentReleaseError).toBe('')
    expect(store.currentRelease).toEqual(identity)
    expect(store.currentReleaseID).toBe('release-current')
  })
})
