<script setup lang="ts">
// 教程流程：lesson.md front matter flow 配置的节点序列（2-12 步）。
// 小屏自然换行，不引入横向滚动或动画。
import { Workflow } from '@lucide/vue'

defineProps<{ steps: string[] }>()

const pad = (n: number) => String(n).padStart(2, '0')
</script>

<template>
  <section class="lesson-flow" aria-labelledby="lesson-flow-title">
    <header class="flow-head">
      <span class="flow-title">
        <Workflow :size="15" aria-hidden="true" />
        <span id="lesson-flow-title">教程流程</span>
      </span>
      <span class="flow-hint">按节点推进，遇到知识链接可点开查看</span>
    </header>
    <ol class="flow-track">
      <li v-for="(step, i) in steps" :key="`${i}-${step}`" class="flow-item">
        <span class="flow-pill">
          <span class="flow-index mono">{{ pad(i + 1) }}</span>
          <span class="flow-label">{{ step }}</span>
        </span>
        <span v-if="i < steps.length - 1" class="flow-arrow" aria-hidden="true">→</span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.lesson-flow {
  max-width: min(
    var(--lesson-reading-width, 780px),
    calc(100% - var(--lesson-toc-width, 220px) - var(--lesson-gutter, var(--space-10)))
  );
  margin-bottom: var(--space-6);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
}
.flow-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-bottom: var(--space-3);
}
.flow-title {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--foreground);
  font-size: var(--text-sm);
  font-weight: 600;
}
.flow-title svg { color: var(--accent-foreground); }
.flow-hint { color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-relaxed); }
.flow-track {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.flow-item { display: flex; align-items: center; gap: var(--space-2); }
.flow-pill {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--muted);
  color: var(--foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-normal);
}
.flow-index { color: var(--accent-foreground); font-size: var(--text-xs); font-weight: 600; }
.flow-arrow { color: var(--muted-foreground); font-size: var(--text-xs); }

@media (max-width: 1024px) {
  .lesson-flow { max-width: 100%; }
}
@media (max-width: 640px) {
  .flow-track { flex-direction: column; align-items: stretch; }
  .flow-pill { justify-content: flex-start; border-radius: var(--radius-control); }
  .flow-arrow { display: none; }
}
</style>
