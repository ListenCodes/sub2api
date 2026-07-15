<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-[1600px] px-4 py-5 sm:px-6 lg:px-8" data-testid="extensions-center">
      <div class="min-w-0">
        <RouterView />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
</script>
