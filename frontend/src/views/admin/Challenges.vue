<script setup lang="ts">
// 题目管理：主流程为「选择 challenge.json → 前端校验 → 编辑弹窗确认 → 一键上传」
// （打包即把表单数据规范化回 JSON 后 POST /admin/challenges）。
// zip 题目包通道保留给 image: . 场景（需 Dockerfile/compose，上传时构建）。
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminChallengeApi, type ChallengeHintInput, type ChallengeJSON, type TrainingItem } from '../../api'
import { downloadSampleChallenge, parseChallengeJSON, validateChallenge } from '../../admin/challengeFormat'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const items = ref<TrainingItem[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const uploading = ref(false)
const specVisible = ref(false)

// ===== challenge.json 导入 + 编辑弹窗 =====
const editVisible = ref(false)
const editSaving = ref(false)
const editSourceFile = ref('')
// 表单模型：env 用 has_env 开关承载（env 段要么都填要么都不填）。
const form = reactive({
  slug: '',
  title: '',
  category: '',
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
})

// 表单 → 规范化题目 JSON（"自动打包"）。
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
  if (form.description_md.trim()) payload.description_md = form.description_md
  if (form.has_env && form.image.trim()) payload.env = { image: form.image.trim(), port: form.port }
  if (form.tutorial_slug.trim()) payload.tutorial_slug = form.tutorial_slug.trim()
  payload.hints = form.hints
    .filter((h) => h.title || h.content)
    .map((h) => ({ title: h.title.trim(), content: h.content.trim(), cost: h.cost ?? 0 }))
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
}

function addHint() {
  form.hints.push({ title: '', content: '', cost: 10 })
}
function removeHint(i: number) {
  form.hints.splice(i, 1)
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
  const errs = validateChallenge(chal)
  if (errs.length) {
    // 格式不合法也允许进弹窗修正（错误先展示，修正后才可上传）。
    ElMessage.warning({ message: `题目格式有 ${errs.length} 处问题，请在弹窗中修正后上传`, duration: 3500 })
  }
  fillForm(chal)
  editSourceFile.value = file.name
  editVisible.value = true
  return false // 阻止 el-upload 默认行为
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

async function load() {
  loading.value = true
  try {
    const resp = await adminChallengeApi.list(page.value)
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

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

onMounted(load)
</script>

<template>
  <h2>题目管理</h2>
  <p class="hint">
    选择 challenge.json 文件：自动校验格式并弹出编辑确认，点「确认上传」自动打包导入。
    完整样例见仓库 <code>examples/challenges/sqli-login-bypass</code>。
  </p>
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
    <el-button @click="specVisible = !specVisible">
      {{ specVisible ? '收起格式规范' : '查看格式规范' }}
    </el-button>
  </div>

  <el-collapse-transition>
    <el-card v-show="specVisible" class="spec">
      <h3>challenge.json 字段</h3>
      <pre class="json">{
  "slug": "sqli-login-bypass",        // 必填 [a-z0-9-]+
  "title": "SQL 注入登录绕过",          // 必填
  "category": "sql-injection",         // 必填 [a-z0-9-]+
  "difficulty": "apprentice",          // apprentice | practitioner | expert
  "description_md": "# 背景…",         // Markdown 描述（可选）
  "score": 100,
  "flag_mode": "shared",               // shared | instance（instance 需含 {token} 模板 + env）
  "challenge": { "type": "static", "flag": "flag{answer}" },
  "env": { "image": "vulnapp:1.0", "port": 80 },   // 可选；容器环境
  "hints": [ { "title": "提示一", "content": "看 UNION", "cost": 10 } ],
  "tutorial_slug": "sqli-basics"       // 可选关联教程
}</pre>
      <p class="dim">上传接口：POST /admin/challenges（单对象或数组，逐题校验、整体拒绝）。</p>
      <h3>题目包 zip（image: . 容器题）</h3>
      <pre class="json">challenge.yml + Dockerfile + docker-compose.yml + src/…</pre>
      <p class="dim">接口：POST /admin/challenges/package。<code>image: .</code> 落库为 yak-academy/&lt;slug&gt;:latest。</p>
    </el-card>
  </el-collapse-transition>

  <el-table :data="items" v-loading="loading">
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
    <el-table-column v-if="auth.user?.global_role === 'admin'" label="操作" width="90">
      <template #default="{ row }">
        <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>
  <el-pagination
    v-if="total > 20"
    layout="prev, pager, next"
    :total="total"
    :page-size="20"
    v-model:current-page="page"
    @current-change="load"
  />

  <!-- 题目编辑弹窗：选择 challenge.json 后展示，可修正再上传 -->
  <el-dialog v-model="editVisible" :title="`编辑题目${editSourceFile ? `（${editSourceFile}）` : ''}`" width="680px" top="4vh">
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
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; display: flex; gap: 0.5rem; align-items: flex-start; }
.spec h3 { margin-top: 0; }
.spec pre { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; font-size: 0.82rem; overflow: auto; }
.spec .json { line-height: 1.5; font-family: Consolas, monospace; }
.dim { color: var(--el-text-color-secondary); font-size: 0.85rem; }
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
</style>
