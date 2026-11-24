<script setup lang="ts">
// 列表页工具栏：搜索/筛选在左，主操作在右；窄屏换行而不是横向溢出。
withDefaults(defineProps<{
  ariaLabel?: string
}>(), {
  ariaLabel: '筛选条件',
})
</script>

<template>
  <form
    class="filter-bar"
    role="search"
    :aria-label="ariaLabel"
    @submit.prevent
  >
    <div class="filter-bar__fields">
      <slot />
    </div>
    <div v-if="$slots.actions" class="filter-bar__actions">
      <slot name="actions" />
    </div>
  </form>
</template>

<style scoped>
.filter-bar {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

.filter-bar__fields {
  display: flex;
  align-items: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
  min-width: 0;
  flex: 1 1 320px;
}

.filter-bar__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  flex-shrink: 0;
}

@media (max-width: 720px) {
  .filter-bar {
    align-items: stretch;
  }

  .filter-bar__fields,
  .filter-bar__actions {
    width: 100%;
  }

  .filter-bar__actions :deep([data-slot='button']) {
    min-height: 44px;
  }
}
</style>
