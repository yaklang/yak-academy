<script setup lang="ts">
// 管理面板：竞赛详情页。配置编辑 + 题目绑定/批量导入 + 参赛人员管理
// （邀请赛：固定格式表格导入 / 一键生成账号并导出；公开赛：广场自助报名）
// + 竞赛容器 + 反作弊警告 + 解题报告提交情况 + 双榜。
// 状态由起止时间派生：running 期间禁改 start_time、禁解绑题目（后端同样校验）；
// 进行中的竞赛可手动结束，已结束的竞赛可重新配置起止时间再次开赛。
import { computed, nextTick, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { Button } from '@/components/ui/button'
import PageHeader from '@/components/layout/PageHeader.vue'
import MetricStrip from '@/components/layout/MetricStrip.vue'
import StatCard from '@/components/layout/StatCard.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import FormSection from '@/components/layout/FormSection.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import CompetitionCertificatePanel from '@/components/competition/CompetitionCertificatePanel.vue'
import CompetitionQualificationPanel from '@/components/competition/CompetitionQualificationPanel.vue'
import {
  adminChallengeApi,
  adminCompetitionApi,
  FORMAT_LABELS,
  KIND_LABELS,
  SCORING_MODE_LABELS,
  type AdminLabInstance,
  type ChallengeJSON,
  type CheatWarningItem,
  type Competition,
  type CompetitionDetail,
  type CompetitionFormat,
  type CompetitionParticipantItem,
  type CompetitionRosterEntry,
  type CompetitionRosterImportResult,
  type CompetitionRosterRow,
  type CompetitionScoringMode,
  type CtfAdjustment,
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
const loadError = ref('')
const activeTab = ref('config')

async function loadDetail() {
  loading.value = true
  loadError.value = ''
  try {
    detail.value = await adminCompetitionApi.detail(compId)
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '竞赛详情加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function reloadDetail() {
  await loadDetail()
  syncForm()
}

const status = computed<Competition['status']>(() => detail.value?.status ?? 'scheduled')
const running = computed(() => status.value === 'running')

function fmt(ts: string | null | undefined) {
  return formatDateTime(ts)
}

// ---------- 配置 ----------
const form = ref({
  name: '',
  description: '',
  start: null as Date | null,
  end: null as Date | null,
  scoringMode: 'dynamic' as CompetitionScoringMode,
  bonusCap: 0,
  registrationEnabled: false,
  registrationRange: null as [Date, Date] | null,
})
const saving = ref(false)

function syncForm() {
  if (!detail.value) return
  const regStart = detail.value.registration_start ? new Date(detail.value.registration_start) : null
  const regEnd = detail.value.registration_end ? new Date(detail.value.registration_end) : null
  form.value = {
    name: detail.value.name,
    description: detail.value.description,
    start: new Date(detail.value.start_time),
    end: new Date(detail.value.end_time),
    scoringMode: detail.value.scoring_mode,
    bonusCap: detail.value.bonus_cap ?? 0,
    registrationEnabled: !!(regStart && regEnd),
    registrationRange: regStart && regEnd ? [regStart, regEnd] : null,
  }
}

async function saveConfig() {
  if (!form.value.name.trim() || !form.value.start || !form.value.end) {
    notify.warning('名称与起止时间必填')
    return
  }
  if (form.value.registrationEnabled && !form.value.registrationRange) {
    notify.warning('请选择报名起止时间，或关闭报名窗口')
    return
  }
  if (form.value.registrationEnabled && form.value.registrationRange) {
    const [, regEnd] = form.value.registrationRange
    if (regEnd > form.value.end) {
      notify.warning('报名截止时间不能晚于竞赛结束时间')
      return
    }
  }
  if (!Number.isInteger(form.value.bonusCap) || form.value.bonusCap < 0) {
    notify.warning('附加分上限必须为非负整数（0 = 不限制）')
    return
  }
  const body: {
    name: string
    description: string
    start_time: string
    end_time: string
    scoring_mode?: CompetitionScoringMode
    bonus_cap?: number
    registration_start?: string | null
    registration_end?: string | null
    clear_registration?: boolean
  } = {
    name: form.value.name.trim(),
    description: form.value.description,
    start_time: form.value.start.toISOString(),
    end_time: form.value.end.toISOString(),
  }
  if (detail.value?.format === 'ctf' && status.value === 'scheduled') {
    body.scoring_mode = form.value.scoringMode
  }
  if (detail.value?.format === 'ctf') {
    body.bonus_cap = form.value.bonusCap
  }
  if (form.value.registrationEnabled && form.value.registrationRange) {
    body.registration_start = form.value.registrationRange[0].toISOString()
    body.registration_end = form.value.registrationRange[1].toISOString()
  } else if (detail.value?.registration_start || detail.value?.registration_end) {
    body.clear_registration = true
  }
  saving.value = true
  try {
    detail.value = await adminCompetitionApi.update(compId, body)
    syncForm()
    notify.success('配置已保存')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

// 手动结束竞赛（仅 running）：end_time 置为当前时刻；参赛者解锁、解题报告开放提交。
// 结束后仍可重新配置起止时间再次开赛。
const ending = ref(false)

async function endCompetition() {
  try {
    await dialogs.confirm(
      `手动结束竞赛「${detail.value?.name}」？立即生效：邀请赛参赛者解除锁定、停止提交与容器，解题报告转为可提交。结束后仍可重新配置起止时间再次开赛。`,
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
    notify.success('竞赛已结束')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    ending.value = false
  }
}

// ---------- 题目 ----------
const challenges = ref<TrainingItem[]>([])
const challengesLoading = ref(true)
const challengesError = ref('')

async function loadChallenges() {
  challengesLoading.value = true
  challengesError.value = ''
  challenges.value = []
  try {
    const resp = await adminCompetitionApi.challenges(compId)
    challenges.value = resp.items
  } catch (e) {
    challengesError.value = e instanceof Error ? e.message : '题目列表加载失败，请稍后重试'
  } finally {
    challengesLoading.value = false
  }
}

const showBind = ref(false)
const bindMode = ref<'pick' | 'slugs' | 'json'>('pick')
const bindSlugs = ref('')
const binding = ref(false)
const unbinding = ref('')

// 从题库勾选：管理端题目列表（GET /admin/challenges，搜索/分页/bank 过滤）
// + 表格多选。已绑定题目禁选并打「已绑定」标记（绑定为幂等，但再选只是
// 白跑一趟，直接禁掉更清楚）。勾选跨页保留（selection 与列表数据独立维护）。
const pickQuery = ref('')
const pickBank = ref<'ctf' | 'acm' | 'tutorial' | ''>('')
const pickPage = ref(1)
const pickTotal = ref(0)
const pickItems = ref<TrainingItem[]>([])
const pickLoading = ref(false)
const pickError = ref('')
const pickedSlugs = ref<string[]>([])
const pickTable = ref()
// 翻页回填 selection 的过渡窗口：此间 selection-change 是数据替换的副作用，
// 不同步 pickedSlugs（避免跨页勾选被清空事件误删）。
const suspending = ref(false)

async function loadPickList() {
  pickLoading.value = true
  pickError.value = ''
  pickItems.value = []
  pickTotal.value = 0
  try {
    const resp = await adminChallengeApi.list({
      page: pickPage.value,
      q: pickQuery.value.trim() || undefined,
      bank: pickBank.value || undefined,
    })
    pickItems.value = resp.items
    pickTotal.value = resp.total
    // 数据替换会触发 selection-change（全量清空）——先挂起同步，回填完成再恢复，
    // 否则跨页勾选会被这次清空事件误删。
    suspending.value = true
    restorePickSelection()
    await nextTick()
    suspending.value = false
  } catch (e) {
    pickError.value = e instanceof Error ? e.message : '可选题库加载失败，请稍后重试'
  } finally {
    pickLoading.value = false
  }
}

// selectable：已绑定的题目禁选（按 slug 对照当前已绑定列表）。
function pickSelectable(row: TrainingItem) {
  return !challenges.value.some((c) => c.slug === row.slug)
}

// 翻页/过滤后恢复此前页勾上的行（selection 随数据清空，需手动回填）。
function restorePickSelection() {
  const table = pickTable.value
  if (!table) return
  for (const row of pickItems.value) {
    if (pickedSlugs.value.includes(row.slug)) table.toggleRowSelection(row, true)
  }
}

function searchPick() {
  pickPage.value = 1
  loadPickList()
}

// 表格 selection-change：以 pickedSlugs 为准增删当前页变化量
//（跨页勾选的行不在当前页数据里，保留不动）。suspending 窗口内跳过（回填）。
function onPickSelectionChange(rows: TrainingItem[]) {
  if (suspending.value) return
  const pageSlugs = pickItems.value.map((i) => i.slug)
  const offPage = pickedSlugs.value.filter((s) => !pageSlugs.includes(s))
  const onPage = rows.map((r) => r.slug)
  pickedSlugs.value = [...offPage, ...onPage]
}

const pickedCount = computed(() => pickedSlugs.value.length)

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
  bindMode.value = 'pick'
  bindSlugs.value = ''
  staged.value = []
  bindPaste.value = ''
  pickedSlugs.value = []
  pickQuery.value = ''
  pickBank.value = ''
  pickPage.value = 1
  showBind.value = true
  loadPickList()
}

function stageChallenges(text: string, source: string): boolean {
  const { list, error } = parseChallengeJSON(text)
  if (error) {
    notify.warning(`${source}：${error}`)
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
    if (bindMode.value === 'pick') {
      if (!pickedSlugs.value.length) {
        notify.warning('请先勾选要绑定的题目')
        return
      }
      body = { slugs: pickedSlugs.value }
    } else if (bindMode.value === 'slugs') {
      const slugs = bindSlugs.value.split(/[\s,，;；]+/).filter(Boolean)
      if (!slugs.length) {
        notify.warning('请填写题目 slug')
        return
      }
      body = { slugs }
    } else {
      if (!staged.value.length) {
        notify.warning('请先选择题目 JSON 文件或粘贴内容')
        return
      }
      if (stagedInvalid.value) {
        notify.warning('暂存列表中存在校验未通过的题目，请修正文件或移除对应条目')
        return
      }
      body = staged.value.map((s) => s.chal)
    }
    const resp = await adminCompetitionApi.bindChallenges(compId, body)
    notify.success(`绑定完成：新增 ${resp.added} 题`)
    showBind.value = false
    await Promise.all([loadChallenges(), loadDetail()])
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    binding.value = false
  }
}

async function unbind(item: TrainingItem) {
  try {
    await dialogs.confirm(`解绑题目「${item.title}」（${item.slug}）？参赛者将无法再访问该题。`, '解绑确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  unbinding.value = item.slug
  try {
    const resp = await adminCompetitionApi.unbindChallenges(compId, [item.slug])
    notify.success(`已解绑 ${resp.removed} 题`)
    await Promise.all([loadChallenges(), loadDetail()])
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    unbinding.value = ''
  }
}

// ---------- 参赛人员 ----------
const participants = ref<CompetitionParticipantItem[]>([])
const rosterRows = ref<CompetitionRosterRow[]>([])
const rosterByUser = computed(() => {
  const map: Record<number, CompetitionRosterRow> = {}
  for (const row of rosterRows.value) map[row.user_id] = row
  return map
})
const teams = ref<TeamLeaderboardEntry[]>([])
const partTotal = ref(0)
const partPage = ref(1)
const partLoading = ref(true)
const partError = ref('')

async function loadParticipants() {
  partLoading.value = true
  partError.value = ''
  participants.value = []
  teams.value = []
  partTotal.value = 0
  try {
    const [resp, roster] = await Promise.all([
      adminCompetitionApi.participants(compId, partPage.value),
      adminCompetitionApi.roster(compId),
    ])
    participants.value = resp.items
    teams.value = resp.teams
    partTotal.value = resp.total
    rosterRows.value = roster.items
  } catch (e) {
    partError.value = e instanceof Error ? e.message : '参赛人员加载失败，请稍后重试'
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
    notify.warning('请先上传名单表格（每行一条：账号编码或用户名）')
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
    notify.error((e as Error).message)
  } finally {
    importing.value = false
  }
}

// ---------- 身份名册 CSV 导入（仅邀请赛） ----------
// 列支持中英文表头；条目引用既有账号，缺失条目不阻断并在结果中列出。
const showRoster = ref(false)
const importingRoster = ref(false)
const stagedRoster = ref<CompetitionRosterEntry[]>([])
const rosterResult = ref<CompetitionRosterImportResult | null>(null)
const showRosterResult = ref(false)

const ROSTER_HEADER_ALIASES: Record<string, keyof CompetitionRosterEntry> = {
  entry: 'entry',
  account: 'entry',
  username: 'entry',
  账号: 'entry',
  账号编码: 'entry',
  用户名: 'entry',
  realname: 'real_name',
  name: 'real_name',
  姓名: 'real_name',
  真实姓名: 'real_name',
  school: 'school',
  学校: 'school',
  单位: 'school',
  studentno: 'student_no',
  学号: 'student_no',
  email: 'email',
  邮箱: 'email',
  phone: 'phone',
  手机: 'phone',
  手机号: 'phone',
  电话: 'phone',
}

function openRoster() {
  stagedRoster.value = []
  rosterResult.value = null
  showRoster.value = true
}

function normalizeRosterHeader(value: string) {
  return value.replace(/^\uFEFF/, '').trim().toLowerCase().replace(/\s+/g, '')
}

// 支持双引号包裹（含逗号）的简化 CSV 行解析；空行由调用方跳过。
function parseRosterCsvLine(line: string): string[] {
  const cells: string[] = []
  let current = ''
  let quoted = false
  for (let i = 0; i < line.length; i += 1) {
    const char = line[i]
    if (quoted) {
      if (char === '"' && line[i + 1] === '"') {
        current += '"'
        i += 1
      } else if (char === '"') {
        quoted = false
      } else {
        current += char
      }
    } else if (char === '"') {
      quoted = true
    } else if (char === ',' || char === '，') {
      cells.push(current.trim())
      current = ''
    } else {
      current += char
    }
  }
  cells.push(current.trim())
  return cells
}

function parseRosterCsv(text: string): CompetitionRosterEntry[] {
  const lines = text.split(/\r?\n/).filter((line) => line.trim())
  if (!lines.length) return []
  const headerCells = parseRosterCsvLine(lines[0])
  const mapped = headerCells.map((cell) => ROSTER_HEADER_ALIASES[normalizeRosterHeader(cell)])
  const hasHeader = mapped.some(Boolean)
  const columns: Array<keyof CompetitionRosterEntry | null> = hasHeader
    ? mapped
    : ['entry', 'real_name', 'school', 'student_no', 'email', 'phone']
  const entries: CompetitionRosterEntry[] = []
  for (const line of lines.slice(hasHeader ? 1 : 0)) {
    const cells = parseRosterCsvLine(line)
    const entry: CompetitionRosterEntry = {
      entry: '',
      real_name: '',
      school: '',
      student_no: '',
      email: '',
      phone: '',
    }
    columns.forEach((column, index) => {
      if (!column) return
      entry[column] = cells[index]?.trim() ?? ''
    })
    if (entry.entry) entries.push(entry)
  }
  return entries
}

async function onRosterFile(file: File) {
  const parsed = parseRosterCsv(await file.text())
  const valid = parsed.filter((entry) => entry.real_name)
  const skipped = parsed.length - valid.length
  const merged = new Map(stagedRoster.value.map((entry) => [entry.entry, entry]))
  for (const entry of valid) merged.set(entry.entry, entry)
  stagedRoster.value = Array.from(merged.values())
  if (skipped) notify.warning(`已跳过 ${skipped} 行缺少姓名的记录`)
  if (!parsed.length) notify.warning('未解析到名册数据，请检查表头或编码')
  return false
}

function downloadRosterTemplate() {
  const content =
    '\uFEFF账号编码,姓名,学校,学号,邮箱,手机\n' +
    'YA26-00001,张三,示例大学,2026001,zhangsan@example.com,13800000000\n'
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = '竞赛身份名册.csv'
  link.click()
  URL.revokeObjectURL(url)
}

async function submitRoster() {
  if (!stagedRoster.value.length) {
    notify.warning('请先上传名册 CSV')
    return
  }
  importingRoster.value = true
  try {
    rosterResult.value = await adminCompetitionApi.importRoster(compId, stagedRoster.value)
    showRoster.value = false
    showRosterResult.value = true
    await loadParticipants()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    importingRoster.value = false
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
    notify.warning('请填写用户名前缀（如 class26，生成 class26-01、class26-02…）')
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
    notify.error((e as Error).message)
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
const instLoading = ref(true)
const instError = ref('')

async function loadInstances() {
  instLoading.value = true
  instError.value = ''
  instances.value = []
  instTotal.value = 0
  try {
    const resp = await adminCompetitionApi.instances(compId, instPage.value)
    instances.value = resp.items
    instTotal.value = resp.total
  } catch (e) {
    instError.value = e instanceof Error ? e.message : '竞赛容器加载失败，请稍后重试'
  } finally {
    instLoading.value = false
  }
}

// ---------- 反作弊 ----------
const warnings = ref<CheatWarningItem[]>([])
const warningsLoading = ref(true)
const warningsError = ref('')

async function loadWarnings() {
  warningsLoading.value = true
  warningsError.value = ''
  warnings.value = []
  try {
    const resp = await adminCompetitionApi.warnings(compId)
    warnings.value = resp.items
  } catch (e) {
    warningsError.value = e instanceof Error ? e.message : '警告列表加载失败，请稍后重试'
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

// ---------- 解题报告（单位 = 题目 × 队伍/个人，仅返回已提交条目） ----------
const writeups = ref<WriteupItem[]>([])
const writeupsLoading = ref(true)
const writeupsError = ref('')
const viewing = ref<WriteupItem | null>(null)

async function loadWriteups() {
  writeupsLoading.value = true
  writeupsError.value = ''
  writeups.value = []
  try {
    const resp = await adminCompetitionApi.writeups(compId)
    writeups.value = resp.items
  } catch (e) {
    writeupsError.value = e instanceof Error ? e.message : '解题报告列表加载失败，请稍后重试'
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
    notify.error((e as Error).message)
  }
}

// ---------- 排行榜 ----------
const boardMode = ref<'team' | 'individual'>('team')
const board = ref<TeamLeaderboardEntry[]>([])
const boardLoading = ref(true)
const boardError = ref('')
const boardExporting = ref(false)

async function loadBoard() {
  boardLoading.value = true
  boardError.value = ''
  board.value = []
  try {
    const resp = await adminCompetitionApi.leaderboard(compId, boardMode.value)
    board.value = resp.items
  } catch (e) {
    boardError.value = e instanceof Error ? e.message : '排行榜加载失败，请稍后重试'
  } finally {
    boardLoading.value = false
  }
}

async function onBoardModeChange() {
  await loadBoard()
}

async function exportBoard() {
  boardExporting.value = true
  try {
    const blob = await adminCompetitionApi.standingsCsv(compId, boardMode.value)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `竞赛榜单-${boardMode.value === 'team' ? '战队' : '个人'}.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '榜单导出失败，请稍后重试')
  } finally {
    boardExporting.value = false
  }
}

// ---------- 附加分（append-only） ----------
const adjustments = ref<CtfAdjustment[]>([])
const adjustmentsLoading = ref(true)
const adjustmentsError = ref('')
const showAdjustment = ref(false)
const adjustmentSaving = ref(false)
const adjustmentCandidates = ref<CompetitionParticipantItem[]>([])
const adjustmentForm = ref({ userId: null as number | null, points: 10, reason: '' })

async function loadAdjustments() {
  adjustmentsLoading.value = true
  adjustmentsError.value = ''
  adjustments.value = []
  try {
    const resp = await adminCompetitionApi.adjustments(compId)
    adjustments.value = resp.items
  } catch (e) {
    adjustmentsError.value = e instanceof Error ? e.message : '附加分加载失败，请稍后重试'
  } finally {
    adjustmentsLoading.value = false
  }
}

async function openAdjustment() {
  adjustmentForm.value = { userId: null, points: 10, reason: '' }
  try {
    const resp = await adminCompetitionApi.participants(compId, 1, 1000)
    adjustmentCandidates.value = resp.items
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '参赛人员加载失败')
    return
  }
  showAdjustment.value = true
}

async function submitAdjustment() {
  if (!adjustmentForm.value.userId) {
    notify.warning('请选择参赛选手')
    return
  }
  if (!Number.isInteger(adjustmentForm.value.points) || adjustmentForm.value.points === 0) {
    notify.warning('附加分必须为非 0 整数')
    return
  }
  if (adjustmentForm.value.reason.trim().length < 2) {
    notify.warning('请填写附加分原因')
    return
  }
  adjustmentSaving.value = true
  try {
    await adminCompetitionApi.addAdjustment(compId, {
      user_id: adjustmentForm.value.userId,
      points: adjustmentForm.value.points,
      reason: adjustmentForm.value.reason.trim(),
    })
    showAdjustment.value = false
    notify.success('附加分已追加')
    await Promise.all([loadAdjustments(), loadBoard()])
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '附加分追加失败')
  } finally {
    adjustmentSaving.value = false
  }
}

// 晋级/证书共享面板：只由本页派生状态门控，数据加载与操作在面板内完成。
const qualificationDisabled = computed(() => status.value !== 'scheduled')
const certificateAllowed = computed(() => status.value === 'ended')

async function onTab(name: string | number) {
  // 懒加载：进入对应标签页时拉取数据。
  if (name === 'challenges' && !challenges.value.length) await loadChallenges()
  if (name === 'participants' && !participants.value.length) await loadParticipants()
  if (name === 'instances' && !instances.value.length) await loadInstances()
  if (name === 'warnings') await loadWarnings()
  if (name === 'writeups') await loadWriteups()
  if (name === 'leaderboard') await loadBoard()
  if (name === 'adjustments') await loadAdjustments()
}

onMounted(async () => {
  await loadDetail()
  syncForm()
})
</script>

<template>
  <div v-if="loading" class="detail-skeleton" role="status" aria-label="正在加载竞赛详情">
    <div class="sk-head" aria-hidden="true">
      <i class="sk-line title" />
      <i class="sk-line medium" />
    </div>
    <div class="sk-stats" aria-hidden="true">
      <div v-for="n in 4" :key="n" class="sk-stat"><i class="sk-line short" /><i class="sk-line medium" /></div>
    </div>
    <div class="sk-tabs" aria-hidden="true"><i v-for="n in 5" :key="n" /></div>
    <div class="sk-panel" aria-hidden="true"><i class="sk-line" /><i class="sk-line medium" /><i class="sk-line short" /></div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="竞赛详情加载失败"
    :description="loadError"
    action-label="重试"
    @action="reloadDetail"
  />
  <div v-else>
    <PageHeader
      :title="detail?.name ?? '竞赛详情'"
      description="起止时间决定竞赛状态；进行中不能改开赛时间或解绑题目，结束后可重新配置起止时间再次开赛。"
    >
      <template #badges>
        <StatusTag kind="competition" :value="status" />
        <span v-if="detail" class="meta-chip">{{ KIND_LABELS[detail.kind] }}</span>
        <span v-if="detail" class="meta-chip">{{ FORMAT_LABELS[detail.format as CompetitionFormat] }}</span>
        <span v-if="detail && detail.format === 'ctf'" class="meta-chip">{{ SCORING_MODE_LABELS[detail.scoring_mode] }}</span>
      </template>
      <template #actions>
        <Button variant="outline" as-child>
          <RouterLink to="/admin/panel/competitions">返回列表</RouterLink>
        </Button>
      </template>
    </PageHeader>
    <p class="hint">
      {{ fmt(detail?.start_time) }} ~ {{ fmt(detail?.end_time) }}
      <span v-if="detail?.registration_start && detail?.registration_end">
        · 报名 {{ fmt(detail.registration_start) }} ~ {{ fmt(detail.registration_end) }}
      </span>
      <span v-if="detail?.description"> · {{ detail.description }}</span>
    </p>

    <!-- 统计卡片 -->
    <MetricStrip v-if="detail" class="stats" :columns="4">
      <StatCard label="战队" :value="detail.stats.teams" />
      <StatCard label="参赛人数" :value="detail.stats.participants" />
      <StatCard label="已入场" :value="detail.stats.entered" />
      <StatCard label="题目" :value="detail.stats.challenges" />
      <StatCard label="容器" :value="detail.stats.instances" />
      <StatCard label="反作弊警告" :value="detail.stats.warnings" :tone="detail.stats.warnings ? 'danger' : 'neutral'" />
      <StatCard label="解题报告" :value="detail.stats.writeups" />
    </MetricStrip>

    <UiTabs v-model="activeTab" class="detail-tabs" @tab-change="onTab">
      <!-- 配置 -->
      <UiTabPane label="配置" name="config">
        <FormSection title="竞赛配置" description="起止时间决定竞赛状态；计分模式开赛后锁定。">
          <UiForm label-width="90px" class="config-form">
            <UiFormItem label="竞赛名称">
              <UiInput v-model="form.name" maxlength="100" />
            </UiFormItem>
            <UiFormItem label="竞赛说明">
              <UiInput v-model="form.description" type="textarea" :rows="3" />
            </UiFormItem>
            <UiFormItem label="开始时间">
              <UiDatePicker v-model="form.start" type="datetime" :disabled="running" class="date-full" />
              <div v-if="running" class="dim">进行中的竞赛不能修改开赛时间，可先手动结束</div>
              <div v-else-if="status === 'ended'" class="dim">已结束的竞赛可重新配置起止时间，再次开赛</div>
            </UiFormItem>
            <UiFormItem label="结束时间">
              <UiDatePicker v-model="form.end" type="datetime" class="date-full" />
            </UiFormItem>
            <UiFormItem v-if="detail?.format === 'ctf'" label="计分模式">
              <UiRadioGroup v-model="form.scoringMode" :disabled="status !== 'scheduled'">
                <UiRadio value="dynamic">动态计分（按通过人数衰减）</UiRadio>
                <UiRadio value="fixed">固定计分（按题目分值）</UiRadio>
              </UiRadioGroup>
              <div class="dim">{{ status === 'scheduled' ? '开赛后不可切换' : '已开赛，计分模式已锁定' }}</div>
            </UiFormItem>
            <UiFormItem v-if="detail?.format === 'ctf'" label="附加分上限">
              <UiInputNumber v-model="form.bonusCap" :min="0" :max="1000000" :step="1" />
              <div class="dim">单人正向附加分合计上限（0 = 不限制），用于 AI 附加分等赛务规则。</div>
            </UiFormItem>
            <UiFormItem label="报名窗口">
              <UiCheckbox v-model="form.registrationEnabled">启用报名起止时间</UiCheckbox>
              <UiDatePicker
                v-if="form.registrationEnabled"
                v-model="form.registrationRange"
                type="datetimerange"
                start-placeholder="报名开始"
                end-placeholder="报名截止"
                class="reg-range date-full"
              />
              <div class="dim">公开赛生效；不启用则报名开放到竞赛结束，截止不得晚于竞赛结束。</div>
            </UiFormItem>
            <UiFormItem>
              <UiButton type="primary" :loading="saving" @click="saveConfig">保存配置</UiButton>
              <UiButton
                v-if="running"
                type="danger"
                plain
                :loading="ending"
                @click="endCompetition"
              >手动结束竞赛</UiButton>
            </UiFormItem>
          </UiForm>
        </FormSection>
      </UiTabPane>

      <!-- 题目 -->
      <UiTabPane label="题目" name="challenges">
        <DataTablePanel title="竞赛题目" :count="challenges.length" description="进行中不能解绑题目；结束后可移除。">
          <template #toolbar>
            <Button type="button" size="sm" @click="openBind">绑定 / 导入题目</Button>
          </template>
          <EmptyState
            v-if="challengesError"
            compact
            tone="error"
            title="竞赛题目加载失败"
            :description="challengesError"
            action-label="重试"
            @action="loadChallenges"
          />
          <UiTable v-else :data="challenges" :loading="challengesLoading" aria-label="竞赛题目">
            <UiTableColumn label="题目" min-width="220">
              <template #default="{ row }">
                <span class="cell-title">{{ row.title }}</span>
                <code class="cell-code">{{ row.slug }}</code>
              </template>
            </UiTableColumn>
            <UiTableColumn label="分类" width="120">
              <template #default="{ row }">{{ row.category }}</template>
            </UiTableColumn>
            <UiTableColumn label="难度" width="100">
              <template #default="{ row }">
                <StatusTag kind="difficulty" :value="row.difficulty" />
              </template>
            </UiTableColumn>
            <UiTableColumn label="分值" width="90">
              <template #default="{ row }">{{ row.score }} 分</template>
            </UiTableColumn>
            <UiTableColumn label="容器" width="90">
              <template #default="{ row }">
                <span :class="row.has_env ? 'cell-on' : 'cell-muted'">{{ row.has_env ? '支持' : '—' }}</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="操作" width="100">
              <template #default="{ row }">
                <Button
                  variant="link"
                  size="sm"
                  class="danger-action"
                  :disabled="running || unbinding === row.slug"
                  @click="unbind(row)"
                >
                  {{ unbinding === row.slug ? '解绑中…' : '解绑' }}
                </Button>
              </template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="尚未绑定题目" description="使用上方「绑定 / 导入题目」从题库勾选，或直接填写题目标识。" />
            </template>
          </UiTable>
        </DataTablePanel>
      </UiTabPane>

      <!-- 参赛人员 -->
      <UiTabPane :label="`参赛人员（${partTotal}）`" name="participants">
        <DataTablePanel title="参赛人员" :count="partTotal" description="邀请赛可导入名册、生成账号或按表格导入；公开赛由用户自助报名。">
          <template #toolbar>
            <template v-if="invitational">
              <Button type="button" variant="outline" size="sm" @click="openRoster">导入身份名册</Button>
              <Button type="button" variant="outline" size="sm" @click="openGenerate">一键生成账号</Button>
              <Button type="button" size="sm" @click="openImport">表格导入队伍 / 人员</Button>
            </template>
          </template>
          <div v-if="!invitational" class="notice">
            公开赛无需导入参赛者：竞赛展示在竞赛广场，用户自助报名；已报名者开赛后免邀请码直接入场。
          </div>
          <EmptyState
            v-if="partError"
            compact
            tone="error"
            title="参赛人员加载失败"
            :description="partError"
            action-label="重试"
            @action="loadParticipants"
          />
          <UiTable v-else :data="participants" :loading="partLoading" aria-label="参赛人员">
            <UiTableColumn label="账号" width="140">
              <template #default="{ row }"><span class="cell-title">{{ row.username }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="姓名" width="120">
              <template #default="{ row }">
                <span v-if="rosterByUser[row.user_id]?.has_profile">{{ rosterByUser[row.user_id]?.real_name }}</span>
                <span v-else class="cell-muted">未填写</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="学校 / 单位" min-width="160">
              <template #default="{ row }">{{ rosterByUser[row.user_id]?.school || '—' }}</template>
            </UiTableColumn>
            <UiTableColumn label="学号" width="120">
              <template #default="{ row }">{{ rosterByUser[row.user_id]?.student_no || '—' }}</template>
            </UiTableColumn>
            <UiTableColumn label="队伍" width="140">
              <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
            </UiTableColumn>
            <UiTableColumn label="邀请码" width="160">
              <template #default="{ row }"><code class="cell-code">{{ row.invite_code }}</code></template>
            </UiTableColumn>
            <UiTableColumn label="已入场" width="100">
              <template #default="{ row }">
                <StatusTag kind="competition" :value="row.entered ? 'running' : 'scheduled'" :label="row.entered ? '已入场' : '未入场'" />
              </template>
            </UiTableColumn>
            <UiTableColumn label="入场时间" width="170">
              <template #default="{ row }"><span class="cell-muted">{{ fmt(row.entered_at) }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="最近 IP" width="130">
              <template #default="{ row }"><span class="cell-muted">{{ row.last_ip || '—' }}</span></template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="尚无参赛人员" description="邀请赛可一键生成账号或导入表格名单。" />
            </template>
          </UiTable>
          <template v-if="partTotal > 20" #footer>
            <UiPagination
              v-model:current-page="partPage"
              background
              layout="total, prev, pager, next"
              :total="partTotal"
              :page-size="20"
              @current-change="loadParticipants"
            />
          </template>
        </DataTablePanel>
        <DataTablePanel v-if="!partError" class="detail-subpanel" title="队伍总览" :count="teams.length">
          <UiTable :data="teams" :loading="partLoading" aria-label="队伍总览">
            <UiTableColumn label="队伍" width="180">
              <template #default="{ row }"><span class="cell-title">{{ row.team_name }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="成员" min-width="220">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="暂无队伍" description="导入名单时填写队伍名称即可自动组队。" />
            </template>
          </UiTable>
        </DataTablePanel>
      </UiTabPane>

      <!-- 证书（赛后按排名/分数签发） -->
      <UiTabPane label="证书" name="certificates">
        <CompetitionCertificatePanel :competition-id="compId" :allowed="certificateAllowed" />
      </UiTabPane>

      <!-- 容器 -->
      <UiTabPane label="容器" name="instances">
        <DataTablePanel title="竞赛容器" :count="instTotal" description="参赛者打开的容器环境；超时会自动回收。">
          <template #toolbar>
            <Button type="button" variant="outline" size="sm" @click="loadInstances">刷新</Button>
          </template>
          <EmptyState
            v-if="instError"
            compact
            tone="error"
            title="竞赛容器加载失败"
            :description="instError"
            action-label="重试"
            @action="loadInstances"
          />
          <UiTable v-else :data="instances" :loading="instLoading" aria-label="竞赛容器">
            <UiTableColumn label="参赛者" width="140">
              <template #default="{ row }"><span class="cell-title">{{ row.username }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="题目" min-width="200">
              <template #default="{ row }">{{ row.challenge_title }}</template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="110">
              <template #default="{ row }"><StatusTag kind="container" :value="row.status" /></template>
            </UiTableColumn>
            <UiTableColumn label="访问地址" min-width="180">
              <template #default="{ row }">
                <code v-if="row.endpoint" class="cell-code">{{ row.endpoint }}</code>
                <span v-else class="cell-muted">尚未分配</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="创建时间" width="170">
              <template #default="{ row }"><span class="cell-muted">{{ fmt(row.created_at) }}</span></template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="暂无竞赛容器" description="参赛者打开题目环境后会出现在这里。" />
            </template>
          </UiTable>
          <template v-if="instTotal > 20" #footer>
            <UiPagination
              v-model:current-page="instPage"
              background
              layout="total, prev, pager, next"
              :total="instTotal"
              :page-size="20"
              @current-change="loadInstances"
            />
          </template>
        </DataTablePanel>
      </UiTabPane>

      <!-- 反作弊 -->
      <UiTabPane :label="`反作弊${detail?.stats.warnings ? `（${detail.stats.warnings}）` : ''}`" name="warnings">
        <DataTablePanel title="反作弊警告" :count="warnings.length" description="提交跨队 flag、来源 IP 异常等赛务风险记录。">
          <template #toolbar>
            <Button type="button" variant="outline" size="sm" @click="loadWarnings">刷新</Button>
          </template>
          <EmptyState
            v-if="warningsError"
            compact
            tone="error"
            title="警告记录加载失败"
            :description="warningsError"
            action-label="重试"
            @action="loadWarnings"
          />
          <UiTable v-else :data="warnings" :loading="warningsLoading" aria-label="反作弊警告">
            <UiTableColumn label="时间" width="170">
              <template #default="{ row }"><span class="cell-muted">{{ fmt(row.created_at) }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="类型" width="130">
              <template #default="{ row }">
                <span class="warning-chip" :data-tone="warningType(row.kind) === 'danger' ? 'danger' : 'warning'">
                  {{ warningLabel(row.kind) }}
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="参赛者" width="130">
              <template #default="{ row }"><span class="cell-title">{{ row.username }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="队伍" width="150">
              <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
            </UiTableColumn>
            <UiTableColumn label="原因" min-width="260">
              <template #default="{ row }"><span class="cell-muted">{{ row.reason }}</span></template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="暂无警告" description="出现跨队 flag 或异常来源时，记录会出现在这里。" />
            </template>
          </UiTable>
        </DataTablePanel>
      </UiTabPane>

      <!-- 解题报告（按题目分组） -->
      <UiTabPane label="解题报告" name="writeups">
        <DataTablePanel
          title="解题报告提交情况"
          :count="writeups.length"
          description="比赛结束后按题目提交，一份报告对应一支队伍或个人；可下载附带压缩包。"
        >
          <template #toolbar>
            <Button type="button" variant="outline" size="sm" @click="loadWriteups">刷新</Button>
          </template>
          <EmptyState
            v-if="writeupsError"
            compact
            tone="error"
            title="解题报告加载失败"
            :description="writeupsError"
            action-label="重试"
            @action="loadWriteups"
          />
          <div v-else-if="writeupsLoading" class="wp-skeleton" role="status" aria-label="正在加载解题报告">
            <i v-for="n in 4" :key="n" class="sk-line" />
          </div>
          <div v-else class="wp-body">
            <template v-if="writeupsByChallenge.length">
              <div v-for="group in writeupsByChallenge" :key="group.title" class="wp-group">
                <h3 class="wp-group__title">{{ group.title }}（{{ group.items.length }} 份）</h3>
                <UiTable :data="group.items" aria-label="解题报告提交">
                  <UiTableColumn label="队伍 / 选手" width="170">
                    <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
                  </UiTableColumn>
                  <UiTableColumn label="最近修改人" width="130">
                    <template #default="{ row }"><span class="cell-title">{{ row.username }}</span></template>
                  </UiTableColumn>
                  <UiTableColumn label="提交时间" width="170">
                    <template #default="{ row }"><span class="cell-muted">{{ fmt(row.submitted_at) }}</span></template>
                  </UiTableColumn>
                  <UiTableColumn label="压缩包" width="140">
                    <template #default="{ row }">
                      <UiLink
                        v-if="row.zip_name"
                        type="primary"
                        :underline="false"
                        @click="downloadWriteupZip(row)"
                      >
                        {{ row.zip_name }}
                      </UiLink>
                      <span v-else class="cell-muted">—</span>
                    </template>
                  </UiTableColumn>
                  <UiTableColumn label="操作" width="90">
                    <template #default="{ row }">
                      <Button variant="link" size="sm" @click="viewing = row">查看</Button>
                    </template>
                  </UiTableColumn>
                </UiTable>
                <p v-if="wpMissing(group.items[0].challenge_id).length" class="wp-missing">
                  未提交：{{ wpMissing(group.items[0].challenge_id).join('、') }}
                </p>
              </div>
            </template>
            <EmptyState
              v-else
              compact
              title="暂无解题报告"
              description="比赛结束后，参赛者可在竞赛页按题目提交解题报告。"
            />
          </div>
        </DataTablePanel>
      </UiTabPane>

      <!-- 排行榜 -->
      <UiTabPane label="排行榜" name="leaderboard">
        <DataTablePanel title="竞赛排行榜" :count="board.length" description="按得分与附加分排序，可切换战队榜或个人榜并导出。">
          <template #toolbar>
            <UiRadioGroup v-model="boardMode" size="small" @change="onBoardModeChange">
              <UiRadioButton value="team">战队榜</UiRadioButton>
              <UiRadioButton value="individual">个人榜</UiRadioButton>
            </UiRadioGroup>
            <Button type="button" variant="outline" size="sm" :disabled="boardExporting" @click="exportBoard">
              {{ boardExporting ? '导出中…' : '导出 CSV' }}
            </Button>
          </template>
          <EmptyState
            v-if="boardError"
            compact
            tone="error"
            title="排行榜加载失败"
            :description="boardError"
            action-label="重试"
            @action="loadBoard"
          />
          <UiTable v-else :data="board" :loading="boardLoading" aria-label="竞赛排行榜">
            <UiTableColumn label="名次" width="80">
              <template #default="{ row }"><span class="board-rank">{{ row.rank }}</span></template>
            </UiTableColumn>
            <UiTableColumn :label="boardMode === 'team' ? '队伍' : '选手'" min-width="150">
              <template #default="{ row }"><span class="cell-title">{{ row.team_name ?? '—' }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="成员" min-width="200">
              <template #default="{ row }">{{ row.members.join('、') }}</template>
            </UiTableColumn>
            <UiTableColumn label="得分" width="100">
              <template #default="{ row }"><span class="board-score">{{ row.score }}</span></template>
            </UiTableColumn>
            <UiTableColumn label="附加分" width="100">
              <template #default="{ row }">{{ row.bonus }}</template>
            </UiTableColumn>
            <UiTableColumn label="解题数" width="100">
              <template #default="{ row }">{{ row.solved_count }}</template>
            </UiTableColumn>
            <UiTableColumn label="最近通关" width="170">
              <template #default="{ row }"><span class="cell-muted">{{ fmt(row.last_solve_at) }}</span></template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="暂无成绩" description="竞赛开始且有人通关后，排行榜会自动生成。" />
            </template>
          </UiTable>
        </DataTablePanel>
      </UiTabPane>

      <!-- 附加分（CTF） -->
      <UiTabPane v-if="detail?.format === 'ctf'" label="附加分" name="adjustments">
        <DataTablePanel
          title="附加分明细"
          :count="adjustments.length"
          description="附加分只追加、不修改，直接计入个人或队伍总分；正向合计受附加分上限约束。"
        >
          <template #toolbar>
            <Button type="button" size="sm" @click="openAdjustment">追加附加分</Button>
          </template>
          <EmptyState
            v-if="adjustmentsError"
            compact
            tone="error"
            title="附加分明细加载失败"
            :description="adjustmentsError"
            action-label="重试"
            @action="loadAdjustments"
          />
          <UiTable v-else :data="adjustments" :loading="adjustmentsLoading" aria-label="附加分明细">
            <UiTableColumn label="选手" min-width="130">
              <template #default="{ row }">
                <span class="cell-title">{{ row.username ?? `#${row.user_id}` }}</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="队伍" min-width="130">
              <template #default="{ row }">{{ row.team_name ?? '—' }}</template>
            </UiTableColumn>
            <UiTableColumn label="分值" width="100">
              <template #default="{ row }">
                <span class="points-chip" :data-positive="row.points > 0 ? 'true' : undefined">
                  {{ row.points > 0 ? '+' : '' }}{{ row.points }}
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="原因" min-width="240">
              <template #default="{ row }">{{ row.reason }}</template>
            </UiTableColumn>
            <UiTableColumn label="时间" width="170">
              <template #default="{ row }"><span class="cell-muted">{{ fmt(row.created_at) }}</span></template>
            </UiTableColumn>
            <template #empty>
              <EmptyState compact title="暂无附加分记录" description="AI 附加分或其他赛务调整会记录在这里。" />
            </template>
          </UiTable>
        </DataTablePanel>
      </UiTabPane>

      <!-- 晋级（仅邀请赛；来源 CTF/AWD/考试排名 → 本场参赛名单） -->
      <UiTabPane v-if="detail?.kind === 'invitational'" label="晋级" name="qualifications">
        <CompetitionQualificationPanel
          :competition-id="compId"
          :disabled="qualificationDisabled"
          @applied="loadDetail"
        />
      </UiTabPane>
    </UiTabs>
  </div>

  <!-- 追加附加分 -->
  <UiDialog v-model="showAdjustment" title="追加附加分" width="520px">
    <UiForm label-width="90px">
      <UiFormItem label="参赛选手">
        <UiSelect v-model="adjustmentForm.userId" filterable placeholder="选择选手" class="field-full">
          <UiOption
            v-for="p in adjustmentCandidates"
            :key="p.user_id"
            :label="`${p.username}${p.team_name ? `（${p.team_name}）` : ''}`"
            :value="p.user_id"
          />
        </UiSelect>
      </UiFormItem>
      <UiFormItem label="附加分">
        <UiInputNumber v-model="adjustmentForm.points" :min="-1000000" :max="1000000" :step="1" />
        <div class="dim">正数加分、负数纠正；0 不允许。</div>
      </UiFormItem>
      <UiFormItem label="原因">
        <UiInput v-model="adjustmentForm.reason" type="textarea" :rows="3" maxlength="512" placeholder="如：AI 辅助附加分 / 申诉纠正" />
      </UiFormItem>
    </UiForm>
    <template #footer>
      <UiButton @click="showAdjustment = false">取消</UiButton>
      <UiButton type="primary" :loading="adjustmentSaving" @click="submitAdjustment">追加</UiButton>
    </template>
  </UiDialog>

  <!-- 绑定/导入题目（从题库勾选 / slug 直填 / 文件粘贴导入 challenge.json） -->
  <UiDialog v-model="showBind" title="绑定 / 导入题目" width="860px">
    <UiRadioGroup v-model="bindMode" class="bind-mode">
      <UiRadioButton value="pick">从题库勾选</UiRadioButton>
      <UiRadioButton value="slugs">填写 slug</UiRadioButton>
      <UiRadioButton value="json">导入题目 JSON</UiRadioButton>
    </UiRadioGroup>
    <template v-if="bindMode === 'pick'">
      <p class="dim">
        从题库勾选已有题目绑定进本场（跨页可累计勾选；已绑定的题不再列出可选）。
      </p>
      <div class="pick-filters">
        <UiInput
          v-model="pickQuery"
          placeholder="搜索：标题 / slug / 分类"
          clearable
          class="pick-q"
          @keyup.enter="searchPick"
          @clear="searchPick"
        />
        <UiSelect v-model="pickBank" placeholder="题库（全部）" clearable class="pick-bank" @change="searchPick">
          <UiOption label="CTF 题库" value="ctf" />
          <UiOption label="ACM 题库" value="acm" />
          <UiOption label="教程题目" value="tutorial" />
        </UiSelect>
        <UiButton @click="searchPick">搜索</UiButton>
        <span class="dim picked-sum">已勾选 {{ pickedCount }} 题</span>
      </div>
      <UiLoadError v-if="pickError" compact :message="pickError" @retry="loadPickList" class="block" />
      <UiTable
        v-else
        ref="pickTable"
        :data="pickItems"
        :loading="pickLoading"
        size="small"
        max-height="340"
        row-key="slug"
        empty-text="题库为空或无匹配题目"
        @selection-change="onPickSelectionChange"
      >
        <UiTableColumn type="selection" width="42" :selectable="pickSelectable" />
        <UiTableColumn prop="title" label="题目" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.title }}
            <span class="dim">（{{ row.slug }}）</span>
          </template>
        </UiTableColumn>
        <UiTableColumn prop="category" label="分类" width="110" show-overflow-tooltip />
        <UiTableColumn label="题库" width="90">
          <template #default="{ row }">
            <UiTag size="small" :type="row.bank === 'ctf' ? 'danger' : row.bank === 'acm' ? 'warning' : 'info'">
              {{ row.bank === 'ctf' ? 'CTF' : row.bank === 'acm' ? 'ACM' : '教程' }}
            </UiTag>
          </template>
        </UiTableColumn>
        <UiTableColumn label="难度" width="80">
          <template #default="{ row }">{{ row.difficulty }}</template>
        </UiTableColumn>
        <UiTableColumn label="状态" width="90">
          <template #default="{ row }">
            <UiTag v-if="!pickSelectable(row)" type="success" size="small">已绑定</UiTag>
          </template>
        </UiTableColumn>
      </UiTable>
      <UiPagination
        background
        layout="total, prev, pager, next"
        :total="pickTotal"
        :page-size="20"
        :current-page="pickPage"
        @current-change="loadPickList"
        class="pick-pager"
      />
    </template>
    <template v-else-if="bindMode === 'slugs'">
      <p class="dim">填写题库已有题目的 slug（逗号或换行分隔）。绑定后题目进入本场竞赛，题库侧不受影响。</p>
      <UiInput v-model="bindSlugs" type="textarea" :rows="5" placeholder="web-lfi&#10;crypto-caesar" />
    </template>
    <template v-else>
      <p class="dim">
        与题目管理上传同一格式：可一次选择多个 .json 文件批量导入，也可选单个逐题导入，或直接粘贴 JSON（单对象或数组）。
        所有题目先经本地校验进入下方暂存列表，全部通过后一次性导入并绑定进本场（导入的题目同时进入题库（教程题目 tab），如需修改请到题目管理编辑）。
      </p>
      <div class="stage-actions">
        <UiUpload multiple accept=".json,application/json" :show-file-list="false" :before-upload="onStageFile" :disabled="binding">
          <UiButton size="small">选择 JSON 文件（可多选）</UiButton>
        </UiUpload>
        <UiInput v-model="bindPaste" type="textarea" :rows="3" placeholder='或粘贴 challenge.json（单对象或数组）' />
        <UiButton size="small" @click="stagePaste">解析并加入</UiButton>
      </div>
      <UiTable v-if="staged.length" :data="staged" size="small" class="stage-table" empty-text="暂存列表为空">
        <UiTableColumn label="来源" width="140">
          <template #default="{ row }">{{ row.source }}</template>
        </UiTableColumn>
        <UiTableColumn label="题目" min-width="160">
          <template #default="{ row }">
            {{ row.chal.title || '（未命名）' }}
            <span class="dim">（{{ row.chal.slug || '无 slug' }}）</span>
          </template>
        </UiTableColumn>
        <UiTableColumn label="校验" min-width="220">
          <template #default="{ row }">
            <UiTag v-if="!row.errors.length" type="success" size="small">通过</UiTag>
            <div v-else class="stage-errs">
              <UiTag type="danger" size="small">未通过</UiTag>
              <span>{{ row.errors.join('；') }}</span>
            </div>
          </template>
        </UiTableColumn>
        <UiTableColumn label="操作" width="80">
          <template #default="{ row }">
            <UiButton size="small" type="danger" link @click="removeStaged(row)">移除</UiButton>
          </template>
        </UiTableColumn>
      </UiTable>
      <p v-else class="dim">尚未暂存任何题目：选择文件或粘贴 JSON 后将在此逐题列出。</p>
    </template>
    <template #footer>
      <UiButton @click="showBind = false">取消</UiButton>
      <UiButton type="primary" :loading="binding" @click="submitBind">
        {{ bindMode === 'pick' ? `绑定所选（${pickedCount} 题）` : bindMode === 'slugs' ? '绑定' : `导入并绑定（${staged.length} 题）` }}
      </UiButton>
    </template>
  </UiDialog>

  <!-- 参赛人员表格导入（固定格式 CSV；仅邀请赛） -->
  <UiDialog v-model="showImport" title="表格导入队伍 / 参赛人员" width="720px">
    <p class="dim">
      条目 = 账号编码（YA26-00001）或用户名，引用既有账号（不自动建号，缺失条目在导入结果中列出——
      可用「一键生成账号」补号后重导）。每人生成唯一邀请码并投递站内邀请邮件。
    </p>
    <UiRadioGroup v-model="importMode" class="bind-mode">
      <UiRadioButton value="individuals">个人名单</UiRadioButton>
      <UiRadioButton value="teams">队伍名单</UiRadioButton>
    </UiRadioGroup>
    <div class="stage-actions">
      <div class="tpl-row">
        <UiUpload accept=".csv,text/csv" :show-file-list="false" :before-upload="onImportFile" :disabled="importing">
          <UiButton size="small">选择 CSV 文件</UiButton>
        </UiUpload>
        <UiButton size="small" link type="primary" @click="downloadTemplate(importMode)">下载模板</UiButton>
        <span class="dim tpl-hint">
          {{ importMode === 'individuals' ? '每行一条：账号编码或用户名' : '每行：队伍名,成员1,成员2,…（成员 = 账号编码或用户名）' }}
        </span>
      </div>
    </div>
    <template v-if="importMode === 'individuals'">
      <UiTable v-if="stagedIndividuals.length" :data="stagedIndividuals.map((v, i) => ({ i, v }))" size="small" max-height="240">
        <UiTableColumn label="序号" width="70">
          <template #default="{ row }">{{ row.i + 1 }}</template>
        </UiTableColumn>
        <UiTableColumn label="条目（账号编码或用户名）">
          <template #default="{ row }">
            <code>{{ row.v }}</code>
            <UiButton size="small" type="danger" link class="stage-del" @click="stagedIndividuals.splice(row.i, 1)">移除</UiButton>
          </template>
        </UiTableColumn>
      </UiTable>
      <p v-else class="dim">尚未解析到条目：选择 CSV 文件后在此列出，可反复上传合并。</p>
    </template>
    <template v-else>
      <UiTable v-if="stagedTeams.length" :data="stagedTeams" size="small" max-height="240">
        <UiTableColumn prop="name" label="队伍" width="140" />
        <UiTableColumn label="成员">
          <template #default="{ row }">
            <code v-for="m in row.members" :key="m" class="member-chip">{{ m }}</code>
          </template>
        </UiTableColumn>
        <UiTableColumn label="操作" width="80">
          <template #default="{ row }">
            <UiButton size="small" type="danger" link @click="removeStagedTeam(row)">移除</UiButton>
          </template>
        </UiTableColumn>
      </UiTable>
      <p v-else class="dim">尚未解析到队伍：选择 CSV 文件后在此列出，可反复上传合并。</p>
    </template>
    <template #footer>
      <UiButton @click="showImport = false">取消</UiButton>
      <UiButton
        type="primary"
        :loading="importing"
        :disabled="!stagedIndividuals.length && !stagedTeams.length"
        @click="submitImport"
      >
        导入{{ stagedTeams.length ? `（${stagedTeams.length} 队` : '' }}{{ stagedTeams.length && stagedIndividuals.length ? ' + ' : '' }}{{ stagedIndividuals.length ? `${stagedIndividuals.length} 人` : '' }}）
      </UiButton>
    </template>
  </UiDialog>

  <!-- 导入结果 -->
  <UiDialog v-model="showResult" title="导入结果" width="760px">
    <UiAlert
      v-if="importResult && importResult.missing.length"
      type="warning"
      :closable="false"
      class="block"
      :title="`以下 ${importResult.missing.length} 个条目未匹配到账号（账号编码与用户名均未命中），未导入：`"
      :description="importResult.missing.join('、') + '。可修正条目重导，或先用「一键生成账号」补号。'"
    />
    <UiTable v-if="importResult" :data="importResult.participants" empty-text="无参赛者">
      <UiTableColumn prop="username" label="参赛者" width="130" />
      <UiTableColumn prop="account_code" label="账号编码" width="130">
        <template #default="{ row }"><code>{{ row.account_code || '-' }}</code></template>
      </UiTableColumn>
      <UiTableColumn label="队伍" width="140">
        <template #default="{ row }">{{ row.team_name ?? '（个人）' }}</template>
      </UiTableColumn>
      <UiTableColumn prop="invite_code" label="邀请码" width="160">
        <template #default="{ row }"><code>{{ row.invite_code }}</code></template>
      </UiTableColumn>
      <UiTableColumn label="邮件" width="90">
        <template #default="{ row }">
          <UiTag :type="row.mail_delivered ? 'success' : 'danger'" size="small">{{ row.mail_delivered ? '已投递' : '失败' }}</UiTag>
        </template>
      </UiTableColumn>
    </UiTable>
    <template #footer>
      <UiButton @click="showResult = false">关闭</UiButton>
    </template>
  </UiDialog>

  <!-- 身份名册 CSV 导入（仅邀请赛） -->
  <UiDialog v-model="showRoster" title="导入身份名册" width="820px">
    <p class="dim">
      列：账号编码/用户名、姓名、学校、学号、邮箱、手机（中英文表头均可；姓名必填）。
      条目引用既有账号，未命中条目在结果中列出；命中者写入实名信息并导入本场参赛者。
    </p>
    <div class="stage-actions">
      <div class="tpl-row">
        <UiUpload accept=".csv,text/csv" :show-file-list="false" :before-upload="onRosterFile" :disabled="importingRoster">
          <UiButton size="small">选择 CSV 文件</UiButton>
        </UiUpload>
        <UiButton size="small" link type="primary" @click="downloadRosterTemplate">下载模板</UiButton>
        <span class="dim tpl-hint">支持双引号包裹含逗号的字段；可多次上传合并。</span>
      </div>
    </div>
    <UiTable v-if="stagedRoster.length" :data="stagedRoster" size="small" max-height="320" row-key="entry">
      <UiTableColumn prop="entry" label="账号" width="120" />
      <UiTableColumn prop="real_name" label="姓名" width="90" />
      <UiTableColumn prop="school" label="学校" min-width="140" />
      <UiTableColumn prop="student_no" label="学号" width="110" />
      <UiTableColumn prop="email" label="邮箱" min-width="170" />
      <UiTableColumn prop="phone" label="手机" width="120" />
      <UiTableColumn label="操作" width="70">
        <template #default="{ row }">
          <UiButton size="small" type="danger" link @click="stagedRoster = stagedRoster.filter((item) => item.entry !== row.entry)">
            移除
          </UiButton>
        </template>
      </UiTableColumn>
    </UiTable>
    <p v-else class="dim">尚未解析到名册：选择 CSV 文件后在此预览。</p>
    <template #footer>
      <UiButton @click="showRoster = false">取消</UiButton>
      <UiButton
        type="primary"
        :loading="importingRoster"
        :disabled="importingRoster || !stagedRoster.length"
        @click="submitRoster"
      >
        导入名册（{{ stagedRoster.length }} 人）
      </UiButton>
    </template>
  </UiDialog>

  <!-- 身份名册导入结果 -->
  <UiDialog v-model="showRosterResult" title="名册导入结果" width="820px">
    <template v-if="rosterResult">
      <UiAlert
        v-if="rosterResult.missing.length"
        type="warning"
        :closable="false"
        class="block"
        :title="`以下 ${rosterResult.missing.length} 个条目未匹配到账号，未导入：`"
        :description="rosterResult.missing.join('、') + '。可修正后重导，或先用「一键生成账号」补号。'"
      />
      <UiAlert
        type="success"
        :closable="false"
        class="block"
        :title="`已导入 ${rosterResult.imported} 人名册，并确保其为参赛者。`"
      />
      <UiTable :data="rosterResult.profiles" size="small" max-height="320" empty-text="无名册记录">
        <UiTableColumn prop="username" label="用户名" width="120" />
        <UiTableColumn prop="real_name" label="姓名" width="90" />
        <UiTableColumn prop="school" label="学校" min-width="140" />
        <UiTableColumn prop="student_no" label="学号" width="110" />
        <UiTableColumn prop="email" label="邮箱" min-width="170" />
        <UiTableColumn prop="phone" label="手机" width="120" />
      </UiTable>
    </template>
    <template #footer>
      <UiButton @click="showRosterResult = false">关闭</UiButton>
    </template>
  </UiDialog>

  <!-- 一键生成账号（仅邀请赛） -->
  <UiDialog v-model="showGenerate" title="一键生成参赛账号" width="680px">
    <template v-if="!genResult">
      <p class="dim">
        按前缀批量生成账号（用户名 = 前缀-零填充序号，如 class26-01…class26-10）并全部导入为本场参赛者；
        可配置自动组队（按生成顺序每 N 人一队，余数进最后一队）。初始密码默认 123456。
      </p>
      <UiForm label-width="90px">
        <UiFormItem label="用户名前缀" required>
          <UiInput v-model="genForm.prefix" placeholder="如 class26（1-16 位小写字母/数字/连字符）" />
        </UiFormItem>
        <UiFormItem label="生成数量" required>
          <UiInputNumber v-model="genForm.count" :min="1" :max="500" />
        </UiFormItem>
        <UiFormItem label="每队人数">
          <UiInputNumber v-model="genForm.teamSize" :min="0" :max="10" />
          <div class="dim">0 = 不组队（个人参赛）；&gt;0 = 自动编入「第 N 队」</div>
        </UiFormItem>
        <UiFormItem label="初始密码">
          <UiInput v-model="genForm.password" placeholder="留空 = 默认 123456" clearable />
        </UiFormItem>
      </UiForm>
      <p class="dim">同名既有账号复用（沿用原密码），新建账号的初始密码仅在生成结果中显示，请导出后立即分发。</p>
    </template>
    <template v-else>
      <UiAlert type="success" :closable="false" class="block"
        :title="`生成完成：新建 ${genResult.accounts.filter((a) => a.created).length} 个账号、复用 ${genResult.accounts.filter((a) => !a.created).length} 个既有账号${genResult.teams.length ? `，编入 ${genResult.teams.length} 支队伍` : ''}。`" />
      <UiTable :data="genResult.accounts" size="small" max-height="320">
        <UiTableColumn prop="username" label="用户名" width="140" />
        <UiTableColumn prop="account_code" label="账号编码" width="130">
          <template #default="{ row }"><code>{{ row.account_code }}</code></template>
        </UiTableColumn>
        <UiTableColumn label="初始密码" width="140">
          <template #default="{ row }"><code>{{ row.initial_password ?? '（复用既有账号，沿用原密码）' }}</code></template>
        </UiTableColumn>
        <UiTableColumn label="队伍" min-width="120">
          <template #default="{ row }">
            {{ genResult.participants.find((p) => p.username === row.username)?.team_name ?? '（个人）' }}
          </template>
        </UiTableColumn>
      </UiTable>
    </template>
    <template #footer>
      <template v-if="!genResult">
        <UiButton @click="showGenerate = false">取消</UiButton>
        <UiButton type="primary" :loading="generating" @click="submitGenerate">生成并导入</UiButton>
      </template>
      <template v-else>
        <UiButton @click="showGenerate = false">关闭</UiButton>
        <UiButton type="primary" @click="exportGeneratedCsv">导出账号 CSV</UiButton>
        <UiButton @click="genResult = null">再生成一批</UiButton>
      </template>
    </template>
  </UiDialog>

  <!-- 解题报告查看 -->
  <UiDialog
    :model-value="viewing !== null"
    :title="`解题报告：${viewing?.challenge_title ?? ''} · ${viewing?.team_name ?? viewing?.username ?? ''}`"
    width="680px"
    @close="viewing = null"
  >
    <p v-if="viewing" class="dim wp-dialog-meta">
      最近修改：{{ viewing.username }} · {{ fmt(viewing.submitted_at) }}
      <UiLink
        v-if="viewing.zip_name"
        type="primary"
        :underline="false"
        class="wp-dialog-zip"
        @click="downloadWriteupZip(viewing)"
      >
        下载 zip：{{ viewing.zip_name }}
      </UiLink>
    </p>
    <pre class="wp">{{ viewing?.content || '（仅上传了 zip，未填写文字）' }}</pre>
  </UiDialog>
</template>

<style scoped>
.reg-range { margin-top: var(--space-2); }
.date-full,
.field-full { width: 100%; }
.config-form { max-width: 560px; }
.hint {
  margin-top: 0;
  margin-bottom: var(--space-4);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-normal);
}
.meta-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  white-space: nowrap;
}
.detail-tabs :deep([data-slot='tabs-list']) {
  min-height: 0;
  height: auto;
  padding: 0;
  border-bottom: 1px solid var(--border);
  border-radius: 0;
  background: transparent;
  gap: var(--space-6);
}
.detail-tabs :deep([data-slot='tabs-trigger']) {
  min-height: 40px;
  padding: var(--space-2) 0;
  border-radius: 0;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  font-weight: 500;
  box-shadow: none;
}
.detail-tabs :deep([data-slot='tabs-trigger'][data-state='active']) {
  background: transparent;
  color: var(--action-primary);
  box-shadow: inset 0 -2px 0 var(--action-primary);
}
.detail-subpanel { margin-top: var(--space-4); }
.notice {
  margin: var(--space-3) var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}
.cell-title {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  overflow-wrap: anywhere;
}
.cell-code {
  display: block;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  overflow-wrap: anywhere;
}
.cell-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}
.cell-on {
  color: var(--status-success);
  font-size: var(--text-meta);
}
.warning-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid color-mix(in srgb, var(--status-warning) 28%, transparent);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-warning) 10%, var(--bg-surface));
  color: var(--status-warning);
  font-size: var(--text-meta);
  font-weight: 500;
  white-space: nowrap;
}
.warning-chip[data-tone='danger'] {
  border-color: color-mix(in srgb, var(--status-danger) 26%, transparent);
  background: color-mix(in srgb, var(--status-danger) 8%, var(--bg-surface));
  color: var(--status-danger);
}
.points-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 40px;
  height: 24px;
  padding-inline: var(--space-2);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 8%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-meta);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.points-chip[data-positive='true'] {
  background: color-mix(in srgb, var(--status-success) 10%, var(--bg-surface));
  color: var(--status-success);
}
.board-rank {
  color: var(--text-tertiary);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.board-score {
  color: var(--text-primary);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.qualification-actions { margin-top: var(--space-3); }
.cert-subtitle { margin: var(--space-4) 0 var(--space-2); font-size: var(--text-caption); font-weight: 600; }
.cert-code-link { color: var(--action-subtle-fg); text-underline-offset: 3px; text-decoration: underline; }
.cert-subtitle:first-child { margin-top: 0; }
.qualification-history { margin-top: var(--space-5); }
.qualification-history h4 { margin: 0 0 var(--space-2); font-size: var(--text-caption); }
.block { margin-bottom: var(--space-5); }
.dim { color: var(--text-tertiary); font-size: var(--text-meta); }
.bind-mode { margin-bottom: var(--space-3); }
.pick-filters { display: flex; gap: var(--space-2); align-items: center; margin-bottom: var(--space-2); flex-wrap: wrap; }
.pick-q { width: 240px; max-width: 100%; }
.pick-bank { width: 150px; max-width: 100%; }
.picked-sum { margin-left: auto; }
.pick-pager { margin-top: var(--space-2); display: flex; justify-content: flex-end; }
.tpl-row { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; }
.tpl-hint { margin: 0; }
.stage-del { margin-left: var(--space-3); }
.member-chip {
  display: inline-block; margin: 0 var(--space-1) var(--space-1) 0; padding: 0 var(--space-1);
  background: var(--bg-subtle); border-radius: var(--radius-control);
}
.stage-actions { display: flex; flex-direction: column; gap: var(--space-2); align-items: flex-start; margin-bottom: var(--space-3); }
.stage-actions .ui-button { margin-left: 0; }
.stage-table { margin-bottom: var(--space-1); }
.stage-errs { display: flex; align-items: flex-start; gap: var(--space-1); }
.stage-errs span { color: var(--status-danger); font-size: var(--text-meta); line-height: var(--leading-normal); }
.wp { white-space: pre-wrap; word-break: break-word; max-height: 60vh; overflow: auto; }
.wp-body { display: grid; gap: var(--space-4); padding: var(--space-4); }
.wp-group { display: grid; gap: var(--space-2); }
.wp-group__title { color: var(--text-primary); font-size: var(--text-caption); font-weight: 600; }
.wp-missing { margin: var(--space-1) 0 0; color: var(--text-tertiary); font-size: var(--text-meta); }
.wp-dialog-meta { margin: 0 0 var(--space-2); }
.wp-dialog-zip { margin-left: var(--space-4); }

.detail-skeleton { display: grid; gap: var(--space-5); }
.sk-head { display: grid; gap: var(--space-3); }
.sk-line { display: block; width: 100%; height: 12px; border-radius: var(--radius-control); background: var(--muted); }
.sk-line.short { width: 32%; }
.sk-line.medium { width: 58%; }
.sk-line.title { width: 36%; height: 28px; }
.sk-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-3); }
.sk-stat { display: grid; gap: var(--space-2); padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--panel); }
.sk-tabs { display: flex; gap: var(--space-2); border-bottom: 1px solid var(--border); padding-bottom: var(--space-2); }
.sk-tabs i { width: 84px; height: 28px; border-radius: var(--radius-control); background: var(--muted); }
.sk-panel { display: grid; gap: var(--space-3); padding: var(--space-5); border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--panel); }
.wp-skeleton { display: grid; gap: var(--space-3); padding: var(--space-4) 0; }
.detail-skeleton .sk-line,
.detail-skeleton .sk-tabs i,
.wp-skeleton .sk-line { animation: detail-skeleton-pulse 1.4s ease-in-out infinite; }
@keyframes detail-skeleton-pulse { 50% { opacity: .45; } }
@media (max-width: 720px) {
  .sk-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
