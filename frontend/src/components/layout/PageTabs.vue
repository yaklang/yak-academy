<script setup lang="ts">
// 页内分区切换：管理端分组与竞赛详情复用同一视觉，不再维护第二条 subnav。
import { RouterLink } from 'vue-router'

export interface PageTabItem {
  label: string
  value?: string | number
  to?: string
  count?: number
  disabled?: boolean
}

defineProps<{
  tabs: PageTabItem[]
  modelValue?: string | number
  ariaLabel?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>()
</script>

<template>
  <nav class="page-tabs" role="tablist" :aria-label="ariaLabel || '页面分区'">
    <template v-for="tab in tabs" :key="tab.value ?? tab.to ?? tab.label">
      <RouterLink
        v-if="tab.to"
        :to="tab.to"
        class="page-tabs__item"
        active-class="is-active"
        role="tab"
      >
        {{ tab.label }}
        <span v-if="tab.count !== undefined" class="page-tabs__count">{{ tab.count }}</span>
      </RouterLink>
      <button
        v-else
        type="button"
        class="page-tabs__item"
        role="tab"
        :class="{ 'is-active': tab.value === modelValue }"
        :disabled="tab.disabled"
        :aria-selected="tab.value === modelValue"
        :aria-current="tab.value === modelValue ? 'page' : undefined"
        @click="emit('update:modelValue', tab.value as string | number)"
      >
        {{ tab.label }}
        <span v-if="tab.count !== undefined" class="page-tabs__count">{{ tab.count }}</span>
      </button>
    </template>
  </nav>
</template>

<style scoped>
.page-tabs {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: none;
}

.page-tabs::-webkit-scrollbar {
  display: none;
}

.page-tabs__item {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  padding: var(--space-2) 0 var(--space-3);
  border-bottom: 2px solid transparent;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  font-weight: 500;
  white-space: nowrap;
}

.page-tabs__item:hover {
  color: var(--text-primary);
}

.page-tabs__item.is-active {
  color: var(--action-primary);
  border-bottom-color: var(--action-primary);
}

.page-tabs__item:disabled {
  color: var(--text-tertiary);
  cursor: not-allowed;
}

.page-tabs__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding-inline: 4px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-micro);
  font-variant-numeric: tabular-nums;
}

.page-tabs__item.is-active .page-tabs__count {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

@media (max-width: 390px) {
  .page-tabs {
    gap: var(--space-4);
  }

  .page-tabs__item {
    min-height: 44px;
    padding-block: var(--space-3);
  }
}
</style>
