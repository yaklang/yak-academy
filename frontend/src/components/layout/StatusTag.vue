<script setup lang="ts">
// 状态标签：颜色 + 文字双通道，颜色只强化语义，不单独承担信息。
import { computed } from 'vue'
import { statusMeta, type StatusKind } from '@/lib/labels'

const props = withDefaults(defineProps<{
  kind: StatusKind
  value?: string | null
  label?: string
  dot?: boolean
}>(), {
  value: undefined,
  label: undefined,
  dot: false,
})

const meta = computed(() => statusMeta(props.kind, props.value))
const text = computed(() => props.label ?? meta.value.label)
</script>

<template>
  <span class="status-tag" :data-tone="meta.tone">
    <span v-if="dot" class="status-tag__dot" aria-hidden="true" />
    {{ text }}
  </span>
</template>

<style scoped>
.status-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid transparent;
  border-radius: var(--radius-control);
  font-size: var(--text-meta);
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
}

.status-tag__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.status-tag[data-tone='neutral'] {
  border-color: var(--border);
  background: var(--bg-subtle);
  color: var(--text-secondary);
}

.status-tag[data-tone='primary'] {
  border-color: color-mix(in srgb, var(--action-primary) 24%, transparent);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

.status-tag[data-tone='success'] {
  border-color: color-mix(in srgb, var(--status-success) 24%, transparent);
  background: color-mix(in srgb, var(--status-success) 10%, var(--bg-surface));
  color: var(--status-success);
}

.status-tag[data-tone='warning'] {
  border-color: color-mix(in srgb, var(--status-warning) 28%, transparent);
  background: color-mix(in srgb, var(--status-warning) 12%, var(--bg-surface));
  color: var(--status-warning);
}

.status-tag[data-tone='danger'] {
  border-color: color-mix(in srgb, var(--status-danger) 24%, transparent);
  background: color-mix(in srgb, var(--status-danger) 10%, var(--bg-surface));
  color: var(--status-danger);
}

.status-tag[data-tone='info'] {
  border-color: color-mix(in srgb, var(--status-info) 24%, transparent);
  background: color-mix(in srgb, var(--status-info) 10%, var(--bg-surface));
  color: var(--status-info);
}
</style>
