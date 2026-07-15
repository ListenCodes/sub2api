<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-[1600px] px-4 py-5 sm:px-6 lg:px-8" data-testid="extensions-center">
      <header class="border-b border-gray-200 pb-4 dark:border-dark-700">
        <h1 class="text-2xl font-semibold text-gray-950 dark:text-white">扩展中心</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">用户安全与运行质量</p>
        <nav class="mt-5 flex max-w-full gap-1 overflow-x-auto" aria-label="扩展中心">
          <RouterLink
            v-for="tab in tabs"
            :key="tab.path"
            :to="tab.path"
            class="shrink-0 border-b-2 px-4 py-2 text-sm font-medium transition-colors"
            :class="isActive(tab.prefix) ? 'border-primary-600 text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
          >
            {{ tab.label }}
          </RouterLink>
        </nav>
      </header>
      <div class="min-w-0 pt-5">
        <RouterView />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'

import AppLayout from '@/components/layout/AppLayout.vue'

const route = useRoute()
const tabs = [
  { label: '用户风控', path: '/admin/extensions/user-risk/users', prefix: '/admin/extensions/user-risk' },
  { label: '账号监控', path: '/admin/extensions/account-monitor', prefix: '/admin/extensions/account-monitor' },
  { label: '分组监控', path: '/admin/extensions/group-monitor', prefix: '/admin/extensions/group-monitor' },
]

function isActive(prefix: string) {
  return route.path.startsWith(prefix)
}
</script>
