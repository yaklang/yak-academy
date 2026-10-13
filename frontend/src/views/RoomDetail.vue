<script setup lang="ts">
// Room 详情页：教程正文（Markdown 渲染 + sanitize）+ 内嵌 Lab 提交区（教题一体单元）。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { topicApi, submissionApi, instanceApi, type Room, type LabInstance } from '../api'

const route = useRoute()
const room = ref<Room | null>(null)
const answer = ref('')
const submitting = ref(false)
const polling = ref(false)
const pollCount = ref(0)
const result = ref<{ status: string; score: number } | null>(null)
const instance = ref<LabInstance | null>(null)
const instanceLoading = ref(false)

const renderedLesson = computed(() =>
  DOMPurify.sanitize(marked.parse(room.value?.lesson_md ?? '') as string),
)

async function load() {
  const { topicSlug, roomSlug } = route.params as { topicSlug: string; roomSlug: string }
  try {
    room.value = await topicApi.room(topicSlug, roomSlug)
    if (room.value.lab?.has_env) loadInstance()
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

async function loadInstance() {
  if (!room.value?.lab?.has_env) return
  try {
    instance.value = await instanceApi.get(room.value.slug)
  } catch {
    instance.value = null // 无实例（含 501 驱动未配置）静默处理
  }
}

async function startInstance() {
  if (!room.value?.lab?.has_env) return
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.start(room.value.slug)
  } catch (e) {
    ElMessage.error(`环境启动失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
  }
}

async function destroyInstance() {
  if (!room.value?.lab?.has_env) return
  instanceLoading.value = true
  try {
    await instanceApi.destroy(room.value.slug)
    instance.value = null
  } catch (e) {
    ElMessage.error(`环境销毁失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
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

      <!-- Lab 环境实例：仅 has_env（lab.yaml env 段）时展示，启动/销毁/状态（幂等启动，TTL 自动回收） -->
      <div v-if="room.lab.has_env" class="instance-row">
        <template v-if="instance && instance.status !== 'destroyed' && instance.status !== 'stopped'">
          <el-tag :type="instance.status === 'running' ? 'success' : 'warning'" size="small">
            环境{{ instance.status === 'running' ? '运行中' : '启动中' }}
          </el-tag>
          <code v-if="instance.endpoint" class="endpoint">{{ instance.endpoint }}</code>
          <el-button size="small" :loading="instanceLoading" @click="destroyInstance">销毁环境</el-button>
        </template>
        <el-button v-else size="small" :loading="instanceLoading" @click="startInstance">启动环境</el-button>
      </div>

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
.lesson :deep(pre) { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; overflow: auto; }
.lesson :deep(code) { background: var(--el-fill-color-light); padding: 0.1rem 0.3rem; border-radius: 3px; }
.lab { max-width: 640px; }
.score { color: var(--el-text-color-secondary); margin: 0 0 0.8rem; }
.instance-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.8rem; }
.endpoint { background: var(--el-fill-color-light); padding: 0.1rem 0.4rem; border-radius: 3px; font-family: Consolas, monospace; color: var(--el-color-primary); }
.submit-row { display: flex; gap: 0.5rem; }
.result { margin-top: 1rem; }
</style>