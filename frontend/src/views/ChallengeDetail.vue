<script setup lang="ts">
// 题目详情页（Burp lab 页风格）：描述 markdown、提示折叠、配套教程入口、
// 容器环境生命周期与 flag 提交都集中在这里（列表页只做浏览）。
// 环境运行中时轮询详情：题目内置 checker 经检测点回调自动判定
// （CHECKPOINT_URL，服务端直判），通关状态无需用户手动提交。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  trainingApi, instanceApi, downloadAttachment,
  type AdminLabInstance, type ChallengeDetail, type CodeRunView, type JudgeSample, type LabInstance,
  verdictLabels,
} from '../api'
import type { ApiError } from '../api/client'
import CodeEditor from '../components/CodeEditor.vue'
import type { YakLibPolicy } from '../components/yakCompletion'
import { LIB_DESC } from '../components/yakVocab'
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

// ACM 编程题（judge 段存在）：源码提交 + 样例数据卡（flag 判定题 samples 恒空）。
const isCode = computed(() => !!challenge.value?.judge)
const samples = ref<JudgeSample[]>([])
const source = ref('')

// 库策略（防高危库执行）：白名单/黑名单由服务端随 judge 段下发——
// 编辑器补全过滤（CodeEditor libPolicy）与题面库标签同源；提交/自测
// 在服务端受理前还有一道静态扫描拦截（前端只是提示与引导，不是安全边界）。
const libPolicy = computed<YakLibPolicy>(() => ({
  allowed: challenge.value?.judge?.allowed_libs ?? null,
  blocked: challenge.value?.judge?.blocked_libs ?? [],
}))
const libLabel = (lib: string) => LIB_DESC[lib] ?? 'yak 标准库'
const blockedLibsNote = computed(() => {
  const blocked = libPolicy.value.blocked
  if (!blocked.length) return ''
  return `已禁用高危库：${blocked.join('、')}`
})

// 引擎装配守卫：engine_available=false（ACADEMY_YAK_BIN 未配置且未下载）时
// 提前禁用提交/自测入口并提示——代替点击后才收到 internal_error/422 的滞后反馈。
const engineMissing = computed(
  () => isCode.value && challenge.value?.judge?.engine_available === false,
)

// 自测运行（动态执行框）：对样例组或自定义输入跑一次当前源码，
// 异步票据 + 1s 轮询；运营态，不产生提交/事件/计分（与提交判题互不影响）。
const RUN_CUSTOM = '__custom'
const runTarget = ref('')
const customStdin = ref('')
const running = ref(false)
const runResult = ref<CodeRunView | null>(null)
let runTimer: number | null = null

async function loadSamples() {
  try {
    const res = await trainingApi.judgeSamples(slug())
    samples.value = res.samples
  } catch {
    samples.value = [] // 样例拉取失败不阻断提交（题面通常也含样例）
  }
  runTarget.value = samples.value.length ? samples.value[0].name : RUN_CUSTOM
}

async function runCode() {
  if (!source.value) return
  if (runTarget.value === RUN_CUSTOM && !customStdin.value.trim()) {
    ElMessage.warning('请先填写自定义输入（作为程序的标准输入）')
    return
  }
  running.value = true
  runResult.value = null
  try {
    const body = runTarget.value === RUN_CUSTOM
      ? { source: source.value, stdin: customStdin.value }
      : { source: source.value, sample: runTarget.value }
    const { id } = await trainingApi.judgeRun(slug(), body)
    runTimer = window.setInterval(() => pollRun(id), 1000)
  } catch (e) {
    running.value = false
    ElMessage.error(friendlyRunError(e))
  }
}

async function pollRun(id: string) {
  try {
    const view = await trainingApi.judgeRunGet(slug(), id)
    runResult.value = view
    if (view.status === 'done') {
      stopRunPolling()
      running.value = false
    }
  } catch {
    stopRunPolling()
    running.value = false
    ElMessage.error('自测运行结果获取失败，请重试')
  }
}

