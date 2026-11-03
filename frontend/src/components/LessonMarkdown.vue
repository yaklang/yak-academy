<script setup lang="ts">
// 教程正文渲染：Markdown → sanitize → 结构增强（目录 / 代码复制 / 知识链接）。
// 点击 #knowledge:<id> 链接不跳转，向上抛事件由页面打开知识弹窗。
import { computed } from 'vue'
import { buildLesson } from '@/lib/lesson'

const props = defineProps<{ markdown: string; title?: string }>()
const emit = defineEmits<{ knowledge: [id: string] }>()

const rendered = computed(() => buildLesson(props.markdown, props.title))

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 回退到 execCommand（非安全上下文 / 权限被拒时）
  }
  const area = document.createElement('textarea')
  area.value = text
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.focus()
  area.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  return ok
}

async function onClick(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target : null
  if (!target) return

  const copyButton = target.closest('button[data-copy]') as HTMLButtonElement | null
  if (copyButton) {
    event.preventDefault()
    const code = copyButton.closest('.code-shell')?.querySelector('pre code')
    if (!code) return
    const ok = await writeClipboard(code.textContent ?? '')
    copyButton.textContent = ok ? '已复制' : '复制失败'
    window.setTimeout(() => {
      copyButton.textContent = '复制'
    }, 1500)
    return
  }

  const link = target.closest('a[data-knowledge]') as HTMLAnchorElement | null
  if (link) {
    event.preventDefault()
    const id = link.dataset.knowledge
    if (id) emit('knowledge', id)
  }
}
</script>

<template>
  <details v-if="rendered.toc.length" class="toc-mobile">
    <summary>本页导航 · {{ rendered.toc.length }} 节</summary>
    <nav class="toc-list" aria-label="本页目录">
      <a
        v-for="item in rendered.toc"
        :key="item.id"
        :href="`#${item.id}`"
        :class="['toc-link', `level-${item.level}`]"
      >{{ item.text }}</a>
    </nav>
  </details>

  <div class="lesson-layout">
    <article class="lesson lesson-md" @click="onClick" v-html="rendered.html" />

    <aside v-if="rendered.toc.length" class="lesson-toc" aria-label="本页目录">
      <p class="toc-title">本页导航</p>
      <nav class="toc-list">
        <a
          v-for="item in rendered.toc"
          :key="item.id"
          :href="`#${item.id}`"
          :class="['toc-link', `level-${item.level}`]"
        >{{ item.text }}</a>
      </nav>
    </aside>
  </div>
</template>

