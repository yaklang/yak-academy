<script setup lang="ts">
import NotificationHost from './components/NotificationHost.vue'
import ClickSpark from './components/ClickSpark.vue'
</script>

<template>
  <router-view />
  <NotificationHost />
  <ClickSpark />
</template>
