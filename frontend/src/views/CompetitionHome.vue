<script setup lang="ts">
// 竞赛首页（参赛者满屏视图，锁定期间的全部可用功能）。
// 头部仅保留锁定白名单能力：竞赛信息 + 本人（邮箱/资料不可达）+ 登出。
// 题目抽屉内完成开容器/提 flag（复用训练场交互模式），异步判题后延时刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { UploadFile } from 'element-plus'
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
    ElMessage.error(loadError.value)
    if (compStore.lockedCompetitionId === compId) compStore.unlock()
    router.replace('/')
  }
}

async function refreshAfterEnd() {
  // 比赛结束瞬间：解除锁定态并刷新（可访问平台 + 可交 WP）。
  compStore.unlock()
  await loadView()
  ElMessage.info('比赛已结束，感谢参与！可提交 Writeup 或返回平台。')
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
    ElMessage.success(`容器已启动：${instance.value.endpoint}`)
    await loadView()
  } catch (e) {
    const err = e as { code?: string; message?: string; data?: Record<string, unknown> | null }
    if (err.code === 'conflict') {
      const current = err.data?.current as { challenge_title?: string; expire_at?: string } | undefined
      ElMessage.warning(
        `已有活跃容器（${current?.challenge_title ?? '其他题目'}），请先关闭后再启动本题容器`,
      )
    } else {
      ElMessage.error(err.message ?? '启动失败')
    }
  } finally {
    starting.value = false
  }
}

async function destroyInstance() {
  if (!drawer.value) return
  try {
    await ElMessageBox.confirm('销毁本题容器？题目进度（flag）不受影响，可重新启动。', '销毁确认', { type: 'warning' })
  } catch {
    return
  }
  starting.value = true
  try {
    await instanceApi.destroy(drawer.value.slug)
    instance.value = null
    ElMessage.success('容器已销毁')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    starting.value = false
  }
}

async function submitFlag() {
  if (!drawer.value || !answer.value.trim()) {
    ElMessage.warning('请输入 flag')
    return
  }
  submitting.value = true
  try {
    await competitionApi.submitFlag(compId, drawer.value.slug, answer.value.trim())
    ElMessage.success('已提交，异步判定中…')
    answer.value = ''
    // 异步判题：稍后刷新首页（题目通关状态 + 我的提交）。
    setTimeout(loadView, 1200)
    setTimeout(loadView, 3000)
  } catch (e) {
    ElMessage.error((e as Error).message)
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
    ElMessage.error((e as Error).message)
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
    ElMessage.error((e as Error).message)
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
    ElMessage.warning('请填写 WP 文字或上传 zip（至少一项）')
    return
  }
  wpSaving.value = true
  try {
    await competitionApi.saveWriteup(compId, item.id, content)
    ElMessage.success(`「${item.title}」WP 已提交（可继续修改）`)
    await loadWps()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    wpSaving.value = false
  }
}

