<template>
  <div class="relative">
    <!-- Admin: Full version badge with dropdown -->
    <template v-if="isAdmin">
      <button
        @click="toggleDropdown"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
        :class="[
          hasUpdate
            ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:hover:bg-amber-900/50'
            : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400 dark:hover:bg-dark-700'
        ]"
        :title="hasUpdate ? t('version.updateAvailable') : t('version.upToDate')"
      >
        <span v-if="currentVersion" class="font-medium">v{{ currentVersion }}</span>
        <span
          v-else
          class="h-3 w-12 animate-pulse rounded bg-gray-200 font-medium dark:bg-dark-600"
        ></span>
        <!-- Update indicator -->
        <span v-if="hasUpdate" class="relative flex h-2 w-2">
          <span
            class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
          ></span>
          <span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
        </span>
      </button>

      <!-- Dropdown -->
      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          ref="dropdownRef"
          class="absolute left-0 z-50 mt-2 overflow-hidden whitespace-normal rounded-xl border border-gray-200 bg-white shadow-lg transition-all duration-200 dark:border-dark-700 dark:bg-dark-800"
          :class="rollbackPanelOpen && isReleaseBuild ? 'w-80' : 'w-64'"
        >
          <!-- Header with refresh button -->
          <div
            class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"
          >
            <span class="text-sm font-medium text-gray-700 dark:text-dark-300">{{
              t('version.currentVersion')
            }}</span>
            <button
              @click="refreshVersion(true)"
              class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-dark-200"
              :disabled="loading || updating"
              :title="t('version.refresh')"
            >
              <Icon
                name="refresh"
                size="sm"
                :stroke-width="2"
                :class="{ 'animate-spin': loading }"
              />
            </button>
          </div>

          <div class="p-4">
            <!-- Loading state -->
            <div v-if="loading" class="flex items-center justify-center py-6">
              <svg class="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                ></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
            </div>

            <!-- Content -->
            <template v-else>
              <!-- Version display - centered and prominent -->
              <div class="mb-4 text-center">
                <div class="text-xs font-medium text-gray-500 dark:text-dark-400" data-testid="current-official-version">
                  Official {{ appStore.currentOfficialVersion || `v${currentVersion}` }}
                </div>
                <div class="text-xs font-medium text-gray-500 dark:text-dark-400" data-testid="current-custom-version">
                  Custom {{ appStore.currentCustomVersion || '--' }}
                </div>
                <div class="inline-flex items-center gap-2">
                  <span
                    v-if="currentVersion"
                    class="text-2xl font-bold text-gray-900 dark:text-white"
                    >v{{ currentVersion }}</span
                  >
                  <span v-else class="text-2xl font-bold text-gray-400 dark:text-dark-500">--</span>
                  <!-- Show check mark when up to date -->
                  <span
                    v-if="!hasUpdate"
                    class="flex h-5 w-5 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
                  >
                    <svg
                      class="h-3 w-3 text-green-600 dark:text-green-400"
                      fill="currentColor"
                      viewBox="0 0 20 20"
                    >
                      <path
                        fill-rule="evenodd"
                        d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                        clip-rule="evenodd"
                      />
                    </svg>
                  </span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{
                    hasUpdate
                      ? t('version.latestVersion') + ': v' + latestVersion
                      : t('version.upToDate')
                  }}
                </p>
                <p
                  v-if="targetOfficialVersion || targetCustomVersion"
                  class="mt-1 text-xs text-gray-500 dark:text-dark-400"
                  data-testid="target-version-pair"
                >
                  Official {{ targetOfficialVersion || '--' }} / Custom {{ targetCustomVersion || '--' }}
                </p>
              </div>

              <div
                v-if="!detectionComplete && updateWarning"
                class="mb-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-300"
              >
                {{ t('version.updateDetectionIncomplete') }}: {{ updateWarning }}
              </div>

              <!-- Priority 1: Durable release job progress -->
              <div v-if="preparedJobID" class="space-y-2">
                <div class="flex items-center gap-3 rounded-lg border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-800/50 dark:bg-emerald-900/20">
                  <Icon name="check" size="sm" :stroke-width="2" class="text-emerald-600 dark:text-emerald-400" />
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-emerald-700 dark:text-emerald-300">{{ t('version.prepared') }}</p>
                    <p class="break-words text-xs text-emerald-600/80 dark:text-emerald-400/80">
                      {{ preparedRemainingSeconds > 0 ? t('version.preparedRemaining', { seconds: preparedRemainingSeconds }) : t('version.preparedExpired') }}
                    </p>
                    <p v-if="customShortSHA" class="mt-1 text-[11px] text-emerald-600/70 dark:text-emerald-400/70">custom-release@{{ customShortSHA }}</p>
                  </div>
                </div>
                <button
                  @click="handleApply"
                  :disabled="applying || preparedRemainingSeconds <= 0"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Icon v-if="applying" name="refresh" size="sm" :stroke-width="2" class="animate-spin" />
                  <Icon v-else name="upload" size="sm" :stroke-width="2" />
                  {{ applying ? t('version.confirmingUpdate') : t('version.confirmUpdate') }}
                </button>
              </div>

              <div v-else-if="updating" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-blue-200 bg-blue-50 p-3 dark:border-blue-800/50 dark:bg-blue-900/20"
                >
                  <svg
                    class="h-5 w-5 flex-shrink-0 animate-spin text-blue-600 dark:text-blue-400"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-blue-700 dark:text-blue-300">
                      {{ t(`version.releaseState.${updateStage || 'checking_release'}`) }}
                    </p>
                    <p class="break-words text-xs text-blue-600/70 dark:text-blue-400/70">
                      {{ updateStageMessage }}
                    </p>
                  </div>
                </div>
              </div>

              <!-- Priority 2: Update error -->
              <div v-else-if="updateError" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800/50 dark:bg-red-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/50"
                  >
                    <Icon
                      name="x"
                      size="sm"
                      :stroke-width="2"
                      class="text-red-600 dark:text-red-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-red-700 dark:text-red-300">
                      {{ t('version.updateFailed') }}
                    </p>
                    <p class="truncate text-xs text-red-600/70 dark:text-red-400/70">
                      {{ updateError }}
                    </p>
                    <p
                      v-if="rollbackMessage"
                      class="mt-1 break-words text-xs text-red-600/70 dark:text-red-400/70"
                    >
                      {{ rollbackMessage }}
                    </p>
                    <div
                      v-if="conflictFiles.length"
                      class="mt-2 rounded-md border border-red-200 bg-white/70 p-2 text-xs text-red-700 dark:border-red-800/60 dark:bg-dark-900/30 dark:text-red-300"
                    >
                      <p class="font-medium">{{ t('version.updateConflict') }}</p>
                      <p class="mt-1">{{ t('version.updateConflictNoProductionChange') }}</p>
                      <p class="mt-2 font-medium">{{ t('version.updateConflictFiles') }}</p>
                      <ul class="mt-1 list-disc space-y-0.5 pl-4 break-words">
                        <li v-for="file in conflictFiles" :key="file">{{ file }}</li>
                      </ul>
                      <p v-if="conflictBase || conflictRelease || conflictUpstream" class="mt-2 break-all text-[11px] opacity-75">
                        {{ t('version.updateConflictCommits') }}:
                        {{ conflictBase.slice(0, 12) || '-' }} ->
                        {{ formatConflictTarget(conflictRelease, releaseTag, releaseCommit, conflictUpstream) }}
                      </p>
                      <p v-if="resolutionHint" class="mt-2">{{ resolutionHint }}</p>
                      <p v-if="conflictLog" class="mt-2 break-all text-[11px] opacity-75">
                        {{ t('version.updateConflictLog') }}: {{ conflictLog }}
                      </p>
                    </div>
                  </div>
                </div>

                <!-- Retry button -->
                <button
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {{ t('version.retry') }}
                </button>
              </div>

              <!-- Priority 3: Update/preparation success -->
              <div v-else-if="updateSuccess" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800/50 dark:bg-green-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/50"
                  >
                    <svg
                      class="h-4 w-4 text-green-600 dark:text-green-400"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-green-700 dark:text-green-300">
                      {{
                        successKind === 'rollback'
                          ? t('version.rollbackComplete')
                          : published
                            ? t('version.updatePublished')
                          : needRestart
                            ? t('version.updateComplete')
                            : t('version.updatePrepared')
                      }}
                    </p>
                    <p class="text-xs text-green-600/70 dark:text-green-400/70">
                      {{ needRestart ? t('version.restartRequired') : updateSuccessMessage }}
                    </p>
                    <p
                      v-if="releaseTag || (published && publishedCommit)"
                      class="mt-1 text-[11px] text-green-600/60 dark:text-green-400/60"
                    >
                      <template v-if="releaseTag">{{ releaseTag }}</template>
                      <template v-if="releaseTag && (releaseCommit || publishedCommit)"> · </template>
                      <template v-if="releaseCommit || publishedCommit">
                        commit {{ (releaseCommit || publishedCommit).slice(0, 12) }}
                      </template>
                      <template v-if="releasePublishedAt"> · {{ releasePublishedAt }}</template>
                    </p>
                  </div>
                </div>

                <!-- Restart button with countdown -->
                <button
                  v-if="needRestart"
                  @click="handleRestart"
                  :disabled="restarting"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-green-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-green-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg
                    v-if="restarting"
                    class="h-4 w-4 animate-spin"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <svg
                    v-else
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                    />
                  </svg>
                  <template v-if="restarting">
                    <span>{{ t('version.restarting') }}</span>
                    <span v-if="restartCountdown > 0" class="tabular-nums"
                      >({{ restartCountdown }}s)</span
                    >
                  </template>
                  <span v-else>{{ t('version.restartNow') }}</span>
                </button>
              </div>

              <!-- Priority 4: Update available for source/custom build - show sync button -->
              <div v-else-if="canPrepareUpdate && !isReleaseBuild" class="space-y-2">
                <div
                  class="flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800/50 dark:bg-amber-900/20"
                >
                  <div
                    class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                  >
                    <Icon
                      name="download"
                      size="sm"
                      :stroke-width="2"
                      class="text-amber-600 dark:text-amber-400"
                    />
                  </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      v{{ latestVersion }}
                    </p>
                  </div>
                </div>

                <!-- Sync button -->
                <button
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg v-if="updating" class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  <Icon v-else name="download" size="sm" :stroke-width="2" />
                  {{ updating ? t('version.updating') : t('version.updateNow') }}
                </button>

                <!-- Source build hint -->
                <div
                  class="flex items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 dark:border-blue-800/50 dark:bg-blue-900/20"
                >
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0 text-blue-500 dark:text-blue-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <p class="text-xs text-blue-600 dark:text-blue-400">
                    {{ t('version.sourceModeHint') }}
                  </p>
                </div>
              </div>

              <!-- Priority 5: Update available for release build - show update button -->
              <div v-else-if="canPrepareUpdate && isReleaseBuild" class="space-y-2">
                <!-- Update info card -->
                <div
                  class="flex items-center gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800/50 dark:bg-amber-900/20"
                >
                <div
                  class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/50"
                >
                  <Icon
                    name="download"
                    size="sm"
                    :stroke-width="2"
                    class="text-amber-600 dark:text-amber-400"
                  />
                </div>
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                      {{ t('version.updateAvailable') }}
                    </p>
                    <p class="text-xs text-amber-600/70 dark:text-amber-400/70">
                      v{{ latestVersion }}
                    </p>
                  </div>
                </div>

                <!-- Update button -->
                <button
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <svg v-if="updating" class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  <Icon v-else name="download" size="sm" :stroke-width="2" />
                  {{ updating ? t('version.updating') : t('version.updateNow') }}
                </button>

                <!-- View release link -->
                <a
                  v-if="releaseInfo?.html_url && releaseInfo.html_url !== '#'"
                  :href="releaseInfo.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-1 text-xs text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200"
                >
                  {{ t('version.viewChangelog') }}
                  <Icon name="externalLink" size="xs" :stroke-width="2" />
                </a>
              </div>

              <!-- Priority 6: No upstream version change; custom commits may still be pending -->
              <div v-else class="space-y-2">
                <div v-if="updateKind === 'docs-only'" class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-xs text-sky-700 dark:border-sky-800/50 dark:bg-sky-900/20 dark:text-sky-300">
                  {{ t('version.docsOnlyUpdate') }}
                </div>
                <button
                  v-else-if="hasUpdate"
                  @click="handleUpdate"
                  :disabled="updating"
                  class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Icon name="refresh" size="sm" :stroke-width="2" />
                  {{ t('version.updateNow') }}
                </button>
                <a
                  v-if="releaseInfo?.html_url && releaseInfo.html_url !== '#'"
                  :href="releaseInfo.html_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="flex items-center justify-center gap-2 py-2 text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('version.viewRelease') }}
                </a>

              </div>

              <!-- Version rollback remains available independently of update availability. -->
              <div class="mt-2 border-t border-gray-100 pt-2 dark:border-dark-700">
                <button
                  data-testid="rollback-toggle"
                  @click="toggleRollbackPanel"
                  class="group flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-xs text-gray-400 transition-colors hover:bg-gray-50 hover:text-gray-600 dark:text-dark-500 dark:hover:bg-dark-700/50 dark:hover:text-dark-300"
                >
                  <span class="flex items-center gap-1.5">
                    <Icon name="clock" size="xs" :stroke-width="2" />
                    {{ t('version.rollback') }}
                  </span>
                  <Icon
                    name="chevronDown"
                    size="xs"
                    :stroke-width="2"
                    class="transition-transform duration-200"
                    :class="{ 'rotate-180': rollbackPanelOpen }"
                  />
                </button>

                <transition name="rollback">
                  <div v-if="rollbackPanelOpen" class="mt-2">
                    <ReleaseRollbackPanel
                      v-if="isReleaseBuild && currentReleaseIdentity"
                      v-model:selected="selectedRollbackVersion"
                      :current="currentReleaseIdentity"
                      :releases="rollbackReleases"
                      :operation="rollbackOperation"
                      :loading="rollbackVersionsLoading || rollingBack"
                      :error="rollbackVersionsError || rollbackError"
                      @prepare="handlePrepareRollback"
                      @apply="handleApplyRollback"
                    />
                    <p
                      v-else-if="!isReleaseBuild"
                      class="rounded-lg border border-blue-200 bg-blue-50 p-2 text-xs text-blue-600 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-400"
                    >
                      {{ t('version.rollbackSourceHint') }}
                    </p>
                  </div>
                </transition>
              </div>
            </template>
          </div>
        </div>
      </transition>
    </template>

    <!-- Non-admin: Simple static version text -->
    <span v-else-if="version" class="text-xs text-gray-500 dark:text-dark-400">
      v{{ version }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores'
import { useCustomReleaseStore } from './store'
import ReleaseRollbackPanel from './ReleaseRollbackPanel.vue'
import {
  prepareUpdate,
  applyUpdate,
  getUpdateStatus,
  isTerminalUpdateStatus,
  isPollingSettledUpdateStatus,
  updateNeedsRestart,
  restartService,
  getRollbackReleases,
  prepareRollback,
  applyRollback,
  type ReleaseIdentity,
  type UpdateJob,
  type UpdateJobStatus,
  updateWasPublished
} from './api'
import Icon from '@/components/icons/Icon.vue'

const RELEASE_JOB_STORAGE_KEY = 'sub2api-release-job-id'
const UPDATE_POLL_INTERVAL_MS = 5000
const UPDATE_POLL_DEADLINE_MS = 90 * 60 * 1000

const customReleaseMessages = {
  en: {
    version: {
      sourceModeHint: 'Source build; download and back up changes, then wait for administrator confirmation',
      updateNow: 'Download update',
      updating: 'Preparing update...',
      confirmUpdate: 'Confirm update',
      confirmingUpdate: 'Switching production...',
      prepared: 'Prepared, waiting for confirmation',
      preparedRemaining: '{seconds}s remaining on prepared result',
      preparedExpired: 'Prepared result expired; download the update again',
      updateDetectionIncomplete: 'Update check incomplete; no production action is available',
      docsOnlyUpdate: 'Documentation changes found; no production switch job will be created',
      updatePrepared: 'Upstream integration branch prepared',
      updatePublished: 'Published to production after confirmation',
      updateConflict: 'Stable Release merge conflict; publishing stopped',
      updateConflictFiles: 'Conflicted files',
      updateConflictNoProductionChange: 'Production was not changed',
      updateConflictLog: 'Diagnostic artifact',
      updateConflictCommits: 'Merge base -> stable Release',
      releaseState: {
        resolving_target: 'Resolving release target',
        resolving_snapshot: 'Resolving rollback snapshot',
        verifying_snapshot: 'Verifying rollback snapshot',
        verifying_images: 'Verifying paired images',
        rendering_compose: 'Rendering Compose configuration',
        validating_manifest: 'Validating prepared manifest',
        switching_extensions: 'Switching extensions service',
        switching_main: 'Switching Sub2API service',
        checking_updates: 'Checking for updates',
        checking_release: 'Checking stable Release',
        validating_tag: 'Validating Release tag',
        merging_release: 'Merging stable Release',
        waiting_actions: 'Waiting for GitHub Actions',
        waiting_images: 'Waiting for paired images',
        downloading_images: 'Downloading images',
        preparing_compose: 'Preparing Compose configuration',
        validating_backup: 'Validating backup',
        promoting_release: 'Promoting custom-release',
        backing_up: 'Backing up production',
        prepared: 'Prepared, waiting for confirmation',
        apply_queued: 'Confirmed update queued',
        deploying_extensions: 'Deploying extensions',
        deploying_main: 'Deploying Sub2API',
        health_checking: 'Checking production health',
        rolling_back: 'Restoring previous images',
        success: 'Release completed',
        failed: 'Release failed',
        conflict: 'Release merge conflict',
        expired: 'Prepared result expired',
        drifted: 'Environment drifted; prepare again'
      }
    }
  },
  zh: {
    version: {
      sourceModeHint: '自定义构建，下载并备份后等待管理员确认切换',
      updateNow: '下载更新',
      updating: '正在准备更新...',
      confirmUpdate: '确认更新',
      confirmingUpdate: '正在切换生产环境...',
      prepared: '已准备，等待确认',
      preparedRemaining: '准备结果剩余 {seconds} 秒',
      preparedExpired: '准备结果已过期，请重新下载更新',
      updateDetectionIncomplete: '更新检测不完整，暂不提供生产操作',
      docsOnlyUpdate: '发现文档更新，不会创建生产切换任务',
      updatePrepared: '已准备上游集成分支',
      updatePublished: '确认后已切换到生产环境',
      updateConflict: '稳定 Release 合并存在冲突，已停止发布',
      updateConflictFiles: '冲突文件',
      updateConflictNoProductionChange: '生产环境未改变',
      updateConflictLog: '诊断资料',
      updateConflictCommits: '合并基线 -> 稳定 Release',
      releaseState: {
        resolving_target: '正在解析发布目标',
        resolving_snapshot: '正在解析回退快照',
        verifying_snapshot: '正在校验回退快照',
        verifying_images: '正在校验双镜像',
        rendering_compose: '正在渲染 Compose 配置',
        validating_manifest: '正在校验准备清单',
        switching_extensions: '正在切换扩展服务',
        switching_main: '正在切换 Sub2API 服务',
        checking_updates: '正在检查更新',
        checking_release: '正在检查稳定 Release',
        validating_tag: '正在校验 Release 标签',
        merging_release: '正在合并稳定 Release',
        waiting_actions: '正在等待 GitHub Actions',
        waiting_images: '正在等待双镜像',
        downloading_images: '正在下载镜像',
        preparing_compose: '正在准备 Compose 配置',
        validating_backup: '正在校验备份',
        promoting_release: '正在推进 custom-release',
        backing_up: '正在备份生产环境',
        prepared: '已准备，等待确认',
        apply_queued: '确认更新已排队',
        deploying_extensions: '正在部署扩展服务',
        deploying_main: '正在部署 Sub2API',
        health_checking: '正在检查生产健康状态',
        rolling_back: '正在恢复上一组镜像',
        success: '发布完成',
        failed: '发布失败',
        conflict: 'Release 合并冲突',
        expired: '准备结果已过期',
        drifted: '环境发生漂移，需要重新准备'
      }
    }
  }
}

const { t } = useI18n({ useScope: 'local', messages: customReleaseMessages })

const props = defineProps<{
  version?: string
}>()

const authStore = useAuthStore()
const appStore = useCustomReleaseStore()

const isAdmin = computed(() => authStore.isAdmin)

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

// Use store's cached version state
const loading = computed(() => appStore.versionLoading)
const currentVersion = computed(() => appStore.currentVersion || props.version || '')
const currentReleaseIdentity = computed<ReleaseIdentity | null>(() => appStore.currentRelease || null)
const latestVersion = computed(() => appStore.latestVersion)
const hasUpdate = computed(() => appStore.hasUpdate)
const releaseInfo = computed(() => appStore.releaseInfo)
const buildType = computed(() => appStore.buildType)
const updateKind = computed(() => appStore.updateKind || 'none')
const customShortSHA = computed(() => appStore.targetCustomShortSHA || '')
const targetOfficialVersion = computed(() => appStore.targetOfficialVersion || '')
const targetCustomVersion = computed(() => appStore.targetCustomVersion || '')
const detectionComplete = computed(() => appStore.detectionComplete)
const updateWarning = computed(() => appStore.updateWarning || '')
const canPrepareUpdate = computed(
  () =>
    detectionComplete.value &&
    (hasUpdate.value || updateKind.value === 'custom' || updateKind.value === 'combined') &&
    updateKind.value !== 'docs-only'
)

// Update process states (local to this component)
const updating = ref(false)
const restarting = ref(false)
const needRestart = ref(false)
const updateError = ref('')
const updateSuccess = ref(false)
const updateSuccessMessage = ref('')
const updateStage = ref<UpdateJobStatus | ''>('')
const updateStageMessage = ref('')
const published = ref(false)
const publishedCommit = ref('')
const releaseTag = ref('')
const releaseCommit = ref('')
const releasePublishedAt = ref('')
const conflictFiles = ref<string[]>([])
const conflictBase = ref('')
const conflictUpstream = ref('')
const conflictRelease = ref('')
const conflictLog = ref('')
const resolutionHint = ref('')
const rollbackMessage = ref('')
const restartCountdown = ref(0)
const updatePollTimer = ref<ReturnType<typeof setInterval> | null>(null)
const updatePollDeadlineTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const preparedCountdownTimer = ref<ReturnType<typeof setInterval> | null>(null)
let updatePollInFlight = false
// Distinguishes the success + restart panel between update and rollback flows
const successKind = ref<'update' | 'rollback'>('update')
const preparedJobID = ref('')
const preparedExpiresAt = ref('')
const preparedRemainingSeconds = ref(0)
const applying = ref(false)

// Rollback states
const rollbackPanelOpen = ref(false)
const rollbackReleases = ref<ReleaseIdentity[]>([])
const rollbackOperation = ref<UpdateJob | null>(null)
const rollbackVersionsLoading = ref(false)
const rollbackVersionsError = ref('')
const selectedRollbackVersion = ref('')
const preparedRollbackJobID = ref('')
const rollingBack = ref(false)
const rollbackError = ref('')

// Only show update check for release builds (binary/docker deployment)
const isReleaseBuild = computed(() => buildType.value === 'release')

function toggleDropdown() {
  if (!dropdownOpen.value) {
    resetTerminalUpdateFeedback()
  }
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

async function refreshVersion(force = true) {
  if (!isAdmin.value || updating.value) return

  // Reset update states when refreshing
  updateError.value = ''
  updateSuccess.value = false
  updateSuccessMessage.value = ''
  updateStage.value = ''
  updateStageMessage.value = ''
  published.value = false
  publishedCommit.value = ''
  releaseTag.value = ''
  releaseCommit.value = ''
  releasePublishedAt.value = ''
  conflictFiles.value = []
  conflictBase.value = ''
  conflictUpstream.value = ''
  conflictRelease.value = ''
  conflictLog.value = ''
  resolutionHint.value = ''
  rollbackMessage.value = ''
  needRestart.value = false
  preparedJobID.value = ''
  preparedExpiresAt.value = ''
  preparedRemainingSeconds.value = 0
  stopPreparedCountdown()
  stopUpdatePolling()
  resetRollbackState()

  await Promise.all([appStore.fetchVersion(force), appStore.fetchCurrentRelease?.()])
}

function resetTerminalUpdateFeedback() {
  if (!updateSuccess.value && !updateError.value && !(updateStage.value && isTerminalUpdateStatus(updateStage.value))) {
    return
  }

  updateSuccess.value = false
  updateSuccessMessage.value = ''
  updateError.value = ''
  updateStage.value = ''
  updateStageMessage.value = ''
  published.value = false
  publishedCommit.value = ''
  releaseTag.value = ''
  releaseCommit.value = ''
  releasePublishedAt.value = ''
  conflictFiles.value = []
  conflictBase.value = ''
  conflictUpstream.value = ''
  conflictRelease.value = ''
  conflictLog.value = ''
  resolutionHint.value = ''
  rollbackMessage.value = ''
  preparedJobID.value = ''
  preparedExpiresAt.value = ''
  preparedRemainingSeconds.value = 0
  stopPreparedCountdown()
  needRestart.value = false
  successKind.value = 'update'
}

async function handleUpdate() {
  if (updating.value) return

  updating.value = true
  updateError.value = ''
  updateSuccess.value = false
  updateSuccessMessage.value = ''
  updateStage.value = 'checking_updates'
  updateStageMessage.value = ''
  published.value = false
  publishedCommit.value = ''
  releaseTag.value = ''
  releaseCommit.value = ''
  releasePublishedAt.value = ''
  conflictFiles.value = []
  conflictBase.value = ''
  conflictUpstream.value = ''
  conflictRelease.value = ''
  conflictLog.value = ''
  resolutionHint.value = ''
  rollbackMessage.value = ''

  try {
    const job = await prepareUpdate()
    startUpdatePolling(job.job_id, job)
  } catch (error: unknown) {
    stopUpdatePolling()
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    updateError.value = err.response?.data?.message || err.message || t('version.updateFailed')
    updating.value = false
  }
}

async function handleApply() {
  if (applying.value || !preparedJobID.value) return

  applying.value = true
  updateError.value = ''
  try {
    const job = await applyUpdate(preparedJobID.value)
    preparedJobID.value = ''
    preparedExpiresAt.value = ''
    stopPreparedCountdown()
    startUpdatePolling(job.job_id, job)
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    updateError.value = err.response?.data?.message || err.message || t('version.updateFailed')
  } finally {
    applying.value = false
  }
}

function stopUpdatePolling() {
  if (updatePollTimer.value) {
    clearInterval(updatePollTimer.value)
    updatePollTimer.value = null
  }
  if (updatePollDeadlineTimer.value) {
    clearTimeout(updatePollDeadlineTimer.value)
    updatePollDeadlineTimer.value = null
  }
  updatePollInFlight = false
}

function stopPreparedCountdown() {
  if (preparedCountdownTimer.value) {
    clearInterval(preparedCountdownTimer.value)
    preparedCountdownTimer.value = null
  }
}

function updatePreparedCountdown() {
  const expiresAt = Date.parse(preparedExpiresAt.value)
  preparedRemainingSeconds.value = Number.isFinite(expiresAt)
    ? Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000))
    : 0
  if (preparedRemainingSeconds.value === 0) stopPreparedCountdown()
}

