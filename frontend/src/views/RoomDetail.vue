<script setup lang="ts">
// 章节详情：页头 → 视频 → 流程 → 正文（Markdown）→ 关联题目入口。
// 阅读列最大 72ch，右侧目录由 LessonMarkdown 自己维护。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowRight } from '@lucide/vue'
import { topicApi, type Room } from '../api'
import RoomVideoPlayer from '../components/RoomVideoPlayer.vue'
import LessonFlow from '../components/LessonFlow.vue'
import LessonMarkdown from '../components/LessonMarkdown.vue'
import KnowledgePopover from '../components/KnowledgePopover.vue'
import PageHeader from '@/components/layout/PageHeader.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { Button } from '@/components/ui/button'

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
  <div v-if="loading" class="room-skeleton" role="status" aria-label="正在加载教程">
    <div class="sk-head" aria-hidden="true">
      <span class="sk-line sk-line--title" />
      <span class="sk-line sk-line--short" />
      <span class="sk-line sk-line--short" />
    </div>
    <div class="sk-reading" aria-hidden="true">
      <span class="sk-block" />
      <span v-for="n in 7" :key="n" class="sk-line" :class="{ 'sk-line--short': n === 3 || n === 7 }" />
    </div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="教程加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
  <div v-else-if="room" class="room-page">
    <PageHeader :title="room.title" :description="`教程 · 约 ${readingMinutes} 分钟`">
      <template #badges>
        <StatusTag kind="difficulty" :value="room.difficulty" />
      </template>
      <template v-if="room.related_challenge_slug" #actions>
        <Button variant="outline" as-child>
          <RouterLink :to="`${trainingBase}/training/${room.related_challenge_slug}`">
            前往配套题目练习
            <ArrowRight aria-hidden="true" />
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

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
.room-page {
  --lesson-reading-width: 780px;
  --lesson-toc-width: 220px;
  --lesson-gutter: var(--space-10);
  max-width: calc(var(--lesson-reading-width) + var(--lesson-toc-width) + var(--lesson-gutter, 40px));
  margin-inline: auto;
}

.room-skeleton {
  max-width: 780px;
}

.sk-head {
  display: grid;
  gap: var(--space-3);
  margin-bottom: var(--space-6);
}

.sk-reading {
  display: grid;
  gap: var(--space-3);
}

.sk-line {
  display: block;
  width: 100%;
  height: 12px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-line--short {
  width: 32%;
}

.sk-line--title {
  width: 48%;
  height: 24px;
}

.sk-block {
  display: block;
  height: 240px;
  margin-bottom: var(--space-4);
  border-radius: var(--radius-card);
  background: var(--bg-subtle);
}
</style>
