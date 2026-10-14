<script setup lang="ts">
// 题目详情页（Burp lab 页风格）：描述 markdown、提示折叠、配套教程入口、
// 容器环境生命周期与 flag 提交都集中在这里（列表页只做浏览）。
// 环境运行中时轮询详情：题目内置 checker 经检测点回调自动判定
// （CHECKPOINT_URL，服务端直判），通关状态无需用户手动提交。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  trainingApi, instanceApi, downloadAttachment,
  type AdminLabInstance, type ChallengeDetail, type LabInstance,
} from '../api'
import type { ApiError } from '../api/client'
import { useSubmissionPoll } from '../composables/useSubmissionPoll'

// 附件下载：fetch blob 后触发浏览器保存（附件接口需 JWT，<a href> 无法带头）。
const downloadingFile = ref('')
async function download(filename: string) {
  downloadingFile.value = filename
  try {
    const blob = await downloadAttachment(slug(), filename)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(`附件下载失败：${(e as Error).message}`)
  } finally {
    downloadingFile.value = ''
  }
}

function fileSize(n: number) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(1)} KB`
  return `${n} B`
}

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

// 提交时是否已通关：重复提交判对不重复计分（结算只有一次），文案需要区分。
const solvedBeforeSubmit = ref(false)

const renderedDescription = computed(() =>
  DOMPurify.sanitize(marked.parse(challenge.value?.description_md ?? '') as string),
)

const roomBase = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

const instanceAlive = computed(() =>
  instance.value && instance.value.status !== 'destroyed' && instance.value.status !== 'stopped',
)

// 治理上限：每实例最多续期 2 次（后端 422 兜底，这里只做预展示）。
const maxExtends = 2
const remainingExtends = computed(() => maxExtends - (instance.value?.extend_count ?? 0))

// 自动判定轮询：实例运行中每 5s 刷新详情，checker 回调判过即显示通关。
let autoSolveTimer: number | null = null

function restartAutoSolvePolling() {
  stopAutoSolvePolling()
  if (!instanceAlive.value || challenge.value?.solved) return
  autoSolveTimer = window.setInterval(async () => {
    try {
      const detail = await trainingApi.detail(slug())
      if (detail.solved) {
        challenge.value = detail
        ElMessage.success('环境检测通过，本题通关！')
        stopAutoSolvePolling()
      }
    } catch {
      // 轮询失败静默（下轮重试）；离开页面时统一清理
    }
  }, 5000)
}

function stopAutoSolvePolling() {
  if (autoSolveTimer !== null) {
    window.clearInterval(autoSolveTimer)
    autoSolveTimer = null
  }
}

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
  restartAutoSolvePolling()
}

// 单容器约束：409 时提示关闭上一个容器（显示其题目名并可跳转过去销毁）。
async function startInstance() {
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.start(slug())
    restartAutoSolvePolling()
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
    stopAutoSolvePolling()
  } catch (e) {
    ElMessage.error(`环境销毁失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
  }
}