<style scoped>
.lesson-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 220px;
  align-items: start;
  gap: var(--space-10);
}
.lesson-md {
  min-width: 0;
  color: var(--foreground);
  font-size: var(--text-md);
  line-height: var(--leading-relaxed);
}
.lesson-md :deep(h1) {
  margin: 0 0 var(--space-5);
  font-size: var(--text-2xl);
  font-weight: 650;
  line-height: var(--leading-tight);
  letter-spacing: 0;
}
.lesson-md :deep(h2) {
  margin: var(--space-10) 0 var(--space-3);
  font-size: var(--text-xl);
  font-weight: 650;
  line-height: var(--leading-tight);
  letter-spacing: 0;
}
.lesson-md :deep(h3) {
  margin: var(--space-8) 0 var(--space-2);
  font-size: var(--text-lg);
  font-weight: 600;
  line-height: var(--leading-normal);
  letter-spacing: 0;
}
.lesson-md :deep(h2:first-child),
.lesson-md :deep(h3:first-child),
.lesson-md :deep(p:first-child) { margin-top: 0; }
.lesson-md :deep(h2[id]),
.lesson-md :deep(h3[id]) { scroll-margin-top: calc(var(--space-16) + var(--space-6)); }
.lesson-md :deep(p) { margin: 0 0 var(--space-4); }
.lesson-md :deep(ul),
.lesson-md :deep(ol) {
  margin: 0 0 var(--space-5);
  padding-left: var(--space-6);
}
.lesson-md :deep(li) { margin-bottom: var(--space-2); }
.lesson-md :deep(li > ul),
.lesson-md :deep(li > ol) { margin-top: var(--space-2); margin-bottom: 0; }
.lesson-md :deep(strong) { color: var(--foreground); font-weight: 650; }
.lesson-md :deep(a) {
  color: var(--accent-foreground);
  text-decoration: underline;
  text-underline-offset: 3px;
}
.lesson-md :deep(a:hover) { text-decoration-thickness: 2px; }
.lesson-md :deep(.knowledge-link) {
  text-decoration-style: dotted;
  text-decoration-thickness: 2px;
  font-weight: 500;
}
.lesson-md :deep(.knowledge-mark) {
  display: inline-grid;
  width: var(--space-4);
  height: var(--space-4);
  margin-left: var(--space-1);
  place-items: center;
  border-radius: 999px;
  background: var(--accent);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 700;
  line-height: 1;
  vertical-align: baseline;
}
.lesson-md :deep(.external-link)::after {
  content: '↗';
  margin-left: 2px;
  font-size: var(--text-xs);
}
.lesson-md :deep(blockquote) {
  margin: var(--space-5) 0;
  padding: var(--space-4) var(--space-5);
  border-left: 3px solid var(--accent-foreground);
  border-radius: 0 var(--radius-control) var(--radius-control) 0;
  background: var(--accent);
  color: var(--foreground);
}
.lesson-md :deep(blockquote p:last-child) { margin-bottom: 0; }
.lesson-md :deep(hr) {
  margin: var(--space-8) 0;
  border: 0;
  border-top: 1px solid var(--border);
}
.lesson-md :deep(code) {
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.9em;
}
.lesson-md :deep(.code-shell) {
  margin: var(--space-5) 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--muted);
}
.lesson-md :deep(.code-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--card);
}
.lesson-md :deep(.code-lang) {
  color: var(--muted-foreground);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.lesson-md :deep(.code-copy) {
  min-height: var(--space-8);
  padding-inline: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--card);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
}
.lesson-md :deep(.code-copy:hover) {
  border-color: color-mix(in srgb, var(--primary) 45%, var(--border));
  color: var(--accent-foreground);
}
.lesson-md :deep(.code-copy:focus-visible) {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
.lesson-md :deep(.code-shell pre) {
  margin: 0;
  padding: var(--space-4);
  overflow: auto;
  border: 0;
  background: transparent;
}
.lesson-md :deep(.code-shell code) {
  padding: 0;
  background: transparent;
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.lesson-md :deep(.table-scroll) {
  margin: var(--space-5) 0;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
}
.lesson-md :deep(table) { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
.lesson-md :deep(th),
.lesson-md :deep(td) {
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
.lesson-md :deep(th) { background: var(--muted); font-weight: 600; }
.lesson-md :deep(tr:last-child td) { border-bottom: 0; }
.lesson-md :deep(img) {
  max-width: 100%;
  height: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
}
.lesson-md :deep(details) {
  margin: var(--space-5) 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
}
.lesson-md :deep(summary) {
  padding: var(--space-3) var(--space-4);
  color: var(--foreground);
  font-weight: 600;
  cursor: pointer;
}
.lesson-md :deep(summary:hover) { color: var(--accent-foreground); }
.lesson-md :deep(details[open] summary) { border-bottom: 1px solid var(--border); }
.lesson-md :deep(details > :not(summary)) { margin-inline: var(--space-4); }
.lesson-md :deep(details[open] > :not(summary):first-of-type) { margin-top: var(--space-4); }
.lesson-md :deep(details > :not(summary):last-child) { margin-bottom: var(--space-4); }

.lesson-toc {
  position: sticky;
  top: calc(var(--space-16) + var(--space-4));
  padding-left: var(--space-4);
  border-left: 1px solid var(--border);
}
.toc-title {
  margin: 0 0 var(--space-3);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 0.08em;
}
.toc-list { display: grid; gap: var(--space-2); }
.toc-link {
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-normal);
  text-decoration: none;
}
.toc-link:hover { color: var(--accent-foreground); }
.toc-link.level-3 { padding-left: var(--space-3); font-size: var(--text-xs); }
.toc-mobile {
  display: none;
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
}
.toc-mobile summary {
  color: var(--foreground);
  font-size: var(--text-sm);
  font-weight: 600;
  cursor: pointer;
}
.toc-mobile .toc-list { margin-top: var(--space-3); }

@media (max-width: 1024px) {
  .lesson-layout { grid-template-columns: minmax(0, 1fr); }
  .lesson-toc { display: none; }
  .toc-mobile { display: block; }
}
</style>