function stopRunPolling() {
  if (runTimer !== null) {
    window.clearInterval(runTimer)
    runTimer = null
  }
}

// 后端 422 文案（英文）本地化，其余透传。
function friendlyRunError(e: unknown) {
  const msg = (e as Error).message || '未知错误'
  if (msg.includes('code engine not configured')) {
    return '自测运行不可用：平台未配置判定引擎（ACADEMY_YAK_BIN），请联系管理员'
  }
  if (msg.includes('previous self-test run')) {
    return '已有一次自测运行在进行中，请等它结束后再试'
  }
  return `自测运行失败：${msg}`
}

const runOutcomeLabel = computed(() => {
  const v = runResult.value
  if (!v || v.status !== 'done') return ''
  return v.outcome === 'ok' ? '运行完成' : v.outcome === 'tle' ? '超出时限' : '运行错误'
})
const runOutcomeTag = computed(() => {
  const v = runResult.value
  if (!v || v.status !== 'done') return 'info' as const
  return v.outcome === 'ok' ? ('success' as const) : v.outcome === 'tle' ? ('warning' as const) : ('danger' as const)
})

const renderedDescription = computed(() =>
  DOMPurify.sanitize(marked.parse(challenge.value?.description_md ?? '') as string),
)

// 管理端教程预览路由是 /admin/tutorials/...（题库侧 /training 两端同名，可共用）。
const tutorialBase = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))

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
    if (challenge.value.judge) loadSamples()
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

// 单容器约束：409 时可直接在弹窗中关闭上一题的环境（或跳转过去手动处理）。
async function startInstance() {
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.start(slug())
    restartAutoSolvePolling()
  } catch (e) {
    const err = e as ApiError
    if (err.status === 409 && err.data?.current) {
      promptInstanceConflict(err.data.current as AdminLabInstance)
    } else if (err.status === 501) {
      // 驱动未装配（noop stub）：给管理员可行动的提示，而非裸 "not implemented yet"。
      ElMessage.error('平台未启用容器环境：服务端未装配 Docker 驱动（ACADEMY_ENV_DRIVER），请联系管理员')
    } else {
      ElMessage.error(`环境启动失败：${err.message}`)
    }
  } finally {
    instanceLoading.value = false
  }
}

