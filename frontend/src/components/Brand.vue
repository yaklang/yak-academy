<script setup lang="ts">
withDefaults(defineProps<{ to?: string; compact?: boolean }>(), { to: '/', compact: false })
</script>

<template>
  <RouterLink :to="to" class="academy-brand" aria-label="Yak Academy 首页">
    <svg width="27" height="30" viewBox="0 0 27 30" fill="none" aria-hidden="true">
      <path d="M3 4L13.5 12.5L24 4M13.5 12.5V27M3 12L9 17M24 12L18 17" stroke="currentColor" stroke-width="2.5" stroke-linecap="square" />
    </svg>
    <span>YAK<span v-if="!compact" class="brand-academy"> ACADEMY</span></span>
  </RouterLink>
</template>
