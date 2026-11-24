<script setup lang="ts">
// 题目详情页（Burp lab 页风格）：描述 markdown、提示折叠、配套教程入口、
// 容器环境生命周期与 flag 提交都集中在这里（列表页只做浏览）。
// 环境运行中时轮询详情：题目内置 checker 经检测点回调自动判定
// （CHECKPOINT_URL，服务端直判），通关状态无需用户手动提交。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { renderMarkdownHtml } from '@/lib/markdown'
import { notify, dialogs } from '@/lib/notifications'
import { formatTime } from '@/lib/format'
import {
  trainingApi, instanceApi, downloadAttachment,
  type AdminLabInstance, type ChallengeDetail, type CodeRunView, type JudgeSample, type LabInstance,
  CATEGORY_LABELS, DIFFICULTY_LABELS, verdictLabels,
} from '../api'
import type { ApiError } from '../api/client'
import CodeEditor from '../components/CodeEditor.vue'
import PageHeader from '@/components/layout/PageHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
    notify.error(`附件下载失败：${(e as Error).message}`)
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
const loadError = ref('')
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
    notify.warning('请先填写自定义输入（作为程序的标准输入）')
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
    notify.error(friendlyRunError(e))
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
    notify.error('自测运行结果获取失败，请重试')
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

const renderedDescription = computed(() => renderMarkdownHtml(challenge.value?.description_md))

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
        notify.success('环境检测通过，本题通关！')
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
  loadError.value = ''
  challenge.value = null
  try {
    challenge.value = await trainingApi.detail(slug())
    if (challenge.value.has_env) loadInstance()
    if (challenge.value.judge) loadSamples()
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '题目加载失败，请稍后重试'
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
      notify.error('平台未启用容器环境：服务端未装配 Docker 驱动（ACADEMY_ENV_DRIVER），请联系管理员')
    } else {
      notify.error(`环境启动失败：${err.message}`)
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
    notify.success('已关闭上一个环境，本题环境启动成功')
    restartAutoSolvePolling()
  } catch (e) {
    // 接续启动仍 409（极端并发）：重新弹冲突框，让用户再处理一次。
    const err = e as ApiError
    if (err.status === 409 && err.data?.current) {
      promptInstanceConflict(err.data.current as AdminLabInstance)
    } else {
      notify.error(`环境启动失败：${err.message}`)
    }
  } finally {
    instanceLoading.value = false
  }
}

function promptInstanceConflict(current: AdminLabInstance) {
  const challengeName = current.challenge_title || current.challenge_slug || `题目 #${current.challenge_id}`
  dialogs.confirm(
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
        notify.error('该环境关联的题目已被删除，无法定位与关闭，请联系管理员在容器管理中清理')
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
    notify.error(`环境销毁失败：${(e as Error).message}`)
  } finally {
    instanceLoading.value = false
  }
}

async function extendInstance() {
  instanceLoading.value = true
  try {
    instance.value = await instanceApi.extend(slug())
    notify.success('已续期 1 小时')
  } catch (e) {
    notify.error(`续期失败：${(e as Error).message}`)
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
    notify.success(res.cost > 0 ? `已解锁，扣除 ${res.cost} 分（剩余 ${res.score}）` : '已解锁')
  } catch (e) {
    notify.error(`解锁失败：${(e as Error).message}`)
  } finally {
    unlockingId.value = null
  }
}

onMounted(load)
onUnmounted(() => {
  stopAutoSolvePolling()
  stopRunPolling()
})
</script>

