<script setup lang="ts">
// 考试参赛端（满屏独立布局）：服务端计时、单次作答、自动保存、交卷与
// 编程题判题反馈。与 AWD/CTF 参赛页完全独立，只共享竞赛入口元数据。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import {
  competitionExamApi,
  type ExamAnswerView,
  type ExamParticipantView,
  type ExamQuestionPublic,
  type ExamQuestionType,
} from '../../../api'
import { ApiError } from '../../../api/client'
import { useAuthStore } from '../../../stores/auth'
import { useCompetitionStore } from '../../../stores/competition'

const props = defineProps<{ competitionId: number }>()
const router = useRouter()
const auth = useAuthStore()
const compStore = useCompetitionStore()

const view = ref<ExamParticipantView | null>(null)
const loading = ref(true)
const loadError = ref('')
const starting = ref(false)
const submitting = ref(false)
const activeId = ref(0)
const answers = ref<Record<number, unknown>>({})
const answerMeta = ref<Record<number, ExamAnswerView>>({})
const saveState = ref<'idle' | 'saving' | 'saved' | 'error'>('idle')
const saving = ref<Record<number, boolean>>({})
const codeSubmitting = ref<Record<number, boolean>>({})
const nowTs = ref(Date.now())
const deadlineTs = ref(0)
const expiryPolled = ref(false)
const dirty = new Set<number>()
const timers = new Map<number, number>()
let tick: number | undefined
let poll: number | undefined

const TYPE_LABELS: Record<ExamQuestionType, string> = {
  single: '单选',
  multiple: '多选',
  truefalse: '判断',
  code: '编程',
}

const comp = computed(() => view.value?.competition ?? null)
const paper = computed(() => view.value?.paper ?? null)
const attempt = computed(() => view.value?.attempt ?? null)
const questions = computed(() => view.value?.questions ?? [])
const inProgress = computed(() => attempt.value?.status === 'in_progress')
const terminal = computed(() => attempt.value?.status === 'submitted' || attempt.value?.status === 'expired')
const remaining = computed(() => {
  if (!inProgress.value || !deadlineTs.value) return 0
  return Math.max(0, Math.floor((deadlineTs.value - nowTs.value) / 1000))
})
const countdown = computed(() => formatDuration(remaining.value))
const urgent = computed(() => inProgress.value && remaining.value <= 300)
const timePercent = computed(() => {
  const total = paper.value?.duration_seconds ?? 0
  if (!total) return 0
  return Math.min(100, Math.max(0, Math.round((remaining.value / total) * 100)))
})
const activeIndex = computed(() => questions.value.findIndex((q) => q.id === activeId.value))
const activeQuestion = computed(() => (activeIndex.value >= 0 ? questions.value[activeIndex.value] : null))
const answeredCount = computed(() => questions.value.filter((q) => isAnswered(q)).length)
const isAdmin = computed(() => auth.user?.global_role === 'admin')
const lockedHere = computed(
  () => comp.value?.status === 'running' && comp.value?.kind === 'invitational' && !isAdmin.value,
)
const platformHome = computed(() => (isAdmin.value ? '/admin/tutorials' : '/learn'))
const platformHomeLabel = computed(() => (isAdmin.value ? '返回管理端' : '返回平台'))
const saveStateLabel = computed(() => {
  switch (saveState.value) {
    case 'saving':
      return '自动保存中…'
    case 'saved':
      return '已自动保存'
    case 'error':
      return '保存失败，请重试'
    default:
      return ''
  }
})

