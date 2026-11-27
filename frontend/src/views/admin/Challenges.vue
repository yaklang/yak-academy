<script setup lang="ts">
// 题目管理：三个题库 tab（内置训练 / CTF / ACM）分开管理。
// 内置 tab 只读——目录是内置内容唯一事实源，更新只经「一键导入」整体进行
// （写接口对内置题一律拒绝，前端同步禁用编辑/删除/可见性/附件操作）。
// CTF / ACM tab 支持关键字搜索（标识/标题/分类前缀）与筛选（分类/难度/来源）。
// 上传流程：选择 challenge.json → 前端校验 → 编辑弹窗确认 → 一键上传；
// 行内「编辑」拉取库内完整定义回填同一弹窗，附件管理也在该弹窗内。
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { notify } from '@/lib/notifications'
import { formatBytes, formatDateTime } from '@/lib/format'
import { Search } from '@lucide/vue'
import PageHeader from '@/components/layout/PageHeader.vue'
import PageTabs from '@/components/layout/PageTabs.vue'
import FilterBar from '@/components/layout/FilterBar.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  adminChallengeApi,
  CATEGORY_LABELS,
  DIFFICULTY_LABELS,
  QUESTION_TYPE_LABELS,
  type AdminChallengeQuery,
  type BuiltinImageStatus,
  type ChallengeAttachment,
  type ChallengeBank,
  type ChallengeDifficulty,
  type ChallengeFullJSON,
  type ChallengeHintInput,
  type ChallengeJSON,
  type ChallengeLink,
  type JudgeJSON,
  type QuestionJSON,
  type QuestionOption,
  type QuestionType,
  type TrainingItem,
  type YakEngineStatus,
} from '../../api'
import {
  downloadSampleBasicQuestions,
  downloadSampleChallenge,
  parseChallengeJSON,
  validateChallenge,
} from '../../admin/challengeFormat'
import { LIB_DESC } from '../../components/yakVocab'

// 库白名单候选（教学常用库 + 中文说明；可手输其他 yaklang 库名，
// 后端按完整库表校验——命令执行与文件访问类库配置即被拒绝）。
const libOptions = Object.keys(LIB_DESC).map((lib) => ({ value: lib, label: `${lib}（${LIB_DESC[lib]}）` }))

const items = ref<TrainingItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const loadError = ref('')
const uploading = ref(false)
const specVisible = ref(false)

// ===== 题库 tab（内置只读；CTF/ACM/基础题 可管理）=====
type AdminTab = 'builtin' | 'ctf' | 'acm' | 'basic'
const activeTab = ref<AdminTab>('builtin')
// 内置 tab 按来源聚合（is_builtin=true，不限 bank——内置集语义上是随平台
// 分发的一套，不该被 bank 二次切分）。
const TAB_META: Record<AdminTab, { bank?: ChallengeBank; builtin: boolean; hint: string }> = {
  builtin: { builtin: true, hint: '内置训练集随平台分发，只读展示；更新与清理经「导入内置训练」按目录整体进行。' },
  ctf: { bank: 'ctf', builtin: false, hint: 'CTF 题目：flag 提交判定（明文或正则），可选容器环境。' },
  acm: { bank: 'acm', builtin: false, hint: 'ACM 题目：算法编程题，上传时题库选择 ACM。' },
  basic: { bank: 'basic', builtin: false, hint: '基础题：单选/多选/判断/填空，即时判分；仅训练场可用，不能绑定 CTF 竞赛。' },
}

const tabs = [
  { label: '内置训练', value: 'builtin' },
  { label: 'CTF 题目', value: 'ctf' },
  { label: 'ACM 题目', value: 'acm' },
  { label: '基础题', value: 'basic' },
]

// ===== 搜索 / 筛选（随 tab 与分页联动；关键字 400ms 防抖）=====
const keyword = ref('')
const categoryFilter = ref('')
const difficultyFilter = ref<'' | ChallengeDifficulty>('')
const builtinFilter = ref<'' | 'yes' | 'no'>('')
let keywordTimer: ReturnType<typeof setTimeout> | undefined

function currentQuery(): AdminChallengeQuery {
  const meta = TAB_META[activeTab.value]
  const q: AdminChallengeQuery = { page: page.value }
  if (meta.builtin) {
    q.builtin = true
  } else {
    q.bank = meta.bank
    if (builtinFilter.value === 'yes') q.builtin = true
    if (builtinFilter.value === 'no') q.builtin = false
  }
  if (keyword.value.trim()) q.q = keyword.value.trim()
  if (categoryFilter.value) q.category = categoryFilter.value
  if (difficultyFilter.value) q.difficulty = difficultyFilter.value
  return q
}

function resetAndLoad() {
  page.value = 1
  load()
}

function clearFilters() {
  keyword.value = ''
  categoryFilter.value = ''
  difficultyFilter.value = ''
  builtinFilter.value = ''
  resetAndLoad()
}

const onKeywordInput = () => {
  clearTimeout(keywordTimer)
  keywordTimer = setTimeout(resetAndLoad, 400)
}

watch([activeTab, categoryFilter, difficultyFilter, builtinFilter], resetAndLoad)
// 进入 ACM tab 时加载引擎状态卡（下载任务若仍在进行会自动续上轮询）。
watch(activeTab, (tab) => {
  if (tab === 'acm') loadEngineStatus()
})

const hasFilters = computed(() =>
  Boolean(keyword.value.trim() || categoryFilter.value || difficultyFilter.value || builtinFilter.value),
)

// 当前 tab 的分类选项从列表页可见数据聚合（管理端题目量级小，前端聚合足够；
// 保持与列表过滤同一事实源，避免单独的 distinct 接口）。
const categoryOptions = computed(() => {
  const set = new Set<string>()
  for (const item of items.value) set.add(item.category)
  return [...set].sort()
})

function categoryLabel(category: string) {
  return CATEGORY_LABELS[category] ?? category
}

function codeOf(row: TrainingItem) {
  return row.slug
}

// ===== challenge.json 导入 + 编辑弹窗 =====
const editVisible = ref(false)
const editSaving = ref(false)
const editSourceFile = ref('')
// 行内编辑来源：非空 = 正在编辑已入库题目（附件区随之显示）。
const editExistingSlug = ref('')
const editLoading = ref('')
// 教程题库为内置题集专属：行内编辑该类题时锁定题库选择，
// 提交省略 bank（后端缺省 = 保留原值），避免把内置题挤出教程题目 tab。
const bankLocked = ref(false)
// 判定方式三选一：flag 提交 / 代码判题（ACM）/ 基础题（客观题）。
type FormMode = 'flag' | 'code' | 'question'
const form = reactive({
  slug: '',
  title: '',
  category: '',
  bank: 'ctf' as ChallengeBank,
  mode: 'flag' as FormMode,
  difficulty: 'apprentice' as ChallengeJSON['difficulty'],
  score: 100,
  flag_mode: 'shared' as Required<ChallengeJSON>['flag_mode'],
  type: 'static' as NonNullable<ChallengeJSON['challenge']>['type'],
  flag: '',
  has_env: false,
  image: '',
  port: 80,
  description_md: '',
  tutorial_slug: '',
  hints: [] as ChallengeHintInput[],
  links: [] as ChallengeLink[],
  judge: null as JudgeJSON | null,
  // 基础题（question 段）
  questionType: 'single' as QuestionType,
  options: [] as QuestionOption[],
  answerSingle: '',
  answerMultiple: [] as string[],
  answerTrueFalse: true,
  fillAnswers: [''] as string[],
  matchCaseSensitive: false,
  matchTrim: true,
  matchRegex: false,
  explanation: '',
})