// 弹窗内直接销毁冲突实例并接续启动本题环境。
async function destroyConflictingAndStart(current: AdminLabInstance) {
  instanceLoading.value = true
  try {
    await instanceApi.destroy(current.challenge_slug!)
    instance.value = await instanceApi.start(slug())
    ElMessage.success('已关闭上一个环境，本题环境启动成功')
    restartAutoSolvePolling()
  } catch (e) {
    // 接续启动仍 409（极端并发）：重新弹冲突框，让用户再处理一次。
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
    `同一时间只能启动一个容器。你在「${challengeName}」题目上还有运行中的环境，关闭后即可启动本题。`,
    '已有运行中的容器',
    {
      confirmButtonText: current.challenge_slug ? '关闭并启动本题' : '前往关闭',
      cancelButtonText: '取消',
      type: 'warning',
      confirmButtonClass: 'el-button--danger',
    },
  )
    .then(() => {
      if (current.challenge_slug) {
        destroyConflictingAndStart(current)
      } else {
        // 关联题目已删（无 slug 可寻址，销毁接口也按 slug）：无法自助处理。
        ElMessage.error('该环境关联的题目已被删除，无法定位与关闭，请联系管理员在容器管理中清理')
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
  if (!challenge.value) return
  const payload = isCode.value ? source.value : answer.value
  if (!payload) return
  solvedBeforeSubmit.value = challenge.value.solved
  submitPoll(challenge.value.slug, payload)
}

// 结算模型：题目分值只结算一次，重复判对 score=0；提示扣费按账号永久（不退回）。
// ACM 编程题附带 verdict 文案（wa/tle/re/internal_error）；部分得分制下部分
// 通过组也写入 score（verdict 非 ac 但得分 > 0）。
const resultTitle = computed(() => {
  if (!result.value) return ''
  if (result.value.status !== 'correct') {
    if (result.value.score > 0 && result.value.groups?.length) {
      return `部分通过，得分 ${result.value.score}（未全部通过，题目未通关；补齐失败组后重新提交）`
    }
    if (result.value.verdict) return `判定不通过：${verdictLabels[result.value.verdict]}`
    return '判定不通过'
  }
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
onUnmounted(() => {
  stopAutoSolvePolling()
  stopRunPolling()
})
</script>

<template>
  <el-skeleton v-if="loading" :rows="6" animated />
  <div v-else-if="challenge" class="page">
    <header class="head">
      <h2 class="title">{{ challenge.title }}</h2>
      <span class="tags">
        <el-tag :type="difficultyType(challenge.difficulty)" effect="plain">{{ challenge.difficulty }}</el-tag>
        <el-tag type="info" effect="plain" class="score-tag">{{ challenge.score }} 分</el-tag>
        <el-tag v-if="challenge.solved" type="success">已通关</el-tag>
        <el-tag v-if="challenge.has_env" type="primary" effect="plain">容器环境</el-tag>
        <el-tag v-if="challenge.judge" type="warning" effect="plain">编程题 · {{ challenge.judge.time_limit_s }}s / {{ challenge.judge.memory_limit_mb }}MB</el-tag>
      </span>
    </header>

    <router-link
      v-if="challenge.tutorial_slug && challenge.tutorial_topic_slug"
      class="tutorial-link"
      :to="`${tutorialBase}/${challenge.tutorial_topic_slug}/rooms/${challenge.tutorial_slug}`"
    >配套教程：{{ challenge.tutorial_title || challenge.tutorial_slug }} →</router-link>

    <div class="description" v-html="renderedDescription" />

    <!-- ACM 编程题样例数据（sample=true 的组；隐藏组不下发） -->
    <div v-if="samples.length" class="samples">
      <h4>样例数据</h4>
      <div v-for="s in samples" :key="s.name" class="sample-group">
        <span class="sample-name">{{ s.name }}</span>
        <div class="sample-io">
          <div class="sample-block">
            <span class="sample-label">输入</span>
            <pre>{{ s.input }}</pre>
          </div>
          <div class="sample-block">
            <span class="sample-label">期望输出</span>
            <pre>{{ s.output }}</pre>
          </div>
        </div>
      </div>
    </div>

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
        :title="isCode
          ? '本题已通关（分值已结算）。仍可重复提交源码验证，重复判对不再计分。'
          : '本题已通关（分值已结算）。仍可重复提交 flag 验证，重复判对不再计分。'"
      />
      <!-- ACM 编程题：在线编辑器 + 自测运行（不产生提交记录），提交走正式判题。 -->
      <template v-if="isCode">
        <div class="sample-note">
          提交 yak 源码：对 {{ challenge.judge!.testdata_count }} 组数据逐组运行（时限 {{ challenge.judge!.time_limit_s }}s/组），
          全部通过才算 AC；配置了组分的题目按通过组折算得分。可在下方编辑器编码，先对样例或自定义输入自测运行。
        </div>
        <!-- 引擎未装配：提交/自测入口整体禁用（服务端仍兜底拒绝） -->
        <el-alert
          v-if="engineMissing"
          type="warning"
          :closable="false"
          show-icon
          class="engine-missing"
          title="判题引擎未配置：提交代码与自测运行暂不可用，请联系管理员配置（ACADEMY_YAK_BIN）"
        />
        <!-- 库策略提示：白名单 chips + 黑名单说明（与补全过滤、服务端拦截同源） -->
        <div class="lib-policy">
          <div class="lib-row">
            <span class="lib-title">本题可用库</span>
            <template v-if="libPolicy.allowed">
              <el-tooltip v-for="lib in libPolicy.allowed" :key="lib" :content="libLabel(lib)" placement="top">
                <el-tag size="small" effect="plain" class="lib-chip">{{ lib }}</el-tag>
              </el-tooltip>
            </template>
            <span v-else class="lib-all">全部 yak 标准库（黑名单除外）</span>
          </div>
          <div v-if="blockedLibsNote" class="lib-blocked">{{ blockedLibsNote }}——提交包含此类库的源码将被拒绝。</div>
        </div>
        <CodeEditor v-model="source" class="code-input" :disabled="polling" :lib-policy="libPolicy" />
        <div class="run-panel">
          <div class="run-bar">
            <el-radio-group v-model="runTarget" size="small" :disabled="running">
              <el-radio-button v-for="s in samples" :key="s.name" :value="s.name">
                样例 {{ s.name }}
              </el-radio-button>
              <el-radio-button :value="RUN_CUSTOM">自定义输入</el-radio-button>
            </el-radio-group>
            <el-button
              size="small"
              :loading="running"
              :disabled="!source || !runTarget || engineMissing"
              @click="runCode"
            >运行自测</el-button>
          </div>
          <el-input
            v-if="runTarget === RUN_CUSTOM"
            v-model="customStdin"
            type="textarea"
            :rows="4"
            class="stdin-input"
            placeholder="自定义输入（作为程序的标准输入，回车换行）"
          />
          <div v-if="runResult" class="run-output">
            <div class="run-meta">
              <el-tag v-if="runResult.status !== 'done'" size="small" type="info">
                {{ runResult.status === 'pending' ? '排队中…' : '运行中…' }}
              </el-tag>
              <template v-else>
                <el-tag size="small" :type="runOutcomeTag">{{ runOutcomeLabel }}</el-tag>
                <span class="run-time">耗时 {{ runResult.time_ms }} ms</span>
                <el-tag
                  v-if="runResult.mode === 'sample' && runResult.matches !== null"
                  size="small"
                  :type="runResult.matches ? 'success' : 'danger'"
                  effect="plain"
                >{{ runResult.matches ? '与样例期望一致' : '与样例期望不一致' }}</el-tag>
                <el-tag v-if="runResult.truncated" size="small" type="info" effect="plain">输出超长已截断</el-tag>
              </template>
            </div>
            <pre v-if="runResult.stdout" class="run-stdout">{{ runResult.stdout }}</pre>
            <pre v-if="runResult.stderr" class="run-stderr">{{ runResult.stderr }}</pre>
          </div>
        </div>
        <el-button
          type="primary"
          class="submit-btn"
          :loading="submitting || polling"
          :disabled="!source || engineMissing"
          @click="submit"
        >提交代码</el-button>
      </template>
      <div v-else class="submit-row">
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
      <!-- ACM 部分得分：逐组判定结论（通过组计分、失败组标注判定）。 -->
      <div v-if="result?.groups?.length" class="group-results">
        <div
          v-for="g in result.groups"
          :key="g.name"
          class="group-row"
          :class="{ 'group-pass': g.passed, 'group-fail': !g.passed }"
        >
          <span class="group-name">
            {{ g.sample ? '样例' : '测试' }}组 {{ g.name }}
          </span>
          <el-tag :type="g.passed ? 'success' : 'danger'" size="small" effect="plain">
            {{ g.passed ? '通过' : verdictLabels[g.verdict!] || '未通过' }}
          </el-tag>
          <span v-if="g.score > 0" class="group-score">{{ g.score }} 分</span>
        </div>
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.page { max-width: 860px; }
.head {
  display: flex; align-items: center; justify-content: space-between;
  flex-wrap: wrap; gap: 0.6rem;
  border-bottom: 1px solid var(--el-border-color);
  padding-bottom: 0.9rem; margin-bottom: 0.9rem;
}
.title { margin: 0; font-size: 1.45rem; font-weight: 700; }
.tags { display: flex; gap: 0.4rem; flex-wrap: wrap; }
.score-tag { font-family: Consolas, monospace; }
.tutorial-link { display: inline-block; margin: 0.4rem 0 0.8rem; color: var(--el-color-primary); text-decoration: none; }
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
/* ACM 逐组判定结论（部分得分反馈） */
.group-results {
  display: flex; flex-direction: column; gap: 0.35rem;
  margin-top: 0.8rem; padding-top: 0.8rem;
  border-top: 1px dashed var(--el-border-color);
}
.group-row { display: flex; align-items: center; gap: 0.6rem; }
.group-name {
  font-family: Consolas, monospace; font-size: 0.85rem;
  color: var(--el-text-color-regular); min-width: 6.5rem;
}
.group-score {
  margin-left: auto; font-size: 0.85rem;
  color: var(--el-text-color-secondary); font-family: Consolas, monospace;
}
.group-fail .group-name { color: var(--el-color-danger); }
.instance-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.8rem; flex-wrap: wrap; }
.endpoint { background: var(--el-fill-color-light); padding: 0.1rem 0.4rem; border-radius: 3px; font-family: Consolas, monospace; color: var(--el-color-primary); }
.expire { color: var(--el-text-color-secondary); font-size: 0.8rem; }
.submit-row { display: flex; gap: 0.5rem; }
.samples { margin: 0 0 1.5rem; }
.samples h4 { margin: 0 0 0.4rem; }
.sample-group { margin-bottom: 0.6rem; }
.sample-name {
  font-family: Consolas, monospace; font-size: 0.85rem;
  color: var(--el-text-color-secondary);
}
.sample-io { display: flex; gap: 0.8rem; flex-wrap: wrap; }
.sample-block {
  flex: 1; min-width: 200px; background: var(--el-fill-color-light);
  border-radius: 4px; padding: 0.5rem 0.7rem;
}
.sample-label {
  display: block; font-size: 0.8rem; color: var(--el-text-color-secondary);
  margin-bottom: 0.2rem;
}
.sample-block pre {
  margin: 0; font-family: Consolas, monospace; font-size: 0.85rem;
  white-space: pre-wrap; word-break: break-all;
}
.sample-note {
  margin-bottom: 0.8rem; font-size: 0.85rem;
  color: var(--el-text-color-secondary);
}
.engine-missing { margin-bottom: 0.8rem; }
.lib-policy {
  margin-bottom: 0.7rem; padding: 0.55rem 0.75rem;
  background: var(--el-fill-color-lighter); border-radius: 6px;
  font-size: 0.8rem;
}
.lib-row { display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap; }
.lib-title { color: var(--el-text-color-secondary); margin-right: 0.2rem; }
.lib-chip { font-family: Consolas, monospace; }
.lib-all { color: var(--el-text-color-regular); }
.lib-blocked { margin-top: 0.35rem; color: var(--el-color-danger); opacity: 0.85; }
.run-panel { margin-top: 0.6rem; }
.run-bar { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
.stdin-input { margin-top: 0.5rem; font-family: Consolas, monospace; }
.run-output {
  margin-top: 0.5rem; background: var(--el-fill-color-light);
  border-radius: 4px; padding: 0.5rem 0.7rem;
}
.run-meta { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
.run-time { font-size: 0.8rem; color: var(--el-text-color-secondary); }
.run-output pre {
  margin: 0.4rem 0 0; font-family: Consolas, monospace; font-size: 0.85rem;
  white-space: pre-wrap; word-break: break-all;
}
.run-stderr { color: var(--el-color-danger); }
.submit-btn { margin-top: 0.6rem; }
.solved-note { margin-bottom: 0.8rem; }
.result { margin-top: 0.8rem; }
</style>