async function uploadWpZip(item: CompetitionChallengeItem, file: File) {
  if (!file.name.toLowerCase().endsWith('.zip')) {
    ElMessage.warning('仅支持 zip 文件')
    return
  }
  wpUploading.value = item.id
  try {
    const resp = await competitionApi.uploadWriteupZip(compId, item.id, file)
    ElMessage.success(`「${item.title}」zip 已上传：${resp.zip_name}（可重新上传替换）`)
    await loadWps()
  } catch (e) {
    ElMessage.error((e as Error).message)
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
  <div v-loading="loading" class="comp-home">
    <header class="head">
      <div class="title">
        <h2>{{ comp?.name ?? '竞赛' }}</h2>
        <el-tag v-if="comp" :type="running ? 'success' : ended ? 'info' : 'warning'" size="small">
          {{ running ? '进行中' : ended ? '已结束' : '未开始' }}
        </el-tag>
        <span v-if="running" class="countdown">距结束 {{ countdown }}</span>
        <span v-if="view?.team_name" class="team">队伍：{{ view.team_name }}</span>
      </div>
      <div class="user">
        <span>{{ auth.user?.username }}</span>
        <el-button size="small" @click="logout">登出</el-button>
      </div>
    </header>

    <main class="body">
      <el-tabs v-model="activeTab" @tab-change="onTab">
        <!-- 题目 -->
        <el-tab-pane label="题目" name="challenges">
          <el-table :data="view?.challenges ?? []" empty-text="暂无题目">
            <el-table-column prop="title" label="标题" min-width="180">
              <template #default="{ row }">
                <el-link type="primary" @click="openChallenge(row)">{{ row.title }}</el-link>
              </template>
            </el-table-column>
            <el-table-column prop="category" label="分类" width="110" />
            <el-table-column prop="difficulty" label="难度" width="100" />
            <el-table-column prop="score" label="分值" width="80" />
            <el-table-column label="容器" width="80">
              <template #default="{ row }">
                <el-tag v-if="row.has_env" size="small" type="success">有</el-tag>
                <el-tag v-else size="small" type="info">无</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.solved" type="success" size="small">已通关</el-tag>
                <el-tag v-else type="info" size="small">未通关</el-tag>
              </template>
            </el-table-column>
          </el-table>

          <h4>我的提交</h4>
          <el-table :data="view?.my_submissions ?? []" empty-text="暂无提交" max-height="240">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column label="结果" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.status === 'correct'" type="success" size="small">正确</el-tag>
                <el-tag v-else-if="row.status === 'pending'" type="warning" size="small">判定中</el-tag>
                <el-tag v-else type="danger" size="small">错误</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="score" label="得分" width="80" />
            <el-table-column label="时间" min-width="160">
              <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- 排行榜 -->
        <el-tab-pane label="排行榜" name="leaderboard">
          <div class="board-head">
            <el-radio-group v-model="boardMode" size="small" @change="onBoardMode">
              <el-radio-button value="team">战队榜</el-radio-button>
              <el-radio-button value="individual">个人榜</el-radio-button>
            </el-radio-group>
            <el-button size="small" @click="loadBoard">刷新</el-button>
          </div>
          <el-table :data="board" v-loading="boardLoading" empty-text="暂无成绩">
            <el-table-column prop="rank" label="名次" width="80" />
            <el-table-column :label="boardMode === 'team' ? '队伍' : '选手'" min-width="140">
              <template #default="{ row }">{{ row.team_name ?? '-' }}</template>
            </el-table-column>
            <el-table-column label="成员" min-width="180">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </el-table-column>
            <el-table-column prop="score" label="得分" width="100" />
            <el-table-column prop="solved_count" label="解题数" width="90" />
          </el-table>
        </el-tab-pane>

        <!-- WP（结束后；按题目一份，全队共享可互相修改） -->
        <el-tab-pane v-if="ended" label="WP 提交" name="writeup">
          <el-alert
            type="info"
            :closable="false"
            title="每道题提交一份 WP：文字 + 可选附带 zip（脚本/截图打包）；队伍模式下全队共享同一份，任一成员均可修改或重新上传。"
            class="block"
          />
          <div v-for="item in view?.challenges ?? []" :key="item.id" class="wp-card">
            <div class="wp-card-head">
              <b>{{ item.title }}</b>
              <el-tag v-if="wpOf(item.id)" type="success" size="small">已提交</el-tag>
            </div>
            <p v-if="wpOf(item.id)" class="wp-meta dim">
              最近修改：{{ wpOf(item.id)?.updated_by }} · {{ wpOf(item.id)?.updated_at?.replace('T', ' ').slice(0, 19) }}
              <template v-if="wpOf(item.id)?.zip_name"> · zip：{{ wpOf(item.id)?.zip_name }}</template>
            </p>
            <el-input
              v-model="wpDrafts[item.id]"
              type="textarea"
              :rows="6"
              placeholder="本题解题思路（Markdown）：关键步骤、所用工具、flag 截图说明等。"
            />
            <div class="wp-actions">
              <el-upload
                :show-file-list="false"
                :auto-upload="false"
                accept=".zip"
                :on-change="(f: UploadFile) => uploadWpZip(item, f.raw as File)"
              >
                <el-button size="small" :loading="wpUploading === item.id">
                  {{ wpOf(item.id)?.zip_name ? '重新上传 zip' : '上传 zip' }}
                </el-button>
              </el-upload>
              <el-button type="primary" size="small" :loading="wpSaving" @click="saveWp(item)">
                {{ wpOf(item.id) ? '更新 WP' : '提交 WP' }}
              </el-button>
            </div>
          </div>
          <el-empty v-if="!view?.challenges?.length" description="本竞赛没有绑定题目" />
        </el-tab-pane>
      </el-tabs>
    </main>

    <!-- 题目详情抽屉 -->
    <el-drawer v-model="drawerOpen" :title="drawer?.title ?? ''" size="480px">
      <template v-if="drawer">
        <div class="drawer-meta">
          <el-tag size="small">{{ drawer.category }}</el-tag>
          <el-tag size="small" type="info">{{ drawer.difficulty }}</el-tag>
          <el-tag size="small" type="warning">{{ drawer.score }} 分</el-tag>
          <el-tag v-if="drawer.solved" size="small" type="success">已通关</el-tag>
        </div>
        <div class="description" v-html="renderedDescription" />

        <template v-if="drawer.has_env">
          <el-divider />
          <h4>题目环境</h4>
          <p v-if="instance" class="endpoint">
            <code>{{ instance.endpoint }}</code>
            <el-tag size="small" type="success">running</el-tag>
          </p>
          <p v-else class="dim">暂无运行中的容器</p>
          <div class="inst-actions">
            <el-button type="primary" size="small" :loading="starting" :disabled="!running" @click="startInstance">
              {{ instance ? '重新检测' : '启动容器' }}
            </el-button>
            <el-button v-if="instance" size="small" type="danger" @click="destroyInstance">销毁容器</el-button>
          </div>
          <p v-if="!running" class="dim">比赛{{ ended ? '已结束' : '未开始' }}，容器不可用</p>
        </template>

        <el-divider />
        <h4>提交 flag</h4>
        <el-input v-model="answer" placeholder="flag{...}" :disabled="!running" @keyup.enter="submitFlag" />
        <el-button type="primary" class="submit-btn" :loading="submitting" :disabled="!running" @click="submitFlag">
          提交
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped>
.comp-home { min-height: 100vh; background: var(--el-fill-color-lighter); }
.head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0.8rem 1.6rem; background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.title { display: flex; align-items: center; gap: 0.8rem; }
.title h2 { margin: 0; font-size: 1.2rem; }
.countdown { color: var(--el-color-warning); font-size: 0.9rem; }
.team { color: var(--el-text-color-secondary); font-size: 0.9rem; }
.user { display: flex; align-items: center; gap: 0.8rem; }
.body { padding: 1.2rem 1.6rem; max-width: 1100px; margin: 0 auto; }
.board-head { display: flex; justify-content: space-between; margin-bottom: 0.8rem; }
.block { margin-bottom: 1rem; }
.wp-card { background: var(--el-bg-color); border: 1px solid var(--el-border-color-lighter); border-radius: 6px; padding: 1rem; margin-bottom: 1rem; }
.wp-card-head { display: flex; align-items: center; gap: 0.6rem; margin-bottom: 0.2rem; }
.wp-meta { margin: 0.2rem 0 0.6rem; }
.wp-actions { display: flex; gap: 0.6rem; margin-top: 0.6rem; }
.drawer-meta { display: flex; gap: 0.4rem; margin-bottom: 0.8rem; }
.description { line-height: 1.7; }
.description :deep(pre) { background: var(--el-fill-color-darker); padding: 0.8rem; border-radius: 4px; overflow: auto; }
.endpoint { display: flex; align-items: center; gap: 0.5rem; }
.inst-actions { display: flex; gap: 0.5rem; margin-top: 0.5rem; }
.submit-btn { margin-top: 0.6rem; width: 100%; }
.dim { color: var(--el-text-color-placeholder); font-size: 0.85rem; }
h4 { margin: 0.8rem 0 0.4rem; }
</style>