function defaultQuestionOptions(): QuestionOption[] {
  return [
    { key: 'A', text: '' },
    { key: 'B', text: '' },
  ]
}

function defaultJudge(): JudgeJSON {
  return { engine: 'yak', time_limit_s: 2, memory_limit_mb: 128, allowed_libs: [], testdata: [] }
}

function buildQuestionPayload(): QuestionJSON {
  const q: QuestionJSON = { type: form.questionType, answer: '' }
  if (form.questionType === 'single') {
    q.options = form.options.map((o) => ({ key: o.key.trim(), text: o.text.trim() }))
    q.answer = form.answerSingle.trim()
  } else if (form.questionType === 'multiple') {
    q.options = form.options.map((o) => ({ key: o.key.trim(), text: o.text.trim() }))
    q.answer = [...form.answerMultiple].sort()
  } else if (form.questionType === 'truefalse') {
    q.answer = form.answerTrueFalse
  } else {
    q.answer = form.fillAnswers.map((a) => a.trim()).filter(Boolean)
    q.match = {
      case_sensitive: form.matchCaseSensitive,
      trim: form.matchTrim,
      regex: form.matchRegex,
    }
  }
  if (form.explanation.trim()) q.explanation = form.explanation.trim()
  return q
}

// 表单 → 规范化题目 JSON。bankLocked（编辑内置 tutorial 题）时省略 bank。
function buildPayload(): ChallengeJSON {
  const payload = {
    slug: form.slug.trim(),
    title: form.title.trim(),
    category: form.category.trim(),
    difficulty: form.difficulty,
    score: form.score,
  } as Partial<ChallengeJSON>
  if (form.mode === 'question') {
    payload.question = buildQuestionPayload()
  } else if (form.mode === 'code') {
    const judge = form.judge ?? defaultJudge()
    payload.judge = {
      engine: 'yak',
      time_limit_s: judge.time_limit_s,
      memory_limit_mb: judge.memory_limit_mb,
      allowed_libs: judge.allowed_libs ?? [],
      testdata: judge.testdata.map((t) => ({ name: t.name, sample: t.sample, score: t.score ?? 0 })),
    }
  } else {
    payload.challenge = { type: form.type, flag: form.flag.trim() }
  }
  if (!bankLocked.value) payload.bank = form.mode === 'question' ? 'basic' : form.bank
  if (form.mode !== 'question') {
    payload.flag_mode = form.flag_mode
    if (form.has_env && form.image.trim()) payload.env = { image: form.image.trim(), port: form.port }
  }
  if (form.description_md.trim()) payload.description_md = form.description_md
  if (form.tutorial_slug.trim()) payload.tutorial_slug = form.tutorial_slug.trim()
  payload.hints = form.hints
    .filter((h) => h.title || h.content)
    .map((h) => ({ title: h.title.trim(), content: h.content.trim(), cost: h.cost ?? 0 }))
  const links = form.links.filter((l) => l.name || l.url)
  if (links.length) payload.links = links.map((l) => ({ name: l.name.trim(), url: l.url.trim() }))
  return payload as ChallengeJSON
}

// 实时校验错误（弹窗内展示，保存按钮禁用）。
const formErrors = computed(() => {
  if (!editVisible.value) return []
  return validateChallenge(buildPayload())
})

function fillForm(chal: ChallengeJSON) {
  form.slug = chal.slug ?? ''
  form.title = chal.title ?? ''
  form.category = chal.category ?? ''
  // 教程题目 tab 专属内置题集：上传只允许 ctf/acm/basic，其余（tutorial/空）归 ctf。
  bankLocked.value = chal.bank === 'tutorial'
  form.bank =
    chal.bank === 'acm' || chal.bank === 'basic' || chal.bank === 'tutorial' ? chal.bank : 'ctf'
  form.difficulty = chal.difficulty ?? 'apprentice'
  form.score = chal.score ?? 100
  form.flag_mode = chal.flag_mode ?? 'shared'
  form.type = chal.challenge?.type ?? 'static'
  form.flag = chal.challenge?.flag ?? ''
  form.judge = (chal as ChallengeFullJSON).judge
    ? {
        engine: 'yak',
        time_limit_s: (chal as ChallengeFullJSON).judge!.time_limit_s,
        memory_limit_mb: (chal as ChallengeFullJSON).judge!.memory_limit_mb ?? 128,
        allowed_libs: (chal as ChallengeFullJSON).judge!.allowed_libs ?? [],
        testdata: ((chal as ChallengeFullJSON).judge!.testdata ?? []).map((t) => ({
          name: t.name, sample: t.sample ?? false, score: t.score ?? 0,
        })),
      }
    : null
  form.has_env = Boolean(chal.env?.image)
  form.image = chal.env?.image ?? ''
  form.port = chal.env?.port ?? 80
  form.description_md = chal.description_md ?? ''
  form.tutorial_slug = chal.tutorial_slug ?? ''
  form.hints = (chal.hints ?? []).map((h) => ({ ...h }))
  form.links = (chal.links ?? []).map((l) => ({ ...l }))

  const q = chal.question ?? null
  form.mode = q ? 'question' : form.judge ? 'code' : 'flag'
  form.questionType = q?.type ?? 'single'
  form.options = q?.options?.length
    ? q.options.map((o) => ({ key: o.key, text: o.text }))
    : defaultQuestionOptions()
  form.answerSingle = typeof q?.answer === 'string' ? q.answer : ''
  form.answerMultiple = Array.isArray(q?.answer)
    ? q.answer.filter((k): k is string => typeof k === 'string')
    : []
  form.answerTrueFalse = typeof q?.answer === 'boolean' ? q.answer : true
  const fill = Array.isArray(q?.answer)
    ? q.answer.filter((a): a is string => typeof a === 'string')
    : typeof q?.answer === 'string'
      ? [q.answer]
      : []
  form.fillAnswers = fill.length ? fill : ['']
  form.matchCaseSensitive = q?.match?.case_sensitive ?? false
  form.matchTrim = q?.match?.trim ?? true
  form.matchRegex = q?.match?.regex ?? false
  form.explanation = q?.explanation ?? ''
  // 导入 JSON 缺省 bank 时按判定方式归位（question→basic / judge→acm）。
  onModeChange()
}

// 判定方式切换：题库随模式自动归位（基础题 -> basic；ACM -> acm；flag -> ctf）。
function onModeChange() {
  if (form.mode === 'question') {
    if (!bankLocked.value) form.bank = 'basic'
    form.flag_mode = 'shared'
    form.has_env = false
    if (!form.options.length) form.options = defaultQuestionOptions()
    onQuestionTypeChange(form.questionType)
  } else if (form.mode === 'code') {
    if (!form.judge) form.judge = defaultJudge()
    if (!bankLocked.value && (form.bank === 'basic' || form.bank === 'ctf')) form.bank = 'acm'
  } else if (!bankLocked.value && (form.bank === 'basic' || form.bank === 'acm')) {
    form.bank = 'ctf'
  }
}

// 题型切换：为单选补默认答案，清理与新题型不再匹配的答案。
function onQuestionTypeChange(type: QuestionType) {
  if (type === 'single') {
    if (!form.options.length) form.options = defaultQuestionOptions()
    if (!form.options.some((o) => o.key === form.answerSingle)) {
      form.answerSingle = form.options[0]?.key ?? 'A'
    }
  } else if (type === 'multiple') {
    if (!form.options.length) form.options = defaultQuestionOptions()
    form.answerMultiple = form.answerMultiple.filter((key) =>
      form.options.some((o) => o.key === key),
    )
  } else if (type === 'fill' && !form.fillAnswers.length) {
    form.fillAnswers = ['']
  }
}