function finishPrepared(status: Pick<UpdateJob, 'job_id' | 'message' | 'expires_at'>) {
  stopUpdatePolling()
  preparedJobID.value = status.job_id
  preparedExpiresAt.value = status.expires_at || ''
  updatePreparedCountdown()
  stopPreparedCountdown()
  if (preparedRemainingSeconds.value > 0) {
    preparedCountdownTimer.value = setInterval(updatePreparedCountdown, 1000)
  }
  updateStage.value = 'prepared'
  updateStageMessage.value = status.message
  updateSuccess.value = false
  updateError.value = ''
  updating.value = false
}

async function finishUpdateSuccess(
  status: Pick<
    UpdateJob,
    | 'need_restart'
    | 'published'
    | 'published_commit'
    | 'message'
    | 'release_tag'
    | 'release_commit'
    | 'release_published_at'
  >
) {
  stopUpdatePolling()
  preparedJobID.value = ''
  preparedExpiresAt.value = ''
  preparedRemainingSeconds.value = 0
  stopPreparedCountdown()
  localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
  successKind.value = 'update'
  updateSuccess.value = true
  needRestart.value = updateNeedsRestart({ need_restart: status.need_restart })
  published.value = updateWasPublished(status)
  publishedCommit.value = status.published_commit || ''
  releaseTag.value = status.release_tag || ''
  releaseCommit.value = status.release_commit || ''
  releasePublishedAt.value = status.release_published_at || ''
  updateSuccessMessage.value = status.message
  updateStage.value = 'success'
  updateStageMessage.value = status.message
  updating.value = false
  appStore.clearVersionCache()
  await appStore.fetchCurrentRelease?.()
}

