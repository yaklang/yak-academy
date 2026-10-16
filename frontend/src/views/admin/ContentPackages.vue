<script setup lang="ts">
// 教程管理：教程内容包（zip）上传与版本管理（draft → publish / 归档 / 回滚）。
// 包内只有教程（文字 + 视频）；题目是独立资源，经"题目管理"用 challenge.json 上传。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { contentApi, type ContentPackage, type ContentVersion } from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const packages = ref<ContentPackage[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const uploading = ref(false)

// 版本弹窗（回滚入口）。
const detailVisible = ref(false)
const detailPkg = ref<ContentPackage | null>(null)
const versions = ref<ContentVersion[]>([])
const versionsLoading = ref(false)
const rollbackLoading = ref(0)
const specVisible = ref(false)

async function load() {
  loading.value = true
  try {
    const resp = await contentApi.list(page.value)
    packages.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function onUpload(file: File) {
  uploading.value = true
  try {
    await contentApi.upload(file)
    ElMessage.success('上传成功，进入 draft 状态')
    await load()
  } catch (e) {
    ElMessage.error(`上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
  return false // 阻止 el-upload 默认行为
}

async function publish(pkg: ContentPackage) {
  try {
    await contentApi.publish(pkg.id)
    ElMessage.success(`已发布 ${pkg.name}@${pkg.latest_version}`)
    await load()
  } catch (e) {
    ElMessage.error(`发布失败：${(e as Error).message}`)
  }
}

async function archive(pkg: ContentPackage) {
  try {
    await contentApi.archive(pkg.id)
    ElMessage.success(`已归档 ${pkg.name}（内容保持可见，包冻结）`)
    await load()
  } catch (e) {
    ElMessage.error(`归档失败：${(e as Error).message}`)
  }
}

// 删除不可恢复：包 + 历代版本写入的教程内容一并移除，
// 题目本体与学生完成进度/事件历史保留（进度行随被删教程清理）。
async function removePackage(pkg: ContentPackage) {
  try {
    await ElMessageBox.confirm(
      `将删除内容包「${pkg.name}」及其历代版本写入的全部教程（专题 + 教程页），学生的完成进度随之清空，事件历史按审计要求保留。此操作不可恢复，确定删除？`,
      '删除内容包',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return // 用户取消
  }
  try {
    const res = await contentApi.remove(pkg.id)
    ElMessage.success(`已删除 ${pkg.name}（移除 ${res.topics} 个专题 / ${res.rooms} 篇教程）`)
    await load()
  } catch (e) {
    ElMessage.error(`删除失败：${(e as Error).message}`)
  }
}

async function openDetail(pkg: ContentPackage) {
  detailPkg.value = pkg
  detailVisible.value = true
  versionsLoading.value = true
  try {
    versions.value = await contentApi.versions(pkg.id)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    versionsLoading.value = false
  }
}

async function rollback(version: ContentVersion) {
  if (!detailPkg.value) return
  rollbackLoading.value = version.id
  try {
    const pkg = await contentApi.rollback(detailPkg.value.id, version.id)
    ElMessage.success(`已回滚到 ${pkg.latest_version}`)
    detailPkg.value = pkg
    versions.value = await contentApi.versions(pkg.id)
    await load()
  } catch (e) {
    ElMessage.error(`回滚失败：${(e as Error).message}`)
  } finally {
    rollbackLoading.value = 0
  }
}

function statusType(s: ContentPackage['status']) {
  return s === 'published' ? 'success' : s === 'draft' ? 'info' : 'warning'
}

onMounted(load)
</script>

<template>
  <h2>教程管理</h2>
  <p class="hint">教程以内容包（zip）上传：文字教程 + 配套视频，走版本化管线（draft → 发布 → 归档/回滚）。题目是独立资源，请到"题目管理"用 JSON 上传。</p>
  <div class="toolbar">
    <el-upload
      :show-file-list="false"
      accept=".zip"
      :before-upload="onUpload"
      :disabled="uploading"
    >
      <el-button type="primary" :loading="uploading">上传内容包（zip）</el-button>
    </el-upload>
    <el-button @click="specVisible = !specVisible">
      {{ specVisible ? '收起格式规范' : '查看标准格式规范' }}
    </el-button>
  </div>

  <el-collapse-transition>
    <el-card v-show="specVisible" class="spec">
      <h3>教程内容包格式</h3>
      <p>zip 根目录（或唯一一层包裹目录）下：</p>
      <pre class="tree">package.zip
├── manifest.yaml        # 包元数据（必填）
└── topics/
    └── &lt;topic-slug&gt;/
        ├── topic.md     # topic 首页（front matter 必填）
        └── rooms/
            └── &lt;room-slug&gt;/
                └── lesson.md   # 教程正文（front matter 必填，可含 video）</pre>

      <h4>manifest.yaml（必填）</h4>
      <pre class="yaml">name: my-course        # 包名（同名再传 = 升级出新版本）
version: 1.0.0         # 语义化版本
language: zh
topics:
  - web-security       # topic slug 列表</pre>

      <h4>lesson.md front matter（必填）</h4>
      <pre class="yaml">---
title: SQL 注入基础
difficulty: apprentice   # apprentice / practitioner / expert 三级
video: https://cdn.example.com/sqli.mp4   # 可选：mp4/webm 直链或可 embed 的页面
---
正文为 Markdown……</pre>
      <p class="dim">上传后进入 draft，由 admin 发布才对学生可见；同一包可多次上传新版本，旧版本支持归档 / 回滚。题目（flag、容器环境、提示）不经教程包——用"题目管理"的 challenge.json 定义。</p>
    </el-card>
  </el-collapse-transition>
  <el-table :data="packages" v-loading="loading">
    <el-table-column prop="id" label="ID" width="60" />
    <el-table-column prop="name" label="包名" />
    <el-table-column prop="latest_version" label="版本" width="100" />
    <el-table-column label="状态" width="110">
      <template #default="{ row }">
        <el-tag :type="statusType(row.status)">{{ row.status }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="error" label="错误" show-overflow-tooltip />
    <el-table-column label="操作" width="280">
      <template #default="{ row }">
        <el-button
          v-if="auth.user?.global_role === 'admin' && row.status === 'draft'"
          size="small"
          type="success"
          @click="publish(row)"
        >发布</el-button>
        <el-button
          v-if="auth.user?.global_role === 'admin' && row.status === 'published'"
          size="small"
          type="warning"
          @click="archive(row)"
        >归档</el-button>
        <el-button size="small" @click="openDetail(row)">版本</el-button>
        <el-button
          v-if="auth.user?.global_role === 'admin'"
          size="small"
          type="danger"
          @click="removePackage(row)"
        >删除</el-button>
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

  <el-dialog v-model="detailVisible" :title="`版本历史 - ${detailPkg?.name ?? ''}`" width="560px">
    <el-table :data="versions" v-loading="versionsLoading" size="small">
      <el-table-column prop="version" label="版本" width="100" />
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="checksum" label="Checksum" show-overflow-tooltip />
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button
            v-if="auth.user?.global_role === 'admin' && detailPkg?.status === 'published' && row.status === 'archived'"
            size="small"
            type="primary"
            :loading="rollbackLoading === row.id"
            @click="rollback(row)"
          >回滚</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; display: flex; gap: 0.5rem; }
.spec h3 { margin-top: 0; }
.spec pre { background: var(--el-fill-color-light); padding: 0.8rem; border-radius: 4px; font-size: 0.82rem; overflow: auto; }
.spec .tree { line-height: 1.5; }
.spec .yaml { line-height: 1.6; }
.dim { color: var(--el-text-color-secondary); font-size: 0.85rem; }
</style>