<template>
  <div v-if="loading" class="challenge-skeleton" role="status" aria-label="正在加载题目">
    <div class="sk-head" aria-hidden="true">
      <span class="sk-title" />
      <span class="sk-tags"><i v-for="n in 4" :key="n" /></span>
    </div>
    <div class="sk-body" aria-hidden="true">
      <span v-for="n in 6" :key="n" class="sk-line" :class="{ 'sk-line--short': n === 3 || n === 6 }" />
    </div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="题目加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
  <div v-else-if="challenge" class="challenge-page">
    <PageHeader
      :title="challenge.title"
      :description="`${CATEGORY_LABELS[challenge.category] ?? challenge.category} · ${challenge.score} 分`"
    >
      <template #badges>
        <StatusTag kind="difficulty" :value="challenge.difficulty" />
        <span v-if="challenge.solved" class="challenge-badge" data-tone="success">已通关</span>
        <span v-if="challenge.has_env" class="challenge-badge">容器环境</span>
        <span v-if="challenge.judge" class="challenge-badge">
          编程题 · {{ challenge.judge.time_limit_s }}s / {{ challenge.judge.memory_limit_mb }}MB
        </span>
      </template>
      <template v-if="challenge.tutorial_slug && challenge.tutorial_topic_slug" #actions>
        <Button variant="outline" as-child>
          <RouterLink :to="`${tutorialBase}/${challenge.tutorial_topic_slug}/rooms/${challenge.tutorial_slug}`">
            配套教程：{{ challenge.tutorial_title || challenge.tutorial_slug }} →
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <div class="challenge-layout">
      <article class="challenge-main">
        <div class="description" v-html="renderedDescription" />

        <section v-if="samples.length" class="challenge-block" aria-labelledby="samples-title">
          <h2 id="samples-title" class="challenge-block__title">样例数据</h2>
          <div v-for="sample in samples" :key="sample.name" class="sample-group">
            <span class="sample-name">{{ sample.name }}</span>
            <div class="sample-io">
              <div class="sample-block">
                <span class="sample-label">输入</span>
                <pre>{{ sample.input }}</pre>
              </div>
              <div class="sample-block">
                <span class="sample-label">期望输出</span>
                <pre>{{ sample.output }}</pre>
              </div>
            </div>
          </div>
        </section>

        <section v-if="challenge.attachments?.length" class="challenge-block" aria-labelledby="attachments-title">
          <h2 id="attachments-title" class="challenge-block__title">附件</h2>
          <div v-for="attachment in challenge.attachments" :key="attachment.filename" class="attach-row">
            <span class="attach-name">{{ attachment.filename }}</span>
            <span class="attach-size">{{ fileSize(attachment.size) }}</span>
            <Button size="sm" variant="outline" :loading="downloadingFile === attachment.filename" @click="download(attachment.filename)">
              下载
            </Button>
          </div>
        </section>

        <section v-if="challenge.links?.length" class="challenge-block" aria-labelledby="links-title">
          <h2 id="links-title" class="challenge-block__title">相关链接</h2>
          <div class="links">
            <a
              v-for="link in challenge.links"
              :key="link.url"
              :href="link.url"
              target="_blank"
              rel="noopener noreferrer"
            >{{ link.name }} ↗</a>
          </div>
        </section>

        <section v-if="challenge.hints.length" class="challenge-block" aria-labelledby="hints-title">
          <h2 id="hints-title" class="challenge-block__title">提示</h2>
          <details v-for="hint in challenge.hints" :key="hint.id" class="hint-item">
            <summary class="hint-summary">
              <span class="hint-title">{{ hint.title }}</span>
              <span v-if="hint.unlocked" class="hint-cost" data-tone="success">已解锁</span>
              <span v-else class="hint-cost" data-tone="warning">
                {{ hint.cost > 0 ? `解锁 -${hint.cost} 分` : '免费解锁' }}
              </span>
            </summary>
            <div class="hint-body">
              <p v-if="hint.unlocked" class="hint-content">{{ hint.content }}</p>
              <div v-else class="hint-locked">
                <p class="hint-locked-text">提示内容已折叠，解锁后可见。</p>
                <Button
                  size="sm"
                  variant="outline"
                  :loading="unlockingId === hint.id"
                  @click.stop="unlockHint(hint.id)"
                >{{ hint.cost > 0 ? `花费 ${hint.cost} 分解锁` : '解锁提示' }}</Button>
              </div>
            </div>
          </details>
        </section>
      </article>

      <aside class="challenge-side">
        <section class="solve-panel" aria-labelledby="solve-title">
          <h2 id="solve-title" class="solve-panel__title">解答</h2>

          <div v-if="challenge.has_env" class="instance-row">
            <template v-if="instanceAlive">
              <StatusTag kind="container" :value="instance!.status" />
              <code v-if="instance!.endpoint" class="endpoint">{{ instance!.endpoint }}</code>
              <span v-if="instance!.expire_at" class="expire">到期 {{ formatTime(instance!.expire_at) }}</span>
              <Button
                v-if="instance!.status === 'running'"
                size="sm"
                variant="outline"
                :disabled="remainingExtends <= 0"
                :title="remainingExtends > 0 ? `还可续期 ${remainingExtends} 次（每次 1 小时）` : '续期次数已用完，请销毁后重新启动'"
                :loading="instanceLoading"
                @click="extendInstance"
              >续期{{ remainingExtends > 0 ? `（剩 ${remainingExtends} 次）` : '' }}</Button>
              <Button size="sm" variant="outline" :loading="instanceLoading" @click="destroyInstance">销毁环境</Button>
            </template>
            <Button v-else size="sm" :loading="instanceLoading" @click="startInstance">启动容器</Button>
          </div>

          <p v-if="challenge.solved" class="notice" data-tone="success" role="status">
            {{ isCode ? '本题已通关（分值已结算）。仍可重复提交源码验证，重复判对不再计分。' : '本题已通关（分值已结算）。仍可重复提交 flag 验证，重复判对不再计分。' }}
          </p>

          <template v-if="isCode">
            <p class="solve-hint">
              提交 yak 源码：对 {{ challenge.judge!.testdata_count }} 组数据逐组运行（时限 {{ challenge.judge!.time_limit_s }}s/组），
              全部通过才算 AC；配置了组分的题目按通过组折算得分。
            </p>
            <p v-if="engineMissing" class="notice engine-missing" data-tone="warning" role="alert">
              判题引擎未配置：提交代码与自测运行暂不可用，请联系管理员配置（ACADEMY_YAK_BIN）。
            </p>
            <div class="lib-policy">
              <div class="lib-row">
                <span class="lib-title">本题可用库</span>
                <template v-if="libPolicy.allowed">
                  <span v-for="lib in libPolicy.allowed" :key="lib" class="lib-chip" :title="libLabel(lib)">{{ lib }}</span>
                </template>
                <span v-else class="lib-all">全部 yak 标准库（黑名单除外）</span>
              </div>
              <p v-if="blockedLibsNote" class="lib-blocked">{{ blockedLibsNote }}——提交包含此类库的源码将被拒绝。</p>
            </div>

            <CodeEditor v-model="source" class="code-input" :disabled="polling" :lib-policy="libPolicy" />

            <div class="run-panel">
              <div class="run-bar">
                <select v-model="runTarget" class="run-select" :disabled="running" aria-label="自测输入">
                  <option v-for="sample in samples" :key="sample.name" :value="sample.name">样例 {{ sample.name }}</option>
                  <option :value="RUN_CUSTOM">自定义输入</option>
                </select>
                <Button size="sm" variant="outline" :disabled="!source || !runTarget || engineMissing" :loading="running" @click="runCode">
                  运行自测
                </Button>
              </div>
              <textarea
                v-if="runTarget === RUN_CUSTOM"
                v-model="customStdin"
                class="run-textarea"
                rows="4"
                placeholder="自定义输入（作为程序的标准输入，回车换行）"
              />
              <div v-if="runResult" class="run-output">
                <div class="run-meta">
                  <StatusTag v-if="runResult.status !== 'done'" kind="submission" value="pending" :label="runResult.status === 'pending' ? '排队中' : '运行中'" />
                  <template v-else>
                    <StatusTag kind="submission" :value="runResult.outcome === 'ok' ? 'correct' : 'incorrect'" :label="runOutcomeLabel" />
                    <span class="run-time">耗时 {{ runResult.time_ms }} ms</span>
                    <span v-if="runResult.mode === 'sample' && runResult.matches !== null" class="run-time">
                      {{ runResult.matches ? '与样例期望一致' : '与样例期望不一致' }}
                    </span>
                    <span v-if="runResult.truncated" class="run-time">输出超长已截断</span>
                  </template>
                </div>
                <pre v-if="runResult.stdout" class="run-stdout">{{ runResult.stdout }}</pre>
                <pre v-if="runResult.stderr" class="run-stderr">{{ runResult.stderr }}</pre>
              </div>
            </div>

            <Button class="solve-submit" :disabled="!source || engineMissing" :loading="submitting || polling" @click="submit">
              提交代码
            </Button>
          </template>

          <div v-else class="submit-row">
            <Input v-model="answer" placeholder="输入 flag" :disabled="polling" @keyup.enter="submit" />
            <Button :disabled="!answer" :loading="submitting || polling" @click="submit">提交</Button>
          </div>

          <p
            v-if="result"
            class="notice result-notice"
            :data-tone="result.status === 'correct' ? 'success' : 'danger'"
            :role="result.status === 'correct' ? 'status' : 'alert'"
          >{{ resultTitle }}</p>

          <div v-if="result?.groups?.length" class="group-results">
            <div
              v-for="group in result.groups"
              :key="group.name"
              class="group-row"
              :data-passed="group.passed ? 'true' : 'false'"
            >
              <span class="group-name">{{ group.sample ? '样例' : '测试' }}组 {{ group.name }}</span>
              <StatusTag
                kind="submission"
                :value="group.passed ? 'correct' : 'incorrect'"
                :label="group.passed ? '通过' : verdictLabels[group.verdict!] || '未通过'"
              />
              <span v-if="group.score > 0" class="group-score">{{ group.score }} 分</span>
            </div>
          </div>
        </section>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.challenge-page {
  min-width: 0;
}

