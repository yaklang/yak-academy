<script setup lang="ts">
// 学习端布局：应用外壳 + 学习导航。页面内容由路由渲染。
import AppShell from '@/components/layout/AppShell.vue'
import { studentNav } from '@/components/layout/navigation'
</script>

<template>
  <AppShell
    :nav="studentNav"
    brand-to="/learn"
    mode="student"
    footer-note="YAK ACADEMY · 学以致用，知行合一。"
  >
    <RouterView />
  </AppShell>
</template>
