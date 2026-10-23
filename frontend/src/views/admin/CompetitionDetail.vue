<script setup lang="ts">
// 管理面板：竞赛详情页。配置编辑 + 题目绑定/批量导入 + 参赛人员管理
// （邀请赛：固定格式表格导入 / 一键生成账号并导出；公开赛：广场自助报名）
// + 竞赛容器 + 反作弊警告 + WP 提交情况 + 双榜。
// 状态由起止时间派生：running 期间禁改 start_time、禁解绑题目（后端同样校验）；
// 进行中的竞赛可手动结束，已结束的竞赛可重新配置起止时间再次开赛。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  adminCompetitionApi,
  KIND_LABELS,
  type AdminLabInstance,
  type ChallengeJSON,
  type CheatWarningItem,
  type Competition,
  type CompetitionDetail,
  type CompetitionParticipantItem,
  type GenerateUsersResult,
  type ParticipantImportResult,
  type TeamLeaderboardEntry,
  type TrainingItem,
  type WriteupItem,
} from '../../api'
import { parseChallengeJSON, validateChallenge } from '../../admin/challengeFormat'

const route = useRoute()
const compId = Number(route.params.id)

const detail = ref<CompetitionDetail | null>(null)
const loading = ref(true)
const activeTab = ref('config')