.challenge-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  align-items: start;
  gap: var(--space-8);
}

.challenge-main {
  min-width: 0;
}

.challenge-side {
  position: sticky;
  top: calc(var(--space-12) + var(--space-2));
  min-width: 0;
}

.challenge-badge {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-weight: 500;
  white-space: nowrap;
}

.challenge-badge[data-tone='success'] {
  border-color: color-mix(in srgb, var(--status-success) 24%, transparent);
  background: color-mix(in srgb, var(--status-success) 10%, var(--bg-surface));
  color: var(--status-success);
}

.description {
  max-width: 72ch;
  color: var(--text-primary);
  font-size: var(--text-body);
  line-height: var(--leading-relaxed);
}

.description :deep(p) {
  margin: 0 0 var(--space-4);
}

.description :deep(h1),
.description :deep(h2),
.description :deep(h3) {
  margin: var(--space-6) 0 var(--space-3);
  font-weight: 600;
  line-height: 1.4;
  letter-spacing: 0;
}

.description :deep(h1) {
  font-size: var(--text-section);
}

.description :deep(h2) {
  font-size: var(--text-section);
}

.description :deep(h3) {
  font-size: var(--text-body);
}

.description :deep(ul),
.description :deep(ol) {
  margin: 0 0 var(--space-4);
  padding-left: var(--space-5);
}

