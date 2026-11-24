<script setup lang="ts">
// 空态不是一句“暂无数据”：说明为什么空、下一步能做什么。
// tone 区分首次空、筛选空、错误与无权限；错误用 role="alert" 而不是伪装成空。
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { CircleAlert, Inbox, Lock, SearchX } from '@lucide/vue'
import { Button } from '@/components/ui/button'

type EmptyTone = 'empty' | 'filtered' | 'error' | 'forbidden'

const props = withDefaults(defineProps<{
  tone?: EmptyTone
  title?: string
  description?: string
  actionLabel?: string
  actionTo?: string
  compact?: boolean
}>(), {
  tone: 'empty',
  compact: false,
})

const emit = defineEmits<{ action: [] }>()

const icons = { empty: Inbox, filtered: SearchX, error: CircleAlert, forbidden: Lock }
const defaultTitles: Record<EmptyTone, string> = {
  empty: '暂时还没有内容',
  filtered: '没有匹配的结果',
  error: '加载失败',
  forbidden: '没有访问权限',
}

const icon = computed(() => icons[props.tone])
const title = computed(() => props.title ?? defaultTitles[props.tone])
</script>

<template>
  <div
    class="empty-state"
    :class="{ 'is-compact': compact }"
    :data-tone="tone"
    :role="tone === 'error' ? 'alert' : 'status'"
  >
    <span class="empty-state__icon" aria-hidden="true">
      <slot name="icon">
        <component :is="icon" />
      </slot>
    </span>
    <p class="empty-state__title">{{ title }}</p>
    <p v-if="description || $slots.description" class="empty-state__description">
      <slot name="description">{{ description }}</slot>
    </p>
    <div v-if="$slots.default || actionLabel" class="empty-state__actions">
      <slot>
        <Button
          v-if="actionTo"
          as-child
          variant="outline"
        >
          <RouterLink :to="actionTo">{{ actionLabel }}</RouterLink>
        </Button>
        <Button
          v-else
          variant="outline"
          @click="emit('action')"
        >
          {{ actionLabel }}
        </Button>
      </slot>
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: 220px;
  padding: var(--space-8) var(--space-5);
  text-align: center;
}

.empty-state.is-compact {
  min-height: 160px;
  padding: var(--space-6) var(--space-4);
}

.empty-state__icon {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  margin-bottom: var(--space-2);
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  color: var(--text-tertiary);
}

.empty-state__icon :deep(svg) {
  width: 20px;
  height: 20px;
}

.empty-state[data-tone='error'] .empty-state__icon {
  background: color-mix(in srgb, var(--status-danger) 10%, var(--bg-surface));
  color: var(--status-danger);
}

.empty-state[data-tone='forbidden'] .empty-state__icon {
  background: color-mix(in srgb, var(--status-warning) 12%, var(--bg-surface));
  color: var(--status-warning);
}

.empty-state__title {
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 600;
  line-height: var(--leading-normal);
}

.empty-state__description {
  max-width: 52ch;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.empty-state__actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  margin-top: var(--space-3);
  flex-wrap: wrap;
}
</style>
