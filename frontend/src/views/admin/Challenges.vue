<script setup lang="ts">
// 题目管理：challenge.json（固定格式）上传（单对象或数组，upsert 即生效）+
// 题目表格 + 删除。附格式规范折叠面板。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminChallengeApi, type TrainingItem } from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const items = ref<TrainingItem[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const uploading = ref(false)
const specVisible = ref(false)

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

// el-upload before-upload：读文件文本，尝试解析 JSON（单对象或数组）后上传。
async function onUpload(file: File) {
  uploading.value = true
  try {
    const text = await file.text()
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch {
      throw new Error('不是合法的 JSON 文件')
    }
    const list = Array.isArray(parsed) ? parsed : [parsed]
    const resp = await adminChallengeApi.upload(list)
    ElMessage.success(`已导入 ${resp.imported} 个题目：${resp.slugs.join(', ')}`)
    await load()
  } catch (e) {
    ElMessage.error(`上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
  return false // 阻止 el-upload 默认行为
}

async function remove(row: TrainingItem) {
  try {
    await ElMessageBox.confirm(`确定删除题目「${row.title}」？判定答案与提示将一并删除。`, '删除题目', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await adminChallengeApi.remove(row.slug)
    ElMessage.success(`已删除 ${row.slug}`)
    await load()
  } catch (e) {
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
  <p class="hint">题目通过固定格式 JSON（challenge.json）上传：单对象或对象数组，slug 重复 = 更新，上传即生效（无 draft / publish 流程）。</p>
  <div class="toolbar">
    <el-upload
      :show-file-list="false"
      accept=".json,application/json"
      :before-upload="onUpload"
      :disabled="uploading"
    >
      <el-button type="primary" :loading="uploading">上传题目 JSON</el-button>
    </el-upload>
    <el-button @click="specVisible = !specVisible">
      {{ specVisible ? '收起格式规范' : '查看 JSON 格式规范' }}
    </el-button>
  </div>

  <el-collapse-transition>
    <el-card v-show="specVisible" class="spec">
      <h3>challenge.json 固定格式</h3>
      <pre class="json">{
  "slug": "sqli-login-bypass",       // 全局唯一，重复上传 = 更新
  "title": "SQL 注入登录绕过",
  "category": "sql-injection",      // 训练场分组键
  "difficulty": "apprentice",        // apprentice / practitioner / expert
  "description_md": "# 背景\n任务目标……（Markdown）",
  "score": 100,
  "challenge": {
    "type": "static",                // static（明文）/ regex（正则匹配）
    "flag": "flag{answer}"
  },
  "env": {                           // 可选：容器环境，image/port 成对出现
    "image": "vulnapp:1.0",
    "port": 80
  },
  "hints": [                         // 可选：提示列表
    { "title": "提示一", "content": "看 UNION", "cost": 10 }
  ],
  "tutorial_slug": "sqli-basics"     // 可选：关联教程 Room slug
}</pre>
      <p class="dim">支持 JSON 数组批量导入；逐题校验，单题失败整体不入库。description_md 展示在题目详情页；容器环境由学生在题目详情页自行启动。</p>
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
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; display: flex; gap: 0.5rem; }
.spec h3 { margin-top: 0; }
.spec pre { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; font-size: 0.82rem; overflow: auto; }
.spec .json { line-height: 1.5; font-family: Consolas, monospace; }
.dim { color: var(--el-text-color-secondary); font-size: 0.85rem; }
</style>