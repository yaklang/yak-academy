<script setup lang="ts">
// 统一 KPI 卡片：标签、数值、可选说明与图标。数值使用等宽数字，
// 同一行的卡片高度一致，不做整行渐变或彩色底。
import type { Component } from 'vue'

withDefaults(defineProps<{
  label: string
  value: string | number
  hint?: string
  suffix?: string
  icon?: Component
  valueTestId?: string
  tone?: 'neutral' | 'primary' | 'success' | 'warning' | 'danger'
}>(), {
  tone: 'neutral',
})
</script>

<template>
  <article class="stat-card" :data-tone="tone">
    <div class="stat-card__head">
      <span class="stat-card__label">{{ label }}</span>
      <component :is="icon" v-if="icon" class="stat-card__icon" aria-hidden="true" />
    </div>
    <p class="stat-card__value" :data-testid="valueTestId">
      {{ value }}<small v-if="suffix" class="stat-card__suffix">{{ suffix }}</small>
    </p>
    <p v-if="hint" class="stat-card__hint">{{ hint }}</p>
  </article>
</template>

<style scoped>
.stat-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.stat-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.stat-card__label {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-weight: 500;
  line-height: var(--leading-normal);
}

.stat-card__icon {
  width: 16px;
  height: 16px;
  color: var(--text-tertiary);
}

.stat-card__value {
  color: var(--text-primary);
  font-size: var(--text-metric);
  font-weight: 600;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

.stat-card__suffix {
  margin-left: var(--space-1);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  font-weight: 500;
}

.stat-card__hint {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.stat-card[data-tone='primary'] .stat-card__value {
  color: var(--action-primary);
}

.stat-card[data-tone='success'] .stat-card__value {
  color: var(--status-success);
}

.stat-card[data-tone='warning'] .stat-card__value {
  color: var(--status-warning);
}

.stat-card[data-tone='danger'] .stat-card__value {
  color: var(--status-danger);
}
</style>
