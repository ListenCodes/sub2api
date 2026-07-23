import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  checkUpdates as checkUpdatesAPI,
  type ReleaseInfo,
  type UpdateKind,
  type VersionInfo
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

  async function fetchVersion(force = false): Promise<VersionInfo | null> {
    if (versionLoaded.value && !force) {
      return {
        current_version: currentVersion.value,
        latest_version: latestVersion.value,
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
        warning: updateWarning.value || undefined,
        cached: true
      }
    }
    if (versionLoading.value) return null

    versionLoading.value = true
    try {
      const data = await checkUpdatesAPI(force)
      currentVersion.value = data.current_version
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
      versionLoaded.value = true
      return data
    } catch (error) {
      console.error('Failed to fetch custom release version:', error)
      return null
    } finally {
      versionLoading.value = false
    }
  }

  function clearVersionCache(): void {
    versionLoaded.value = false
    hasUpdate.value = false
    updateKind.value = 'none'
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
    fetchVersion,
    clearVersionCache
  }
})