async function extendInstance() {
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.extend(slug())
    ElMessage.success('已续期 1 小时')
  } catch (e) {
    ElMessage.error(`续期失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
  }
}

function submit() {
  if (!challenge.value || !answer.value) return
  solvedBeforeSubmit.value = challenge.value.solved
  submitPoll(challenge.value.slug, answer.value)
}

// 结算模型：题目分值只结算一次，重复判对 score=0；提示扣费按账号永久（不退回）。
const resultTitle = computed(() => {
  if (!result.value) return ''
  if (result.value.status !== 'correct') return '判定不通过'
  if (result.value.score > 0) return `判定通过，得分 ${result.value.score}`
  return solvedBeforeSubmit.value ? '判定通过——本题分值已结算过，重复提交不重复计分' : '判定通过（本题 0 分）'
})

const unlockingId = ref<number | null>(null)

async function unlockHint(hintId: number) {
  if (!challenge.value) return
  unlockingId.value = hintId
  try {
    const res = await trainingApi.unlockHint(slug(), hintId)
    const h = challenge.value.hints.find((x) => x.id === hintId)
    if (h) {
      h.unlocked = true
      h.content = res.content
    }
    ElMessage.success(res.cost > 0 ? `已解锁，扣除 ${res.cost} 分（剩余 ${res.score}）` : '已解锁')
  } catch (e) {
    ElMessage.error(`解锁失败：${(e as Error).message}`)
  } finally {
    unlockingId.value = null
  }
}

function difficultyType(d: ChallengeDetail['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

onMounted(load)
onUnmounted(stopAutoSolvePolling)
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

    <div v-if="challenge.attachments?.length" class="attachments">
      <h4>附件</h4>
      <div v-for="a in challenge.attachments" :key="a.filename" class="attach-row">
        <span class="attach-name">{{ a.filename }}</span>
        <span class="attach-size">{{ fileSize(a.size) }}</span>
        <el-button
          size="small"
          :loading="downloadingFile === a.filename"
          @click="download(a.filename)"
        >下载</el-button>
      </div>
    </div>

    <div v-if="challenge.links?.length" class="links">
      <h4>相关链接</h4>
      <a
        v-for="l in challenge.links"
        :key="l.url"
        :href="l.url"
        target="_blank"
        rel="noopener noreferrer"
      >{{ l.name }} ↗</a>
    </div>

    <el-collapse v-if="challenge.hints.length" class="hints">
      <el-collapse-item v-for="h in challenge.hints" :key="h.id" :name="h.id">
        <template #title>
          <span class="hint-title">{{ h.title }}</span>
          <el-tag v-if="h.unlocked" size="small" type="success" class="cost">已解锁</el-tag>
          <el-tag v-else size="small" type="warning" class="cost">
            {{ h.cost > 0 ? `解锁 -${h.cost} 分` : '免费解锁' }}
          </el-tag>
        </template>
        <template v-if="h.unlocked">
          <p class="hint-content">{{ h.content }}</p>
        </template>
        <div v-else class="hint-locked">
          <p class="hint-locked-text">提示内容已折叠，解锁后可见。</p>
          <el-button
            type="warning"
            size="small"
            :loading="unlockingId === h.id"
            @click.stop="unlockHint(h.id)"
          >{{ h.cost > 0 ? `花费 ${h.cost} 分解锁` : '解锁提示' }}</el-button>
        </div>
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
          <el-button
            v-if="instance!.status === 'running'"
            size="small"
            :disabled="remainingExtends <= 0"
            :title="remainingExtends > 0 ? `还可续期 ${remainingExtends} 次（每次 1 小时）` : '续期次数已用完，请销毁后重新启动'"
            :loading="instanceLoading"
            @click="extendInstance"
          >续期{{ remainingExtends > 0 ? `（剩 ${remainingExtends} 次）` : '' }}</el-button>
          <el-button size="small" :loading="instanceLoading" @click="destroyInstance">销毁环境</el-button>
        </template>
        <el-button v-else type="primary" size="small" :loading="instanceLoading" @click="startInstance">
          启动容器
        </el-button>
      </div>

      <!-- 结算模型：分值只结算一次，但题目可重复提交/重复完成——已通关不禁用。 -->
      <el-alert
        v-if="challenge.solved"
        type="success"
        :closable="false"
        class="solved-note"
        title="本题已通关（分值已结算）。仍可重复提交 flag 验证，重复判对不再计分。"
      />
      <div class="submit-row">
        <el-input
          v-model="answer"
          placeholder="输入 flag"
          :disabled="polling"
          @keyup.enter="submit"
        />
        <el-button
          type="primary"
          :loading="submitting || polling"
          :disabled="!answer"
          @click="submit"
        >提交</el-button>
      </div>
      <el-alert
        v-if="result"
        :title="resultTitle"
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
.attachments { margin: 0 0 1.2rem; }
.attachments h4, .links h4 { margin: 0 0 0.4rem; }
.attach-row { display: flex; align-items: center; gap: 0.8rem; padding: 0.25rem 0; }
.attach-name { font-family: Consolas, monospace; }
.attach-size { color: var(--el-text-color-secondary); font-size: 0.85rem; }
.links { margin: 0 0 1.2rem; display: flex; flex-direction: column; gap: 0.25rem; }
.links a { color: var(--el-color-primary); text-decoration: none; }
.hints { margin-bottom: 1.5rem; }
.hint-title { margin-right: 0.6rem; }
.cost { margin-left: 0.3rem; }
.hint-content { margin: 0; color: var(--el-text-color-regular); }
.hint-locked { display: flex; align-items: center; gap: 0.8rem; flex-wrap: wrap; }
.hint-locked-text { margin: 0; color: var(--el-text-color-secondary); font-size: 0.9rem; }
.solve { max-width: 640px; }
.instance-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.8rem; flex-wrap: wrap; }
.endpoint { background: var(--el-fill-color-light); padding: 0.1rem 0.4rem; border-radius: 3px; font-family: Consolas, monospace; color: var(--el-color-primary); }
.expire { color: var(--el-text-color-secondary); font-size: 0.8rem; }
.submit-row { display: flex; gap: 0.5rem; }
.solved-note { margin-bottom: 0.8rem; }
.result { margin-top: 0.8rem; }
</style>