function formatDuration(seconds: number) {
  if (seconds <= 0) return '00:00'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  const mm = String(m).padStart(2, '0')
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

function shortDateTime(value: string) {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

function md(value: string) {
  return DOMPurify.sanitize(marked.parse(value || '') as string)
}

function typeLabel(type: ExamQuestionType) {
  return TYPE_LABELS[type] ?? type
}

function answerOf(questionID: number) {
  return answers.value[questionID] ?? null
}

function metaOf(questionID: number) {
  return answerMeta.value[questionID]
}

function codeDraft(questionID: number) {
  const value = answers.value[questionID]
  return typeof value === 'string' ? value : ''
}

function isSelected(questionID: number, key: string) {
  const value = answers.value[questionID]
  return Array.isArray(value) && value.includes(key)
}

function isAnswered(question: ExamQuestionPublic) {
  const value = answers.value[question.id]
  switch (question.type) {
    case 'multiple':
      return Array.isArray(value) && value.length > 0
    case 'truefalse':
      return typeof value === 'boolean'
    case 'code':
      return typeof value === 'string' && value.trim().length > 0
    default:
      return typeof value === 'string' && value.length > 0
  }
}

function answerLabel(question: ExamQuestionPublic) {
  const value = answers.value[question.id]
  if (question.type === 'code') return value ? '已保存源码' : '未作答'
  if (question.type === 'truefalse') {
    if (typeof value !== 'boolean') return '未作答'
    return value ? '正确' : '错误'
  }
  if (Array.isArray(value)) return value.length ? value.join('、') : '未作答'
  if (typeof value === 'string' && value) {
    const option = question.options.find((item) => item.key === value)
    return option ? `${value}. ${option.text}` : value
  }
  return '未作答'
}

function applyView(next: ExamParticipantView, initial: boolean) {
  const previousAttemptID = view.value?.attempt?.id
  view.value = next
  const meta: Record<number, ExamAnswerView> = {}
  for (const answer of next.answers) meta[answer.question_id] = answer
  answerMeta.value = meta
  if (initial || next.attempt?.status !== 'in_progress') {
    const map: Record<number, unknown> = {}
    for (const answer of next.answers) map[answer.question_id] = answer.answer
    answers.value = map
  } else {
    const map = { ...answers.value }
    for (const answer of next.answers) {
      if (!dirty.has(answer.question_id) && !saving.value[answer.question_id]) {
        map[answer.question_id] = answer.answer
      }
    }
    answers.value = map
  }
  if (!next.questions.some((q) => q.id === activeId.value)) {
    activeId.value = next.questions[0]?.id ?? 0
  }
  if (next.attempt?.status === 'in_progress') {
    if (initial || previousAttemptID !== next.attempt.id) {
      deadlineTs.value = Date.now() + next.attempt.remaining_seconds * 1000
      expiryPolled.value = false
    }
  } else {
    deadlineTs.value = 0
  }
}

async function load(silent = false) {
  if (!silent) {
    loading.value = true
    loadError.value = ''
  }
  try {
    const next = await competitionExamApi.view(props.competitionId)
    applyView(next, !view.value)
    if (lockedHere.value) compStore.lock(props.competitionId)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return
    if (!silent || !view.value) {
      loadError.value = e instanceof Error ? e.message : '考试加载失败，请稍后重试'
    }
  } finally {
    if (!silent) loading.value = false
  }
}

async function startExam() {
  if (starting.value) return
  starting.value = true
  try {
    const next = await competitionExamApi.startAttempt(props.competitionId)
    applyView(next, true)
    notify.success('考试已开始')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    starting.value = false
  }
}

function scheduleSave(questionID: number) {
  dirty.add(questionID)
  saveState.value = 'saving'
  const existing = timers.get(questionID)
  if (existing) window.clearTimeout(existing)
  timers.set(
    questionID,
    window.setTimeout(() => {
      timers.delete(questionID)
      void saveQuestion(questionID)
    }, 500),
  )
}

async function saveQuestion(questionID: number) {
  if (!dirty.has(questionID)) return
  dirty.delete(questionID)
  saving.value = { ...saving.value, [questionID]: true }
  try {
    const answer = await competitionExamApi.saveAnswer(
      props.competitionId,
      questionID,
      answers.value[questionID] ?? null,
    )
    answerMeta.value = { ...answerMeta.value, [questionID]: answer }
    saveState.value = 'saved'
  } catch (e) {
    saveState.value = 'error'
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    saving.value = { ...saving.value, [questionID]: false }
  }
}

function setAnswer(questionID: number, value: unknown) {
  answers.value = { ...answers.value, [questionID]: value }
  scheduleSave(questionID)
}

function onSingle(questionID: number, value: unknown) {
  setAnswer(questionID, value)
}

function onMultiple(questionID: number, key: string, checked: boolean) {
  const current = Array.isArray(answers.value[questionID])
    ? [...(answers.value[questionID] as string[])]
    : []
  const next = checked ? [...current.filter((item) => item !== key), key] : current.filter((item) => item !== key)
  next.sort()
  setAnswer(questionID, next)
}

function onBoolean(questionID: number, value: unknown) {
  if (typeof value !== 'boolean') return
  setAnswer(questionID, value)
}

function onCodeDraft(questionID: number, value: string) {
  setAnswer(questionID, value)
}

async function submitCode(questionID: number) {
  if (codeSubmitting.value[questionID]) return
  const source = codeDraft(questionID).trim()
  if (!source) {
    notify.warning('请先编写代码')
    return
  }
  const timer = timers.get(questionID)
  if (timer) {
    window.clearTimeout(timer)
    timers.delete(questionID)
  }
  await saveQuestion(questionID)
  codeSubmitting.value = { ...codeSubmitting.value, [questionID]: true }
  try {
    const receipt = await competitionExamApi.submitCode(props.competitionId, questionID, source)
    answerMeta.value = {
      ...answerMeta.value,
      [questionID]: {
        ...(answerMeta.value[questionID] ?? { question_id: questionID, answer: source }),
        code_submission_id: receipt.submission_id,
        graded: false,
        correct: false,
        score: 0,
      },
    }
    notify.success('代码已提交判题，结果稍后刷新')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    codeSubmitting.value = { ...codeSubmitting.value, [questionID]: false }
  }
}

async function submitExam() {
  if (!inProgress.value || submitting.value) return
  await dialogs.confirm('交卷后不可修改答案，确认交卷？', '确认交卷', { confirmButtonText: '交卷' })
  submitting.value = true
  try {
    for (const timer of timers.values()) window.clearTimeout(timer)
    timers.clear()
    const pending = Array.from(dirty)
    await Promise.all(pending.map((questionID) => saveQuestion(questionID)))
    const next = await competitionExamApi.submitAttempt(props.competitionId)
    applyView(next, true)
    notify.success('已交卷')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

async function logout() {
  await auth.logout()
  router.replace('/login')
}

function returnToPlatform() {
  router.push(platformHome.value)
}

onMounted(async () => {
  await load()
  tick = window.setInterval(() => {
    nowTs.value = Date.now()
    if (inProgress.value && remaining.value === 0 && !expiryPolled.value) {
      expiryPolled.value = true
      void load(true)
    }
  }, 1000)
  poll = window.setInterval(() => {
    if (!loading.value) void load(true)
  }, 10000)
})

onUnmounted(() => {
  if (tick) window.clearInterval(tick)
  if (poll) window.clearInterval(poll)
  for (const timer of timers.values()) window.clearTimeout(timer)
  timers.clear()
})
</script>

<template>
  <div class="exam-home">
    <header class="exam-header">
      <div class="exam-header-inner">
        <div class="exam-identity">
          <span class="exam-mark" aria-hidden="true">
            <svg width="22" height="24" viewBox="0 0 27 30" fill="none">
              <path
                d="M3 4L13.5 12.5L24 4M13.5 12.5V27M3 12L9 17M24 12L18 17"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="square"
              />
            </svg>
          </span>
          <div class="exam-title">
            <p class="exam-eyebrow">YAK ACADEMY · 考试</p>
            <div class="exam-name-row">
              <h1>{{ comp?.name ?? '在线考试' }}</h1>
              <UiTag v-if="comp" size="small" :type="comp.status === 'running' ? 'success' : 'info'">
                {{ comp.status === 'running' ? '进行中' : comp.status === 'ended' ? '已结束' : '未开始' }}
              </UiTag>
              <UiTag v-if="comp" size="small" type="info">{{ comp.kind === 'public' ? '公开赛' : '邀请赛' }}</UiTag>
              <UiTag v-if="attempt" size="small" :type="attempt.status === 'submitted' ? 'success' : attempt.status === 'expired' ? 'warning' : 'info'">
                {{ attempt.status === 'submitted' ? '已交卷' : attempt.status === 'expired' ? '超时自动交卷' : '作答中' }}
              </UiTag>
            </div>
            <p v-if="comp" class="exam-window mono">
              {{ shortDateTime(comp.start_time) }} → {{ shortDateTime(comp.end_time) }}
            </p>
          </div>
        </div>

        <div v-if="inProgress" class="exam-clock">
          <div class="exam-clock-line">
            <span>剩余时间</span>
            <b class="mono" :class="{ urgent }">{{ countdown }}</b>
          </div>
          <UiProgress :percentage="timePercent" :show-text="false" />
          <span class="exam-clock-sub">到期自动交卷 · 已答 {{ answeredCount }}/{{ questions.length }}</span>
        </div>
        <div v-else-if="paper" class="exam-clock exam-clock-quiet">
          <div class="exam-clock-line">
            <span>考试时长</span>
            <b class="mono">{{ Math.round(paper.duration_seconds / 60) }} 分钟</b>
          </div>
          <span class="exam-clock-sub">共 {{ questions.length }} 题 · 总分 {{ paper.total_score }}</span>
        </div>

        <div class="exam-user">
          <span class="exam-user-name">{{ auth.user?.username }}</span>
          <UiButton v-if="!lockedHere" size="small" variant="ghost" @click="returnToPlatform">
            {{ platformHomeLabel }}
          </UiButton>
          <UiButton size="small" variant="ghost" @click="logout">登出</UiButton>
        </div>
      </div>
    </header>

    <UiAlert
      v-if="loadError"
      type="error"
      :closable="false"
      :title="loadError"
      description="当前显示上一次成功加载的数据。"
      class="exam-refresh-error"
    >
      <UiButton size="small" @click="load()">重试</UiButton>
    </UiAlert>

    <main v-if="loading && !view" class="exam-body">
      <UiSkeleton :rows="7" />
    </main>
    <main v-else-if="loadError && !view" class="exam-body exam-center">
      <UiLoadError title="无法加载考试" :message="loadError" @retry="load()" />
    </main>

    <main v-else-if="view && !attempt" class="exam-body exam-intro">
      <UiCard header="考前须知">
        <h2 class="exam-intro-title">{{ view.paper.title }}</h2>
        <div v-if="view.paper.description" class="exam-md" v-html="md(view.paper.description)" />
        <div class="exam-stats">
          <div class="exam-stat">
            <span>考试时长</span>
            <b>{{ Math.round(view.paper.duration_seconds / 60) }} 分钟</b>
          </div>
          <div class="exam-stat">
            <span>题目数量</span>
            <b>{{ view.questions.length }} 题</b>
          </div>
          <div class="exam-stat">
            <span>试卷总分</span>
            <b>{{ view.paper.total_score }}</b>
          </div>
          <div class="exam-stat">
            <span>通过分</span>
            <b>{{ view.paper.pass_score || '不限' }}</b>
          </div>
        </div>
        <UiAlert
          v-if="view.competition.status !== 'running'"
          type="warning"
          :closable="false"
          :title="view.competition.status === 'ended' ? '考试已结束' : '考试尚未开始'"
          :description="view.competition.status === 'ended' ? '本场考试已结束，无法开始作答。' : '考试开始后可点击下方按钮作答。'"
        />
        <UiAlert
          v-else-if="!view.questions.length"
          type="warning"
          :closable="false"
          title="试卷尚未就绪"
          description="管理员尚未添加试题，请稍后再试。"
        />
        <div class="exam-intro-actions">
          <UiButton
            type="primary"
            :loading="starting"
            :disabled="starting || view.competition.status !== 'running' || !view.questions.length"
            @click="startExam"
          >
            开始考试
          </UiButton>
          <span class="exam-hint">单次作答；开始后按服务端计时，到期自动交卷。</span>
        </div>
      </UiCard>
    </main>

    <main v-else-if="view && inProgress" class="exam-body exam-workspace">
      <aside class="exam-nav">
        <UiCard header="答题卡">
          <div class="exam-nav-grid">
            <button
              v-for="(question, index) in questions"
              :key="question.id"
              type="button"
              class="exam-nav-item"
              :class="{ active: question.id === activeId, answered: isAnswered(question) }"
              :aria-label="`第 ${index + 1} 题${isAnswered(question) ? '（已答）' : ''}`"
              @click="activeId = question.id"
            >
              {{ index + 1 }}
            </button>
          </div>
          <div class="exam-nav-meta">
            <span>已答 {{ answeredCount }}/{{ questions.length }}</span>
            <span class="exam-save-state">{{ saveStateLabel }}</span>
          </div>
          <UiButton type="primary" class="exam-submit" :loading="submitting" :disabled="submitting" @click="submitExam">
            交卷
          </UiButton>
          <p class="exam-hint">交卷后不可修改；到期将自动交卷。</p>
        </UiCard>
      </aside>

      <section v-if="activeQuestion" class="exam-question">
        <UiCard :header="`第 ${activeIndex + 1} 题 · ${typeLabel(activeQuestion.type)} · ${activeQuestion.score} 分`">
          <h3 class="exam-q-title">{{ activeQuestion.title }}</h3>
          <div v-if="activeQuestion.content" class="exam-md" v-html="md(activeQuestion.content)" />

          <UiRadioGroup
            v-if="activeQuestion.type === 'single'"
            :model-value="answerOf(activeId)"
            class="exam-options"
            @update:model-value="(value) => onSingle(activeId, value)"
          >
            <UiRadio v-for="option in activeQuestion.options" :key="option.key" :value="option.key">
              {{ option.key }}. {{ option.text }}
            </UiRadio>
          </UiRadioGroup>

          <div v-else-if="activeQuestion.type === 'multiple'" class="exam-options exam-options-col">
            <UiCheckbox
              v-for="option in activeQuestion.options"
              :key="option.key"
              :model-value="isSelected(activeId, option.key)"
              @update:model-value="(checked) => onMultiple(activeId, option.key, checked)"
            >
              {{ option.key }}. {{ option.text }}
            </UiCheckbox>
          </div>

          <UiRadioGroup
            v-else-if="activeQuestion.type === 'truefalse'"
            :model-value="answerOf(activeId)"
            class="exam-options"
            @update:model-value="(value) => onBoolean(activeId, value)"
          >
            <UiRadio :value="true">正确</UiRadio>
            <UiRadio :value="false">错误</UiRadio>
          </UiRadioGroup>

          <template v-else>
            <UiInput
              type="textarea"
              :rows="14"
              class="mono exam-code"
              :model-value="codeDraft(activeId)"
              placeholder="在此编写 yak 代码，支持自动保存草稿…"
              @update:model-value="(value) => onCodeDraft(activeId, value)"
            />
            <div class="exam-code-meta">
              <span v-if="metaOf(activeId)?.code_submission_id" class="exam-dim mono">
                提交 #{{ metaOf(activeId)?.code_submission_id }}
              </span>
              <UiTag v-if="metaOf(activeId)?.graded" :type="metaOf(activeId)?.correct ? 'success' : 'danger'">
                {{ metaOf(activeId)?.correct ? '通过' : '未通过' }} · {{ metaOf(activeId)?.score }} 分
              </UiTag>
              <UiTag v-else-if="metaOf(activeId)?.code_submission_id" type="info">判定中</UiTag>
              <UiButton
                :loading="codeSubmitting[activeId]"
                :disabled="codeSubmitting[activeId] || !codeDraft(activeId).trim()"
                @click="submitCode(activeId)"
              >
                提交判题
              </UiButton>
            </div>
            <p class="exam-hint">可多次提交，以最新一次提交的判定结果计分。</p>
          </template>

          <div class="exam-q-foot">
            <span class="exam-save-state">{{ saveStateLabel }}</span>
            <span class="exam-dim">答案自动保存</span>
          </div>
        </UiCard>
      </section>
    </main>

    <main v-else-if="view && terminal" class="exam-body exam-result">
      <UiCard header="考试结果">
        <div class="exam-result-head">
          <div class="exam-result-score-wrap">
            <p class="exam-result-label">总分</p>
            <div>
              <b class="exam-result-score">{{ attempt?.total_score ?? 0 }}</b>
              <span class="exam-dim"> / {{ view.paper.total_score }}</span>
            </div>
          </div>
          <UiTag :type="attempt?.passed ? 'success' : 'warning'" size="medium">
            {{ attempt?.passed ? '通过' : '未通过' }}
          </UiTag>
        </div>
        <div class="exam-stats">
          <div class="exam-stat">
            <span>客观题</span>
            <b>{{ attempt?.objective_score ?? 0 }}</b>
          </div>
          <div class="exam-stat">
            <span>编程题</span>
            <b>{{ attempt?.code_score ?? 0 }}</b>
          </div>
          <div class="exam-stat">
            <span>通过分</span>
            <b>{{ view.paper.pass_score || '不限' }}</b>
          </div>
          <div class="exam-stat">
            <span>交卷时间</span>
            <b class="exam-stat-time">{{ fmt(attempt?.submitted_at) }}</b>
          </div>
        </div>
        <UiAlert
          v-if="attempt?.auto_submitted"
          type="warning"
          :closable="false"
          title="超时自动交卷"
          description="考试时间结束，系统已按到期时的作答自动交卷。"
          class="exam-result-tip"
        />
        <div class="exam-result-actions">
          <router-link to="/competitions"><UiButton>返回竞赛广场</UiButton></router-link>
          <span class="exam-hint">
            编程题判定可在结果页刷新后更新；成绩以管理员最终公示为准。
          </span>
        </div>
      </UiCard>

      <UiCard header="答题回顾">
        <div class="exam-review-list">
          <article v-for="(question, index) in questions" :key="question.id" class="exam-review-item">
            <div class="exam-review-head">
              <div>
                <span class="exam-review-index">第 {{ index + 1 }} 题</span>
                <span class="exam-dim">{{ typeLabel(question.type) }} · {{ question.score }} 分</span>
              </div>
              <div class="exam-review-score">
                <UiTag v-if="metaOf(question.id)?.graded" :type="metaOf(question.id)?.correct ? 'success' : 'danger'" size="small">
                  {{ metaOf(question.id)?.score ?? 0 }} 分
                </UiTag>
                <UiTag v-else type="info" size="small">
                  {{ question.type === 'code' && metaOf(question.id)?.code_submission_id ? '判定中' : '未作答' }}
                </UiTag>
              </div>
            </div>
            <h4 class="exam-review-title">{{ question.title }}</h4>
            <p class="exam-review-answer">
              我的答案：<b>{{ answerLabel(question) }}</b>
            </p>
            <div v-if="question.explanation" class="exam-review-explanation">
              <span class="exam-dim">解析</span>
              <div class="exam-md" v-html="md(question.explanation)" />
            </div>
          </article>
        </div>
      </UiCard>
    </main>
  </div>
</template>

<style scoped>
.exam-home { min-height: 100vh; background: var(--background); }
.exam-header {
  position: sticky;
  top: 0;
  z-index: 20;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--background) 92%, transparent);
  backdrop-filter: blur(10px);
}
.exam-header-inner {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: var(--space-5);
  width: min(100%, 1280px);
  margin-inline: auto;
  padding: var(--space-3) var(--space-5);
}
.exam-identity { display: flex; align-items: center; gap: var(--space-3); min-width: 0; }
.exam-mark {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--primary);
  background: var(--card);
  flex: none;
}
.exam-title { min-width: 0; }
.exam-eyebrow {
  margin: 0 0 2px;
  font-size: var(--text-xs);
  letter-spacing: 0.12em;
  color: var(--muted-foreground);
  text-transform: uppercase;
}
.exam-name-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.exam-name-row h1 { margin: 0; font-size: var(--text-lg); font-weight: 600; line-height: 1.3; }
.exam-window { margin: 3px 0 0; font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-clock {
  min-width: 240px;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--card);
}
.exam-clock-quiet { opacity: 0.85; }
.exam-clock-line { display: flex; align-items: baseline; justify-content: space-between; gap: var(--space-3); font-size: var(--text-sm); }
.exam-clock-line b { font-size: var(--text-xl); font-variant-numeric: tabular-nums; }
.exam-clock-line b.urgent { color: var(--destructive); }
.exam-clock-sub { display: block; margin-top: var(--space-1); font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-user { display: flex; align-items: center; justify-content: flex-end; gap: var(--space-2); min-width: 0; }
.exam-user-name { font-size: var(--text-sm); color: var(--muted-foreground); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.exam-refresh-error { width: min(100%, 1280px); margin: var(--space-4) auto 0; }
.exam-body {
  width: min(100%, 1280px);
  margin-inline: auto;
  padding: var(--space-6) var(--space-5) var(--space-10);
}
.exam-center { display: grid; place-items: center; min-height: 50vh; }
.exam-intro { max-width: 880px; }
.exam-intro-title { margin: 0 0 var(--space-3); font-size: var(--text-2xl); font-weight: 600; }
.exam-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
  margin: var(--space-5) 0;
}
.exam-stat {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--muted);
}
.exam-stat span { font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-stat b { font-size: var(--text-lg); font-weight: 600; }
.exam-stat-time { font-size: var(--text-sm); }
.exam-intro-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); margin-top: var(--space-5); }
.exam-hint { margin: 0; font-size: var(--text-xs); color: var(--muted-foreground); line-height: 1.6; }
.exam-workspace { display: grid; grid-template-columns: 260px minmax(0, 1fr); gap: var(--space-5); align-items: start; }
.exam-nav { position: sticky; top: 92px; }
.exam-nav-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: var(--space-2); }
.exam-nav-item {
  display: grid;
  place-items: center;
  aspect-ratio: 1;
  min-height: 36px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--card);
  color: var(--foreground);
  font-size: var(--text-sm);
  font-variant-numeric: tabular-nums;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.exam-nav-item:hover { border-color: var(--primary); }
