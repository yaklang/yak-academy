<script setup lang="ts">
// 竞赛首页（参赛者满屏视图；邀请赛锁定期间的全部可用功能）。
// 邀请赛锁定期间头部不出现平台导航；公开赛/管理员/结束后提供「返回平台」入口。
// 题目抽屉内完成开容器/提 flag（复用题库详情交互模式），异步判题后延时刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import type { UploadFile } from '@/components/system/composite'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import {
  CATEGORY_LABELS,
  DIFFICULTY_LABELS,
  competitionApi,
  instanceApi,
  type CompetitionChallengeItem,
  type CompetitionHomeView,
  type LabInstance,
  type MyWriteupItem,
  type TeamLeaderboardEntry,
} from '../api'
import { useAuthStore } from '../stores/auth'
import { useCompetitionStore } from '../stores/competition'
import { ApiError } from '../api/client'
import CompetitionOverview from '../components/competition/CompetitionOverview.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const compStore = useCompetitionStore()

const compId = Number(route.params.id)
const view = ref<CompetitionHomeView | null>(null)
const loading = ref(true)
const activeTab = ref('overview')
const loadError = ref('')

// 倒计时
const nowTs = ref(Date.now())
let tick: number | undefined
const clock = window.setInterval(() => (nowTs.value = Date.now()), 1000)

