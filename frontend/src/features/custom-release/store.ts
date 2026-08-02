import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  checkUpdates as checkUpdatesAPI,
  type ReleaseInfo,
  type UpdateKind,
  type VersionInfo,
  getCurrentRelease,
  markCustomReleaseRead,
  type ReleaseIdentity
} from './api'

export const useCustomReleaseStore = defineStore('custom-release', () => {
  const versionLoaded = ref(false)
  const versionLoading = ref(false)
  const currentVersion = ref('')
  const latestVersion = ref('')
  const hasUpdate = ref(false)
  const buildType = ref('source')
  const releaseInfo = ref<ReleaseInfo | null>(null)
  const updateKind = ref<UpdateKind>('none')
  const officialUpdate = ref(false)
  const customUpdate = ref(false)
  const docsOnlyUpdate = ref(false)
  const runtimeUpdate = ref(false)
  const detectionComplete = ref(true)
  const productionCommit = ref('')
  const targetCustomCommit = ref('')
  const targetCustomShortSHA = ref('')
  const productionStableTag = ref('')
  const productionStableCommit = ref('')
  const updateWarning = ref('')
  const currentOfficialVersion = ref('')
  const currentCustomVersion = ref('')
  const currentReleaseID = ref('')
  const currentRelease = ref<ReleaseIdentity | null>(null)
  const targetOfficialVersion = ref('')
  const targetOfficialCommit = ref('')
  const targetCustomVersion = ref('')
  const updateFingerprint = ref('')
  const noticeUnread = ref(false)
  const noticeWarning = ref('')
  const currentReleaseLoading = ref(false)
  const currentReleaseError = ref('')

  async function fetchCurrentRelease(): Promise<ReleaseIdentity | null> {
    currentReleaseLoading.value = true
    currentReleaseError.value = ''
    try {
      const identity = await getCurrentRelease()
      currentRelease.value = identity
      currentOfficialVersion.value = identity.official_version
      currentCustomVersion.value = identity.custom_version
      currentReleaseID.value = identity.release_id
      currentVersion.value = identity.official_version.replace(/^v/, '')
      return identity
    } catch (error: unknown) {
      currentRelease.value = null
      currentOfficialVersion.value = ''
      currentCustomVersion.value = ''
      currentReleaseID.value = ''
      currentReleaseError.value = normalizeCustomReleaseError(error, 'Failed to load current release')
      return null
    } finally {
      currentReleaseLoading.value = false
    }
  }

  async function fetchVersion(force = false): Promise<VersionInfo | null> {
    if (versionLoaded.value && !force) {
      return {
        current_version: currentVersion.value,
        latest_version: latestVersion.value,
        release_id: currentReleaseID.value || undefined,
        current_official_version: currentOfficialVersion.value || undefined,
        current_custom_version: currentCustomVersion.value || undefined,
        has_update: hasUpdate.value,
        build_type: buildType.value,
        release_info: releaseInfo.value || undefined,
        update_kind: updateKind.value,
        official_update: officialUpdate.value,
        custom_update: customUpdate.value,
        docs_only: docsOnlyUpdate.value,
        runtime_update: runtimeUpdate.value,
        detection_complete: detectionComplete.value,
        production_commit: productionCommit.value || undefined,
        target_custom_commit: targetCustomCommit.value || undefined,
        target_custom_short_sha: targetCustomShortSHA.value || undefined,
        production_stable_tag: productionStableTag.value || undefined,
        production_stable_commit: productionStableCommit.value || undefined,
        target_official_version: targetOfficialVersion.value || undefined,
        target_official_commit: targetOfficialCommit.value || undefined,
        target_custom_version: targetCustomVersion.value || undefined,
        update_fingerprint: updateFingerprint.value || undefined,
        notice_unread: noticeUnread.value,
        notice_warning: noticeWarning.value || undefined,
        warning: updateWarning.value || undefined,
        cached: true
      }
    }
    if (versionLoading.value) return null

    versionLoading.value = true
    try {
      const data = await checkUpdatesAPI(force)
      currentVersion.value = data.current_version
      currentOfficialVersion.value = data.current_official_version || ''
      currentCustomVersion.value = data.current_custom_version || ''
      currentReleaseID.value = data.release_id || ''
      if (currentRelease.value?.release_id !== currentReleaseID.value) {
        currentRelease.value = null
      }
      latestVersion.value = data.latest_version
      hasUpdate.value = data.has_update
      buildType.value = data.build_type || 'source'
      releaseInfo.value = data.release_info || null
      updateKind.value = data.update_kind
      officialUpdate.value = data.official_update
      customUpdate.value = data.custom_update
      docsOnlyUpdate.value = data.docs_only
      runtimeUpdate.value = data.runtime_update
      detectionComplete.value = data.detection_complete
      productionCommit.value = data.production_commit || ''
      targetCustomCommit.value = data.target_custom_commit || ''
      targetCustomShortSHA.value = data.target_custom_short_sha || ''
      productionStableTag.value = data.production_stable_tag || ''
      productionStableCommit.value = data.production_stable_commit || ''
      updateWarning.value = data.warning || ''
      targetOfficialVersion.value = data.target_official_version || data.release_tag || data.latest_version || ''
      targetOfficialCommit.value = data.target_official_commit || ''
      targetCustomVersion.value = data.target_custom_version || ''
      updateFingerprint.value = data.update_fingerprint || ''
      noticeUnread.value = data.notice_unread === true
      noticeWarning.value = data.notice_warning || ''
      versionLoaded.value = true
      return data
    } catch (error) {
      console.error('Failed to fetch custom release version:', error)
      return null
    } finally {
      versionLoading.value = false
    }
  }

  async function markCurrentNoticeRead(): Promise<void> {
    const fingerprint = updateFingerprint.value
    if (updateKind.value !== 'docs-only' || !fingerprint || !noticeUnread.value) return
    noticeUnread.value = false
    try {
      await markCustomReleaseRead(fingerprint)
    } catch {
      // The next server response remains authoritative after an advisory write failure.
    }
  }

  function clearVersionCache(): void {
    versionLoaded.value = false
    hasUpdate.value = false
    updateKind.value = 'none'
    targetOfficialVersion.value = ''
    targetOfficialCommit.value = ''
    targetCustomVersion.value = ''
    updateFingerprint.value = ''
    noticeUnread.value = false
    noticeWarning.value = ''
  }

  return {
    versionLoaded,
    versionLoading,
    currentVersion,
    latestVersion,
    hasUpdate,
    buildType,
    releaseInfo,
    updateKind,
    officialUpdate,
    customUpdate,
    docsOnlyUpdate,
    runtimeUpdate,
    detectionComplete,
    productionCommit,
    targetCustomCommit,
    targetCustomShortSHA,
    productionStableTag,
    productionStableCommit,
    updateWarning,
    currentOfficialVersion,
    currentCustomVersion,
    currentReleaseID,
    currentRelease,
    targetOfficialVersion,
    targetOfficialCommit,
    targetCustomVersion,
    updateFingerprint,
    noticeUnread,
    noticeWarning,
    currentReleaseLoading,
    currentReleaseError,
    fetchCurrentRelease,
    fetchVersion,
    markCurrentNoticeRead,
    clearVersionCache
  }
})

function normalizeCustomReleaseError(error: unknown, fallback: string): string {
  const requestError = error as {
    response?: { data?: { message?: string } }
    message?: string
  }
  return requestError.response?.data?.message || requestError.message || fallback
}
