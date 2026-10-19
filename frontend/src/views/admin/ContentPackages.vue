<script setup lang="ts">
// 教程管理：教程内容包（zip）上传与版本管理（draft → publish / 归档 / 回滚），
// 以及内置训练集（vulinbox）教程目录展示与重新导入。
// 包内只有教程（文字 + 视频）；题目是独立资源，经"题目管理"用 challenge.json 上传。
// 内置教程不走内容包记录（一键导入直接 upsert Topic/Room）：此处仅目录 + 导入
// 入口，无删除/编辑操作——内置内容只经重新导入更新，普通包也不得覆盖其 slug。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  contentApi,
  adminChallengeApi,
  type BuiltinTutorialCatalog,
  type BuiltinTutorialRoom,
  type ContentPackage,
  type ContentVersion,
} from '../../api'
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

// ===== 内置训练集（vulinbox）教程 =====
// 目录数据源 GET /admin/challenges/builtin/tutorials（帐面清单来自平台分发
// 资产，不依赖已导入）；导入/更新走题目管理同一入口 POST /admin/challenges/builtin
//（教程与题目同批写入，一教程对一题）。
const builtin = ref<BuiltinTutorialCatalog | null>(null)
const builtinLoading = ref(false)
const builtinImporting = ref(false)
const builtinExpanded = ref(false)

async function loadBuiltin() {
  builtinLoading.value = true
  try {
    builtin.value = await adminChallengeApi.builtinTutorialCatalog()
  } catch (e) {
    ElMessage.error(`内置训练集目录加载失败：${(e as Error).message}`)
  } finally {
    builtinLoading.value = false
  }
}

function builtinRoomCount() {
  return builtin.value?.topics.reduce((n, t) => n + t.rooms.length, 0) ?? 0
}

function roomState(room: BuiltinTutorialRoom) {
  return room.imported ? '已导入' : '未导入'
}

// 内置教程不可删除/编辑（服务端按 is_builtin 标记拦截覆盖与删除），
// 更新只能重新一键导入（幂等：重复导入 = 更新）。
async function reimportBuiltin() {
  try {
    await ElMessageBox.confirm(
      '将重新导入随平台分发的内置训练集（配套教程 + 题目，一个教程对应一道题目）。已导入的内置内容会被更新到当前平台版本；配置了 Docker 时会同时构建靶场基础镜像（需联网下载约 165MB 的官方二进制）。是否继续？',
      '重新导入内置训练',
      { type: 'info', confirmButtonText: '导入/更新', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  builtinImporting.value = true
  try {
    const resp = await adminChallengeApi.importBuiltin()
    const summary = `内置训练 v${resp.version}：新增 ${resp.created} 题、更新 ${resp.updated} 题、教程 ${resp.rooms} 篇`
    if (resp.image_built) {
      ElMessage.success(`${summary}；靶场镜像已构建`)
    } else {
      ElMessage.warning({ message: `${summary}；未配置 Docker，镜像未构建（启用 Docker 后重新导入即可）`, duration: 6000 })
    }
    await loadBuiltin()
  } catch (e) {
    ElMessage.error(`内置训练导入失败：${(e as Error).message}`)
  } finally {
    builtinImporting.value = false
  }
}

onMounted(() => {
  load()
  loadBuiltin()
})
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
  <el-card v-loading="builtinLoading" class="builtin-card" shadow="never">
    <template #header>
      <div class="builtin-header">
        <span class="builtin-title">
          内置训练集（vulinbox）
          <el-tag v-if="builtin" size="small" type="info">v{{ builtin.version }}</el-tag>
          <el-tag
            v-if="builtin"
            size="small"
            :type="builtin.imported_rooms === builtinRoomCount() && builtinRoomCount() > 0 ? 'success' : 'warning'"
          >
            {{ builtin.imported_rooms === builtinRoomCount() && builtinRoomCount() > 0 ? '已导入' : '未完整导入' }}
          </el-tag>
        </span>
        <el-button
          v-if="auth.user?.global_role === 'admin'"
          size="small"
          type="primary"
          :loading="builtinImporting"
          @click="reimportBuiltin"
        >{{ builtin && builtin.imported_rooms > 0 ? '重新导入/更新' : '导入内置训练' }}</el-button>
      </div>
    </template>
    <p v-if="builtin" class="dim">
      平台自带 {{ builtin.topics.length }} 个专题 / {{ builtinRoomCount() }} 篇教程（一教程对一道配套题目，随平台版本分发）。内置教程不走内容包版本管线，<strong>不支持删除与编辑</strong>——更新随平台升级由「{{ builtin.imported_rooms > 0 ? '重新导入/更新' : '导入内置训练' }}」完成，普通内容包也不得覆盖内置教程。
    </p>
    <el-collapse-transition>
      <div v-show="builtinExpanded && builtin">
        <div v-for="topic in builtin?.topics" :key="topic.slug" class="builtin-topic">
          <h4>{{ topic.title }}<span class="dim slug">（{{ topic.slug }}）</span></h4>
          <ul class="builtin-rooms">
            <li v-for="room in topic.rooms" :key="room.slug" :class="{ 'not-imported': !room.imported }">
              <span class="room-title">{{ room.title }}</span>
              <el-tag size="small" :type="room.imported ? 'success' : 'info'">{{ roomState(room) }}</el-tag>
            </li>
          </ul>
        </div>
      </div>
    </el-collapse-transition>
    <el-button v-if="builtin" link type="primary" @click="builtinExpanded = !builtinExpanded">
      {{ builtinExpanded ? '收起目录' : `展开目录（${builtin.topics.length} 专题 / ${builtinRoomCount()} 篇）` }}
    </el-button>
  </el-card>

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
.builtin-card { margin-bottom: 1rem; }
.builtin-header { display: flex; align-items: center; justify-content: space-between; }
.builtin-title { display: inline-flex; align-items: center; gap: 0.5rem; font-weight: 600; }
.builtin-topic h4 { margin: 0.6rem 0 0.3rem; }
.builtin-topic .slug { font-weight: 400; }
.builtin-rooms { list-style: none; margin: 0; padding: 0 0 0 0.2rem; display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 0.3rem 1.2rem; }
.builtin-rooms li { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.15rem 0; }
.builtin-rooms li.not-imported .room-title { color: var(--el-text-color-secondary); }
.builtin-rooms .room-title { font-size: 0.88rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>