const comp = computed(() => view.value?.competition ?? null)
const ended = computed(() => comp.value?.status === 'ended')
const running = computed(() => comp.value?.status === 'running')
const countdown = computed(() => {
  if (!comp.value) return ''
  if (comp.value.status === 'scheduled') return '未开始'
  if (comp.value.status === 'ended') return '已结束'
  const ms = new Date(comp.value.end_time).getTime() - nowTs.value
  if (ms <= 0) return '已结束'
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = Math.floor((ms % 60_000) / 1000)
  return h > 24 ? `${Math.floor(h / 24)} 天 ${h % 24} 小时` : `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
})
// 赛程进度：0 = 未开始，100 = 已结束；用于头部与 KPI 的进度条。
const elapsedPercent = computed(() => {
  if (!comp.value) return 0
  const start = new Date(comp.value.start_time).getTime()
  const end = new Date(comp.value.end_time).getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return 0
  const p = ((nowTs.value - start) / (end - start)) * 100
  return Math.min(100, Math.max(0, Math.round(p)))
})
const kindLabel = computed(() => (comp.value?.kind === 'public' ? '公开赛' : '邀请赛'))
// 当前页面是否处于「锁定」态：仅进行中的邀请赛锁普通参赛者；公开赛与管理员不锁。
const isAdmin = computed(() => auth.user?.global_role === 'admin')
const lockedHere = computed(() => running.value && comp.value?.kind === 'invitational' && !isAdmin.value)
// 非锁定态提供返回入口：公开赛回平台，管理员回管理端，结束后的邀请赛也回平台。
const platformHome = computed(() => (isAdmin.value ? '/admin/tutorials' : '/learn'))
const platformHomeLabel = computed(() => (isAdmin.value ? '返回管理端' : '返回平台'))
function returnToPlatform() {
  router.push(platformHome.value)
}
// 头部只展示到分钟：秒级精度留给倒计时，避免赛程行过长。
function shortDateTime(value: string) {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function loadView() {
  try {
    view.value = await competitionApi.view(compId)
  } catch (e) {
    // 登出时在途的轮询请求会以 401 返回：此时导航已由 auth.logout() 接管，
    // 这里再跳转会把刚打开的 /login 覆盖成首页（登出后停在 landing）。
    if (e instanceof ApiError && e.status === 401) return
    // 未入场 / 竞赛不存在：回到入场页或平台。
    loadError.value = (e as Error).message
    notify.error(loadError.value)
    if (compStore.lockedCompetitionId === compId) compStore.unlock()
    router.replace('/')
  }
}

async function refreshAfterEnd() {
  // 比赛结束瞬间：解除锁定态并刷新（可访问平台 + 可交 WP）。
  compStore.unlock()
  await loadView()
  notify.info('比赛已结束，感谢参与！可提交 Writeup 或返回平台。')
}

// 提交后统一刷新：概览聚合与排行榜必须同拍更新，避免名次/得分不同步。
async function refreshAll() {
  await loadView()
  await loadBoard()
}

// ---------- 题目抽屉 ----------
const drawerOpen = ref(false)
const drawer = ref<CompetitionChallengeItem | null>(null)
const instance = ref<LabInstance | null>(null)
const starting = ref(false)
const answer = ref('')
const submitting = ref(false)

const renderedDescription = computed(() =>
  DOMPurify.sanitize(marked.parse(drawer.value?.description_md ?? '') as string),
)

async function openChallenge(item: CompetitionChallengeItem) {
  drawer.value = item
  drawerOpen.value = true
  answer.value = ''
  instance.value = null
  // 已通关且环境在跑的场景由"启动容器"自幂等返回覆盖，无需预取。
}

async function startInstance() {
  if (!drawer.value) return
  starting.value = true
  try {
    instance.value = await competitionApi.startInstance(compId, drawer.value.slug)
    notify.success(`容器已启动：${instance.value.endpoint}`)
    await loadView()
  } catch (e) {
    const err = e as { code?: string; message?: string; data?: Record<string, unknown> | null }
    if (err.code === 'conflict') {
      const current = err.data?.current as { challenge_title?: string; expire_at?: string } | undefined
      notify.warning(
        `已有活跃容器（${current?.challenge_title ?? '其他题目'}），请先关闭后再启动本题容器`,
      )
    } else {
      notify.error(err.message ?? '启动失败')
    }
  } finally {
    starting.value = false
  }
}

async function destroyInstance() {
  if (!drawer.value) return
  try {
    await dialogs.confirm('销毁本题容器？题目进度（flag）不受影响，可重新启动。', '销毁确认', { type: 'warning' })
  } catch {
    return
  }
  starting.value = true
  try {
    await instanceApi.destroy(drawer.value.slug)
    instance.value = null
    notify.success('容器已销毁')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    starting.value = false
  }
}

async function submitFlag() {
  if (!drawer.value || !answer.value.trim()) {
    notify.warning('请输入 flag')
    return
  }
  submitting.value = true
  try {
    await competitionApi.submitFlag(compId, drawer.value.slug, answer.value.trim())
    notify.success('已提交，异步判定中…')
    answer.value = ''
    // 异步判题：稍后刷新概览 + 排行榜（题目状态、得分曲线、名次都会变）。
    setTimeout(() => void refreshAll(), 1200)
    setTimeout(() => void refreshAll(), 3000)
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

// ---------- 排行榜 ----------
// 概览的"当前排名"与排行榜 tab 共用同一份数据；默认按参赛形态选模式
// （组队 → 战队榜，个人 → 个人榜），用户切换后保持一致。
const boardMode = ref<'team' | 'individual'>('team')
const board = ref<TeamLeaderboardEntry[]>([])
const boardLoading = ref(false)
const boardError = ref('')

async function loadBoard() {
  boardLoading.value = true
  boardError.value = ''
  try {
    const resp = await competitionApi.leaderboard(compId, boardMode.value)
    board.value = resp.items
  } catch (e) {
    board.value = []
    boardError.value = (e as Error).message
  } finally {
    boardLoading.value = false
  }
}

async function onBoardMode() {
  await loadBoard()
}

function openBoard() {
  // tab-change 会触发一次加载，这里只负责切页，避免重复请求。
  activeTab.value = 'leaderboard'
}

// ---------- 题目筛选 ----------
const challengeSearch = ref('')
const challengeCategory = ref('')
const challengeStatus = ref<'all' | 'solved' | 'unsolved'>('all')

const categories = computed(() => {
  const seen = new Set<string>()
  for (const c of view.value?.challenges ?? []) seen.add(c.category)
  return [...seen]
})

const filteredChallenges = computed(() => {
  const q = challengeSearch.value.trim().toLowerCase()
  return (view.value?.challenges ?? []).filter((c) => {
    if (q && !c.title.toLowerCase().includes(q) && !c.slug.toLowerCase().includes(q)) return false
    if (challengeCategory.value && c.category !== challengeCategory.value) return false
    if (challengeStatus.value === 'solved' && !c.solved) return false
    if (challengeStatus.value === 'unsolved' && c.solved) return false
    return true
  })
})

const challengeEmptyText = computed(() => {
  if ((view.value?.challenges ?? []).length === 0) return '本竞赛还没有绑定题目'
  return '没有符合筛选条件的题目'
})

function solveRate(c: CompetitionChallengeItem) {
  const total = view.value?.analytics.participant_count ?? 0
  return total > 0 ? Math.round((c.solve_count / total) * 100) : 0
}

function difficultyLabel(value: string) {
  return DIFFICULTY_LABELS[value as keyof typeof DIFFICULTY_LABELS] ?? value
}

const boardMaxScore = computed(() => Math.max(1, ...board.value.map((e) => e.score)))
function boardPercent(score: number) {
  return score > 0 ? Math.max(3, Math.round((score / boardMaxScore.value) * 100)) : 0
}

// ---------- WP（按题目分次：文字 + 附带 zip，全队一份，可反复修改） ----------
const wps = ref<MyWriteupItem[]>([])
const wpSaving = ref(false)
const wpUploading = ref(0) // challengeId → 上传中标记

async function loadWps() {
  try {
    const resp = await competitionApi.myWriteups(compId)
    wps.value = resp.items
    // 打开卡片时用已提交内容初始化草稿；用户改过的草稿不覆盖。
    for (const w of resp.items) {
      if (!(w.challenge_id in wpDrafts.value)) {
        wpDrafts.value[w.challenge_id] = w.content_md
      }
    }
  } catch (e) {
    notify.error((e as Error).message)
  }
}

function wpOf(challengeId: number): MyWriteupItem | undefined {
  return wps.value.find((w) => w.challenge_id === challengeId)
}

// 编辑态：challengeId → 草稿文字（loadWps 时从已提交内容初始化）。
const wpDrafts = ref<Record<number, string>>({})

async function saveWp(item: CompetitionChallengeItem) {
  const content = wpDrafts.value[item.id] ?? ''
  const existing = wpOf(item.id)
  if (!content.trim() && !existing?.zip_name) {
    notify.warning('请填写 WP 文字或上传 zip（至少一项）')
    return
  }
  wpSaving.value = true
  try {
    await competitionApi.saveWriteup(compId, item.id, content)
    notify.success(`「${item.title}」WP 已提交（可继续修改）`)
    await loadWps()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    wpSaving.value = false
  }
}

async function uploadWpZip(item: CompetitionChallengeItem, file: File) {
  if (!file.name.toLowerCase().endsWith('.zip')) {
    notify.warning('仅支持 zip 文件')
    return
  }
  wpUploading.value = item.id
  try {
    const resp = await competitionApi.uploadWriteupZip(compId, item.id, file)
    notify.success(`「${item.title}」zip 已上传：${resp.zip_name}（可重新上传替换）`)
    await loadWps()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    wpUploading.value = 0
  }
}

async function onTab(name: string | number) {
  if (name === 'leaderboard') await loadBoard()
  if (name === 'writeup' && ended.value) await loadWps()
}

async function logout() {
  await auth.logout()
  router.replace('/login')
}

onMounted(async () => {
  await loadView()
  loading.value = false
  // 未组队时个人榜才是自己的名次；有队伍则默认看战队榜。
  if (view.value && !view.value.team_name) boardMode.value = 'individual'
  // 仅邀请赛参赛者保持锁定标记（刷新/登出重登后守卫仍能引导回此页）；
  // 公开赛与管理员不锁，清除可能残留的本竞赛锁定缓存。
  if (lockedHere.value) compStore.lock(compId)
  else if (compStore.lockedCompetitionId === compId) compStore.unlock()
  if (view.value) await loadBoard()
  // 双节拍：秒级推进倒计时；周期检查比赛是否结束（结束后解锁并提示可交 WP）。
  tick = window.setInterval(() => {
    void checkEnded()
  }, 5000)
})

async function checkEnded() {
  if (!running.value) return
  // running/ended 均派生自页面持有的旧 view，需先重取才能感知结束；
  // 首请求后 view 已是 ended，第二次起直接短路，不重复触发刷新。
  await loadView()
  if (!running.value || !ended.value) return
  if (compStore.lockedCompetitionId === compId) {
    await refreshAfterEnd()
  }
}

onUnmounted(() => {
  window.clearInterval(clock)
  if (tick) window.clearInterval(tick)
})
</script>

<template>
  <div v-busy="loading" class="comp-home">
    <header class="comp-header">
      <div class="comp-header-inner">
        <div class="comp-identity">
          <span class="comp-mark" aria-hidden="true">
            <svg width="22" height="24" viewBox="0 0 27 30" fill="none">
              <path d="M3 4L13.5 12.5L24 4M13.5 12.5V27M3 12L9 17M24 12L18 17" stroke="currentColor" stroke-width="2.5" stroke-linecap="square" />
            </svg>
          </span>
          <div class="comp-title">
            <p class="comp-eyebrow">YAK ACADEMY · CTF</p>
            <div class="comp-name-row">
              <h1>{{ comp?.name ?? '竞赛' }}</h1>
              <UiTag v-if="comp" :type="running ? 'success' : ended ? 'info' : 'warning'" size="small">
                {{ running ? '进行中' : ended ? '已结束' : '未开始' }}
              </UiTag>
              <UiTag v-if="comp" size="small" type="info">{{ kindLabel }}</UiTag>
            </div>
            <p v-if="comp" class="comp-window mono">{{ shortDateTime(comp.start_time) }} → {{ shortDateTime(comp.end_time) }}</p>
          </div>
        </div>

        <div class="comp-clock" :class="{ ended }">
          <div class="clock-line">
            <span class="clock-label">{{ ended ? '比赛结束' : comp?.status === 'scheduled' ? '尚未开始' : '剩余时间' }}</span>
            <b class="mono">{{ countdown }}</b>
          </div>
          <span class="race-track" aria-hidden="true"><i :style="{ width: elapsedPercent + '%' }" /></span>
          <span class="clock-sub">
            赛程 {{ elapsedPercent }}%<template v-if="view?.team_name"> · 队伍 {{ view.team_name }}</template>
          </span>
        </div>

        <div class="comp-user">
          <span class="user-name">{{ auth.user?.username }}</span>
          <UiButton v-if="!lockedHere" size="small" variant="ghost" @click="returnToPlatform">
            {{ platformHomeLabel }}
          </UiButton>
          <UiButton size="small" variant="ghost" @click="logout">登出</UiButton>
        </div>
      </div>
    </header>

    <main class="comp-body">
      <UiTabs v-model="activeTab" @tab-change="onTab">
        <!-- 概览：KPI + 图表，首屏先回答"我现在什么位置、还剩多少时间" -->
        <UiTabPane label="概览" name="overview">
          <CompetitionOverview
            v-if="view"
            :view="view"
            :board="board"
            :board-loading="boardLoading"
            :board-error="boardError"
            :mode="boardMode"
            :username="auth.user?.username ?? ''"
            :countdown="countdown"
            :status="comp?.status ?? 'running'"
            :elapsed-percent="elapsedPercent"
            @open-board="openBoard"
            @retry-board="loadBoard"
          />
          <div v-else class="tab-loading">正在加载竞赛数据…</div>
        </UiTabPane>

        <!-- 题目 -->
        <UiTabPane label="题目" name="challenges">
          <div class="challenge-toolbar">
            <UiInput
              v-model="challengeSearch"
              placeholder="搜索题目 / slug"
              clearable
              class="challenge-search"
              aria-label="搜索题目"
            />
            <div class="chip-row" role="group" aria-label="按分类筛选">
              <button
                type="button"
                class="filter-chip"
                :class="{ active: challengeCategory === '' }"
                :aria-pressed="challengeCategory === ''"
                @click="challengeCategory = ''"
              >
                全部
              </button>
              <button
                v-for="c in categories"
                :key="c"
                type="button"
                class="filter-chip"
                :class="{ active: challengeCategory === c }"
                :aria-pressed="challengeCategory === c"
                @click="challengeCategory = c"
              >
                {{ CATEGORY_LABELS[c] ?? c }}
              </button>
            </div>
            <UiRadioGroup v-model="challengeStatus" size="small" class="status-filter">
              <UiRadioButton value="all">全部</UiRadioButton>
              <UiRadioButton value="solved">已通关</UiRadioButton>
              <UiRadioButton value="unsolved">未通关</UiRadioButton>
            </UiRadioGroup>
          </div>

          <UiTable :data="filteredChallenges" :empty-text="challengeEmptyText">
            <UiTableColumn prop="title" label="标题" min-width="240" align="left">
              <template #default="{ row }">
                <div class="challenge-cell">
                  <UiLink type="primary" class="challenge-link" @click="openChallenge(row)">{{ row.title }}</UiLink>
                  <div class="challenge-rate">
                    <span class="mini-track" aria-hidden="true"><i :style="{ width: solveRate(row) + '%' }" /></span>
                    <span class="mini-meta mono">{{ row.solve_count }}/{{ view?.analytics.participant_count ?? 0 }} 人通过</span>
                  </div>
                </div>
              </template>
            </UiTableColumn>
            <UiTableColumn label="分类" width="110" align="left">
              <template #default="{ row }">{{ CATEGORY_LABELS[row.category] ?? row.category }}</template>
            </UiTableColumn>
            <UiTableColumn label="难度" width="90" align="left">
              <template #default="{ row }">{{ difficultyLabel(row.difficulty) }}</template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="分值" width="80" align="right" />
            <UiTableColumn label="容器" width="70" align="center">
              <template #default="{ row }">
                <UiTag v-if="row.has_env" size="small" type="success">有</UiTag>
                <UiTag v-else size="small" type="info">无</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="90" align="center">
              <template #default="{ row }">
                <UiTag v-if="row.solved" type="success" size="small">已通关</UiTag>
                <UiTag v-else type="info" size="small">未通关</UiTag>
              </template>
            </UiTableColumn>
          </UiTable>

          <div class="section-head">
            <h4>我的提交</h4>
            <span class="card-meta mono">最近 {{ view?.my_submissions.length ?? 0 }} 条</span>
          </div>
          <UiTable :data="view?.my_submissions ?? []" empty-text="暂无提交" max-height="240">
            <UiTableColumn prop="id" label="ID" width="80" align="left" />
            <UiTableColumn label="结果" width="100" align="center">
              <template #default="{ row }">
                <UiTag v-if="row.status === 'correct'" type="success" size="small">正确</UiTag>
                <UiTag v-else-if="row.status === 'pending'" type="warning" size="small">判定中</UiTag>
                <UiTag v-else type="danger" size="small">错误</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="得分" width="90" align="right" />
            <UiTableColumn label="时间" min-width="160" align="left">
              <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <!-- 排行榜 -->
        <UiTabPane label="排行榜" name="leaderboard">
          <div class="board-head">
            <UiRadioGroup v-model="boardMode" size="small" @change="onBoardMode">
              <UiRadioButton value="team">战队榜</UiRadioButton>
              <UiRadioButton value="individual">个人榜</UiRadioButton>
            </UiRadioGroup>
            <UiButton size="small" :loading="boardLoading" @click="loadBoard">刷新</UiButton>
          </div>
          <UiAlert v-if="boardError" type="error" :closable="false" :title="boardError" class="block">
            <UiButton size="small" @click="loadBoard">重试</UiButton>
          </UiAlert>
          <UiTable :data="board" v-busy="boardLoading" empty-text="暂无成绩，提交首题后自动生成">
            <UiTableColumn prop="rank" label="名次" width="80" align="center" />
            <UiTableColumn :label="boardMode === 'team' ? '队伍' : '选手'" min-width="140" align="left">
              <template #default="{ row }">
                {{ boardMode === 'team' ? (row.team_name ?? '-') : (row.members[0] ?? '-') }}
              </template>
            </UiTableColumn>
            <UiTableColumn v-if="boardMode === 'team'" label="成员" min-width="180" align="left">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </UiTableColumn>
            <UiTableColumn label="得分" width="180" align="left">
              <template #default="{ row }">
                <div class="board-score">
                  <b class="mono">{{ row.score }}</b>
                  <span class="mini-track" aria-hidden="true"><i :style="{ width: boardPercent(row.score) + '%' }" /></span>
                </div>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="solved_count" label="解题数" width="90" align="right" />
          </UiTable>
        </UiTabPane>

        <!-- WP（结束后；按题目一份，全队共享可互相修改） -->
        <UiTabPane v-if="ended" label="WP 提交" name="writeup">
          <UiAlert
            type="info"
            :closable="false"
            title="每道题提交一份 WP：文字 + 可选附带 zip（脚本/截图打包）；队伍模式下全队共享同一份，任一成员均可修改或重新上传。"
            class="block"
          />
          <div v-for="item in view?.challenges ?? []" :key="item.id" class="wp-card">
            <div class="wp-card-head">
              <b>{{ item.title }}</b>
              <UiTag v-if="wpOf(item.id)" type="success" size="small">已提交</UiTag>
            </div>
            <p v-if="wpOf(item.id)" class="wp-meta dim">
              最近修改：{{ wpOf(item.id)?.updated_by }} · {{ wpOf(item.id)?.updated_at?.replace('T', ' ').slice(0, 19) }}
              <template v-if="wpOf(item.id)?.zip_name"> · zip：{{ wpOf(item.id)?.zip_name }}</template>
            </p>
            <UiInput
              v-model="wpDrafts[item.id]"
              type="textarea"
              :rows="6"
              placeholder="本题解题思路（Markdown）：关键步骤、所用工具、flag 截图说明等。"
            />
            <div class="wp-actions">
              <UiUpload
                :show-file-list="false"
                :auto-upload="false"
                accept=".zip"
                :on-change="(f: UploadFile) => uploadWpZip(item, f.raw as File)"
              >
                <UiButton size="small" :loading="wpUploading === item.id">
                  {{ wpOf(item.id)?.zip_name ? '重新上传 zip' : '上传 zip' }}
                </UiButton>
              </UiUpload>
              <UiButton type="primary" size="small" :loading="wpSaving" @click="saveWp(item)">
                {{ wpOf(item.id) ? '更新 WP' : '提交 WP' }}
              </UiButton>
            </div>
          </div>
          <UiEmpty v-if="!view?.challenges?.length" description="本竞赛没有绑定题目" />
        </UiTabPane>
      </UiTabs>
    </main>

    <!-- 题目详情抽屉 -->
    <UiDrawer v-model="drawerOpen" :title="drawer?.title ?? ''" size="480px">
      <template v-if="drawer">
        <div class="drawer-meta">
          <UiTag size="small">{{ drawer.category }}</UiTag>
          <UiTag size="small" type="info">{{ drawer.difficulty }}</UiTag>
          <UiTag size="small" type="warning">{{ drawer.score }} 分</UiTag>
          <UiTag v-if="drawer.solved" size="small" type="success">已通关</UiTag>
        </div>
        <div class="description" v-html="renderedDescription" />

        <template v-if="drawer.has_env">
          <UiDivider />
          <h4>题目环境</h4>
          <p v-if="instance" class="endpoint">
            <code>{{ instance.endpoint }}</code>
            <UiTag size="small" type="success">running</UiTag>
          </p>
          <p v-else class="dim">暂无运行中的容器</p>
          <div class="inst-actions">
            <UiButton type="primary" size="small" :loading="starting" :disabled="!running" @click="startInstance">
              {{ instance ? '重新检测' : '启动容器' }}
            </UiButton>
            <UiButton v-if="instance" size="small" type="danger" @click="destroyInstance">销毁容器</UiButton>
          </div>
          <p v-if="!running" class="dim">比赛{{ ended ? '已结束' : '未开始' }}，容器不可用</p>
        </template>

        <UiDivider />
        <h4>提交 flag</h4>
        <UiInput v-model="answer" placeholder="flag{...}" :disabled="!running" @keyup.enter="submitFlag" />
        <UiButton type="primary" class="submit-btn" :loading="submitting" :disabled="!running" @click="submitFlag">
          提交
        </UiButton>
      </template>
    </UiDrawer>
  </div>
</template>

<style scoped>
.comp-home { min-height: 100vh; background: var(--background); }

/* 竞赛顶栏：品牌 + 赛事身份 + 赛程时钟 + 本人；邀请赛锁定期间不出现平台导航。 */
.comp-header {
  position: sticky;
  top: 0;
  z-index: 20;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--background) 94%, transparent);
  backdrop-filter: blur(12px);
}
.comp-header-inner {
  display: flex;
  align-items: center;
  gap: 28px;
  max-width: 1280px;
  margin-inline: auto;
  padding: 14px 32px;
}
.comp-identity { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }
.comp-mark { display: grid; flex-shrink: 0; place-items: center; color: var(--primary); }
.comp-title { min-width: 0; }
.comp-eyebrow {
  margin: 0 0 2px;
  color: var(--muted-foreground);
  font-size: 10px;
  letter-spacing: 0.12em;
}
.comp-name-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.comp-name-row h1 {
  margin: 0;
  overflow: hidden;
  color: var(--foreground);
  font-size: 19px;
  font-weight: 600;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.comp-window { margin: 2px 0 0; color: var(--muted-foreground); font-size: 11px; white-space: nowrap; }

.comp-clock { display: grid; flex-shrink: 0; gap: 4px; width: 170px; }
.clock-line { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.clock-label { color: var(--muted-foreground); font-size: 11px; }
.clock-line b { color: var(--accent-foreground); font-size: 14px; font-weight: 600; }
.comp-clock.ended .clock-line b { color: var(--muted-foreground); }
.race-track { display: block; height: 3px; overflow: hidden; border-radius: 999px; background: var(--muted); }
.race-track i { display: block; height: 100%; background: var(--primary); transition: width 180ms ease-out; }
.clock-sub { color: var(--muted-foreground); font-size: 10px; }

.comp-user { display: flex; flex-shrink: 0; align-items: center; gap: 8px; }
.user-name {
  max-width: 160px;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.comp-body { max-width: 1280px; margin-inline: auto; padding: 24px 32px 48px; }
.tab-loading { padding: 48px 0; color: var(--muted-foreground); font-size: 13px; text-align: center; }

.challenge-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.challenge-search { width: 240px; max-width: 100%; }
.chip-row { display: flex; flex-wrap: wrap; gap: 6px; }
.filter-chip {
  padding: 5px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--panel);
  color: var(--muted-foreground);
  font-size: 12px;
  cursor: pointer;
}
.filter-chip:hover { border-color: var(--input); color: var(--foreground); }
.filter-chip.active {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 10%, transparent);
  color: var(--accent-foreground);
}
.filter-chip:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.status-filter { margin-left: auto; }

.section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin: 24px 0 10px;
}
.section-head h4 { margin: 0; }
.card-meta { color: var(--muted-foreground); font-size: 12px; }

.challenge-cell { display: grid; gap: 5px; min-width: 0; justify-items: start; text-align: left; }
.challenge-link { text-align: left; }
.challenge-rate { display: flex; align-items: center; gap: 8px; }
.mini-track {
  display: block;
  flex-shrink: 0;
  width: 84px;
  height: 3px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--muted);
}
.mini-track i { display: block; height: 100%; background: var(--primary); }
.mini-meta { color: var(--muted-foreground); font-size: 10px; white-space: nowrap; }

.board-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.board-score { display: flex; align-items: center; gap: 8px; }
.board-score b { min-width: 44px; font-size: 12px; font-weight: 600; text-align: right; }
.board-score .mini-track { flex: 1; width: auto; max-width: 120px; }
.block { margin-bottom: 12px; }

.wp-card {
  margin-bottom: 16px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.wp-card-head { display: flex; align-items: center; gap: 8px; margin-bottom: 2px; }
.wp-meta { margin: 2px 0 8px; }
.wp-actions { display: flex; gap: 8px; margin-top: 8px; }

.drawer-meta { display: flex; gap: 6px; margin-bottom: 12px; }
.description { line-height: 1.7; }
.description :deep(pre) {
  padding: 12px;
  overflow: auto;
  border-radius: 8px;
  background: var(--muted);
}
.endpoint { display: flex; align-items: center; gap: 8px; }
.inst-actions { display: flex; gap: 8px; margin-top: 8px; }
.submit-btn { width: 100%; margin-top: 8px; }
.dim { color: var(--muted-foreground); font-size: 13px; }
h4 { margin: 12px 0 6px; }

@media (max-width: 1100px) {
  .comp-header-inner { gap: 16px; padding: 12px 20px; }
  .comp-clock { width: 150px; }
  .comp-window { display: none; }
  .comp-body { padding: 20px 20px 40px; }
}
@media (max-width: 760px) {
  .comp-header-inner { flex-wrap: wrap; gap: 10px 16px; }
  .comp-identity { flex-basis: 100%; }
  .comp-clock { width: auto; flex: 1; }
  .comp-user .user-name { display: none; }
  .status-filter { margin-left: 0; }
}
@media (max-width: 560px) {
  .comp-name-row h1 { font-size: 16px; }
  .comp-body { padding: 16px 16px 32px; }
  .challenge-search { width: 100%; }
}
</style>