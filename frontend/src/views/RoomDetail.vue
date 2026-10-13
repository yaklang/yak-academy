<script setup lang="ts">
// Room 详情页：纯教程页（文字 Markdown + 视频）。题目是独立资源，
// 本页不再内嵌 Lab——有关联题目时给"前往题目"入口跳题目详情页。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { topicApi, type Room } from '../api'

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
    ElMessage.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div v-if="room" class="page">
    <h2>{{ room.title }}</h2>
    <el-tag size="small">{{ room.difficulty }}</el-tag>

    <router-link
      v-if="room.related_challenge_slug"
      class="challenge-link"
      :to="`${trainingBase}/training/${room.related_challenge_slug}`"
    >前往配套题目练习 →</router-link>

    <div class="lesson" v-html="renderedLesson" />

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
.challenge-link { display: inline-block; margin: 0.8rem 0 0; color: var(--el-color-primary); text-decoration: none; font-weight: 600; }
.lesson { line-height: 1.7; margin: 1rem 0 2rem; }
.lesson :deep(pre) { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; overflow: auto; }
.lesson :deep(code) { background: var(--el-fill-color-light); padding: 0.1rem 0.3rem; border-radius: 3px; }
.video { width: 100%; max-width: 720px; border-radius: 4px; background: #000; }
.embed { aspect-ratio: 16/9; height: auto; border: 1px solid var(--el-border-color); }
</style>