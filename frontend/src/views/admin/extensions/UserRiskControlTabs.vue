<template>
  <nav class="tabs max-w-full overflow-x-auto" :aria-label="t('admin.userRiskControl.tabsLabel')">
    <RouterLink
      v-for="tab in tabs"
      :key="tab.path"
      :to="tab.path"
      class="tab shrink-0"
      exact-active-class="tab-active"
    >
      {{ t(tab.labelKey) }}
    </RouterLink>
  </nav>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const tabs = [
  { labelKey: 'admin.userRiskControl.usersTab', path: '/admin/extensions/user-risk/users' },
  { labelKey: 'admin.userRiskControl.rulesTab', path: '/admin/extensions/user-risk/rules' },
  { labelKey: 'admin.userRiskControl.auditTab', path: '/admin/extensions/user-risk/audit' },
]
</script>
