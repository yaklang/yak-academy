<script setup lang="ts">
// 知识链接弹窗：条目来自 lib/knowledge.ts（按官方文档整理），
// lesson.md 中的 #knowledge:<id> 链接由 RoomDetail 拦截后打开。
import { computed } from 'vue'
import { ExternalLink, ShieldCheck, Wrench } from '@lucide/vue'
import { findKnowledge, type KnowledgeKind } from '@/lib/knowledge'

const props = defineProps<{ modelValue: boolean; id: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const entry = computed(() => findKnowledge(props.id))
const kindLabel = computed(() => (entry.value?.kind === 'yakit' ? 'Yakit 知识' : '网络安全'))

const kindIcon = (kind: KnowledgeKind) => (kind === 'yakit' ? Wrench : ShieldCheck)

function onOpenChange(value: boolean) {
  emit('update:modelValue', value)
}
</script>

<template>
  <UiDialog
    :model-value="modelValue"
    :title="entry?.title ?? '知识链接'"
    width="720px"
    @update:model-value="onOpenChange"
  >
    <div v-if="entry" class="knowledge">
      <div class="knowledge-meta">
        <span class="knowledge-kind" :data-kind="entry.kind">
          <component :is="kindIcon(entry.kind)" :size="14" aria-hidden="true" />
          {{ kindLabel }}
        </span>
        <span class="knowledge-source">来源：{{ entry.source.label }}</span>
      </div>
      <p class="knowledge-summary">{{ entry.summary }}</p>
      <ul class="knowledge-points">
        <li v-for="point in entry.points" :key="point">{{ point }}</li>
      </ul>
      <a class="knowledge-origin" :href="entry.source.url" target="_blank" rel="noopener noreferrer">
        查看官网原文
        <ExternalLink :size="14" aria-hidden="true" />
      </a>
    </div>
    <div v-else class="knowledge-missing">
      <p>该知识条目暂未收录。</p>
      <a href="https://www.yaklang.com/products/intro/" target="_blank" rel="noopener noreferrer">
        前往 Yakit 官网手册
      </a>
    </div>
    <template #footer>
      <UiButton type="primary" @click="onOpenChange(false)">知道了</UiButton>
    </template>
  </UiDialog>
</template>

<style scoped>
.knowledge { display: grid; gap: var(--space-4); }
.knowledge-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.knowledge-kind {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
  border-radius: 999px;
  background: var(--accent);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
}
.knowledge-kind[data-kind='yakit'] {
  background: var(--muted);
  color: var(--foreground);
}
.knowledge-source { color: var(--muted-foreground); font-size: var(--text-xs); }
.knowledge-summary {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-md);
  line-height: var(--leading-relaxed);
}
.knowledge-points {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding-left: var(--space-5);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.knowledge-origin {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  justify-self: start;
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 500;
}
.knowledge-origin:hover { text-decoration: underline; text-underline-offset: 3px; }
.knowledge-missing { color: var(--muted-foreground); font-size: var(--text-sm); }
.knowledge-missing a { color: var(--accent-foreground); text-decoration: underline; text-underline-offset: 3px; }
</style>