function finishUpdateFailure(
  status: Pick<
    UpdateJob,
    | 'message'
    | 'base_commit'
    | 'conflict_files'
    | 'conflict_base'
    | 'conflict_upstream'
    | 'conflict_release'
    | 'release_tag'
    | 'release_commit'
    | 'conflict_log'
    | 'resolution_hint'
    | 'rollback'
  >
) {
  stopUpdatePolling()
  preparedJobID.value = ''
  preparedExpiresAt.value = ''
  preparedRemainingSeconds.value = 0
  stopPreparedCountdown()
  localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
  updateError.value = status.message || t('version.updateFailed')
  conflictFiles.value = status.conflict_files || []
  conflictBase.value = status.conflict_base || status.base_commit || ''
  conflictUpstream.value = status.conflict_upstream || ''
  releaseTag.value = status.release_tag || ''
  releaseCommit.value = status.release_commit || ''
  conflictRelease.value = status.conflict_release || ''
  conflictLog.value = status.conflict_log || ''
  resolutionHint.value = status.resolution_hint || ''
  rollbackMessage.value = status.rollback?.attempted ? status.rollback.message : ''
  updateStage.value = 'failed'
  updateStageMessage.value = status.message
  updating.value = false
}

function finishRollbackPrepared(status: UpdateJob) {
  stopUpdatePolling()
  rollbackOperation.value = status
  preparedRollbackJobID.value = status.job_id
  rollingBack.value = false
  updating.value = false
  localStorage.setItem(RELEASE_JOB_STORAGE_KEY, status.job_id)
}

