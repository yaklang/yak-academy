<script setup lang="ts">
// Room 详情页：纯教程页（文字 Markdown + 视频 + 流程 + 知识链接）。
// 题目是独立资源；有关联题目时给"前往题目"入口跳题目详情页。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { DIFFICULTY_LABELS, topicApi, type Room } from '../api'
import RoomVideoPlayer from '../components/RoomVideoPlayer.vue'
import LessonFlow from '../components/LessonFlow.vue'
import LessonMarkdown from '../components/LessonMarkdown.vue'
import KnowledgePopover from '../components/KnowledgePopover.vue'

const route = useRoute()
const room = ref<Room | null>(null)
const loading = ref(true)
const loadError = ref('')
const knowledgeOpen = ref(false)
const knowledgeId = ref('')
const knowledgeAnchor = ref({ x: 0, y: 0, linkLeft: 0, linkRight: 0 })

const videoSrc = computed(() => room.value?.video_url?.trim() ?? '')
const flowSteps = computed(() => room.value?.flow ?? [])
const readingMinutes = computed(() =>
  Math.max(1, Math.round((room.value?.lesson_md?.length ?? 0) / 500)),
)

const trainingBase = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

function openKnowledge(payload: {
  id: string
  x: number
  y: number
  linkLeft: number
  linkRight: number
}) {
  knowledgeId.value = payload.id
  knowledgeAnchor.value = {
    x: payload.x,
    y: payload.y,
    linkLeft: payload.linkLeft,
    linkRight: payload.linkRight,
  }
  knowledgeOpen.value = true
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const { topicSlug, roomSlug } = route.params as { topicSlug: string; roomSlug: string }
    room.value = await topicApi.room(topicSlug, roomSlug)
  } catch (e) {
    room.value = null
    loadError.value = e instanceof Error ? e.message : '教程加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <UiSkeleton v-if="loading" :rows="8" animated />
  <div v-else-if="loadError" class="load-error" role="alert">
    <p>{{ loadError }}</p>
    <UiButton size="small" @click="load">重试</UiButton>
  </div>
  <div v-else-if="room" class="page">
    <header class="page-head">
      <div>
        <h2 class="page-title">{{ room.title }}</h2>
        <p class="page-sub">
          <UiTag size="small" effect="plain">{{ DIFFICULTY_LABELS[room.difficulty] }}</UiTag>
          <span class="lesson-kind">教程 · 约 {{ readingMinutes }} 分钟</span>
        </p>
      </div>
      <div v-if="room.related_challenge_slug" class="page-actions">
        <router-link
          class="challenge-link"
          :to="`${trainingBase}/training/${room.related_challenge_slug}`"
        >前往配套题目练习 <span aria-hidden="true">→</span></router-link>
      </div>
    </header>

    <RoomVideoPlayer v-if="videoSrc" :src="videoSrc" />

    <LessonFlow v-if="flowSteps.length" :steps="flowSteps" />

    <LessonMarkdown
      :markdown="room.lesson_md ?? ''"
      :title="room.title"
      @knowledge="openKnowledge"
    />

    <KnowledgePopover
      v-model:open="knowledgeOpen"
      :id="knowledgeId"
      :anchor="knowledgeAnchor"
    />
  </div>
</template>

<style scoped>
.page {
  --lesson-reading-width: 780px;
  --lesson-toc-width: 220px;
  --lesson-gutter: var(--space-10);
  max-width: calc(var(--lesson-reading-width) + var(--lesson-toc-width) + var(--lesson-gutter));
  margin-inline: auto;
}
.page-head { max-width: var(--lesson-reading-width); }
.load-error {
  display: grid;
  justify-items: start;
  gap: var(--space-3);
  max-width: 860px;
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
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
</style>