function addQuestionOption() {
  const used = new Set(form.options.map((o) => o.key.trim()))
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')
  let key = alphabet.find((k) => !used.has(k))
  if (!key) {
    let n = 1
    while (used.has(String(n))) n++
    key = String(n)
  }
  form.options.push({ key, text: '' })
}

function removeQuestionOption(index: number) {
  const [removed] = form.options.splice(index, 1)
  if (!removed) return
  if (form.answerSingle === removed.key) form.answerSingle = ''
  form.answerMultiple = form.answerMultiple.filter((key) => key !== removed.key)
}

function toggleMultipleAnswer(key: string, checked: boolean) {
  if (checked) {
    if (!form.answerMultiple.includes(key)) form.answerMultiple.push(key)
  } else {
    form.answerMultiple = form.answerMultiple.filter((k) => k !== key)
  }
}

function addFillAnswer() {
  form.fillAnswers.push('')
}

function removeFillAnswer(index: number) {
  form.fillAnswers.splice(index, 1)
  if (!form.fillAnswers.length) form.fillAnswers = ['']
}

// 判定方式/题型的展示辅助。
function questionTypeLabel(row: TrainingItem) {
  if (row.type === 'single' || row.type === 'multiple' || row.type === 'truefalse' || row.type === 'fill') {
    return `基础题 · ${QUESTION_TYPE_LABELS[row.type]}`
  }
  if (row.type === 'code') return '代码判题'
  if (row.type === 'static' || row.type === 'regex') return 'flag 提交'
  if (row.bank === 'acm') return '代码判题'
  if (row.bank === 'basic') return '基础题'
  return 'flag 提交'
}

function addHint() {
  form.hints.push({ title: '', content: '', cost: 10 })
}
function removeHint(index: number) {
  form.hints.splice(index, 1)
}
function addLink() {
  form.links.push({ name: '', url: '' })
}
function removeLink(index: number) {
  form.links.splice(index, 1)
}

// 文件导入：读文件 → 解析 → 校验 → 单题进编辑弹窗，多题走批量导入确认。
const bulkList = ref<ChallengeJSON[] | null>(null)

async function onImport(file: File) {
  let parsed
  try {
    const text = await file.text()
    parsed = parseChallengeJSON(text)
  } catch (e) {
    notify.error(`读取文件失败：${(e as Error).message}`)
    return false
  }
  if (parsed.error) {
    notify.error(parsed.error)
    return false
  }
  const { list } = parsed
  if (list.length > 1) {
    // 数组文件：编辑弹窗只支持单题，提供直接批量导入的出路。
    bulkList.value = list
    return false
  }
  const chal = list[0]
  if (chal.bank === 'tutorial') {
    notify.warning({ message: '教程题库为内置题集专属：该文件声明的题库不再接受上传，将按判定方式归入对应题库', duration: 5000 })
  }
  const errors = validateChallenge(chal)
  if (errors.some((err) => err.includes('三选一互斥'))) {
    // 冲突的判定段无法在同构表单中同时表达；直接拦截，避免保存时静默丢弃其一。
    notify.error({
      message: '题目判定定义冲突：question / judge / challenge 三选一，请修改文件后重新选择',
      duration: 6000,
    })
    return false
  }
  if (errors.length) {
    notify.warning({ message: `题目格式有 ${errors.length} 处问题，请在弹窗中修正后上传`, duration: 3500 })
  }
  fillForm(chal)
  if (bankLocked.value) {
    bankLocked.value = false
    form.bank = form.mode === 'question' ? 'basic' : form.mode === 'code' ? 'acm' : 'ctf'
  }
  editExistingSlug.value = ''
  editSourceFile.value = file.name
  editVisible.value = true
  return false
}

async function confirmBulkImport() {
  const list = bulkList.value
  if (!list) return
  await doUpload(list)
  bulkList.value = null
}

// 行内「编辑」：拉取库内完整定义回填弹窗（含 flag/hints/env），附件区随之加载。
async function openRowEdit(row: TrainingItem) {
  editLoading.value = row.slug
  try {
    const full: ChallengeFullJSON = await adminChallengeApi.getJSON(row.slug)
    fillForm(full)
    if (full.flags.length > 1) {
      notify.warning({ message: `该题有 ${full.flags.length} 个 flag（题目包上传），保存后只保留表单中的这一个`, duration: 6000 })
    }
    editSourceFile.value = ''
    editExistingSlug.value = row.slug
    editVisible.value = true
    await loadAttachments()
  } catch (e) {
    notify.error(`读取题目定义失败：${(e as Error).message}`)
  } finally {
    editLoading.value = ''
  }
}