async function finishRollbackSuccess(status: UpdateJob) {
  stopUpdatePolling()
  localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
  rollbackOperation.value = status
  preparedRollbackJobID.value = ''
  rollingBack.value = false
  updating.value = false
  successKind.value = 'rollback'
  updateSuccess.value = true
  needRestart.value = updateNeedsRestart({ need_restart: status.need_restart })
  updateSuccessMessage.value = status.message
  rollbackPanelOpen.value = false
  appStore.clearVersionCache()
  await appStore.fetchCurrentRelease?.()
}

function finishRollbackFailure(status: UpdateJob) {
  stopUpdatePolling()
  localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
  rollbackOperation.value = status
  preparedRollbackJobID.value = ''
  rollingBack.value = false
  updating.value = false
  rollbackError.value = status.message || t('version.rollbackFailed')
  rollbackMessage.value = status.rollback?.attempted ? status.rollback.message : ''
}

async function pollUpdateStatus(jobID: string) {
  if (updatePollInFlight) return
  updatePollInFlight = true
  try {
    const status = await getUpdateStatus(jobID)
    updateStage.value = status.status
    updateStageMessage.value = status.message
    if (status.operation_kind === 'rollback') {
      rollbackOperation.value = status
      if (status.status === 'prepared') finishRollbackPrepared(status)
      else if (status.status === 'success') await finishRollbackSuccess(status)
      else if (isTerminalUpdateStatus(status.status)) finishRollbackFailure(status)
      return
    }
    if (status.status === 'prepared') {
      finishPrepared(status)
    } else if (status.status === 'success') {
      await finishUpdateSuccess(status)
    } else if (isTerminalUpdateStatus(status.status)) {
      finishUpdateFailure(status)
    }
  } catch {
    // Keep polling transient request failures until the long-running CI deadline.
  } finally {
    updatePollInFlight = false
  }
}

