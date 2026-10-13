<script setup lang="ts">
// 题目详情页（Burp lab 页风格）：描述 markdown、提示折叠、配套教程入口、
// 容器环境生命周期与 flag 提交都集中在这里（列表页只做浏览）。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { ElMessage, ElMessageBox } from 'element-plus'
import { trainingApi, instanceApi, type AdminLabInstance, type ChallengeDetail, type LabInstance } from '../api'
import type { ApiError } from '../api/client'
import { useSubmissionPoll } from '../composables/useSubmissionPoll'

const route = useRoute()
const router = useRouter()
const challenge = ref<ChallengeDetail | null>(null)
const loading = ref(true)
const answer = ref('')
const instance = ref<LabInstance | null>(null)
const instanceLoading = ref(false)

const { submitting, polling, result, submit: submitPoll } = useSubmissionPoll(() => {
  if (challenge.value) challenge.value.solved = true
})

const renderedDescription = computed(() =>
  DOMPurify.sanitize(marked.parse(challenge.value?.description_md ?? '') as string),
)

const roomBase = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

const instanceAlive = computed(() =>
  instance.value && instance.value.status !== 'destroyed' && instance.value.status !== 'stopped',
)

function slug() {
  return route.params.challengeSlug as string
}

async function load() {
  loading.value = true
  try {
    challenge.value = await trainingApi.detail(slug())
    if (challenge.value.has_env) loadInstance()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function loadInstance() {
  try {
    instance.value = await instanceApi.get(slug())
  } catch {
    instance.value = null // 无实例（含 501 驱动未配置）静默处理
  }
}

// 单容器约束：409 时提示关闭上一个容器（显示其题目名并可跳转过去销毁）。
async function startInstance() {
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.start(slug())
  } catch (e) {
    const err = e as ApiError
    if (err.status === 409 && err.data?.current) {
      promptInstanceConflict(err.data.current as AdminLabInstance)
    } else {
      ElMessage.error(`环境启动失败：${err.message}`)
    }
  } finally {
    instanceLoading.value = false
  }
}

function promptInstanceConflict(current: AdminLabInstance) {
  const challengeName = current.challenge_title || current.challenge_slug || `题目 #${current.challenge_id}`
  ElMessageBox.confirm(
    `同一时间只能启动一个容器。请先关闭你正在「${challengeName}」题目上运行的环境，再启动本题。`,
    '已有运行中的容器',
    { confirmButtonText: '前往关闭', cancelButtonText: '知道了', type: 'warning' },
  )
    .then(() => {
      if (current.challenge_slug) {
        router.push(`${roomBase}/training/${current.challenge_slug}`)
      }
    })
    .catch(() => {})
}

async function destroyInstance() {
  instanceLoading.value = true
  try {
    await instanceApi.destroy(slug())
    instance.value = null
  } catch (e) {
    ElMessage.error(`环境销毁失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
  }
}

function submit() {
  if (!challenge.value || !answer.value) return
  submitPoll(challenge.value.slug, answer.value)
}

function difficultyType(d: ChallengeDetail['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

onMounted(load)
</script>

<template>
  <el-skeleton v-if="loading" :rows="6" animated />
  <div v-else-if="challenge" class="page">
    <div class="head">
      <h2>{{ challenge.title }}</h2>
      <span class="tags">
        <el-tag :type="difficultyType(challenge.difficulty)">{{ challenge.difficulty }}</el-tag>
        <el-tag type="info">{{ challenge.score }} 分</el-tag>
        <el-tag v-if="challenge.solved" type="success">已通关</el-tag>
        <el-tag v-if="challenge.has_env" type="primary">容器环境</el-tag>
      </span>
    </div>

    <router-link
      v-if="challenge.tutorial_slug && challenge.tutorial_topic_slug"
      class="tutorial-link"
      :to="`${roomBase}/topics/${challenge.tutorial_topic_slug}/rooms/${challenge.tutorial_slug}`"
    >配套教程：{{ challenge.tutorial_title || challenge.tutorial_slug }} →</router-link>

    <div class="description" v-html="renderedDescription" />

    <el-collapse v-if="challenge.hints.length" class="hints">
      <el-collapse-item v-for="(h, i) in challenge.hints" :key="i">
        <template #title>
          <span class="hint-title">{{ h.title }}</span>
          <el-tag size="small" type="warning" class="cost">提示 -{{ h.cost }} 分</el-tag>
        </template>
        <p class="hint-content">{{ h.content }}</p>
      </el-collapse-item>
    </el-collapse>

    <el-card class="solve">
      <template #header><span>解答</span></template>
      <!-- 容器环境生命周期：has_env 题目专用（启动后题目部署在容器里，TTL 自动回收） -->
      <div v-if="challenge.has_env" class="instance-row">
        <template v-if="instanceAlive">
          <el-tag :type="instance!.status === 'running' ? 'success' : 'warning'" size="small">
            环境{{ instance!.status === 'running' ? '运行中' : '启动中' }}
          </el-tag>
          <code v-if="instance!.endpoint" class="endpoint">{{ instance!.endpoint }}</code>
          <span v-if="instance!.expire_at" class="expire">
            到期 {{ new Date(instance!.expire_at).toLocaleTimeString() }}
          </span>
          <el-button size="small" :loading="instanceLoading" @click="destroyInstance">销毁环境</el-button>
        </template>
        <el-button v-else type="primary" size="small" :loading="instanceLoading" @click="startInstance">
          启动容器
        </el-button>
      </div>

      <div class="submit-row">
        <el-input
          v-model="answer"
          placeholder="输入 flag"
          :disabled="challenge.solved || polling"
          @keyup.enter="submit"
        />
        <el-button
          type="primary"
          :loading="submitting || polling"
          :disabled="challenge.solved || !answer"
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
.page { max-width: 860px; }
.head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 0.5rem; }
.tags { display: flex; gap: 0.4rem; }
.tutorial-link { display: inline-block; margin: 0.5rem 0; color: var(--el-color-primary); text-decoration: none; }
.description { line-height: 1.7; margin: 1rem 0 2rem; }
.description :deep(pre) { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; overflow: auto; }
.description :deep(code) { background: var(--el-fill-color-light); padding: 0.1rem 0.3rem; border-radius: 3px; }
.hints { margin-bottom: 1.5rem; }
.hint-title { margin-right: 0.6rem; }
.cost { margin-left: 0.3rem; }
.hint-content { margin: 0; color: var(--el-text-color-regular); }
.solve { max-width: 640px; }
.instance-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.8rem; flex-wrap: wrap; }
.endpoint { background: var(--el-fill-color-light); padding: 0.1rem 0.4rem; border-radius: 3px; font-family: Consolas, monospace; color: var(--el-color-primary); }
.expire { color: var(--el-text-color-secondary); font-size: 0.8rem; }
.submit-row { display: flex; gap: 0.5rem; }
.result { margin-top: 0.8rem; }
</style>