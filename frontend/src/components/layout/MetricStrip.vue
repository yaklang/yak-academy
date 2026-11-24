<script setup lang="ts">
// KPI 条：统一卡片间距与断点，避免每个页面自己写 grid。
withDefaults(defineProps<{
  columns?: 2 | 3 | 4 | 5
}>(), {
  columns: 4,
})
</script>

<template>
  <div class="metric-strip" :data-columns="columns">
    <slot />
  </div>
</template>

<style scoped>
.metric-strip {
  display: grid;
  grid-template-columns: repeat(v-bind(columns), minmax(0, 1fr));
  gap: var(--space-4);
  margin-bottom: var(--space-6);
}

@media (max-width: 1024px) {
  .metric-strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .metric-strip {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
}
</style>