async function doUpload(list: ChallengeJSON[]) {
  uploading.value = true
  try {
    const resp = await adminChallengeApi.upload(list)
    notify.success(`已导入 ${resp.imported} 个题目：${resp.slugs.join(', ')}`)
    await load()
  } catch (e) {
    notify.error(`上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
}

async function saveFromEdit() {
  if (formErrors.value.length) return
  editSaving.value = true
  try {
    const payload = buildPayload()
    const resp = await adminChallengeApi.upload([payload])
    notify.success(`已导入题目：${resp.slugs.join(', ')}`)
    editVisible.value = false
    await load()
  } catch (e) {
    notify.error(`上传失败：${(e as Error).message}`)
  } finally {
    editSaving.value = false
  }
}

async function onUploadPackage(file: File) {
  uploading.value = true
  try {
    const resp = await adminChallengeApi.uploadPackage(file)
    notify.success(`已导入题目包：${resp.slugs.join(', ')}`)
    await load()
  } catch (e) {
    notify.error(`题目包上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
  return false
}

// ===== 内置训练（vulinbox）一键导入 =====
// 幂等：已导入的同名题目会被更新；镜像在 Docker 可用时构建。
const builtinImporting = ref(false)
const builtinConfirmOpen = ref(false)

async function confirmImportBuiltin() {
  builtinImporting.value = true
  try {
    const resp = await adminChallengeApi.importBuiltin()
    let summary = `内置训练 v${resp.version}：新增 ${resp.created} 题、更新 ${resp.updated} 题、教程 ${resp.rooms} 篇`
    if (resp.removed > 0) {
      summary += `、清理目录外残留 ${resp.removed} 题`
    }
    if (resp.image_built) {
      notify.success(`${summary}；靶场镜像已构建`)
    } else {
      notify.warning({ message: `${summary}；未配置 Docker，镜像未构建（启用 Docker 后重新导入即可）`, duration: 6000 })
    }
    builtinConfirmOpen.value = false
    await load()
  } catch (e) {
    notify.error(`内置训练导入失败：${(e as Error).message}`)
  } finally {
    builtinImporting.value = false
  }
}

// ===== vulinbox 靶场镜像状态（OSS 构建） =====
const imageStatus = ref<BuiltinImageStatus | null>(null)
const imageBuilding = ref(false)
const imageConfirmOpen = ref(false)

async function loadImageStatus() {
  try {
    imageStatus.value = await adminChallengeApi.builtinImageStatus()
  } catch {
    imageStatus.value = null // 状态查询失败不阻塞页面
  }
}

async function confirmBuildImage() {
  imageBuilding.value = true
  try {
    const resp = await adminChallengeApi.buildBuiltinImage()
    notify.success(`镜像已构建：${resp.image_tag}`)
    imageConfirmOpen.value = false
    await loadImageStatus()
  } catch (e) {
    notify.error({ message: `镜像构建失败：${(e as Error).message}`, duration: 8000 })
  } finally {
    imageBuilding.value = false
  }
}

let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  try {
    const resp = await adminChallengeApi.list(currentQuery())
    if (seq !== loadSeq) return
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    if (seq !== loadSeq) return
    loadError.value = e instanceof Error ? e.message : '题目列表加载失败，请稍后重试'
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

// ===== yak 判题引擎状态（OSS 异步下载，仅 ACM tab） =====
const engineStatus = ref<YakEngineStatus | null>(null)
const engineConfirmOpen = ref(false)
let enginePollTimer: number | undefined

async function loadEngineStatus() {
  try {
    engineStatus.value = await adminChallengeApi.yakEngineStatus()
    resumeEnginePolling()
  } catch {
    engineStatus.value = null // 状态查询失败不阻塞页面
  }
}

function resumeEnginePolling() {
  if (engineStatus.value?.download.state === 'running' && enginePollTimer === undefined) {
    enginePollTimer = window.setInterval(pollEngineOnce, 1000)
  }
}

async function pollEngineOnce() {
  try {
    engineStatus.value = await adminChallengeApi.yakEngineStatus()
  } catch {
    return // 瞬时失败容忍，下一拍重试
  }
  const state = engineStatus.value?.download.state
  if (state !== 'running') {
    stopEnginePolling()
    if (state === 'done') {
      notify.success('yak 引擎已就绪，ACM 判题与自测运行已启用')
    } else if (state === 'failed') {
      notify.error({ message: `引擎下载失败：${engineStatus.value?.download.error || '未知错误'}`, duration: 8000 })
    }
  }
}

function stopEnginePolling() {
  if (enginePollTimer !== undefined) {
    clearInterval(enginePollTimer)
    enginePollTimer = undefined
  }
}

const engineDownloadPercent = computed(() => {
  const download = engineStatus.value?.download
  if (!download || download.total <= 0) return 0
  return Math.min(100, Math.round((download.received / download.total) * 100))
})

async function confirmDownloadEngine() {
  try {
    engineStatus.value = await adminChallengeApi.yakEngineDownload()
    engineConfirmOpen.value = false
    resumeEnginePolling()
  } catch (e) {
    notify.error(`引擎下载启动失败：${(e as Error).message}`)
  }
}

// ===== 删除 / 可见性 =====
const deleteTarget = ref<TrainingItem | null>(null)
const deleting = ref(false)

async function confirmRemove() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await adminChallengeApi.remove(target.slug)
    notify.success(`已删除 ${target.title}`)
    deleteTarget.value = null
    await load()
  } catch (e) {
    // 已有学员通关的题目不可删除（事件历史只追加）。
    notify.error(`删除失败：${(e as Error).message}`)
  } finally {
    deleting.value = false
  }
}

const visSaving = ref('')

async function toggleVisible(row: TrainingItem) {
  visSaving.value = row.slug
  try {
    const updated = await adminChallengeApi.updateVisibility(row.slug, { is_visible: !row.is_visible })
    row.is_visible = updated.is_visible
    notify.success(updated.is_visible ? `「${row.title}」已对学员可见` : `「${row.title}」已隐藏`)
  } catch (e) {
    notify.error(`可见性更新失败：${(e as Error).message}`)
  } finally {
    visSaving.value = ''
  }
}

// ===== 附件管理（编辑弹窗内：pcap/exe 等配套文件，学员登录后下载） =====
const attachments = ref<ChallengeAttachment[]>([])
const attachLoading = ref(false)
const attachError = ref('')
const attachUploading = ref(false)
const attachmentTarget = ref('')
const attachmentDeleting = ref(false)

async function loadAttachments() {
  if (!editExistingSlug.value) return
  attachLoading.value = true
  attachError.value = ''
  attachments.value = []
  try {
    attachments.value = await adminChallengeApi.listAttachments(editExistingSlug.value)
  } catch (e) {
    attachError.value = `附件列表加载失败：${(e as Error).message}`
  } finally {
    attachLoading.value = false
  }
}

// 同名文件覆盖更新（服务端按题目与文件名更新）。
async function onUploadAttachment(file: File) {
  attachUploading.value = true
  try {
    await adminChallengeApi.uploadAttachment(editExistingSlug.value, file)
    notify.success(`已上传附件：${file.name}`)
    await loadAttachments()
  } catch (e) {
    notify.error(`附件上传失败：${(e as Error).message}`)
  } finally {
    attachUploading.value = false
  }
  return false
}

async function confirmRemoveAttachment() {
  const filename = attachmentTarget.value
  if (!filename) return
  attachmentDeleting.value = true
  try {
    await adminChallengeApi.deleteAttachment(editExistingSlug.value, filename)
    attachmentTarget.value = ''
    await loadAttachments()
  } catch (e) {
    notify.error(`附件删除失败：${(e as Error).message}`)
  } finally {
    attachmentDeleting.value = false
  }
}

// 内置 tab 只读：编辑/删除/可见性/附件全部收口（后端同语义兜底）。
const readonlyTab = computed(() => TAB_META[activeTab.value].builtin)

function onEmptyAction() {
  if (hasFilters.value) {
    clearFilters()
  } else if (readonlyTab.value) {
    builtinConfirmOpen.value = true
  } else {
    onDownloadSample()
  }
}

function onDownloadSample() {
  if (activeTab.value === 'basic') downloadSampleBasicQuestions()
  else downloadSampleChallenge()
}

onMounted(() => {
  load()
  loadImageStatus()
})

onUnmounted(stopEnginePolling)
</script>

<template>
  <PageHeader
    title="题目管理"
    description="维护内置题集、CTF 题目、ACM 题目与基础题；上传、可见性、附件与配套教程都在这一个页面完成。"
  >
    <template #badges>
      <span v-if="total" class="head-count">共 {{ total }} 题</span>
    </template>
    <template #actions>
      <template v-if="readonlyTab">
        <Button type="button" variant="outline" @click="load">刷新</Button>
        <Button type="button" :disabled="builtinImporting" @click="builtinConfirmOpen = true">
          导入内置训练（vulinbox）
        </Button>
      </template>
      <template v-else>
        <Button type="button" variant="outline" @click="specVisible = !specVisible">
          {{ specVisible ? '收起格式规范' : '查看格式规范' }}
        </Button>
        <Button type="button" variant="outline" @click="onDownloadSample">下载样例 JSON</Button>
        <UiUpload
          :show-file-list="false"
          accept=".zip,application/zip"
          :before-upload="onUploadPackage"
          :disabled="uploading"
        >
          <UiButton :loading="uploading">上传题目包 zip</UiButton>
        </UiUpload>
        <UiUpload
          :show-file-list="false"
          accept=".json,application/json"
          :before-upload="onImport"
          :disabled="uploading"
        >
          <UiButton type="primary" :loading="uploading">选择 challenge.json</UiButton>
        </UiUpload>
      </template>
    </template>
  </PageHeader>

  <PageTabs v-model="activeTab" :tabs="tabs" aria-label="题库切换" />
  <p class="tab-hint">{{ TAB_META[activeTab].hint }}</p>

  <section v-if="specVisible && !readonlyTab" class="spec-panel">
    <div class="spec-panel__head">
      <h2 class="spec-panel__title">题目格式</h2>
      <Button type="button" variant="ghost" size="sm" @click="specVisible = false">收起</Button>
    </div>
    <p class="spec-text">用 challenge.json 定义题目，字段如下（下载样例 JSON 可直接修改）：</p>
    <table class="spec-table">
      <thead>
        <tr><th scope="col">字段</th><th scope="col">必填</th><th scope="col">说明</th></tr>
      </thead>
      <tbody>
        <tr><td>题目标识</td><td>是</td><td>全站唯一，小写字母、数字与连字符</td></tr>
        <tr><td>标题</td><td>是</td><td>学员看到的题目名称</td></tr>
        <tr><td>分类</td><td>是</td><td>小写字母、数字与连字符，用于筛选</td></tr>
        <tr><td>题库</td><td>否</td><td>CTF / ACM / 基础题；教程题库为内置题集专属</td></tr>
        <tr><td>难度</td><td>是</td><td>入门 / 进阶 / 专家</td></tr>
        <tr><td>分值</td><td>是</td><td>非负整数</td></tr>
        <tr><td>描述</td><td>基础题必填</td><td>Markdown 正文</td></tr>
        <tr><td>判定方式</td><td>三选一</td><td>flag 提交（明文/正则）、代码判题（ACM）、基础题（单选/多选/判断/填空）</td></tr>
        <tr><td>基础题答案</td><td>基础题必填</td><td>单选 key / 多选 key 数组 / 判断布尔值 / 填空文本（可多个）</td></tr>
        <tr><td>容器环境</td><td>否</td><td>镜像与端口（仅 flag 题可用）</td></tr>
        <tr><td>提示</td><td>否</td><td>标题、内容与花费积分</td></tr>
        <tr><td>外链</td><td>否</td><td>名称与 URL</td></tr>
        <tr><td>关联教程</td><td>否</td><td>填写教程标识</td></tr>
      </tbody>
    </table>
    <h3 class="spec-subtitle">容器题（自定义镜像）</h3>
    <p class="spec-text">题目包 zip 根目录放 Dockerfile、docker-compose.yml 与题目定义；上传时构建镜像，学生打开题目即获得独立环境。</p>
    <p class="spec-note">附件（pcap、exe 等）不写进 JSON：在行内「编辑」弹窗的附件区上传，学员登录后可下载。</p>
  </section>

  <FilterBar class="challenge-filters">
    <label class="filter-field">
      <span class="filter-field__label">搜索</span>
      <span class="filter-field__control">
        <Search class="filter-field__icon" aria-hidden="true" />
        <Input
          v-model="keyword"
          class="filter-field__input"
          placeholder="搜索标识 / 标题 / 分类（前缀匹配）"
          @input="onKeywordInput"
        />
      </span>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">分类</span>
      <select v-model="categoryFilter" class="filter-select" aria-label="分类">
        <option value="">全部分类</option>
        <option v-for="category in categoryOptions" :key="category" :value="category">
          {{ categoryLabel(category) }}（{{ category }}）
        </option>
      </select>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">难度</span>
      <select v-model="difficultyFilter" class="filter-select" aria-label="难度">
        <option value="">全部难度</option>
        <option v-for="(label, value) in DIFFICULTY_LABELS" :key="value" :value="value">{{ label }}</option>
      </select>
    </label>
    <label v-if="!readonlyTab" class="filter-field">
      <span class="filter-field__label">来源</span>
      <select v-model="builtinFilter" class="filter-select" aria-label="来源">
        <option value="">全部来源</option>
        <option value="yes">内置题集</option>
        <option value="no">管理员上传</option>
      </select>
    </label>
    <template #actions>
      <Button v-if="hasFilters" type="button" variant="ghost" size="sm" class="filter-clear" @click="clearFilters">
        清除筛选
      </Button>
    </template>
  </FilterBar>

  <!-- vulinbox 靶场镜像状态：仅内置训练 tab 显示 -->
  <section v-if="readonlyTab && imageStatus" class="status-panel image-card">
    <div class="status-panel__main">
      <p class="status-panel__title">vulinbox 靶场镜像</p>
      <div class="status-panel__chips">
        <span class="status-chip" :data-tone="imageStatus.driver === 'docker' ? 'success' : 'neutral'">
          驱动：{{ imageStatus.driver === 'docker' ? 'Docker' : '未启用' }}
        </span>
        <span
          class="status-chip"
          :data-tone="imageStatus.image_present ? 'success' : imageStatus.can_build ? 'warning' : 'danger'"
        >
          {{ imageStatus.image_present ? '镜像已就绪' : imageStatus.can_build ? '镜像缺失' : '镜像未构建（需 Docker）' }}
        </span>
        <code class="image-tag">{{ imageStatus.image_tag }}</code>
      </div>
    </div>
    <div class="status-panel__actions">
      <Button type="button" variant="outline" size="sm" @click="loadImageStatus">刷新</Button>
      <Button
        type="button"
        size="sm"
        :disabled="!imageStatus.can_build || imageBuilding"
        :title="imageStatus.can_build ? '从官方 OSS 下载二进制并构建镜像（约 165MB，1-3 分钟）' : '服务端未启用容器运行时，无法构建镜像'"
        @click="imageConfirmOpen = true"
      >
        {{ imageBuilding ? '正在构建…' : '从 OSS 构建镜像' }}
      </Button>
    </div>
  </section>

  <!-- yak 判题引擎状态：仅 ACM tab 显示 -->
  <section v-if="activeTab === 'acm' && engineStatus" class="status-panel image-card">
    <div class="status-panel__main">
      <p class="status-panel__title">yak 判题引擎</p>
      <div class="status-panel__chips">
        <StatusTag
          kind="container"
          :value="engineStatus.configured ? 'running' : 'stopped'"
          :label="engineStatus.configured ? '已装配' : '未装配'"
        />
        <span v-if="engineStatus.version" class="status-chip">{{ engineStatus.version }}</span>
        <code class="image-tag">{{ engineStatus.path }}</code>
      </div>
      <div v-if="engineStatus.download.state === 'running'" class="engine-progress">
        <div class="usage-track" role="presentation">
          <span class="usage-fill" :style="{ width: `${engineDownloadPercent}%` }" />
        </div>
        <span class="engine-progress-text">
          {{ engineStatus.download.total > 0
            ? `${formatBytes(engineStatus.download.received)} / ${formatBytes(engineStatus.download.total)}`
            : '正在下载…' }}
        </span>
      </div>
      <p v-else-if="engineStatus.download.state === 'failed'" class="engine-error">
        上次下载失败：{{ engineStatus.download.error || '未知错误' }}（可重试）
      </p>
    </div>
    <div class="status-panel__actions">
      <Button type="button" variant="outline" size="sm" @click="loadEngineStatus">刷新</Button>
      <Button type="button" size="sm" :disabled="engineStatus.download.state === 'running'" @click="engineConfirmOpen = true">
        {{ engineStatus.download.state === 'running' ? '正在下载…' : '从 OSS 下载引擎' }}
      </Button>
    </div>
  </section>

  <DataTablePanel
    v-if="!loadError"
    title="题目列表"
    :count="total"
    :description="loading ? '正在加载题目…' : hasFilters ? `按当前条件显示 ${total} 题` : undefined"
  >
    <UiTable
      :data="items"
      :loading="loading"
      :loading-rows="7"
      class="chal-table"
      aria-label="题目列表"
    >
      <UiTableColumn label="题目" min-width="260">
        <template #default="{ row }">
          <span class="challenge-title">{{ row.title }}</span>
          <code class="challenge-code">{{ codeOf(row) }}</code>
        </template>
      </UiTableColumn>
      <UiTableColumn label="分类" min-width="130">
        <template #default="{ row }">
          <span class="challenge-category">{{ categoryLabel(row.category) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="难度" width="100">
        <template #default="{ row }">
          <StatusTag kind="difficulty" :value="row.difficulty" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="判定" min-width="120">
        <template #default="{ row }">
          <span class="challenge-muted">{{ questionTypeLabel(row) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="分值" width="80">
        <template #default="{ row }">
          <span class="challenge-score">{{ row.score }} 分</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="容器" width="80">
        <template #default="{ row }">
          <span :class="row.has_env ? 'challenge-flag-yes' : 'challenge-muted'">{{ row.has_env ? '支持' : '—' }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="关联教程" min-width="160">
        <template #default="{ row }">
          <span v-if="row.tutorial_title" class="challenge-muted">{{ row.tutorial_title }}</span>
          <span v-else-if="row.tutorial_slug" class="challenge-muted">{{ row.tutorial_slug }}</span>
          <span v-else class="challenge-muted">—</span>
        </template>
      </UiTableColumn>
      <template v-if="!readonlyTab">
        <UiTableColumn label="可见性" width="100">
          <template #default="{ row }">
            <Switch
              :model-value="row.is_visible !== false"
              :disabled="visSaving === row.slug"
              :aria-label="`切换「${row.title}」可见性`"
              @update:model-value="toggleVisible(row)"
            />
          </template>
        </UiTableColumn>
        <UiTableColumn label="操作" width="150">
          <template #default="{ row }">
            <div class="row-actions">
              <Button type="button" variant="outline" size="sm" :disabled="editLoading === row.slug" @click="openRowEdit(row)">
                {{ editLoading === row.slug ? '读取中…' : '编辑' }}
              </Button>
              <Button type="button" variant="link" size="sm" class="danger-action" @click="deleteTarget = row">删除</Button>
            </div>
          </template>
        </UiTableColumn>
      </template>
      <template #empty>
        <EmptyState
          data-slot="table-empty"
          compact
          :tone="hasFilters ? 'filtered' : 'empty'"
          :title="hasFilters ? '没有匹配的题目' : readonlyTab ? '内置训练尚未导入' : '还没有题目'"
          :description="hasFilters
            ? '试试更换关键词，或清除分类、难度与来源筛选。'
            : readonlyTab
              ? '内置训练随平台分发；导入后可在这里查看题目与配套教程。'
              : '上传 challenge.json 或题目包 zip 后，题目会出现在这里。'"
          :action-label="hasFilters ? '清除筛选' : readonlyTab ? '导入内置训练' : '下载样例 JSON'"
          @action="onEmptyAction"
        />
      </template>
    </UiTable>
    <template v-if="total > pageSize" #footer>
      <UiPagination
        v-model:current-page="page"
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        @current-change="load"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="题目列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <!-- 题目编辑弹窗：行内「编辑」回填库内定义，或选择 challenge.json 后确认上传 -->
  <UiDialog
    v-model="editVisible"
    :title="editExistingSlug ? `编辑题目：${editExistingSlug}` : `编辑题目${editSourceFile ? `（${editSourceFile}）` : ''}`"
    width="680px"
    top="4vh"
  >
    <UiAlert v-if="formErrors.length" type="error" :closable="false" class="form-errors">
      <p v-for="err in formErrors" :key="err" class="err-line">{{ err }}</p>
    </UiAlert>
    <UiForm label-width="110px">
      <div class="form-grid">
        <UiFormItem label="题目标识" required>
          <UiInput v-model="form.slug" placeholder="例如 sqli-login-bypass" />
        </UiFormItem>
        <UiFormItem label="标题" required>
          <UiInput v-model="form.title" />
        </UiFormItem>
        <UiFormItem label="分类" required>
          <UiInput v-model="form.category" placeholder="例如 sql-injection" />
        </UiFormItem>
        <UiFormItem label="判定方式" required>
          <UiSelect v-model="form.mode" :disabled="bankLocked" @change="onModeChange">
            <UiOption value="flag" label="flag 提交" />
            <UiOption value="code" label="代码判题（ACM）" />
            <UiOption value="question" label="基础题（客观题）" />
          </UiSelect>
          <div class="form-hint">flag 题、ACM 编程题、基础题三选一</div>
        </UiFormItem>
        <UiFormItem label="题库">
          <UiSelect v-model="form.bank" :disabled="bankLocked || form.mode === 'question'">
            <UiOption v-if="bankLocked" value="tutorial" label="教程题目（内置，不可改）" />
            <UiOption value="ctf" label="CTF 题目" />
            <UiOption value="acm" label="ACM 题目" />
            <UiOption value="basic" label="基础题" />
          </UiSelect>
          <div class="form-hint">基础题自动归「基础题」题库；教程题库经「导入内置训练」写入</div>
        </UiFormItem>
        <UiFormItem label="难度" required>
          <UiSelect v-model="form.difficulty">
            <UiOption value="apprentice" label="入门" />
            <UiOption value="practitioner" label="进阶" />
            <UiOption value="expert" label="专家" />
          </UiSelect>
        </UiFormItem>
        <UiFormItem label="分值" required>
          <UiInputNumber v-model="form.score" :min="0" />
        </UiFormItem>
        <UiFormItem label="关联教程">
          <UiInput v-model="form.tutorial_slug" placeholder="可选，填写教程标识" />
        </UiFormItem>
        <!-- ACM 编程题：判定配置回填；测试数据经题目包 zip 维护，组分值可在此调整 -->
        <template v-if="form.judge && form.mode === 'code'">
          <UiFormItem label="判定引擎">
            <UiTag>代码判题（yak）</UiTag>
            <div class="form-hint">
              学员提交 yak 源码，引擎逐组运行测试数据判定（不验 flag）。
              当前共 {{ form.judge.testdata.length }} 组测试数据，内容经题目包 zip 维护，此处保存只更新元数据。
            </div>
          </UiFormItem>
          <UiFormItem label="时限（秒）">
            <UiInputNumber v-model="form.judge.time_limit_s" :min="1" :max="60" controls-position="right" />
          </UiFormItem>
          <UiFormItem label="内存上限（MB）">
            <UiInputNumber v-model="form.judge.memory_limit_mb" :min="16" :max="512" controls-position="right" />
          </UiFormItem>
          <UiFormItem label="库白名单">
            <UiSelect
              v-model="form.judge.allowed_libs"
              multiple
              filterable
              allow-create
              :reserve-keyword="false"
              placeholder="留空 = 允许全部 yak 库（禁用库除外）"
              class="lib-select"
            >
              <UiOption v-for="option in libOptions" :key="option.value" :value="option.value" :label="option.label" />
            </UiSelect>
            <div class="form-hint">
              配置后学员源码只能引用白名单内的库；命令执行与宿主文件访问类库始终不可用。
            </div>
          </UiFormItem>
          <UiFormItem label="测试组与分值">
            <div v-if="form.judge.testdata.length" class="testdata-list">
              <div v-for="test in form.judge.testdata" :key="test.name" class="testdata-row">
                <span class="testdata-name">{{ test.name }}</span>
                <UiTag v-if="test.sample" size="small" effect="plain">样例</UiTag>
                <UiInputNumber
                  v-model="test.score"
                  :min="0"
                  size="small"
                  controls-position="right"
                  class="testdata-score"
                />
              </div>
            </div>
            <div v-else class="form-hint">无数据组（保存会保留库内测试数据）。</div>
            <div class="form-hint">
              组分值 0 = 全有全无（全部通过才得分）；大于 0 时按权重分摊题面分值，通过即得。
            </div>
          </UiFormItem>
        </template>
        <template v-else-if="form.mode === 'flag'">
          <UiFormItem label="判定类型" required>
            <UiSelect v-model="form.type">
              <UiOption value="static" label="明文 flag" />
              <UiOption value="regex" label="正则匹配" />
            </UiSelect>
          </UiFormItem>
          <UiFormItem label="flag" required>
            <UiInput v-model="form.flag" :placeholder="form.flag_mode === 'instance' ? 'flag{lab-{token}}' : 'flag{answer}'" />
          </UiFormItem>
          <UiFormItem label="flag 作用域">
            <UiSelect v-model="form.flag_mode">
              <UiOption value="shared" label="题目级共享" />
              <UiOption value="instance" label="每实例唯一" />
            </UiSelect>
          </UiFormItem>
        </template>
        <template v-else>
          <UiFormItem label="题型" required>
            <UiSelect v-model="form.questionType" @change="onQuestionTypeChange">
              <UiOption value="single" label="单选" />
              <UiOption value="multiple" label="多选" />
              <UiOption value="truefalse" label="判断" />
              <UiOption value="fill" label="填空" />
            </UiSelect>
          </UiFormItem>
          <UiFormItem label="解析">
            <UiInput v-model="form.explanation" placeholder="可选；学员通关后展示" />
          </UiFormItem>
          <UiFormItem
            v-if="form.questionType === 'single' || form.questionType === 'multiple'"
            label="选项与答案"
            required
            class="form-span"
          >
            <div class="question-options">
              <div v-for="(option, index) in form.options" :key="index" class="question-option-row">
                <UiInput
                  v-model="option.key"
                  maxlength="8"
                  aria-label="选项 key"
                  class="question-option-key"
                />
                <UiInput
                  v-model="option.text"
                  :aria-label="`选项 ${option.key} 文本`"
                  placeholder="选项文本"
                  class="question-option-text"
                />
                <UiRadioGroup
                  v-if="form.questionType === 'single'"
                  v-model="form.answerSingle"
                  class="question-option-pick"
                >
                  <UiRadio :value="option.key" :aria-label="`将 ${option.key} 设为正确答案`">正确答案</UiRadio>
                </UiRadioGroup>
                <UiCheckbox
                  v-else
                  :model-value="form.answerMultiple.includes(option.key)"
                  :aria-label="`将 ${option.key} 设为正确答案`"
                  @update:model-value="(checked) => toggleMultipleAnswer(option.key, checked)"
                >
                  正确答案
                </UiCheckbox>
                <UiButton type="danger" link @click="removeQuestionOption(index)">删除</UiButton>
              </div>
              <UiButton size="small" :disabled="form.options.length >= 26" @click="addQuestionOption">
                添加选项
              </UiButton>
              <div class="form-hint">
                单选/多选需 2–26 个选项，key 用大写字母/数字；勾选「正确答案」的选项参与判分。
              </div>
            </div>
          </UiFormItem>
          <UiFormItem v-else-if="form.questionType === 'truefalse'" label="正确答案" required>
            <UiRadioGroup v-model="form.answerTrueFalse">
              <UiRadio :value="true">正确</UiRadio>
              <UiRadio :value="false">错误</UiRadio>
            </UiRadioGroup>
          </UiFormItem>
          <UiFormItem v-else label="可接受答案" required class="form-span">
            <div class="fill-answers">
              <div v-for="(_, index) in form.fillAnswers" :key="index" class="fill-answer-row">
                <UiInput
                  v-model="form.fillAnswers[index]"
                  :aria-label="`可接受答案 ${index + 1}`"
                  placeholder="例如 Paris"
                />
                <UiButton type="danger" link @click="removeFillAnswer(index)">删除</UiButton>
              </div>
              <UiButton size="small" @click="addFillAnswer">添加答案</UiButton>
              <div class="match-switches">
                <UiCheckbox v-model="form.matchCaseSensitive">区分大小写</UiCheckbox>
                <UiCheckbox v-model="form.matchTrim">忽略首尾空白</UiCheckbox>
                <UiCheckbox v-model="form.matchRegex">按正则匹配</UiCheckbox>
              </div>
              <div class="form-hint">
                填空全对才通关；多选部分正确仅作作答反馈，不计积分。
              </div>
            </div>
          </UiFormItem>
        </template>
      </div>
      <UiFormItem v-if="form.mode !== 'question'" label="容器环境">
        <UiSwitch v-model="form.has_env" aria-label="启用容器环境" />
        <template v-if="form.has_env">
          <UiInput v-model="form.image" placeholder="镜像，例如 nginx:alpine；自定义镜像请走题目包 zip" class="env-image" />
          <UiInputNumber v-model="form.port" :min="1" :max="65535" controls-position="right" />
        </template>
      </UiFormItem>
      <UiFormItem label="描述 Markdown" :required="form.mode === 'question'">
        <UiInput v-model="form.description_md" type="textarea" :rows="6" class="mono" />
        <div v-if="form.mode === 'question'" class="form-hint">基础题的题面与作答说明写在这里（必填）。</div>
      </UiFormItem>
      <UiFormItem label="提示">
        <div class="hints">
          <div v-for="(hint, index) in form.hints" :key="index" class="hint-row">
            <UiInput v-model="hint.title" placeholder="标题" class="hint-title" />
            <UiInput v-model="hint.content" placeholder="内容" class="hint-content" />
            <UiInputNumber v-model="hint.cost" :min="0" controls-position="right" class="hint-cost" />
            <UiButton type="danger" link @click="removeHint(index)">删除</UiButton>
          </div>
          <UiButton size="small" @click="addHint">添加提示</UiButton>
        </div>
      </UiFormItem>
      <UiFormItem label="外链">
        <div class="hints">
          <div v-for="(link, index) in form.links" :key="index" class="hint-row">
            <UiInput v-model="link.name" placeholder="名称（如 Wireshark 下载）" class="hint-title" />
            <UiInput v-model="link.url" placeholder="https://…" class="hint-content" />
            <UiButton type="danger" link @click="removeLink(index)">删除</UiButton>
          </div>
          <UiButton size="small" @click="addLink">添加外链</UiButton>
        </div>
      </UiFormItem>
      <!-- 附件区：仅行内编辑已入库题目时显示 -->
      <template v-if="editExistingSlug">
        <UiDivider content-position="left">附件（学员登录后可下载）</UiDivider>
        <div class="attach-toolbar">
          <UiUpload
            :show-file-list="false"
            :before-upload="onUploadAttachment"
            :disabled="attachUploading"
          >
            <UiButton type="primary" size="small" :loading="attachUploading">上传附件（≤50MB，同名覆盖）</UiButton>
          </UiUpload>
        </div>
        <EmptyState
          v-if="attachError"
          compact
          tone="error"
          title="附件列表加载失败"
          :description="attachError"
          action-label="重试"
          @action="loadAttachments"
        />
        <UiTable v-else :data="attachments" :loading="attachLoading" :loading-rows="3" aria-label="题目附件">
          <UiTableColumn label="文件名" min-width="200">
            <template #default="{ row }">
              <span class="challenge-code">{{ row.filename }}</span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="大小" width="100">
            <template #default="{ row }">{{ formatBytes(row.size) }}</template>
          </UiTableColumn>
          <UiTableColumn label="上传时间" width="160">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </UiTableColumn>
          <UiTableColumn label="操作" width="90">
            <template #default="{ row }">
              <Button type="button" variant="link" size="sm" class="danger-action" @click="attachmentTarget = row.filename">删除</Button>
            </template>
          </UiTableColumn>
          <template #empty>
            <EmptyState compact title="暂无附件" description="上传 pcap、exe 等配套文件后，学员可在题目页下载。" />
          </template>
        </UiTable>
      </template>
    </UiForm>
    <template #footer>
      <UiButton @click="editVisible = false">取消</UiButton>
      <UiButton type="primary" :loading="editSaving" :disabled="formErrors.length > 0" @click="saveFromEdit">
        确认上传
      </UiButton>
    </template>
  </UiDialog>

  <ConfirmDialog
    :open="!!deleteTarget"
    title="删除题目"
    :description="deleteTarget ? `确定删除题目「${deleteTarget.title}」？运行中的容器会被销毁，判定答案与提示一并删除。已有学员通关的题目无法删除。` : ''"
    confirm-label="删除"
    tone="danger"
    :loading="deleting"
    @update:open="!$event && (deleteTarget = null)"
    @confirm="confirmRemove"
  />

  <ConfirmDialog
    :open="builtinConfirmOpen"
    title="导入内置训练"
    description="将导入随平台分发的内置训练集（配套教程与题目）。已导入的同名内容会被更新；配置了 Docker 时会同时构建靶场基础镜像。"
    confirm-label="导入/更新"
    :loading="builtinImporting"
    @update:open="builtinConfirmOpen = $event"
    @confirm="confirmImportBuiltin"
  />

  <ConfirmDialog
    :open="!!bulkList"
    title="批量导入题目"
    :description="bulkList ? `文件包含 ${bulkList.length} 个题目。编辑确认仅支持单题，是否跳过编辑直接批量导入？` : ''"
    confirm-label="直接批量导入"
    :loading="uploading"
    @update:open="!$event && (bulkList = null)"
    @confirm="confirmBulkImport"
  />

  <ConfirmDialog
    :open="imageConfirmOpen"
    title="构建 vulinbox 镜像"
    description="将从官方 OSS 下载 vulinbox 二进制并构建靶场基础镜像（约 165MB，需要联网，耗时约 1-3 分钟）。构建期间请勿关闭页面。"
    confirm-label="开始构建"
    :loading="imageBuilding"
    @update:open="imageConfirmOpen = $event"
    @confirm="confirmBuildImage"
  />

  <ConfirmDialog
    :open="engineConfirmOpen"
    title="下载 yak 判题引擎"
    description="将从 yaklang 官方 OSS 下载 yak 引擎二进制（约 220MB，需要联网，视带宽约 1-5 分钟）。下载在后台进行，完成后自动装配，无需重启服务。"
    confirm-label="开始下载"
    @update:open="engineConfirmOpen = $event"
    @confirm="confirmDownloadEngine"
  />

  <ConfirmDialog
    :open="!!attachmentTarget"
    title="删除附件"
    :description="attachmentTarget ? `确定删除附件「${attachmentTarget}」？学员将无法再从题目页下载该文件。` : ''"
    confirm-label="删除"
    tone="danger"
    :loading="attachmentDeleting"
    @update:open="!$event && (attachmentTarget = '')"
    @confirm="confirmRemoveAttachment"
  />
</template>

<style scoped>
.head-count {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.tab-hint {
  margin-top: var(--space-3);
  margin-bottom: var(--space-4);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.challenge-filters {
  margin-top: var(--space-2);
}

.filter-field {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.filter-field__label {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.filter-field__control {
  position: relative;
  display: block;
}

.filter-field__icon {
  position: absolute;
  top: 50%;
  left: var(--space-3);
  width: 16px;
  height: 16px;
  color: var(--text-tertiary);
  transform: translateY(-50%);
  pointer-events: none;
}

.filter-field__input {
  width: 260px;
  padding-left: var(--space-8);
}

.filter-select {
  min-height: 36px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.spec-panel {
  margin-bottom: var(--space-4);
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.spec-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.spec-panel__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.spec-text,
.spec-note {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.spec-subtitle {
  margin-top: var(--space-4);
  margin-bottom: var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.spec-note {
  margin-top: var(--space-4);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.spec-table {
  width: 100%;
  margin-top: var(--space-3);
  border-collapse: collapse;
  font-size: var(--text-meta);
}

.spec-table th,
.spec-table td {
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}

.spec-table th {
  color: var(--text-tertiary);
  font-weight: 500;
  white-space: nowrap;
}

.spec-table td:first-child {
  color: var(--text-primary);
  font-weight: 500;
  white-space: nowrap;
}

.spec-table td:last-child {
  color: var(--text-secondary);
  line-height: var(--leading-normal);
}

.status-panel {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-bottom: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.status-panel__main {
  min-width: 0;
  flex: 1 1 320px;
}

.status-panel__title {
  margin-bottom: var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.status-panel__chips {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.status-chip {
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

.status-chip[data-tone='success'] {
  color: var(--status-success);
}

.status-chip[data-tone='warning'] {
  color: var(--status-warning);
}

.status-chip[data-tone='danger'] {
  color: var(--status-danger);
}

.status-panel__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

.image-tag {
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  overflow-wrap: anywhere;
}

.engine-progress {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  max-width: 480px;
  margin-top: var(--space-2);
}

.engine-progress .usage-track {
  flex: 1;
}

.usage-track {
  height: 6px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  overflow: hidden;
}

.usage-fill {
  display: block;
  height: 100%;
  border-radius: var(--radius-control);
  background: var(--action-primary);
}

.engine-progress-text {
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  white-space: nowrap;
}

.engine-error {
  margin-top: var(--space-2);
  color: var(--status-danger);
  font-size: var(--text-meta);
}

.challenge-title {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  overflow-wrap: anywhere;
}

.challenge-code {
  display: block;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  overflow-wrap: anywhere;
}

.challenge-category,
.challenge-score {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.challenge-flag-yes {
  color: var(--status-success);
  font-size: var(--text-meta);
}

.challenge-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.row-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.danger-action {
  color: var(--status-danger);
}

.form-errors {
  margin-bottom: var(--space-4);
}

.err-line {
  margin: var(--space-1) 0;
  font-size: var(--text-caption);
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: var(--space-3);
}

.lib-select {
  width: 100%;
}

.mono :deep(textarea) {
  font-family: var(--font-mono);
}

.env-image {
  flex: 1 1 16rem;
  margin-inline: var(--space-3);
}

.hints {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.form-span {
  grid-column: 1 / -1;
}

.question-options,
.fill-answers {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: 100%;
}

.question-option-row,
.fill-answer-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.question-option-key {
  width: 5rem;
  flex: none;
}

.question-option-text {
  flex: 1 1 14rem;
}

.question-option-pick {
  flex: none;
}

.match-switches {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.hint-row {
  display: flex;
  gap: var(--space-2);
  align-items: center;
  flex-wrap: wrap;
}

.hint-title {
  width: 9rem;
  flex: none;
}

.hint-content {
  flex: 1;
}

.hint-cost {
  flex: none;
}

.attach-toolbar {
  margin-bottom: var(--space-3);
}

.form-hint {
  line-height: var(--leading-normal);
  margin-top: var(--space-1);
}

.testdata-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.testdata-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.testdata-name {
  min-width: 6rem;
  font-family: var(--font-mono);
}

.testdata-score {
  width: 8rem;
}

@media (max-width: 720px) {
  .filter-field,
  .filter-field__input,
  .filter-select {
    width: 100%;
  }

  .filter-select {
    min-height: 44px;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }

  .status-panel__actions {
    width: 100%;
  }
}
</style>