function startUpdatePolling(jobID: string, initial?: UpdateJob) {
  stopUpdatePolling()
  localStorage.setItem(RELEASE_JOB_STORAGE_KEY, jobID)
  updating.value = true
  if (initial) {
    updateStage.value = initial.status
    updateStageMessage.value = initial.message
  }
  if (initial?.status === 'prepared') {
    if (initial.operation_kind === 'rollback') finishRollbackPrepared(initial)
    else finishPrepared(initial)
    return
  }
  if (!initial || !isPollingSettledUpdateStatus(initial.status)) {
    void pollUpdateStatus(jobID)
  }
  updatePollTimer.value = setInterval(() => {
    void pollUpdateStatus(jobID)
  }, UPDATE_POLL_INTERVAL_MS)
  updatePollDeadlineTimer.value = setTimeout(() => {
    finishUpdateFailure({ message: `${t('version.updateFailed')}: status polling timed out` })
  }, UPDATE_POLL_DEADLINE_MS)
}

async function resumeUpdatePolling() {
  const storedJobID = localStorage.getItem(RELEASE_JOB_STORAGE_KEY) || undefined
  try {
    const status = await getUpdateStatus(storedJobID)
    if (isTerminalUpdateStatus(status.status)) {
      // The latest terminal job is historical information. Do not replay it in
      // the badge; only durable work that still needs attention is resumable.
      localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
      resetTerminalUpdateFeedback()
      await appStore.fetchVersion(true)
      return
    }
    startUpdatePolling(status.job_id, status)
  } catch (error: unknown) {
    const status = (error as { response?: { status?: number } }).response?.status
    if (storedJobID && status === 404) localStorage.removeItem(RELEASE_JOB_STORAGE_KEY)
  }
}

