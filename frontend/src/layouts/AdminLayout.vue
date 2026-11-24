<script setup lang="ts">
// 管理端布局：与学习端同一应用外壳，只切换管理导航。
import AppShell from '@/components/layout/AppShell.vue'
import { adminNav } from '@/components/layout/navigation'
</script>

<template>
  <AppShell
    :nav="adminNav"
    brand-to="/admin/tutorials"
    mode="admin"
    footer-note="YAK ACADEMY · 管理工作台"
  >
    <RouterView />
  </AppShell>
</template>