.description :deep(li) {
  margin-bottom: var(--space-1);
}

.description :deep(blockquote) {
  margin: 0 0 var(--space-4);
  padding-left: var(--space-4);
  border-left: 2px solid var(--border);
  color: var(--text-secondary);
}

.description :deep(a) {
  color: var(--action-subtle-fg);
  text-decoration: underline;
  text-underline-offset: 3px;
}

.description :deep(pre) {
  padding: var(--space-4);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  overflow: auto;
}

.description :deep(code) {
  padding: var(--space-1) var(--space-1);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  font-size: var(--text-caption);
}

.description :deep(pre code) {
  padding: 0;
  background: none;
}

.description :deep(table) {
  width: 100%;
  margin: 0 0 var(--space-4);
  border-collapse: collapse;
  font-size: var(--text-caption);
}

.description :deep(th),
.description :deep(td) {
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  text-align: left;
}

.description :deep(th) {
  font-weight: 600;
}

.challenge-block {
  margin-top: var(--space-6);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}

.challenge-block__title {
  margin-bottom: var(--space-3);
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.sample-group + .sample-group {
  margin-top: var(--space-3);
}

.sample-name {
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
}

.sample-io {
  display: flex;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-top: var(--space-2);
}

.sample-block {
  flex: 1;
  min-width: 200px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
}

.sample-label {
  display: block;
  margin-bottom: var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-micro);
}