function resetRollbackState() {
  rollbackPanelOpen.value = false
  rollbackReleases.value = []
  rollbackOperation.value = null
  rollbackVersionsError.value = ''
  selectedRollbackVersion.value = ''
  rollbackError.value = ''
}

async function toggleRollbackPanel() {
  if (!isAdmin.value) return
  rollbackPanelOpen.value = !rollbackPanelOpen.value
  // Source builds only show a hint, no version list to fetch
  if (
    rollbackPanelOpen.value &&
    isReleaseBuild.value &&
    rollbackReleases.value.length === 0 &&
    !rollbackVersionsLoading.value
  ) {
    await loadRollbackVersions()
  }
}

async function loadRollbackVersions() {
  if (!isAdmin.value) return
  rollbackVersionsLoading.value = true
  rollbackVersionsError.value = ''
  try {
    rollbackReleases.value = await getRollbackReleases()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackVersionsError.value =
      err.response?.data?.message || err.message || t('version.loadVersionsFailed')
  } finally {
    rollbackVersionsLoading.value = false
  }
}

async function handlePrepareRollback(releaseID: string) {
  if (!isAdmin.value || rollingBack.value) return
  rollingBack.value = true
  rollbackError.value = ''
  try {
    const queued = await prepareRollback(releaseID)
    rollbackOperation.value = queued
    startUpdatePolling(queued.job_id, queued)
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackError.value = err.response?.data?.message || err.message || t('version.rollbackFailed')
    rollingBack.value = false
  }
}

