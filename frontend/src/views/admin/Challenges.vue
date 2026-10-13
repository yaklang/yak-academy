<script setup lang="ts">
// 题目管理：CTFd 风格 zip 包（challenge.yml + 容器文件）优先；
// 兼容 challenge.json 批量导入。upsert 即生效。
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
    const resp = await adminChallengeApi.upload(list as never[])
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
  <p class="hint">
    推荐上传 CTFd 风格题目包 zip（必含 <code>challenge.yml</code>；有容器时还必含
    <code>Dockerfile</code> + <code>docker-compose.yml</code>）。也兼容 challenge.json 批量导入。
    样例见仓库 <code>examples/challenges/vulinbox-xss-reflected</code>。
  </p>
  <div class="toolbar">
    <el-upload
      :show-file-list="false"
      accept=".zip,application/zip"
      :before-upload="onUploadPackage"
      :disabled="uploading"
    >
      <el-button type="primary" :loading="uploading">上传题目包 zip</el-button>
    </el-upload>
    <el-upload
      :show-file-list="false"
      accept=".json,application/json"
      :before-upload="onUpload"
      :disabled="uploading"
    >
      <el-button :loading="uploading">上传 JSON（兼容）</el-button>
    </el-upload>
    <el-button @click="specVisible = !specVisible">
      {{ specVisible ? '收起格式规范' : '查看格式规范' }}
    </el-button>
  </div>

  <el-collapse-transition>
    <el-card v-show="specVisible" class="spec">
      <h3>challenge.yml 题目包（推荐）</h3>
      <pre class="json">challenge.yml          # 必填
Dockerfile             # 有 image 时必填
docker-compose.yml     # 有 image 时必填
src/…                  # 可选源码</pre>
      <p class="dim">接口：POST /admin/challenges/package（字段名 package）。<code>image: .</code> 落库为 yak-academy/&lt;slug&gt;:latest。</p>
      <h3>challenge.json（兼容）</h3>
      <pre class="json">{
  "slug": "sqli-login-bypass",
  "title": "SQL 注入登录绕过",
  "category": "sql-injection",
  "difficulty": "apprentice",
  "description_md": "# 背景\n任务目标……",
  "score": 100,
  "challenge": { "type": "static", "flag": "flag{answer}" },
  "env": { "image": "vulnapp:1.0", "port": 80 },
  "hints": [ { "title": "提示一", "content": "看 UNION", "cost": 10 } ],
  "tutorial_slug": "sqli-basics"
}</pre>
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