.sample-block pre {
  margin: 0;
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  white-space: pre-wrap;
  word-break: break-all;
}

.attach-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding-block: var(--space-1);
}

.attach-name {
  font-family: var(--font-mono);
  font-size: var(--text-caption);
  overflow-wrap: anywhere;
}

.attach-size {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.links {
  display: grid;
  gap: var(--space-2);
}

.links a {
  color: var(--action-subtle-fg);
  font-size: var(--text-caption);
}

.hint-item {
  border-top: 1px solid var(--border);
}

.hint-item:first-of-type {
  border-top: 0;
}

.hint-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding-block: var(--space-3);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 500;
  list-style: none;
}

.hint-summary::-webkit-details-marker {
  display: none;
}

.hint-cost {
  flex-shrink: 0;
  font-size: var(--text-meta);
  color: var(--text-tertiary);
}

.hint-cost[data-tone='warning'] {
  color: var(--status-warning);
}

.hint-cost[data-tone='success'] {
  color: var(--status-success);
}

.hint-body {
  padding-bottom: var(--space-3);
}

.hint-content {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.hint-locked {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.hint-locked-text {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.solve-panel {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.solve-panel__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.instance-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.endpoint {
  padding: var(--space-1) var(--space-1);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--action-subtle-fg);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.expire {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.notice {
  padding: var(--space-3);
  border-radius: var(--radius-control);
  font-size: var(--text-caption);
  line-height: var(--leading-normal);
}

.notice[data-tone='success'] {
  background: color-mix(in srgb, var(--status-success) 10%, var(--bg-surface));
  color: var(--status-success);
}

.notice[data-tone='warning'] {
  background: color-mix(in srgb, var(--status-warning) 12%, var(--bg-surface));
  color: var(--status-warning);
}

.notice[data-tone='danger'] {
  background: color-mix(in srgb, var(--status-danger) 10%, var(--bg-surface));
  color: var(--status-danger);
}

.solve-hint {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.lib-policy {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  font-size: var(--text-meta);
}

.lib-row {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.lib-title {
  margin-right: var(--space-1);
  color: var(--text-tertiary);
}

.lib-chip {
  padding: 0 var(--space-1);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
}

.lib-all {
  color: var(--text-primary);
}

.lib-blocked {
  margin-top: var(--space-1);
  color: var(--status-danger);
}

.run-panel {
  display: grid;
  gap: var(--space-2);
}

.run-bar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.run-select {
  min-height: 32px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-meta);
}

.run-textarea {
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  resize: vertical;
}

.run-output {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
}

.run-meta {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.run-time {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.run-output pre {
  margin: var(--space-1) 0 0;
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  white-space: pre-wrap;
  word-break: break-all;
}

.run-stderr {
  color: var(--status-danger);
}

.submit-row {
  display: flex;
  gap: var(--space-2);
}

.solve-submit {
  width: 100%;
  min-height: 44px;
  justify-content: center;
}

.group-results {
  display: grid;
  gap: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px dashed var(--border);
}

.group-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.group-name {
  min-width: 96px;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
}

.group-row[data-passed='false'] .group-name {
  color: var(--status-danger);
}

.group-score {
  margin-left: auto;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
}

.challenge-skeleton {
  max-width: 900px;
}

.sk-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-3);
  padding-bottom: var(--space-3);
  margin-bottom: var(--space-3);
  border-bottom: 1px solid var(--border);
}

.sk-title {
  width: min(42%, 320px);
  height: 24px;
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
}

.sk-tags {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.sk-tags i {
  width: 56px;
  height: 22px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-body {
  display: grid;
  gap: var(--space-3);
  max-width: 72ch;
  margin: var(--space-4) 0 var(--space-8);
}

.sk-line {
  height: 14px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-line--short {
  width: 62%;
}

@media (max-width: 1023px) {
  .challenge-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
  }

  .challenge-side {
    position: static;
  }
}

@media (max-width: 560px) {
  .solve-panel {
    padding: var(--space-4);
  }

  .submit-row {
    flex-direction: column;
  }

  .submit-row :deep([data-slot='button']) {
    min-height: 44px;
  }
}
</style>