async function handleApplyRollback(jobID: string) {
  if (!isAdmin.value || rollingBack.value) return
  rollingBack.value = true
  rollbackError.value = ''
  try {
    const queued = await applyRollback(jobID)
    rollbackOperation.value = queued
    preparedRollbackJobID.value = ''
    startUpdatePolling(queued.job_id, queued)
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } }; message?: string }
    rollbackError.value = err.response?.data?.message || err.message || t('version.rollbackFailed')
    rollingBack.value = false
  }
}

function formatConflictTarget(
  release: string,
  tag: string,
  commit: string,
  legacyUpstream: string
): string {
  if (release) {
    const separator = release.indexOf('@')
    if (separator >= 0) {
      return `${release.slice(0, separator)}@${release.slice(separator + 1, separator + 13)}`
    }
    return release
  }
  if (tag) {
    return commit ? `${tag}@${commit.slice(0, 12)}` : tag
  }
  return legacyUpstream.slice(0, 12) || '-'
}

async function handleRestart() {
  if (restarting.value) return

  restarting.value = true
  restartCountdown.value = 8

  try {
    await restartService()
    // Service will restart, page will reload automatically or show disconnected
  } catch (error) {
    // Expected - connection will be lost during restart
    console.log('Service restarting...')
  }

  // Start countdown
  const countdownInterval = setInterval(() => {
    restartCountdown.value--
    if (restartCountdown.value <= 0) {
      clearInterval(countdownInterval)
      // Try to check if service is back before reload
      checkServiceAndReload()
    }
  }, 1000)
}

async function checkServiceAndReload() {
  const maxRetries = 5
  const retryDelay = 1000

  for (let i = 0; i < maxRetries; i++) {
    try {
      const response = await fetch('/health', {
        method: 'GET',
        cache: 'no-cache'
      })
      if (response.ok) {
        // Service is back, reload page
        window.location.reload()
        return
      }
    } catch {
      // Service not ready yet
    }

    if (i < maxRetries - 1) {
      await new Promise((resolve) => setTimeout(resolve, retryDelay))
    }
  }

  // After retries, reload anyway
  window.location.reload()
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as Node
  const button = (event.target as Element).closest('button')
  if (dropdownRef.value && !dropdownRef.value.contains(target) && !button?.contains(target)) {
    closeDropdown()
  }
}

onMounted(() => {
  if (isAdmin.value) {
    // Use cached version if available, otherwise fetch
    appStore.fetchVersion(false)
    void appStore.fetchCurrentRelease?.()
    void resumeUpdatePolling()
  }
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  stopUpdatePolling()
  stopPreparedCountdown()
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}

.rollback-enter-active,
.rollback-leave-active {
  transition: all 0.2s ease;
}

.rollback-enter-from,
.rollback-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.line-clamp-3 {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
