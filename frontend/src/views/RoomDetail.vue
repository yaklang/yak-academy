<script setup lang="ts">
// Room 详情页：教程正文（Markdown 渲染 + sanitize）+ 内嵌 Lab 提交区（教题一体单元）。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { topicApi, submissionApi, type Room } from '../api'

const route = useRoute()
const room = ref<Room | null>(null)
const answer = ref('')
const submitting = ref(false)
const polling = ref(false)
const pollCount = ref(0)
const result = ref<{ status: string; score: number } | null>(null)

const renderedLesson = computed(() =>
  DOMPurify.sanitize(marked.parse(room.value?.lesson_md ?? '') as string),
)

async function load() {
  const { topicSlug, roomSlug } = route.params as { topicSlug: string; roomSlug: string }
  try {
    room.value = await topicApi.room(topicSlug, roomSlug)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

async function submit() {
  if (!room.value?.lab || !answer.value) return
  submitting.value = true
  result.value = null
  try {
    const sub = await submissionApi.submit(room.value.slug, answer.value)
    submitting.value = false
    pollSubmission(sub.id)
  } catch (e) {
    submitting.value = false
    ElMessage.error(`提交失败：${(e as Error).message}`)
  }
}

// 轮询判定结果：2s 间隔，上限 30 次（异步判题，event-sourcing-rules skill）。
async function pollSubmission(id: number) {
  polling.value = true
  pollCount.value = 0
  for (let i = 0; i < 30; i++) {
    await new Promise((r) => setTimeout(r, 2000))
    try {
      const sub = await submissionApi.get(id)
      if (sub.status !== 'pending') {
        result.value = sub
        polling.value = false
        if (sub.status === 'correct') {
          ElMessage.success(`回答正确！+${sub.score} 分`)
          load() // 刷新 solved 标记
        } else {
          ElMessage.error('回答错误，再试试')
        }
        return
      }
    } catch (e) {
      polling.value = false
      ElMessage.error((e as Error).message)
      return
    }
  }
  polling.value = false
  ElMessage.warning('判定超时，请稍后刷新查看')
}

onMounted(load)
</script>

<template>
  <div v-if="room">
    <h2>{{ room.title }}</h2>
    <el-tag size="small">{{ room.difficulty }}</el-tag>

    <div class="lesson" v-html="renderedLesson" />

    <el-card v-if="room.lab" class="lab">
      <template #header>
        <span>
          Lab：{{ room.lab.title }}
          <el-tag v-if="room.lab.solved" type="success" size="small">已通关</el-tag>
        </span>
      </template>
      <p class="score">分值：{{ room.lab.score }}</p>
      <div class="submit-row">
        <el-input
          v-model="answer"
          placeholder="输入 flag"
          :disabled="room.lab.solved || polling"
          @keyup.enter="submit"
        />
        <el-button
          type="primary"
          :loading="submitting || polling"
          :disabled="room.lab.solved || !answer"
          @click="submit"
        >提交</el-button>
      </div>
      <el-alert
        v-if="result"
        :title="result.status === 'correct' ? `判定通过，得分 ${result.score}` : '判定不通过'"
        :type="result.status === 'correct' ? 'success' : 'error'"
        :closable="false"
        class="result"
      />
    </el-card>
  </div>
</template>

<style scoped>
.lesson { line-height: 1.7; margin: 1rem 0 2rem; }
.lesson :deep(pre) { background: #f5f7fa; padding: 0.8rem; border-radius: 4px; overflow: auto; }
.lab { max-width: 640px; }
.score { color: #666; margin: 0 0 0.8rem; }
.submit-row { display: flex; gap: 0.5rem; }
.result { margin-top: 1rem; }
</style>