async function loadDetail() {
  try {
    detail.value = await adminCompetitionApi.detail(compId)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

const status = computed<Competition['status']>(() => detail.value?.status ?? 'scheduled')
const running = computed(() => status.value === 'running')

function statusType(s: Competition['status']) {
  return s === 'running' ? 'success' : s === 'scheduled' ? 'warning' : 'info'
}
function statusLabel(s: Competition['status']) {
  return s === 'running' ? '进行中' : s === 'scheduled' ? '未开始' : '已结束'
}
function fmt(ts: string | null | undefined) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

// ---------- 配置 ----------
const form = ref({ name: '', description: '', start: null as Date | null, end: null as Date | null })
const saving = ref(false)

function syncForm() {
  if (!detail.value) return
  form.value = {
    name: detail.value.name,
    description: detail.value.description,
    start: new Date(detail.value.start_time),
    end: new Date(detail.value.end_time),
  }
}

async function saveConfig() {
  if (!form.value.name.trim() || !form.value.start || !form.value.end) {
    ElMessage.warning('名称与起止时间必填')
    return
  }
  saving.value = true
  try {
    detail.value = await adminCompetitionApi.update(compId, {
      name: form.value.name.trim(),
      description: form.value.description,
      start_time: form.value.start.toISOString(),
      end_time: form.value.end.toISOString(),
    })
    ElMessage.success('配置已保存')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

// 手动结束竞赛（仅 running）：end_time 置为当前时刻；参赛者解锁、WP 开放提交。
// 结束后仍可重新配置起止时间再次开赛。
const ending = ref(false)

async function endCompetition() {
  try {
    await ElMessageBox.confirm(
      `手动结束竞赛「${detail.value?.name}」？立即生效：参赛者解除锁定、停止提交与容器，WP 转为可提交。结束后仍可重新配置起止时间再次开赛。`,
      '手动结束竞赛',
      { type: 'warning', confirmButtonText: '结束竞赛', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  ending.value = true
  try {
    detail.value = await adminCompetitionApi.end(compId)
    syncForm()
    ElMessage.success('竞赛已结束')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    ending.value = false
  }
}

// ---------- 题目 ----------
const challenges = ref<TrainingItem[]>([])
const challengesLoading = ref(false)

async function loadChallenges() {
  challengesLoading.value = true
  try {
    const resp = await adminCompetitionApi.challenges(compId)
    challenges.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    challengesLoading.value = false
  }
}

const showBind = ref(false)
const bindMode = ref<'slugs' | 'json'>('slugs')
const bindSlugs = ref('')
const binding = ref(false)
const unbinding = ref('')

// 导入模式：文件（可多选）与粘贴共用一个暂存列表，逐题校验（challengeFormat），
// 全部通过后一次性提交；修正编辑仍走题目管理，此处只做导入与绑定。
interface StagedChallenge {
  key: number
  source: string
  chal: ChallengeJSON
  errors: string[]
}
const staged = ref<StagedChallenge[]>([])
const stagedSeq = ref(0)
const bindPaste = ref('')

function openBind() {
  bindMode.value = 'slugs'
  bindSlugs.value = ''
  staged.value = []
  bindPaste.value = ''
  showBind.value = true
}

function stageChallenges(text: string, source: string): boolean {
  const { list, error } = parseChallengeJSON(text)
  if (error) {
    ElMessage.warning(`${source}：${error}`)
    return false
  }
  for (const chal of list) {
    const errors = [...validateChallenge(chal)]
    if (staged.value.some((s) => s.chal.slug === chal.slug)) errors.push(`slug：与暂存列表中的「${chal.slug}」重复`)
    staged.value.push({ key: stagedSeq.value++, source, chal, errors })
  }
  return true
}

// el-upload before-upload：文件文本进暂存列表（单文件即逐题，多文件即批量）。
async function onStageFile(file: File) {
  stageChallenges(await file.text(), file.name)
  return false // 阻止 el-upload 默认上传行为
}

function stagePaste() {
  if (!bindPaste.value.trim()) return
  if (stageChallenges(bindPaste.value, '粘贴')) bindPaste.value = ''
}

function removeStaged(row: StagedChallenge) {
  staged.value = staged.value.filter((s) => s.key !== row.key)
}

const stagedInvalid = computed(() => staged.value.some((s) => s.errors.length))

async function submitBind() {
  binding.value = true
  try {
    let body: { slugs: string[] } | ChallengeJSON[]
    if (bindMode.value === 'slugs') {
      const slugs = bindSlugs.value.split(/[\s,，;；]+/).filter(Boolean)
      if (!slugs.length) {
        ElMessage.warning('请填写题目 slug')
        return
      }
      body = { slugs }
    } else {
      if (!staged.value.length) {
        ElMessage.warning('请先选择题目 JSON 文件或粘贴内容')
        return
      }
      if (stagedInvalid.value) {
        ElMessage.warning('暂存列表中存在校验未通过的题目，请修正文件或移除对应条目')
        return
      }
      body = staged.value.map((s) => s.chal)
    }
    const resp = await adminCompetitionApi.bindChallenges(compId, body)
    ElMessage.success(`绑定完成：新增 ${resp.added} 题`)
    showBind.value = false
    await Promise.all([loadChallenges(), loadDetail()])
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    binding.value = false
  }
}

async function unbind(item: TrainingItem) {
  try {
    await ElMessageBox.confirm(`解绑题目「${item.title}」（${item.slug}）？参赛者将无法再访问该题。`, '解绑确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  unbinding.value = item.slug
  try {
    const resp = await adminCompetitionApi.unbindChallenges(compId, [item.slug])
    ElMessage.success(`已解绑 ${resp.removed} 题`)
    await Promise.all([loadChallenges(), loadDetail()])
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    unbinding.value = ''
  }
}

// ---------- 参赛人员 ----------
const participants = ref<CompetitionParticipantItem[]>([])
const teams = ref<TeamLeaderboardEntry[]>([])
const partTotal = ref(0)
const partPage = ref(1)
const partLoading = ref(false)

async function loadParticipants() {
  partLoading.value = true
  try {
    const resp = await adminCompetitionApi.participants(compId, partPage.value)
    participants.value = resp.items
    teams.value = resp.teams
    partTotal.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    partLoading.value = false
  }
}

const invitational = computed(() => detail.value?.kind === 'invitational')

// ---------- 固定格式表格导入（仅邀请赛） ----------
// 条目 = 账号编码（YA26-00001）或用户名，引用既有账号；未命中的条目在导入
// 结果 missing 中回报（不自动建号——补号走「一键生成账号」）。
interface StagedTeam {
  key: number
  name: string
  members: string[]
}
const showImport = ref(false)
const importing = ref(false)
const importMode = ref<'individuals' | 'teams'>('individuals')
const stagedIndividuals = ref<string[]>([])
const stagedTeams = ref<StagedTeam[]>([])
const teamSeq = ref(0)
const importResult = ref<ParticipantImportResult | null>(null)
const showResult = ref(false)

function openImport() {
  importMode.value = 'individuals'
  stagedIndividuals.value = []
  stagedTeams.value = []
  showImport.value = true
}

// CSV 解析：中英文逗号均作分隔；首列为表头（账号/队伍等）的行跳过；
// 个人名单每行取首列，队伍名单每行 = 队伍名,成员1,成员2,…。
function parseCsvCells(line: string): string[] {
  return line.split(/[,，]/).map((c) => c.trim()).filter(Boolean)
}
function isHeaderCell(cell: string): boolean {
  return /^(账号|用户名|成员|参赛者|entry|account|username)$/i.test(cell)
}
function isHeaderRow(cells: string[]): boolean {
  return cells.length > 0 && isHeaderCell(cells[0]) && cells.slice(1).every((c) => isHeaderCell(c) || !c)
}

async function onImportFile(file: File) {
  const text = await file.text()
  const lines = text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  const rows = lines.map((l) => parseCsvCells(l)).filter((cells) => cells.length)
  if (rows.length && isHeaderRow(rows[0])) rows.shift()
  if (importMode.value === 'individuals') {
    const entries = rows.map((cells) => cells[0]).filter(Boolean)
    const seen = new Set(stagedIndividuals.value)
    for (const e of entries) if (!seen.has(e)) { stagedIndividuals.value.push(e); seen.add(e) }
  } else {
    const seen = new Set(stagedTeams.value.map((t) => t.name))
    for (const cells of rows) {
      const [name, ...members] = cells
      if (!name || !members.length) continue
      if (seen.has(name)) continue
      stagedTeams.value.push({ key: teamSeq.value++, name, members })
      seen.add(name)
    }
  }
  return false // 阻止 el-upload 默认上传行为
}

// 模板下载（带 BOM，Excel 直接打开不乱码）。
function downloadTemplate(mode: 'individuals' | 'teams') {
  const content =
    mode === 'individuals'
      ? '\uFEFF账号\nYA26-00001\nalice\n'
      : '\uFEFF队伍,成员1,成员2\n一队,YA26-00001,alice\n二队,YA26-00002,bob\n'
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = mode === 'individuals' ? '参赛名单-个人.csv' : '参赛名单-队伍.csv'
  a.click()
  URL.revokeObjectURL(url)
}

function removeStagedTeam(row: StagedTeam) {
  stagedTeams.value = stagedTeams.value.filter((t) => t.key !== row.key)
}

async function submitImport() {
  const teamsReq = stagedTeams.value.map((t) => ({ name: t.name, members: t.members }))
  if (!teamsReq.length && !stagedIndividuals.value.length) {
    ElMessage.warning('请先上传名单表格（每行一条：账号编码或用户名）')
    return
  }
  importing.value = true
  try {
    importResult.value = await adminCompetitionApi.importParticipants(compId, {
      teams: teamsReq,
      individuals: stagedIndividuals.value,
    })
    showImport.value = false
    showResult.value = true
    await Promise.all([loadParticipants(), loadDetail()])
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    importing.value = false
  }
}

// ---------- 一键生成账号（仅邀请赛） ----------
const showGenerate = ref(false)
const generating = ref(false)
const genForm = ref({ prefix: '', count: 10, teamSize: 0, password: '' })
const genResult = ref<GenerateUsersResult | null>(null)

function openGenerate() {
  genForm.value = { prefix: '', count: 10, teamSize: 0, password: '' }
  genResult.value = null
  showGenerate.value = true
}

async function submitGenerate() {
  if (!genForm.value.prefix.trim()) {
    ElMessage.warning('请填写用户名前缀（如 class26，生成 class26-01、class26-02…）')
    return
  }
  generating.value = true
  try {
    genResult.value = await adminCompetitionApi.generateUsers(compId, {
      prefix: genForm.value.prefix.trim(),
      count: genForm.value.count,
      team_size: genForm.value.teamSize,
      password: genForm.value.password || undefined,
    })
    await Promise.all([loadParticipants(), loadDetail()])
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    generating.value = false
  }
}

// 导出生成账号（username,账号编码,初始密码,队伍；Excel 友好带 BOM）。
// 复用既有账号 initial_password 为空（沿用原密码）。
function exportGeneratedCsv() {
  if (!genResult.value) return
  const teamOf = new Map(genResult.value.participants.map((p) => [p.username, p.team_name ?? '']))
  const lines = ['\uFEFF用户名,账号编码,初始密码,队伍']
  for (const a of genResult.value.accounts) {
    lines.push(`${a.username},${a.account_code},${a.initial_password ?? ''},${teamOf.get(a.username) ?? ''}`)
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `参赛账号-${genForm.value.prefix.trim()}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

// ---------- 容器 ----------
const instances = ref<AdminLabInstance[]>([])
const instTotal = ref(0)
const instPage = ref(1)
const instLoading = ref(false)

async function loadInstances() {
  instLoading.value = true
  try {
    const resp = await adminCompetitionApi.instances(compId, instPage.value)
    instances.value = resp.items
    instTotal.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    instLoading.value = false
  }
}

// ---------- 反作弊 ----------
const warnings = ref<CheatWarningItem[]>([])
const warningsLoading = ref(false)

async function loadWarnings() {
  warningsLoading.value = true
  try {
    const resp = await adminCompetitionApi.warnings(compId)
    warnings.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    warningsLoading.value = false
  }
}

function warningLabel(k: CheatWarningItem['kind']) {
  return k === 'cross_team_flag' ? '跨队 flag' : 'IP 异常'
}
function warningType(k: CheatWarningItem['kind']) {
  return k === 'cross_team_flag' ? 'danger' : 'warning'
}

// ---------- WP（单位 = 题目 × 队伍/个人，仅返回已提交条目） ----------
const writeups = ref<WriteupItem[]>([])
const writeupsLoading = ref(false)
const viewing = ref<WriteupItem | null>(null)

async function loadWriteups() {
  writeupsLoading.value = true
  try {
    const resp = await adminCompetitionApi.writeups(compId)
    writeups.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    writeupsLoading.value = false
  }
}

// 按题目分组：题目标题 → 该题已提交的队伍/个人条目。
const writeupsByChallenge = computed(() => {
  const map = new Map<string, WriteupItem[]>()
  for (const w of writeups.value) {
    const key = w.challenge_title
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(w)
  }
  return [...map.entries()].map(([title, items]) => ({ title, items }))
})

// 该题未提交名单 = 参赛单位（队伍/个人）减已提交。
// teams 为队伍总览（参赛人员接口返回）；个人参赛者按 participants 当前页推导。
const wpMissing = (challengeId: number): string[] => {
  const submittedUnits = new Set(
    writeups.value
      .filter((w) => w.challenge_id === challengeId)
      .map((w) => w.team_name ?? `个人：${w.username}`),
  )
  const units: string[] = []
  for (const t of teams.value) {
    if (t.team_name && !submittedUnits.has(t.team_name)) units.push(t.team_name)
  }
  const seen = new Set(units)
  for (const p of participants.value) {
    const key = `个人：${p.username}`
    if (!p.team_id && !submittedUnits.has(key) && !seen.has(key)) {
      units.push(key)
      seen.add(key)
    }
  }
  return units
}

async function downloadWriteupZip(w: WriteupItem) {
  try {
    const blob = await adminCompetitionApi.writeupZip(compId, w.id)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = w.zip_name || 'writeup.zip'
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

// ---------- 排行榜 ----------
const boardMode = ref<'team' | 'individual'>('team')
const board = ref<TeamLeaderboardEntry[]>([])
const boardLoading = ref(false)

async function loadBoard() {
  boardLoading.value = true
  try {
    const resp = await adminCompetitionApi.leaderboard(compId, boardMode.value)
    board.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    boardLoading.value = false
  }
}

async function onBoardModeChange() {
  await loadBoard()
}

async function onTab(name: string | number) {
  // 懒加载：进入对应标签页时拉取数据。
  if (name === 'challenges' && !challenges.value.length) await loadChallenges()
  if (name === 'participants' && !participants.value.length) await loadParticipants()
  if (name === 'instances' && !instances.value.length) await loadInstances()
  if (name === 'warnings') await loadWarnings()
  if (name === 'writeups') await loadWriteups()
  if (name === 'leaderboard') await loadBoard()
}

onMounted(async () => {
  await loadDetail()
  syncForm()
  loading.value = false
})
</script>

<template>
  <div v-loading="loading">
    <div class="head">
      <h2>
        {{ detail?.name ?? '竞赛详情' }}
        <el-tag v-if="detail" :type="detail.kind === 'public' ? 'success' : 'warning'" size="small">
          {{ KIND_LABELS[detail.kind] }}
        </el-tag>
        <el-tag v-if="detail" :type="statusType(status)" size="small">{{ statusLabel(status) }}</el-tag>
      </h2>
      <router-link to="/admin/panel/competitions"><el-button size="small">← 返回列表</el-button></router-link>
    </div>
    <p class="hint">
      {{ fmt(detail?.start_time) }} ~ {{ fmt(detail?.end_time) }}
      <span v-if="detail?.description"> · {{ detail.description }}</span>
    </p>

    <!-- 统计卡片 -->
    <div v-if="detail" class="stats">
      <div class="stat"><b>{{ detail.stats.teams }}</b><span>战队</span></div>
      <div class="stat"><b>{{ detail.stats.participants }}</b><span>参赛人数</span></div>
      <div class="stat"><b>{{ detail.stats.entered }}</b><span>已入场</span></div>
      <div class="stat"><b>{{ detail.stats.challenges }}</b><span>题目</span></div>
      <div class="stat"><b>{{ detail.stats.instances }}</b><span>容器</span></div>
      <div class="stat warn"><b>{{ detail.stats.warnings }}</b><span>反作弊警告</span></div>
      <div class="stat"><b>{{ detail.stats.writeups }}</b><span>WP 提交</span></div>
    </div>

    <el-tabs v-model="activeTab" @tab-change="onTab">
      <!-- 配置 -->
      <el-tab-pane label="配置" name="config">
        <el-card shadow="never">
          <el-form label-width="90px" style="max-width: 560px">
            <el-form-item label="竞赛名称">
              <el-input v-model="form.name" maxlength="100" />
            </el-form-item>
            <el-form-item label="竞赛说明">
              <el-input v-model="form.description" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="开始时间">
              <el-date-picker v-model="form.start" type="datetime" :disabled="running" style="width: 100%" />
              <div v-if="running" class="dim">进行中的竞赛不能修改开赛时间，可先手动结束</div>
              <div v-else-if="status === 'ended'" class="dim">已结束的竞赛可重新配置起止时间，再次开赛</div>
            </el-form-item>
            <el-form-item label="结束时间">
              <el-date-picker v-model="form.end" type="datetime" style="width: 100%" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="saving" @click="saveConfig">保存配置</el-button>
              <el-button
                v-if="running"
                type="danger"
                plain
                :loading="ending"
                @click="endCompetition"
              >手动结束竞赛</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-tab-pane>

      <!-- 题目 -->
      <el-tab-pane label="题目" name="challenges">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>竞赛题目（{{ challenges.length }}）</span>
              <el-button type="primary" size="small" @click="openBind">绑定 / 导入题目</el-button>
            </div>
          </template>
          <el-table :data="challenges" v-loading="challengesLoading" empty-text="尚未绑定题目">
            <el-table-column prop="slug" label="Slug" width="180" />
            <el-table-column prop="title" label="标题" min-width="160" />
            <el-table-column prop="category" label="分类" width="110" />
            <el-table-column prop="difficulty" label="难度" width="100" />
            <el-table-column prop="score" label="分值" width="80" />
            <el-table-column label="容器" width="80">
              <template #default="{ row }">
                <el-tag v-if="row.has_env" size="small" type="success">有</el-tag>
                <el-tag v-else size="small" type="info">无</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="110">
              <template #default="{ row }">
                <el-button
                  size="small"
                  type="danger"
                  link
                  :disabled="running"
                  :loading="unbinding === row.slug"
                  @click="unbind(row)"
                >解绑</el-button>
              </template>
            </el-table-column>
          </el-table>
          <p v-if="running" class="dim">竞赛进行中不允许解绑题目</p>
        </el-card>
      </el-tab-pane>

      <!-- 参赛人员 -->
      <el-tab-pane :label="`参赛人员（${partTotal}）`" name="participants">
        <el-card shadow="never" class="block">
          <template #header>
            <div class="card-head">
              <span>参赛人员</span>
              <div v-if="invitational">
                <el-button size="small" @click="openGenerate">一键生成账号</el-button>
                <el-button type="primary" size="small" @click="openImport">表格导入队伍 / 人员</el-button>
              </div>
            </div>
          </template>
          <el-alert
            v-if="!invitational"
            type="info"
            :closable="false"
            class="block"
            title="公开赛无需导入参赛者"
            description="公开赛展示在竞赛广场，用户自助报名；已报名者开赛后免邀请码直接入场。"
          />
          <el-table :data="participants" v-loading="partLoading" empty-text="尚无参赛人员">
            <el-table-column prop="username" label="用户" width="130" />
            <el-table-column label="队伍" width="140">
              <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
            </el-table-column>
            <el-table-column prop="invite_code" label="邀请码" width="160">
              <template #default="{ row }"><code>{{ row.invite_code }}</code></template>
            </el-table-column>
            <el-table-column label="已入场" width="90">
              <template #default="{ row }">
                <el-tag :type="row.entered ? 'success' : 'info'" size="small">{{ row.entered ? '是' : '否' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="入场时间" width="160">
              <template #default="{ row }">{{ fmt(row.entered_at) }}</template>
            </el-table-column>
            <el-table-column label="最近 IP" width="130">
              <template #default="{ row }">{{ row.last_ip || '-' }}</template>
            </el-table-column>
          </el-table>
          <el-pagination
            background
            layout="total, prev, pager, next"
            :total="partTotal"
            :page-size="20"
            v-model:current-page="partPage"
            @current-change="loadParticipants"
            class="pager"
          />
        </el-card>
        <el-card shadow="never">
          <template #header><span>队伍总览（{{ teams.length }}）</span></template>
          <el-table :data="teams" empty-text="暂无队伍">
            <el-table-column prop="team_name" label="队伍" width="160" />
            <el-table-column label="成员" min-width="200">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 容器 -->
      <el-tab-pane label="容器" name="instances">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>竞赛容器（{{ instTotal }}）</span>
              <el-button size="small" @click="loadInstances">刷新</el-button>
            </div>
          </template>
          <el-table :data="instances" v-loading="instLoading" empty-text="暂无竞赛容器">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="username" label="用户" width="120" />
            <el-table-column prop="challenge_title" label="题目" min-width="160" />
            <el-table-column prop="status" label="状态" width="100" />
            <el-table-column label="Endpoint" min-width="150">
              <template #default="{ row }"><code v-if="row.endpoint">{{ row.endpoint }}</code><span v-else class="dim">-</span></template>
            </el-table-column>
            <el-table-column label="创建时间" width="160">
              <template #default="{ row }">{{ fmt(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-pagination
            background
            layout="total, prev, pager, next"
            :total="instTotal"
            :page-size="20"
            v-model:current-page="instPage"
            @current-change="loadInstances"
            class="pager"
          />
        </el-card>
      </el-tab-pane>

      <!-- 反作弊 -->
      <el-tab-pane :label="`反作弊${detail?.stats.warnings ? `（${detail.stats.warnings}）` : ''}`" name="warnings">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>反作弊警告（提交跨队 flag / 来源 IP 异常）</span>
              <el-button size="small" @click="loadWarnings">刷新</el-button>
            </div>
          </template>
          <el-table :data="warnings" v-loading="warningsLoading" empty-text="暂无警告">
            <el-table-column label="时间" width="160">
              <template #default="{ row }">{{ fmt(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="类型" width="110">
              <template #default="{ row }">
                <el-tag :type="warningType(row.kind)" size="small">{{ warningLabel(row.kind) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="username" label="人员" width="120" />
            <el-table-column label="队伍" width="140">
              <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
            </el-table-column>
            <el-table-column prop="reason" label="原因" min-width="240" show-overflow-tooltip />
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- WP（按题目分组） -->
      <el-tab-pane label="WP 提交情况" name="writeups">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>
                比赛结束后按题目提交 WP（单位 = 题目 × 队伍/个人，已提交
                {{ writeups.length }} 份）
              </span>
              <el-button size="small" @click="loadWriteups">刷新</el-button>
            </div>
          </template>
          <div v-loading="writeupsLoading">
            <template v-if="writeupsByChallenge.length">
              <div v-for="group in writeupsByChallenge" :key="group.title" class="wp-group">
                <h4>{{ group.title }}（{{ group.items.length }} 份）</h4>
                <el-table :data="group.items" size="small" empty-text="暂无提交">
                  <el-table-column label="队伍/选手" width="160">
                    <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
                  </el-table-column>
                  <el-table-column prop="username" label="最近修改人" width="120" />
                  <el-table-column label="提交时间" width="170">
                    <template #default="{ row }">{{ fmt(row.submitted_at) }}</template>
                  </el-table-column>
                  <el-table-column label="zip" width="110">
                    <template #default="{ row }">
                      <el-link
                        v-if="row.zip_name"
                        type="primary"
                        :underline="false"
                        @click="downloadWriteupZip(row)"
                      >
                        {{ row.zip_name }}
                      </el-link>
                      <span v-else class="dim">—</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="80">
                    <template #default="{ row }">
                      <el-button size="small" type="primary" link @click="viewing = row">查看</el-button>
                    </template>
                  </el-table-column>
                </el-table>
                <p v-if="wpMissing(group.items[0].challenge_id).length" class="dim wp-missing">
                  未提交：{{ wpMissing(group.items[0].challenge_id).join('、') }}
                </p>
              </div>
            </template>
            <el-empty v-else description="暂无 WP 提交" />
          </div>
        </el-card>
      </el-tab-pane>

      <!-- 排行榜 -->
      <el-tab-pane label="排行榜" name="leaderboard">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>竞赛排行榜</span>
              <el-radio-group v-model="boardMode" size="small" @change="onBoardModeChange">
                <el-radio-button value="team">战队榜</el-radio-button>
                <el-radio-button value="individual">个人榜</el-radio-button>
              </el-radio-group>
            </div>
          </template>
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
            <el-table-column label="最近通关" width="160">
              <template #default="{ row }">{{ fmt(row.last_solve_at) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>
  </div>

  <!-- 绑定/导入题目（既有 slug 绑定，或文件/粘贴导入 challenge.json） -->
  <el-dialog v-model="showBind" title="绑定 / 导入题目" width="760px">
    <el-radio-group v-model="bindMode" class="bind-mode">
      <el-radio-button value="slugs">绑定既有题目</el-radio-button>
      <el-radio-button value="json">导入题目 JSON</el-radio-button>
    </el-radio-group>
    <template v-if="bindMode === 'slugs'">
      <p class="dim">填写题库已有题目的 slug（逗号或换行分隔）。绑定后题目进入本场竞赛，题库侧不受影响。</p>
      <el-input v-model="bindSlugs" type="textarea" :rows="5" placeholder="web-lfi&#10;crypto-caesar" />
    </template>
    <template v-else>
      <p class="dim">
        与题目管理上传同一格式：可一次选择多个 .json 文件批量导入，也可选单个逐题导入，或直接粘贴 JSON（单对象或数组）。
        所有题目先经本地校验进入下方暂存列表，全部通过后一次性导入并绑定进本场（导入的题目同时进入题库（教程题目 tab），如需修改请到题目管理编辑）。
      </p>
      <div class="stage-actions">
        <el-upload multiple accept=".json,application/json" :show-file-list="false" :before-upload="onStageFile" :disabled="binding">
          <el-button size="small">选择 JSON 文件（可多选）</el-button>
        </el-upload>
        <el-input v-model="bindPaste" type="textarea" :rows="3" placeholder='或粘贴 challenge.json（单对象或数组）' />
        <el-button size="small" @click="stagePaste">解析并加入</el-button>
      </div>
      <el-table v-if="staged.length" :data="staged" size="small" class="stage-table" empty-text="暂存列表为空">
        <el-table-column label="来源" width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.source }}</template>
        </el-table-column>
        <el-table-column label="题目" min-width="160">
          <template #default="{ row }">
            {{ row.chal.title || '（未命名）' }}
            <span class="dim">（{{ row.chal.slug || '无 slug' }}）</span>
          </template>
        </el-table-column>
        <el-table-column label="校验" min-width="220">
          <template #default="{ row }">
            <el-tag v-if="!row.errors.length" type="success" size="small">通过</el-tag>
            <div v-else class="stage-errs">
              <el-tag type="danger" size="small">未通过</el-tag>
              <span>{{ row.errors.join('；') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button size="small" type="danger" link @click="removeStaged(row)">移除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <p v-else class="dim">尚未暂存任何题目：选择文件或粘贴 JSON 后将在此逐题列出。</p>
    </template>
    <template #footer>
      <el-button @click="showBind = false">取消</el-button>
      <el-button type="primary" :loading="binding" @click="submitBind">
        {{ bindMode === 'slugs' ? '绑定' : `导入并绑定（${staged.length} 题）` }}
      </el-button>
    </template>
  </el-dialog>

  <!-- 参赛人员表格导入（固定格式 CSV；仅邀请赛） -->
  <el-dialog v-model="showImport" title="表格导入队伍 / 参赛人员" width="720px">
    <p class="dim">
      条目 = 账号编码（YA26-00001）或用户名，引用既有账号（不自动建号，缺失条目在导入结果中列出——
      可用「一键生成账号」补号后重导）。每人生成唯一邀请码并投递站内邀请邮件。
    </p>
    <el-radio-group v-model="importMode" class="bind-mode">
      <el-radio-button value="individuals">个人名单</el-radio-button>
      <el-radio-button value="teams">队伍名单</el-radio-button>
    </el-radio-group>
    <div class="stage-actions">
      <div class="tpl-row">
        <el-upload accept=".csv,text/csv" :show-file-list="false" :before-upload="onImportFile" :disabled="importing">
          <el-button size="small">选择 CSV 文件</el-button>
        </el-upload>
        <el-button size="small" link type="primary" @click="downloadTemplate(importMode)">下载模板</el-button>
        <span class="dim tpl-hint">
          {{ importMode === 'individuals' ? '每行一条：账号编码或用户名' : '每行：队伍名,成员1,成员2,…（成员 = 账号编码或用户名）' }}
        </span>
      </div>
    </div>
    <template v-if="importMode === 'individuals'">
      <el-table v-if="stagedIndividuals.length" :data="stagedIndividuals.map((v, i) => ({ i, v }))" size="small" max-height="240">
        <el-table-column label="序号" width="70">
          <template #default="{ row }">{{ row.i + 1 }}</template>
        </el-table-column>
        <el-table-column label="条目（账号编码或用户名）">
          <template #default="{ row }">
            <code>{{ row.v }}</code>
            <el-button size="small" type="danger" link class="stage-del" @click="stagedIndividuals.splice(row.i, 1)">移除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <p v-else class="dim">尚未解析到条目：选择 CSV 文件后在此列出，可反复上传合并。</p>
    </template>
    <template v-else>
      <el-table v-if="stagedTeams.length" :data="stagedTeams" size="small" max-height="240">
        <el-table-column prop="name" label="队伍" width="140" />
        <el-table-column label="成员">
          <template #default="{ row }">
            <code v-for="m in row.members" :key="m" class="member-chip">{{ m }}</code>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button size="small" type="danger" link @click="removeStagedTeam(row)">移除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <p v-else class="dim">尚未解析到队伍：选择 CSV 文件后在此列出，可反复上传合并。</p>
    </template>
    <template #footer>
      <el-button @click="showImport = false">取消</el-button>
      <el-button
        type="primary"
        :loading="importing"
        :disabled="!stagedIndividuals.length && !stagedTeams.length"
        @click="submitImport"
      >
        导入{{ stagedTeams.length ? `（${stagedTeams.length} 队` : '' }}{{ stagedTeams.length && stagedIndividuals.length ? ' + ' : '' }}{{ stagedIndividuals.length ? `${stagedIndividuals.length} 人` : '' }}）
      </el-button>
    </template>
  </el-dialog>

  <!-- 导入结果 -->
  <el-dialog v-model="showResult" title="导入结果" width="760px">
    <el-alert
      v-if="importResult && importResult.missing.length"
      type="warning"
      :closable="false"
      class="block"
      :title="`以下 ${importResult.missing.length} 个条目未匹配到账号（账号编码与用户名均未命中），未导入：`"
      :description="importResult.missing.join('、') + '。可修正条目重导，或先用「一键生成账号」补号。'"
    />
    <el-table v-if="importResult" :data="importResult.participants" empty-text="无参赛者">
      <el-table-column prop="username" label="参赛者" width="130" />
      <el-table-column prop="account_code" label="账号编码" width="130">
        <template #default="{ row }"><code>{{ row.account_code || '-' }}</code></template>
      </el-table-column>
      <el-table-column label="队伍" width="140">
        <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
      </el-table-column>
      <el-table-column prop="invite_code" label="邀请码" width="160">
        <template #default="{ row }"><code>{{ row.invite_code }}</code></template>
      </el-table-column>
      <el-table-column label="邮件" width="90">
        <template #default="{ row }">
          <el-tag :type="row.mail_delivered ? 'success' : 'danger'" size="small">{{ row.mail_delivered ? '已投递' : '失败' }}</el-tag>
        </template>
      </el-table-column>
    </el-table>
    <template #footer>
      <el-button @click="showResult = false">关闭</el-button>
    </template>
  </el-dialog>

  <!-- 一键生成账号（仅邀请赛） -->
  <el-dialog v-model="showGenerate" title="一键生成参赛账号" width="680px">
    <template v-if="!genResult">
      <p class="dim">
        按前缀批量生成账号（用户名 = 前缀-零填充序号，如 class26-01…class26-10）并全部导入为本场参赛者；
        可配置自动组队（按生成顺序每 N 人一队，余数进最后一队）。初始密码默认 123456。
      </p>
      <el-form label-width="90px">
        <el-form-item label="用户名前缀" required>
          <el-input v-model="genForm.prefix" placeholder="如 class26（1-16 位小写字母/数字/连字符）" />
        </el-form-item>
        <el-form-item label="生成数量" required>
          <el-input-number v-model="genForm.count" :min="1" :max="500" />
        </el-form-item>
        <el-form-item label="每队人数">
          <el-input-number v-model="genForm.teamSize" :min="0" :max="10" />
          <div class="dim">0 = 不组队（个人参赛）；&gt;0 = 自动编入「第 N 队」</div>
        </el-form-item>
        <el-form-item label="初始密码">
          <el-input v-model="genForm.password" placeholder="留空 = 默认 123456" clearable />
        </el-form-item>
      </el-form>
      <p class="dim">同名既有账号复用（沿用原密码），新建账号的初始密码仅在生成结果中显示，请导出后立即分发。</p>
    </template>
    <template v-else>
      <el-alert type="success" :closable="false" class="block"
        :title="`生成完成：新建 ${genResult.accounts.filter((a) => a.created).length} 个账号、复用 ${genResult.accounts.filter((a) => !a.created).length} 个既有账号${genResult.teams.length ? `，编入 ${genResult.teams.length} 支队伍` : ''}。`" />
      <el-table :data="genResult.accounts" size="small" max-height="320">
        <el-table-column prop="username" label="用户名" width="140" />
        <el-table-column prop="account_code" label="账号编码" width="130">
          <template #default="{ row }"><code>{{ row.account_code }}</code></template>
        </el-table-column>
        <el-table-column label="初始密码" width="140">
          <template #default="{ row }"><code>{{ row.initial_password ?? '（复用既有账号，沿用原密码）' }}</code></template>
        </el-table-column>
        <el-table-column label="队伍" min-width="120">
          <template #default="{ row }">
            {{ genResult.participants.find((p) => p.username === row.username)?.team_name ?? '（个人）' }}
          </template>
        </el-table-column>
      </el-table>
    </template>
    <template #footer>
      <template v-if="!genResult">
        <el-button @click="showGenerate = false">取消</el-button>
        <el-button type="primary" :loading="generating" @click="submitGenerate">生成并导入</el-button>
      </template>
      <template v-else>
        <el-button @click="showGenerate = false">关闭</el-button>
        <el-button type="primary" @click="exportGeneratedCsv">导出账号 CSV</el-button>
        <el-button @click="genResult = null">再生成一批</el-button>
      </template>
    </template>
  </el-dialog>

  <!-- WP 查看 -->
  <el-dialog
    :model-value="viewing !== null"
    :title="`WP：${viewing?.challenge_title ?? ''} · ${viewing?.team_name ?? viewing?.username ?? ''}`"
    width="680px"
    @close="viewing = null"
  >
    <p v-if="viewing" class="dim wp-dialog-meta">
      最近修改：{{ viewing.username }} · {{ fmt(viewing.submitted_at) }}
      <el-link
        v-if="viewing.zip_name"
        type="primary"
        :underline="false"
        class="wp-dialog-zip"
        @click="downloadWriteupZip(viewing)"
      >
        下载 zip：{{ viewing.zip_name }}
      </el-link>
    </p>
    <pre class="wp">{{ viewing?.content || '（仅上传了 zip，未填写文字）' }}</pre>
  </el-dialog>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; }
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.stats { display: flex; gap: 0.8rem; flex-wrap: wrap; margin: 0.8rem 0 1.2rem; }
.stat {
  min-width: 96px; padding: 0.6rem 1rem; text-align: center;
  border: 1px solid var(--el-border-color-lighter); border-radius: 6px;
  display: flex; flex-direction: column; gap: 0.2rem;
}
.stat b { font-size: 1.3rem; }
.stat span { color: var(--el-text-color-secondary); font-size: 0.8rem; }
.stat.warn b { color: var(--el-color-danger); }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.block { margin-bottom: 1.2rem; }
.dim { color: var(--el-text-color-placeholder); font-size: 0.8rem; }
.pager { margin-top: 1rem; }
.bind-mode { margin-bottom: 0.8rem; }
.tpl-row { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
.tpl-hint { margin: 0; }
.stage-del { margin-left: 0.8rem; }
.member-chip {
  display: inline-block; margin: 0 0.4rem 0.3rem 0; padding: 0 0.4rem;
  background: var(--el-fill-color-light); border-radius: 3px;
}
.stage-actions { display: flex; flex-direction: column; gap: 0.5rem; align-items: flex-start; margin-bottom: 0.8rem; }
.stage-actions .el-button { margin-left: 0; }
.stage-table { margin-bottom: 0.4rem; }
.stage-errs { display: flex; align-items: flex-start; gap: 0.4rem; }
.stage-errs span { color: var(--el-color-danger); font-size: 0.82rem; line-height: 1.4; }
.wp { white-space: pre-wrap; word-break: break-word; max-height: 60vh; overflow: auto; }
.wp-group { margin-bottom: 1.4rem; }
.wp-group h4 { margin: 0 0 0.5rem; }
.wp-missing { margin: 0.4rem 0 0; }
.wp-dialog-meta { margin: 0 0 0.6rem; }
.wp-dialog-zip { margin-left: 1rem; }
</style>
