<script setup lang="ts">
// 竞赛首页（参赛者满屏视图，锁定期间的全部可用功能）。
// 头部仅保留锁定白名单能力：竞赛信息 + 本人（邮箱/资料不可达）+ 登出。
// 题目抽屉内完成开容器/提 flag（复用题库详情交互模式），异步判题后延时刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { notify, dialogs } from '@/lib/notifications'
import type { UploadFile } from '@/components/system/composite'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import {
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

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const compStore = useCompetitionStore()

const compId = Number(route.params.id)
const view = ref<CompetitionHomeView | null>(null)
const loading = ref(true)
const activeTab = ref('challenges')
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
  const ms = new Date(comp.value.end_time).getTime() - nowTs.value
  if (ms <= 0) return '已结束'
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = Math.floor((ms % 60_000) / 1000)
  return h > 24 ? `${Math.floor(h / 24)} 天 ${h % 24} 小时` : `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
})

async function loadView() {
  try {
    view.value = await competitionApi.view(compId)
  } catch (e) {
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
    // 异步判题：稍后刷新首页（题目通关状态 + 我的提交）。
    setTimeout(loadView, 1200)
    setTimeout(loadView, 3000)
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

// ---------- 排行榜 ----------
const boardMode = ref<'team' | 'individual'>('team')
const board = ref<TeamLeaderboardEntry[]>([])
const boardLoading = ref(false)

async function loadBoard() {
  boardLoading.value = true
  try {
    const resp = await competitionApi.leaderboard(compId, boardMode.value)
    board.value = resp.items
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    boardLoading.value = false
  }
}

async function onBoardMode() {
  await loadBoard()
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
  // 竞赛期间保持锁定标记（刷新/登出重登后守卫仍能引导回此页）。
  if (running.value) compStore.lock(compId)
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
    <header class="head">
      <div class="title">
        <h2>{{ comp?.name ?? '竞赛' }}</h2>
        <UiTag v-if="comp" :type="running ? 'success' : ended ? 'info' : 'warning'" size="small">
          {{ running ? '进行中' : ended ? '已结束' : '未开始' }}
        </UiTag>
        <span v-if="running" class="countdown">距结束 {{ countdown }}</span>
        <span v-if="view?.team_name" class="team">队伍：{{ view.team_name }}</span>
      </div>
      <div class="user">
        <span>{{ auth.user?.username }}</span>
        <UiButton size="small" @click="logout">登出</UiButton>
      </div>
    </header>

    <main class="body">
      <UiTabs v-model="activeTab" @tab-change="onTab">
        <!-- 题目 -->
        <UiTabPane label="题目" name="challenges">
          <UiTable :data="view?.challenges ?? []" empty-text="暂无题目">
            <UiTableColumn prop="title" label="标题" min-width="180">
              <template #default="{ row }">
                <UiLink type="primary" @click="openChallenge(row)">{{ row.title }}</UiLink>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="category" label="分类" width="110" />
            <UiTableColumn prop="difficulty" label="难度" width="100" />
            <UiTableColumn prop="score" label="分值" width="80" />
            <UiTableColumn label="容器" width="80">
              <template #default="{ row }">
                <UiTag v-if="row.has_env" size="small" type="success">有</UiTag>
                <UiTag v-else size="small" type="info">无</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="90">
              <template #default="{ row }">
                <UiTag v-if="row.solved" type="success" size="small">已通关</UiTag>
                <UiTag v-else type="info" size="small">未通关</UiTag>
              </template>
            </UiTableColumn>
          </UiTable>

          <h4>我的提交</h4>
          <UiTable :data="view?.my_submissions ?? []" empty-text="暂无提交" max-height="240">
            <UiTableColumn prop="id" label="ID" width="70" />
            <UiTableColumn label="结果" width="100">
              <template #default="{ row }">
                <UiTag v-if="row.status === 'correct'" type="success" size="small">正确</UiTag>
                <UiTag v-else-if="row.status === 'pending'" type="warning" size="small">判定中</UiTag>
                <UiTag v-else type="danger" size="small">错误</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="得分" width="80" />
            <UiTableColumn label="时间" min-width="160">
              <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
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
            <UiButton size="small" @click="loadBoard">刷新</UiButton>
          </div>
          <UiTable :data="board" v-busy="boardLoading" empty-text="暂无成绩">
            <UiTableColumn prop="rank" label="名次" width="80" />
            <UiTableColumn :label="boardMode === 'team' ? '队伍' : '选手'" min-width="140">
              <template #default="{ row }">{{ row.team_name ?? '-' }}</template>
            </UiTableColumn>
            <UiTableColumn label="成员" min-width="180">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="得分" width="100" />
            <UiTableColumn prop="solved_count" label="解题数" width="90" />
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
.comp-home { min-height: 100vh; background: var(--muted); }
.head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0.8rem 1.6rem; background: var(--background);
  border-bottom: 1px solid var(--border);
}
.title { display: flex; align-items: center; gap: 0.8rem; }
.title h2 { margin: 0; font-size: 1.2rem; }
.countdown { color: var(--accent-foreground); font-size: 0.9rem; }
.team { color: var(--muted-foreground); font-size: 0.9rem; }
.user { display: flex; align-items: center; gap: 0.8rem; }
.body { padding: 1.2rem 1.6rem; max-width: 1100px; margin: 0 auto; }
.board-head { display: flex; justify-content: space-between; margin-bottom: 0.8rem; }
.block { margin-bottom: 1rem; }
.wp-card { background: var(--background); border: 1px solid var(--border); border-radius: 6px; padding: 1rem; margin-bottom: 1rem; }
.wp-card-head { display: flex; align-items: center; gap: 0.6rem; margin-bottom: 0.2rem; }
.wp-meta { margin: 0.2rem 0 0.6rem; }
.wp-actions { display: flex; gap: 0.6rem; margin-top: 0.6rem; }
.drawer-meta { display: flex; gap: 0.4rem; margin-bottom: 0.8rem; }
.description { line-height: 1.7; }
.description :deep(pre) { background: var(--muted); padding: 0.8rem; border-radius: 4px; overflow: auto; }
.endpoint { display: flex; align-items: center; gap: 0.5rem; }
.inst-actions { display: flex; gap: 0.5rem; margin-top: 0.5rem; }
.submit-btn { margin-top: 0.6rem; width: 100%; }
.dim { color: var(--muted-foreground); font-size: 0.85rem; }
h4 { margin: 0.8rem 0 0.4rem; }
</style>