.exam-nav-item.answered { border-color: var(--primary); background: color-mix(in srgb, var(--primary) 12%, var(--card)); }
.exam-nav-item.active { border-color: var(--primary); box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary) 30%, transparent); }
.exam-nav-meta { display: flex; justify-content: space-between; gap: var(--space-2); margin-top: var(--space-4); font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-submit { width: 100%; margin-top: var(--space-3); }
.exam-q-title { margin: 0 0 var(--space-3); font-size: var(--text-lg); font-weight: 600; }
.exam-options { margin-top: var(--space-4); }
.exam-options-col { display: grid; gap: var(--space-3); }
.exam-code { margin-top: var(--space-4); }
.exam-code-meta { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-3); }
.exam-q-foot { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); margin-top: var(--space-5); padding-top: var(--space-3); border-top: 1px solid var(--border); }
.exam-save-state { font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-result { display: grid; gap: var(--space-5); max-width: 960px; }
.exam-result-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); }
.exam-result-label { margin: 0 0 var(--space-1); font-size: var(--text-xs); color: var(--muted-foreground); }
.exam-result-score { font-size: 56px; line-height: 1; font-weight: 700; font-variant-numeric: tabular-nums; }
.exam-result-tip { margin-top: var(--space-2); }
.exam-result-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); margin-top: var(--space-4); }
.exam-review-list { display: grid; gap: var(--space-4); }
.exam-review-item { padding: var(--space-3) 0; border-bottom: 1px solid var(--border); }
.exam-review-item:last-child { border-bottom: none; padding-bottom: 0; }
.exam-review-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); }
.exam-review-index { margin-right: var(--space-2); font-size: var(--text-sm); font-weight: 600; }
.exam-review-title { margin: var(--space-2) 0; font-size: var(--text-base); font-weight: 600; }
.exam-review-answer { margin: 0 0 var(--space-2); font-size: var(--text-sm); color: var(--muted-foreground); }
.exam-review-explanation { margin-top: var(--space-2); }
.exam-review-explanation > .exam-dim { display: block; margin-bottom: var(--space-1); font-size: var(--text-xs); }
.exam-md { color: var(--foreground); font-size: var(--text-sm); line-height: 1.75; min-width: 0; }
.exam-md :deep(p) { margin: 0 0 var(--space-3); }
.exam-md :deep(pre) { padding: var(--space-3); border-radius: var(--radius-md); background: var(--muted); overflow: auto; }
.exam-md :deep(code) { font-family: var(--font-mono); font-size: 0.9em; }
.exam-md :deep(ul), .exam-md :deep(ol) { padding-left: 1.4em; margin: 0 0 var(--space-3); }
.exam-dim { color: var(--muted-foreground); font-size: var(--text-xs); }
@media (max-width: 900px) {
  .exam-header-inner { grid-template-columns: 1fr; gap: var(--space-3); }
  .exam-clock { min-width: 0; }
  .exam-user { justify-content: flex-start; }
  .exam-workspace { grid-template-columns: 1fr; }
  .exam-nav { position: static; }
  .exam-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
