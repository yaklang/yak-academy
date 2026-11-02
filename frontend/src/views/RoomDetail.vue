<script setup lang="ts">
// Room 详情页：纯教程页（文字 Markdown + 视频）。题目是独立资源，
// 本页不再内嵌 Lab——有关联题目时给"前往题目"入口跳题目详情页。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { notify } from '@/lib/notifications'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { DIFFICULTY_LABELS, topicApi, type Room } from '../api'

const route = useRoute()
const room = ref<Room | null>(null)

const renderedLesson = computed(() =>
  DOMPurify.sanitize(marked.parse(room.value?.lesson_md ?? '') as string),
)

// 视频直链（mp4/webm）用 video 标签，其余 URL（B 站 embed 等）用 iframe。
const isDirectVideo = computed(() => /\.(mp4|webm)(\?|$)/i.test(room.value?.video_url ?? ''))

const trainingBase = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

async function load() {
  const { topicSlug, roomSlug } = route.params as { topicSlug: string; roomSlug: string }
  try {
    room.value = await topicApi.room(topicSlug, roomSlug)
  } catch (e) {
    notify.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div v-if="room" class="page">
    <header class="page-head">
      <div>
        <h2 class="page-title">{{ room.title }}</h2>
        <p class="page-sub">
          <UiTag size="small" effect="plain">{{ DIFFICULTY_LABELS[room.difficulty] }}</UiTag>
          <span class="lesson-kind">教程</span>
        </p>
      </div>
      <div v-if="room.related_challenge_slug" class="page-actions">
        <router-link
          class="challenge-link"
          :to="`${trainingBase}/training/${room.related_challenge_slug}`"
        >前往配套题目练习 <span aria-hidden="true">→</span></router-link>
      </div>
    </header>

    <article class="lesson" v-html="renderedLesson" />

    <video v-if="room.video_url && isDirectVideo" :src="room.video_url" controls class="video" />
    <iframe
      v-else-if="room.video_url"
      :src="room.video_url"
      class="video embed"
      allowfullscreen
    />
  </div>
</template>

<style scoped>
.page { max-width: 860px; }
.lesson-kind { margin-left: var(--space-2); color: var(--muted-foreground); font-size: var(--text-xs); }
.challenge-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-height: 36px;
  padding-inline: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 500;
}
.challenge-link:hover { background: var(--accent); }
.lesson {
  margin: var(--space-6) 0 var(--space-10);
  color: var(--foreground);
  font-size: var(--text-base);
  line-height: var(--leading-relaxed);
}
.lesson :deep(h1),
.lesson :deep(h2) {
  margin: var(--space-8) 0 var(--space-3);
  font-size: var(--text-xl);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
}
.lesson :deep(h3) {
  margin: var(--space-6) 0 var(--space-2);
  font-size: var(--text-lg);
  font-weight: 600;
  line-height: var(--leading-normal);
}
.lesson :deep(p) { margin: 0 0 var(--space-4); }
.lesson :deep(ul),
.lesson :deep(ol) { margin: 0 0 var(--space-4); padding-left: var(--space-6); }
.lesson :deep(li) { margin-bottom: var(--space-1); }
.lesson :deep(a) { color: var(--accent-foreground); text-decoration: underline; text-underline-offset: 3px; }
.lesson :deep(blockquote) {
  margin: var(--space-4) 0;
  padding-left: var(--space-4);
  border-left: 2px solid var(--border);
  color: var(--muted-foreground);
}
.lesson :deep(pre) {
  margin: var(--space-4) 0;
  padding: var(--space-4);
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--muted);
}
.lesson :deep(code) { font-family: var(--font-mono); font-size: 0.92em; }
.lesson :deep(p code) { padding: 2px 5px; border-radius: 4px; background: var(--muted); }
.lesson :deep(img) { border-radius: var(--radius-control); }
.video {
  width: 100%;
  max-width: 720px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: #000;
}
.embed { aspect-ratio: 16/9; height: auto; }
</style>