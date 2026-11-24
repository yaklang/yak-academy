<script setup lang="ts">
// 列表页唯一容器：标题/计数、说明、工具栏、表格与分页在同一个面板里，
// 页面不再“卡片里再套表格卡片”。表格自身负责横向滚动。
withDefaults(defineProps<{
  title?: string
  description?: string
  count?: number
}>(), {
  title: undefined,
  description: undefined,
  count: undefined,
})
</script>

<template>
  <section class="data-table-panel">
    <header v-if="title || description || $slots.toolbar" class="data-table-panel__head">
      <div class="data-table-panel__heading">
        <div v-if="title" class="data-table-panel__title-row">
          <h2 class="data-table-panel__title">{{ title }}</h2>
          <span v-if="count !== undefined" class="data-table-panel__count">{{ count }}</span>
        </div>
        <p v-if="description" class="data-table-panel__description">{{ description }}</p>
      </div>
      <div v-if="$slots.toolbar" class="data-table-panel__toolbar">
        <slot name="toolbar" />
      </div>
    </header>
    <div class="data-table-panel__body">
      <slot />
    </div>
    <footer v-if="$slots.footer && count !== 0" class="data-table-panel__footer">
      <slot name="footer" />
    </footer>
  </section>
</template>

<style scoped>
.data-table-panel {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
  overflow: hidden;
}

.data-table-panel__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.data-table-panel__heading {
  min-width: 0;
  flex: 1 1 240px;
}

.data-table-panel__title-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.data-table-panel__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  line-height: var(--leading-normal);
}

.data-table-panel__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 20px;
  padding-inline: var(--space-2);
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.data-table-panel__description {
  margin-top: var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.data-table-panel__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  flex-shrink: 0;
}

.data-table-panel__body {
  min-width: 0;
}

/* 表格自带 1px 外框；在面板内去掉重复描边，只保留横向滚动。 */
.data-table-panel__body :deep(.ui-data-table) {
  border: 0;
  border-radius: 0;
}

.data-table-panel__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-2) var(--space-4);
  border-top: 1px solid var(--border);
}

@media (max-width: 720px) {
  .data-table-panel__head {
    align-items: stretch;
  }

  .data-table-panel__toolbar {
    width: 100%;
  }
}
</style>
