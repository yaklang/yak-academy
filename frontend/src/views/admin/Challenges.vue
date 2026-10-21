<script setup lang="ts">
// 题目管理：三个题库 tab（内置训练 / CTF / ACM）分开管理。
// 内置 tab 只读——目录是内置内容唯一事实源，更新只经「一键导入」整体进行
// （写接口对内置题一律 422，前端同步禁用编辑/删除/可见性/附件操作）。
// CTF / ACM tab 支持关键字搜索（slug/标题/分类前缀）与筛选（分类/难度/来源）。
// 主上传流程不变：选择 challenge.json → 前端校验 → 编辑弹窗确认 → 一键上传
// （打包即把表单数据规范化回 JSON 后 POST /admin/challenges）。
// 行内「编辑」拉取库内完整定义回填同一弹窗（GET /admin/challenges/{slug}），
// 附件管理也并入了该弹窗；操作列只保留 编辑 / 删除。
// zip 题目包通道保留给 image: . 场景（需 Dockerfile/compose，上传时构建）。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import {
  adminChallengeApi, DIFFICULTY_LABELS,
  type AdminChallengeQuery,
  type BuiltinImageStatus, type ChallengeAttachment, type ChallengeBank, type ChallengeDifficulty,
  type ChallengeFullJSON, type ChallengeHintInput, type ChallengeJSON, type ChallengeLink,
  type TrainingItem,
} from '../../api'
import { downloadSampleChallenge, parseChallengeJSON, validateChallenge } from '../../admin/challengeFormat'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const items = ref<TrainingItem[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const uploading = ref(false)
const specVisible = ref(false)

// ===== 题库 tab（内置只读；CTF/ACM 可管理）=====
type AdminTab = 'builtin' | 'ctf' | 'acm'
const activeTab = ref<AdminTab>('builtin')
// 内置 tab 按来源聚合（is_builtin=true，不限 bank——内置集现全为 tutorial，
// 但语义上「内置训练」= 随二进制分发的一套，不该被 bank 二次切分）。
const TAB_META: Record<AdminTab, { bank?: ChallengeBank; builtin: boolean; hint: string }> = {
  builtin: { builtin: true, hint: '内置训练集随平台分发，只读展示；更新 / 清理一律经「导入内置训练（vulinbox）」按目录整体进行' },
  ctf: { bank: 'ctf', builtin: false, hint: 'CTF 题目：flag 提交判定（静态 / 正则），可选容器环境' },
  acm: { bank: 'acm', builtin: false, hint: 'ACM 题目：算法编程题，challenge.json 归置 bank=acm 上传' },
}

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

// 当前 tab 的分类选项从列表页可见数据聚合（管理端题目量级小，前端聚合足够；
// 保持与列表过滤同一事实源，避免单独的 distinct 接口）。
const categoryOptions = computed(() => {
  const s = new Set<string>()
  for (const it of items.value) s.add(it.category)
  return [...s].sort()
})

// ===== challenge.json 导入 + 编辑弹窗 =====
const editVisible = ref(false)
const editSaving = ref(false)
const editSourceFile = ref('')
// 行内编辑来源：非空 = 正在编辑已入库题目（附件区随之显示）。
const editExistingSlug = ref('')
const editLoading = ref('')
// tutorial 题库为内置题集专属：行内编辑该类题时锁定题库选择，
// 提交省略 bank（后端缺省 = 保留原值），避免把内置题挤出教程题目 tab。
const bankLocked = ref(false)
// 表单模型：env 用 has_env 开关承载（env 段要么都填要么都不填）。
const form = reactive({
  slug: '',
  title: '',
  category: '',
  bank: 'ctf' as ChallengeBank,
  difficulty: 'apprentice' as ChallengeJSON['difficulty'],
  score: 100,
  flag_mode: 'shared' as Required<ChallengeJSON>['flag_mode'],
  type: 'static' as ChallengeJSON['challenge']['type'],
  flag: '',
  has_env: false,
  image: '',
  port: 80,
  description_md: '',
  tutorial_slug: '',
  hints: [] as ChallengeHintInput[],
  links: [] as ChallengeLink[],
})

// 表单 → 规范化题目 JSON（"自动打包"）。
// bankLocked（编辑内置 tutorial 题）时省略 bank：后端缺省 = 保留原值。
function buildPayload(): ChallengeJSON {
  const payload: ChallengeJSON = {
    slug: form.slug.trim(),
    title: form.title.trim(),
    category: form.category.trim(),
    difficulty: form.difficulty,
    score: form.score,
    flag_mode: form.flag_mode,
    challenge: { type: form.type, flag: form.flag.trim() },
  }
  if (!bankLocked.value) payload.bank = form.bank
  if (form.description_md.trim()) payload.description_md = form.description_md
  if (form.has_env && form.image.trim()) payload.env = { image: form.image.trim(), port: form.port }
  if (form.tutorial_slug.trim()) payload.tutorial_slug = form.tutorial_slug.trim()
  payload.hints = form.hints
    .filter((h) => h.title || h.content)
    .map((h) => ({ title: h.title.trim(), content: h.content.trim(), cost: h.cost ?? 0 }))
  const links = form.links.filter((l) => l.name || l.url)
  if (links.length) payload.links = links.map((l) => ({ name: l.name.trim(), url: l.url.trim() }))
  return payload
}

// 实时校验错误（弹窗内 alert 展示，保存按钮禁用）。
const formErrors = computed(() => {
  if (!editVisible.value) return []
  return validateChallenge(buildPayload())
})

function fillForm(chal: ChallengeJSON) {
  form.slug = chal.slug ?? ''
  form.title = chal.title ?? ''
  form.category = chal.category ?? ''
  // 教程题目 tab 专属内置题集：上传只允许 ctf/acm，其余（tutorial/空）归 ctf。
  // 行内编辑 tutorial 题时 bankLocked 置位，保留 tutorial 展示并在提交时省略。
  bankLocked.value = chal.bank === 'tutorial'
  form.bank = chal.bank === 'acm' ? 'acm' : chal.bank === 'tutorial' ? 'tutorial' : 'ctf'
  form.difficulty = chal.difficulty ?? 'apprentice'
  form.score = chal.score ?? 100
  form.flag_mode = chal.flag_mode ?? 'shared'
  form.type = chal.challenge?.type ?? 'static'
  form.flag = chal.challenge?.flag ?? ''
  form.has_env = Boolean(chal.env?.image)
  form.image = chal.env?.image ?? ''
  form.port = chal.env?.port ?? 80
  form.description_md = chal.description_md ?? ''
  form.tutorial_slug = chal.tutorial_slug ?? ''
  form.hints = (chal.hints ?? []).map((h) => ({ ...h }))
  form.links = (chal.links ?? []).map((l) => ({ ...l }))
}

function addHint() {
  form.hints.push({ title: '', content: '', cost: 10 })
}
function removeHint(i: number) {
  form.hints.splice(i, 1)
}
function addLink() {
  form.links.push({ name: '', url: '' })
}
function removeLink(i: number) {
  form.links.splice(i, 1)
}

// el-upload before-upload：读文件 → 解析 → 校验 → 单题进编辑弹窗，多题询问批量直传。
async function onImport(file: File) {
  let parsed
  try {
    const text = await file.text()
    parsed = parseChallengeJSON(text)
  } catch (e) {
    ElMessage.error(`读取文件失败：${(e as Error).message}`)
    return false
  }
  if (parsed.error) {
    ElMessage.error(parsed.error)
    return false
  }
  const { list } = parsed
  if (list.length > 1) {
    // 数组文件：编辑弹窗只支持单题，提供直接批量导入的出路。
    try {
      await ElMessageBox.confirm(
        `文件包含 ${list.length} 个题目。编辑确认仅支持单题，是否跳过编辑直接批量导入（逐题校验，失败整体不入库）？`,
        '批量导入',
        { type: 'info', confirmButtonText: '直接批量导入', cancelButtonText: '取消' },
      )
    } catch {
      return false
    }
    await doUpload(list)
    return false
  }
  const chal = list[0]
  if (chal.bank === 'tutorial') {
    // 文件导入不允许 tutorial：解锁题库选择并归 ctf（上传显式 tutorial 一律 422）。
    ElMessage.warning({ message: '教程题目 tab 专属内置题集：该文件声明的 bank=tutorial 不再接受上传，将按表单所选题库（ctf/acm）入库', duration: 5000 })
  }
  const errs = validateChallenge(chal)
  if (errs.length) {
    // 格式不合法也允许进弹窗修正（错误先展示，修正后才可上传）。
    ElMessage.warning({ message: `题目格式有 ${errs.length} 处问题，请在弹窗中修正后上传`, duration: 3500 })
  }
  fillForm(chal)
  if (bankLocked.value) {
    bankLocked.value = false
    form.bank = 'ctf'
  }
  editExistingSlug.value = ''
  editSourceFile.value = file.name
  editVisible.value = true
  return false // 阻止 el-upload 默认行为
}

// 行内「编辑」：拉取库内完整定义回填弹窗（含 flag/hints/env），
// 多 flag（题目包上传）时提醒保存只保留表单那一个；附件区随弹窗加载。
async function openRowEdit(row: TrainingItem) {
  editLoading.value = row.slug
  try {
    const full: ChallengeFullJSON = await adminChallengeApi.getJSON(row.slug)
    fillForm(full)
    if (full.flags.length > 1) {
      ElMessage.warning({ message: `该题有 ${full.flags.length} 个 flag（题目包上传），保存后只保留表单中的这一个`, duration: 6000 })
    }
    editSourceFile.value = ''
    editExistingSlug.value = row.slug
    editVisible.value = true
    await loadAttachments()
  } catch (e) {
    ElMessage.error(`读取题目定义失败：${(e as Error).message}`)
  } finally {
    editLoading.value = ''
  }
}

async function doUpload(list: ChallengeJSON[]) {
  uploading.value = true
  try {
    const resp = await adminChallengeApi.upload(list)
    ElMessage.success(`已导入 ${resp.imported} 个题目：${resp.slugs.join(', ')}`)
    await load()
  } catch (e) {
    ElMessage.error(`上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
}

// 弹窗「确认上传」：实时校验通过才可达。
async function saveFromEdit() {
  if (formErrors.value.length) return
  editSaving.value = true
  try {
    const payload = buildPayload()
    const resp = await adminChallengeApi.upload([payload])
    ElMessage.success(`已导入题目：${resp.slugs.join(', ')}`)
    editVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(`上传失败：${(e as Error).message}`)
  } finally {
    editSaving.value = false
  }
}

async function onUploadPackage(file: File) {
  uploading.value = true
  try {
    const resp = await adminChallengeApi.uploadPackage(file)
    ElMessage.success(`已导入题目包：${resp.slugs.join(', ')}`)
    await load()
  } catch (e) {
    ElMessage.error(`题目包上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
  return false
}

// ===== 内置训练（vulinbox）一键导入 =====
// 幂等：已导入的同名题目会被更新；镜像在 Docker 可用时构建（ noop 下跳过并提示）。
const builtinImporting = ref(false)

async function onImportBuiltin() {
  try {
    await ElMessageBox.confirm(
      '将导入随平台分发的内置训练集（基于 yaklang vulinbox 靶场引擎）：配套教程（一个教程对应一道题目）+ 11 道题目。已导入的同名内容会被更新；配置了 Docker 时会同时构建靶场基础镜像（需联网下载约 165MB 的官方二进制）。是否继续？',
      '导入内置训练',
      { type: 'info', confirmButtonText: '导入/更新', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  builtinImporting.value = true
  try {
    const resp = await adminChallengeApi.importBuiltin()
    let summary = `内置训练 v${resp.version}：新增 ${resp.created} 题、更新 ${resp.updated} 题、教程 ${resp.rooms} 篇`
    if (resp.removed > 0) {
      summary += `、清理目录外残留 ${resp.removed} 题`
    }
    if (resp.image_built) {
      ElMessage.success(`${summary}；靶场镜像已构建`)
    } else {
      ElMessage.warning({ message: `${summary}；未配置 Docker，镜像未构建（启用 Docker 后重新导入即可）`, duration: 6000 })
    }
    await load()
  } catch (e) {
    ElMessage.error(`内置训练导入失败：${(e as Error).message}`)
  } finally {
    builtinImporting.value = false
  }
}

// ===== vulinbox 靶场镜像状态（OSS 构建） =====
// 状态卡数据源 GET /admin/challenges/builtin/image；构建走 docker build
// （内从官方 OSS 下载二进制，约 1-3 分钟），noop 驱动不可构建。
const imageStatus = ref<BuiltinImageStatus | null>(null)
const imageBuilding = ref(false)

async function loadImageStatus() {
  try {
    imageStatus.value = await adminChallengeApi.builtinImageStatus()
  } catch {
    imageStatus.value = null // 状态查询失败不阻塞页面
  }
}

async function onBuildBuiltinImage() {
  try {
    await ElMessageBox.confirm(
      '将从官方 OSS 下载 vulinbox 二进制并构建靶场基础镜像（约 165MB，需联网，耗时约 1-3 分钟）。构建期间请勿关闭页面。是否继续？',
      '构建 vulinbox 镜像',
      { type: 'info', confirmButtonText: '开始构建', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  imageBuilding.value = true
  try {
    const resp = await adminChallengeApi.buildBuiltinImage()
    ElMessage.success(`镜像已构建：${resp.image_tag}`)
    await loadImageStatus()
  } catch (e) {
    ElMessage.error({ message: `镜像构建失败：${(e as Error).message}`, duration: 8000 })
  } finally {
    imageBuilding.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const resp = await adminChallengeApi.list(currentQuery())
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function remove(row: TrainingItem) {
  try {
    await ElMessageBox.confirm(
      `确定删除题目「${row.title}」？该题的运行中容器将被销毁，判定答案与提示一并删除。` +
        '已有用户通关的题目无法删除（提交历史不可删）。',
      '删除题目',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
      },
    )
  } catch {
    return
  }
  try {
    await adminChallengeApi.remove(row.slug)
    ElMessage.success(`已删除 ${row.slug}`)
    await load()
  } catch (e) {
    // 409：题目存在通关记录，事件历史 append-only 不可删（契约语义直接透出）。
    ElMessage.error(`删除失败：${(e as Error).message}`)
  }
}

// ===== 可见性开关 =====
// 状态语义：隐藏的题目学生端整体 404。定时发布功能已随操作列精简移除
//（PATCH visibility 的 release_at 能力仍在契约中保留）。
const visSaving = ref('')

async function toggleVisible(row: TrainingItem) {
  visSaving.value = row.slug
  try {
    const updated = await adminChallengeApi.updateVisibility(row.slug, { is_visible: !row.is_visible })
    row.is_visible = updated.is_visible
    ElMessage.success(updated.is_visible ? `「${row.title}」已对学生可见` : `「${row.title}」已隐藏`)
  } catch (e) {
    ElMessage.error(`可见性更新失败：${(e as Error).message}`)
  } finally {
    visSaving.value = ''
  }
}

// ===== 附件管理（并入编辑弹窗：pcap/exe 等配套文件，学生端凭 JWT 下载）=====
// 挂在 editExistingSlug（行内编辑已入库题目）上；文件导入新建流程不显示。
const attachments = ref<ChallengeAttachment[]>([])
const attachLoading = ref(false)
const attachUploading = ref(false)

async function loadAttachments() {
  if (!editExistingSlug.value) return
  attachLoading.value = true
  try {
    attachments.value = await adminChallengeApi.listAttachments(editExistingSlug.value)
  } catch (e) {
    ElMessage.error(`附件列表加载失败：${(e as Error).message}`)
  } finally {
    attachLoading.value = false
  }
}

// 同名文件覆盖更新（服务端按 slug+filename upsert）。
async function onUploadAttachment(file: File) {
  attachUploading.value = true
  try {
    await adminChallengeApi.uploadAttachment(editExistingSlug.value, file)
    ElMessage.success(`已上传附件：${file.name}`)
    await loadAttachments()
  } catch (e) {
    ElMessage.error(`附件上传失败：${(e as Error).message}`)
  } finally {
    attachUploading.value = false
  }
  return false // 阻止 el-upload 默认提交
}

async function removeAttachment(filename: string) {
  try {
    await ElMessageBox.confirm(`确定删除附件「${filename}」？`, '删除附件', { type: 'warning' })
  } catch {
    return
  }
  try {
    await adminChallengeApi.deleteAttachment(editExistingSlug.value, filename)
    await loadAttachments()
  } catch (e) {
    ElMessage.error(`附件删除失败：${(e as Error).message}`)
  }
}

function attachSize(n: number) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(1)} KB`
  return `${n} B`
}

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

// 内置 tab 只读：编辑/删除/可见性/附件全部收口（后端同语义 422 兜底）。
const readonlyTab = computed(() => TAB_META[activeTab.value].builtin)

onMounted(() => {
  load()
  loadImageStatus()
})
</script>

<template>
  <header class="page-head">
    <h2 class="page-title">题目<span class="accent">管理</span></h2>
    <span class="head-count" v-if="total">共 {{ total }} 题</span>
  </header>

  <el-tabs v-model="activeTab" class="bank-tabs">
    <el-tab-pane name="builtin">
      <template #label>
        <span class="tab-label">内置训练<span class="tab-sub">只读</span></span>
      </template>
    </el-tab-pane>
    <el-tab-pane name="ctf">
      <template #label><span class="tab-label">CTF 题目</span></template>
    </el-tab-pane>
    <el-tab-pane name="acm">
      <template #label><span class="tab-label">ACM 题目</span></template>
    </el-tab-pane>
  </el-tabs>

  <p class="hint">{{ TAB_META[activeTab].hint }}</p>

  <!-- 内置 tab 只读：收起全部上传入口，仅保留一键导入与镜像状态 -->
  <template v-if="readonlyTab">
    <div class="toolbar">
      <el-button
        :loading="builtinImporting"
        title="导入随平台分发的 vulinbox 内置训练题集（重复导入 = 更新；目录外残留自动清理）"
        @click="onImportBuiltin"
      >
        导入内置训练（vulinbox）
      </el-button>
      <el-button text @click="load">刷新</el-button>
    </div>
  </template>
  <template v-else>
    <div class="toolbar">
      <el-upload
        :show-file-list="false"
        accept=".json,application/json"
        :before-upload="onImport"
        :disabled="uploading"
      >
        <el-button type="primary" :loading="uploading">选择 challenge.json</el-button>
      </el-upload>
      <el-button @click="downloadSampleChallenge">下载样例 JSON</el-button>
      <el-upload
        :show-file-list="false"
        accept=".zip,application/zip"
        :before-upload="onUploadPackage"
        :disabled="uploading"
      >
        <el-button :loading="uploading" title="image: . 的容器题需经 zip 包上传（上传时构建镜像）">
          上传题目包 zip
        </el-button>
      </el-upload>
      <el-button
        @click="specVisible = !specVisible"
      >
        {{ specVisible ? '收起格式规范' : '查看格式规范' }}
      </el-button>
    </div>
  </template>

  <!-- 搜索 / 筛选：全部 tab 可用；来源筛选仅非内置 tab（内置 tab 恒为内置来源） -->
  <div class="filter-bar">
    <el-input
      v-model="keyword"
      class="filter-search"
      placeholder="搜索 slug / 标题 / 分类（前缀匹配）"
      clearable
      :prefix-icon="Search"
      @input="onKeywordInput"
      @clear="resetAndLoad"
    />
    <el-select v-model="categoryFilter" class="filter" placeholder="分类：全部" clearable>
      <el-option label="分类：全部" value="" />
      <el-option v-for="c in categoryOptions" :key="c" :label="c" :value="c" />
    </el-select>
    <el-select v-model="difficultyFilter" class="filter" placeholder="难度：全部" clearable>
      <el-option label="难度：全部" value="" />
      <el-option v-for="(label, v) in DIFFICULTY_LABELS" :key="v" :label="`${v}（${label}）`" :value="v" />
    </el-select>
    <el-select v-if="!readonlyTab" v-model="builtinFilter" class="filter" placeholder="来源：全部" clearable>
      <el-option label="来源：全部" value="" />
      <el-option label="来源：内置题集" value="yes" />
      <el-option label="来源：管理员上传" value="no" />
    </el-select>
    <span v-if="keyword || categoryFilter || difficultyFilter || builtinFilter" class="filter-clear" @click="clearFilters">
      清除筛选
    </span>
  </div>

  <!-- vulinbox 靶场镜像状态：学生端启动实验环境的前提（驱动 + 本地镜像 + 从 OSS 构建） -->
  <el-card v-if="imageStatus" class="image-card" shadow="never">
    <div class="image-status">
      <span class="image-label">vulinbox 靶场镜像</span>
      <el-tag :type="imageStatus.driver === 'docker' ? 'success' : 'info'" size="small">
        驱动：{{ imageStatus.driver === 'docker' ? 'Docker' : '未启用（noop，环境启动 501）' }}
      </el-tag>
      <el-tag
        :type="imageStatus.image_present ? 'success' : imageStatus.can_build ? 'warning' : 'danger'"
        size="small"
      >
        {{ imageStatus.image_present ? '镜像已就绪' : imageStatus.can_build ? '镜像缺失' : '镜像未构建（需 Docker）' }}
      </el-tag>
      <code class="image-tag">{{ imageStatus.image_tag }}</code>
      <el-button
        size="small"
        type="primary"
        :loading="imageBuilding"
        :disabled="!imageStatus.can_build"
        :title="imageStatus.can_build
          ? 'docker build 内从官方 OSS 下载二进制（约 165MB，1-3 分钟）'
          : '服务端未装配 Docker 驱动（ACADEMY_ENV_DRIVER），无法构建镜像'"
        @click="onBuildBuiltinImage"
      >
        从 OSS 构建镜像
      </el-button>
      <el-button size="small" text @click="loadImageStatus">刷新</el-button>
    </div>
  </el-card>

  <el-collapse-transition>
    <el-card v-show="specVisible" class="spec">
      <h3>challenge.json 字段</h3>
      <pre class="json">{
  "slug": "sqli-login-bypass",        // 必填 [a-z0-9-]+
  "title": "SQL 注入登录绕过",          // 必填
  "category": "sql-injection",         // 必填 [a-z0-9-]+
  "bank": "ctf",                      // 题库 tab：ctf（默认）| acm；tutorial 为内置题集专属，上传传即 422
  "difficulty": "apprentice",          // apprentice | practitioner | expert
  "description_md": "# 背景…",         // Markdown 描述（可选）
  "score": 100,
  "flag_mode": "shared",               // shared | instance（instance 需含 {token} 模板 + env）
  "challenge": { "type": "static", "flag": "flag{answer}" },
  "env": { "image": "vulnapp:1.0", "port": 80 },   // 可选；容器环境
  "hints": [ { "title": "提示一", "content": "看 UNION", "cost": 10 } ],
  "links": [ { "name": "Wireshark", "url": "https://…" } ],  // 可选外链
  "tutorial_slug": "sqli-basics"       // 可选关联教程
}</pre>
      <p class="dim">附件（pcap/exe 等文件）不走 JSON：经行内「编辑」弹窗的附件区上传，学生端凭登录态下载。</p>
      <p class="dim">上传接口：POST /admin/challenges（单对象或数组，逐题校验、整体拒绝）。</p>
      <h3>题目包 zip（image: . 容器题）</h3>
      <pre class="json">challenge.yml + Dockerfile + docker-compose.yml + src/…</pre>
      <p class="dim">接口：POST /admin/challenges/package。<code>image: .</code> 落库为 yak-academy/&lt;slug&gt;:latest。</p>
    </el-card>
  </el-collapse-transition>

  <el-table :data="items" v-loading="loading" class="chal-table">
    <el-table-column prop="slug" label="Slug" width="180" show-overflow-tooltip />
    <el-table-column prop="title" label="标题" min-width="160" show-overflow-tooltip />
    <el-table-column prop="category" label="分类" width="140" show-overflow-tooltip />
    <el-table-column label="难度" width="110">
      <template #default="{ row }">
        <el-tag :type="difficultyType(row.difficulty)" size="small">{{ row.difficulty }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="score" label="分值" width="80" />
    <el-table-column label="容器" width="70">
      <template #default="{ row }">
        <el-tag v-if="row.has_env" type="primary" size="small">有</el-tag>
        <span v-else>—</span>
      </template>
    </el-table-column>
    <el-table-column label="关联教程" min-width="140" show-overflow-tooltip>
      <template #default="{ row }">
        <span v-if="row.tutorial_slug">{{ row.tutorial_title || row.tutorial_slug }}</span>
        <span v-else>—</span>
      </template>
    </el-table-column>
    <template v-if="!readonlyTab">
      <el-table-column label="可见性" width="90">
        <template #default="{ row }">
          <el-switch
            :model-value="row.is_visible !== false"
            :loading="visSaving === row.slug"
            inline-prompt
            active-text="可见"
            inactive-text="隐藏"
            @change="toggleVisible(row)"
          />
        </template>
      </el-table-column>
      <el-table-column v-if="auth.user?.global_role === 'admin'" label="操作" width="140">
        <template #default="{ row }">
          <el-button size="small" :loading="editLoading === row.slug" @click="openRowEdit(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
      <el-table-column v-else label="来源" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.is_builtin" size="small" type="info">内置</el-tag>
        </template>
      </el-table-column>
    </template>
  </el-table>
  <el-pagination
    v-if="total > 20"
    layout="prev, pager, next"
    :total="total"
    :page-size="20"
    v-model:current-page="page"
    @current-change="load"
  />

  <!-- 题目编辑弹窗：行内「编辑」回填库内定义，或选择 challenge.json 后确认上传 -->
  <el-dialog
    v-model="editVisible"
    :title="editExistingSlug ? `编辑题目：${editExistingSlug}` : `编辑题目${editSourceFile ? `（${editSourceFile}）` : ''}`"
    width="680px"
    top="4vh"
  >
    <el-alert v-if="formErrors.length" type="error" :closable="false" class="form-errors">
      <p v-for="err in formErrors" :key="err" class="err-line">{{ err }}</p>
    </el-alert>
    <el-form label-width="110px">
      <div class="form-grid">
        <el-form-item label="slug" required>
          <el-input v-model="form.slug" placeholder="sqli-login-bypass" />
        </el-form-item>
        <el-form-item label="标题" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item label="分类" required>
          <el-input v-model="form.category" placeholder="sql-injection" />
        </el-form-item>
        <el-form-item label="题库">
          <el-select v-model="form.bank" :disabled="bankLocked">
            <el-option v-if="bankLocked" value="tutorial" label="教程题目（内置，不可改）" />
            <el-option value="ctf" label="CTF 题目（ctf，默认）" />
            <el-option value="acm" label="ACM 题目（acm）" />
          </el-select>
          <div class="dim form-hint">教程题目 tab 为内置题集专属，经「导入内置训练」写入</div>
        </el-form-item>
        <el-form-item label="难度" required>
          <el-select v-model="form.difficulty">
            <el-option value="apprentice" label="apprentice（入门）" />
            <el-option value="practitioner" label="practitioner（进阶）" />
            <el-option value="expert" label="expert（专家）" />
          </el-select>
        </el-form-item>
        <el-form-item label="分值" required>
          <el-input-number v-model="form.score" :min="0" />
        </el-form-item>
        <el-form-item label="关联教程">
          <el-input v-model="form.tutorial_slug" placeholder="可选，教程 Room slug" />
        </el-form-item>
        <el-form-item label="判定类型" required>
          <el-select v-model="form.type">
            <el-option value="static" label="static（明文 flag）" />
            <el-option value="regex" label="regex（正则匹配）" />
          </el-select>
        </el-form-item>
        <el-form-item label="flag" required>
          <el-input v-model="form.flag" :placeholder="form.flag_mode === 'instance' ? 'flag{lab-{token}}' : 'flag{answer}'" />
        </el-form-item>
        <el-form-item label="flag 作用域">
          <el-select v-model="form.flag_mode">
            <el-option value="shared" label="shared（题目级共享）" />
            <el-option value="instance" label="instance（每实例唯一）" />
          </el-select>
        </el-form-item>
      </div>
      <el-form-item label="容器环境">
        <el-switch v-model="form.has_env" />
        <template v-if="form.has_env">
          <el-input v-model="form.image" placeholder="镜像（如 nginx:alpine；image: . 需走 zip 包通道）" class="env-image" />
          <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" />
        </template>
      </el-form-item>
      <el-form-item label="描述 Markdown">
        <el-input v-model="form.description_md" type="textarea" :rows="6" class="mono" />
      </el-form-item>
      <el-form-item label="提示">
        <div class="hints">
          <div v-for="(h, i) in form.hints" :key="i" class="hint-row">
            <el-input v-model="h.title" placeholder="标题" class="hint-title" />
            <el-input v-model="h.content" placeholder="内容" class="hint-content" />
            <el-input-number v-model="h.cost" :min="0" controls-position="right" class="hint-cost" />
            <el-button type="danger" link @click="removeHint(i)">删除</el-button>
          </div>
          <el-button size="small" @click="addHint">添加提示</el-button>
        </div>
      </el-form-item>
      <el-form-item label="外链">
        <div class="hints">
          <div v-for="(l, i) in form.links" :key="i" class="hint-row">
            <el-input v-model="l.name" placeholder="名称（如 Wireshark 下载）" class="hint-title" />
            <el-input v-model="l.url" placeholder="https://…" class="hint-content" />
            <el-button type="danger" link @click="removeLink(i)">删除</el-button>
          </div>
          <el-button size="small" @click="addLink">添加外链</el-button>
        </div>
      </el-form-item>
      <!-- 附件区：仅行内编辑已入库题目时显示（文件导入新建流程题目尚未落库） -->
      <template v-if="editExistingSlug">
        <el-divider content-position="left">附件（不经 JSON，学生端凭登录态下载）</el-divider>
        <div class="attach-toolbar">
          <el-upload
            :show-file-list="false"
            :before-upload="onUploadAttachment"
            :disabled="attachUploading"
          >
            <el-button type="primary" size="small" :loading="attachUploading">上传附件（≤50MB，同名覆盖）</el-button>
          </el-upload>
        </div>
        <el-table :data="attachments" v-loading="attachLoading" size="small" empty-text="暂无附件">
          <el-table-column prop="filename" label="文件名" min-width="200" show-overflow-tooltip />
          <el-table-column label="大小" width="100">
            <template #default="{ row }">{{ attachSize(row.size) }}</template>
          </el-table-column>
          <el-table-column label="上传时间" width="150">
            <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
          </el-table-column>
          <el-table-column label="操作" width="70">
            <template #default="{ row }">
              <el-button size="small" type="danger" link @click="removeAttachment(row.filename)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-form>
    <template #footer>
      <el-button @click="editVisible = false">取消</el-button>
      <el-button type="primary" :loading="editSaving" :disabled="formErrors.length > 0" @click="saveFromEdit">
        确认上传
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
/* 页头 */
.page-head { display: flex; align-items: baseline; gap: 0.8rem; margin: 0 0 0.6rem; }
.page-title { font-size: 1.35rem; font-weight: 700; margin: 0; }
.page-title .accent { color: var(--el-color-primary); margin-left: 2px; }
.head-count { color: var(--el-text-color-secondary); font-size: 0.88rem; }

/* 题库 tab */
.bank-tabs :deep(.el-tabs__header) { margin-bottom: 0.4rem; }
.tab-label { font-size: 0.95rem; }
.tab-sub {
  font-size: 0.72rem; color: var(--el-color-warning); border: 1px solid var(--el-color-warning-light-5);
  padding: 0 5px; margin-left: 6px; border-radius: 3px; vertical-align: 1px;
}
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin: 0 0 1rem; }

/* 工具栏与筛选 */
.toolbar { margin-bottom: 0.9rem; display: flex; gap: 0.5rem; align-items: flex-start; }
.filter-bar { display: flex; gap: 0.6rem; align-items: center; flex-wrap: wrap; margin-bottom: 0.9rem; }
.filter-search { width: 300px; }
.filter { width: 170px; }
.filter-clear {
  color: var(--el-color-primary); font-size: 0.85rem; cursor: pointer; user-select: none;
}
.filter-clear:hover { opacity: 0.8; }

/* 镜像状态卡 / 规范折叠卡 / 表格 */
.image-card { margin-bottom: 1rem; }
.image-card :deep(.el-card__body) { padding: 0.6rem 1rem; }
.image-status { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
.image-label { font-weight: 600; }
.image-tag { color: var(--el-text-color-secondary); font-size: 0.82rem; }
.spec { margin-bottom: 1rem; }
.spec h3 { margin-top: 0; }
.spec pre { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; font-size: 0.82rem; overflow: auto; }
.spec .json { line-height: 1.5; font-family: Consolas, monospace; }
.chal-table { margin-bottom: 0.8rem; }
.dim { color: var(--el-text-color-secondary); font-size: 0.85rem; }

/* 编辑弹窗 */
.form-errors { margin-bottom: 1rem; }
.err-line { margin: 0.15rem 0; font-size: 0.85rem; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; column-gap: 0.8rem; }
.mono :deep(textarea) { font-family: Consolas, monospace; }
.env-image { width: 320px; margin-left: 0.8rem; margin-right: 0.5rem; }
.hints { width: 100%; display: flex; flex-direction: column; gap: 0.5rem; }
.hint-row { display: flex; gap: 0.5rem; align-items: center; }
.hint-title { width: 140px; flex: none; }
.hint-content { flex: 1; }
.hint-cost { flex: none; }
.attach-toolbar { margin-bottom: 0.8rem; }
.form-hint { line-height: 1.4; margin-top: 2px; }
</style>
