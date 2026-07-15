<template>
  <AppLayout>
    <section
      class="grid w-full grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
      :style="{ minHeight: 'calc(100vh - var(--header-height, 4rem))' }"
    >
      <div class="flex h-12 items-center justify-end gap-1 border-b border-gray-200 px-3 dark:border-dark-700">
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
          :aria-label="t('admin.accountMonitor.reload')"
          :title="t('admin.accountMonitor.reload')"
          data-testid="account-monitor-reload"
          @click="reload"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
        </button>
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
          :aria-label="t('admin.accountMonitor.openInNewTab')"
          :title="t('admin.accountMonitor.openInNewTab')"
          data-testid="account-monitor-open"
          @click="openInNewTab"
        >
          <Icon name="externalLink" size="sm" />
        </button>
      </div>

      <div class="relative min-h-[32rem] bg-gray-50 dark:bg-dark-950">
        <iframe
          :key="frameKey"
          class="absolute inset-0 h-full w-full border-0"
          :src="monitorURL"
          :title="t('admin.accountMonitor.iframeTitle')"
          sandbox="allow-same-origin allow-scripts"
          referrerpolicy="same-origin"
          @load="handleLoad"
          @error="handleError"
        ></iframe>

        <div
          v-if="loading"
          class="absolute inset-0 flex items-center justify-center bg-white dark:bg-dark-900"
          role="status"
        >
          <div class="flex items-center gap-3 text-sm text-gray-500 dark:text-gray-400">
            <span class="h-5 w-5 animate-spin rounded-full border-2 border-gray-200 border-t-primary-600 dark:border-dark-600 dark:border-t-primary-400"></span>
            <span>{{ t('common.loading') }}</span>
          </div>
        </div>

        <div
          v-else-if="loadFailed"
          class="absolute inset-0 flex items-center justify-center bg-white px-6 dark:bg-dark-900"
          role="alert"
        >
          <div class="text-center">
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.accountMonitor.loadFailed') }}
            </p>
            <button type="button" class="btn btn-secondary mt-4 inline-flex items-center gap-2" @click="reload">
              <Icon name="refresh" size="sm" />
              {{ t('admin.accountMonitor.reload') }}
            </button>
          </div>
        </div>
      </div>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const monitorURL = '/api/v1/extensions-self/account-monitor/'
const { t } = useI18n()
const frameKey = ref(0)
const loading = ref(true)
const loadFailed = ref(false)

function handleLoad() {
  loading.value = false
  loadFailed.value = false
}

function handleError() {
  loading.value = false
  loadFailed.value = true
}

function reload() {
  loading.value = true
  loadFailed.value = false
  frameKey.value += 1
}

function openInNewTab() {
  window.open(monitorURL, '_blank', 'noopener,noreferrer')
}
</script>
