<script setup lang="ts">
// 页面级标题：全站唯一的 h1 入口，标题、事实描述与主操作保持同一基线。
// 页面不再手写 .page-head，也不再出现第二个大标题。
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <header class="page-header">
    <div class="page-header__main">
      <div class="page-header__title-row">
        <slot name="title">
          <h1 class="page-header__title">{{ title }}</h1>
        </slot>
        <slot name="badges" />
      </div>
      <p v-if="description || $slots.description" class="page-header__description">
        <slot name="description">{{ description }}</slot>
      </p>
    </div>
    <div v-if="$slots.actions" class="page-header__actions">
      <slot name="actions" />
    </div>
  </header>
  <div v-if="$slots.tabs" class="page-header__tabs">
    <slot name="tabs" />
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-6);
  flex-wrap: wrap;
  margin-bottom: var(--space-6);
}

.page-header__main {
  flex: 1 1 480px;
  min-width: 0;
}

.page-header__title-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  min-width: 0;
}

.page-header__title {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--text-page);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.page-header__description {
  max-width: 680px;
  margin: var(--space-2) 0 0;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.page-header__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  flex-shrink: 0;
}

.page-header__tabs {
  margin-bottom: var(--space-4);
}

@media (max-width: 720px) {
  .page-header {
    align-items: flex-start;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }

  .page-header__actions {
    width: 100%;
  }
}
</style>
