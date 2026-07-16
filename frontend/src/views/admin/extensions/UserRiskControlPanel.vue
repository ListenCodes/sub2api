<template>
  <section class="min-w-0" data-testid="user-risk-control-panel">
    <nav class="mb-4 flex max-w-full gap-2 overflow-x-auto" aria-label="用户风控">
      <RouterLink
        v-for="tab in tabs"
        :key="tab.path"
        :to="tab.path"
        class="shrink-0 rounded-md px-3 py-2 text-sm font-medium transition-colors"
        :class="route.path === tab.path ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-950' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-800'"
      >
        {{ tab.label }}
      </RouterLink>
    </nav>
    <RouterView />
  </section>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()
const tabs = [
  { label: '用户风险', path: '/admin/extensions/user-risk/users' },
  { label: '场景规则', path: '/admin/extensions/user-risk/rules' },
  { label: '操作审计', path: '/admin/extensions/user-risk/audit' },
]
</script>
