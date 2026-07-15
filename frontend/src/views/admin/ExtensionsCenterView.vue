<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-[1600px] px-4 py-5 sm:px-6 lg:px-8" data-testid="extensions-center">
      <header class="border-b border-gray-200 pb-4 dark:border-dark-700">
        <h1 class="text-2xl font-semibold text-gray-950 dark:text-white">扩展中心</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">用户安全与运行质量</p>
      </header>
      <div class="min-w-0 pt-5">
        <RouterView />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
</script>
