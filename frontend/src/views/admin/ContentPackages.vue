<script setup lang="ts">
// 教程管理：教程内容包（zip）上传与版本管理（草稿 → 发布 / 归档 / 回滚），
// 以及内置训练集（vulinbox）教程目录展示与重新导入。
// 包内只有教程（文字 + 视频）；题目是独立资源，经「题目管理」用 challenge.json 上传。
// 内置教程不走内容包记录（一键导入直接写入专题与教程）：此处仅目录 + 导入
// 入口，无删除/编辑操作——内置内容只经重新导入更新，普通包也不得覆盖其标识。
import { computed, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  contentApi,
  adminChallengeApi,
  type BuiltinTutorialCatalog,
  type BuiltinTutorialRoom,
  type ContentPackage,
  type ContentVersion,
} from '../../api'

const packages = ref<ContentPackage[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const loadError = ref('')
const uploading = ref(false)
const specVisible = ref(false)

// 版本弹窗（回滚入口）。
const detailVisible = ref(false)
const detailPkg = ref<ContentPackage | null>(null)
const versions = ref<ContentVersion[]>([])
const versionsLoading = ref(false)
const versionsError = ref('')
const rollbackLoading = ref(0)

// 删除 / 重新导入的二次确认。
const deleteTarget = ref<ContentPackage | null>(null)
const deleting = ref(false)
const builtinConfirmOpen = ref(false)

async function load() {
  loading.value = true
  loadError.value = ''
  packages.value = []
  total.value = 0
  try {
    const resp = await contentApi.list(page.value)
    packages.value = resp.items
    total.value = resp.total
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '内容包列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function onUpload(file: File) {
  uploading.value = true
  try {
    await contentApi.upload(file)
    notify.success('上传成功，已进入草稿状态')
    await load()
  } catch (e) {
    notify.error(`上传失败：${(e as Error).message}`)
  } finally {
    uploading.value = false
  }
  return false // 阻止上传组件默认提交
}

async function publish(pkg: ContentPackage) {
  try {
    await contentApi.publish(pkg.id)
    notify.success(`已发布 ${pkg.name}@${pkg.latest_version}`)
    await load()
  } catch (e) {
    notify.error(`发布失败：${(e as Error).message}`)
  }
}

async function archive(pkg: ContentPackage) {
  try {
    await contentApi.archive(pkg.id)
    notify.success(`已归档 ${pkg.name}（内容保持可见，包冻结）`)
    await load()
  } catch (e) {
    notify.error(`归档失败：${(e as Error).message}`)
  }
}

// 删除不可恢复：包 + 历代版本写入的教程内容一并移除，
// 题目本体与学生完成进度/事件历史保留（进度行随被删教程清理）。
async function confirmRemove() {
  const pkg = deleteTarget.value
  if (!pkg) return
  deleting.value = true
  try {
    const res = await contentApi.remove(pkg.id)
    notify.success(`已删除 ${pkg.name}（移除 ${res.topics} 个专题 / ${res.rooms} 篇教程）`)
    deleteTarget.value = null
    await load()
  } catch (e) {
    notify.error(`删除失败：${(e as Error).message}`)
  } finally {
    deleting.value = false
  }
}

async function loadVersions() {
  if (!detailPkg.value) return
  versionsLoading.value = true
  versionsError.value = ''
  versions.value = []
  try {
    versions.value = await contentApi.versions(detailPkg.value.id)
  } catch (e) {
    versionsError.value = e instanceof Error ? e.message : '版本历史加载失败，请稍后重试'
  } finally {
    versionsLoading.value = false
  }
}

async function openDetail(pkg: ContentPackage) {
  detailPkg.value = pkg
  detailVisible.value = true
  await loadVersions()
}

async function rollback(version: ContentVersion) {
  if (!detailPkg.value) return
  rollbackLoading.value = version.id
  try {
    const pkg = await contentApi.rollback(detailPkg.value.id, version.id)
    notify.success(`已回滚到 ${pkg.latest_version}`)
    detailPkg.value = pkg
    versions.value = await contentApi.versions(pkg.id)
    await load()
  } catch (e) {
    notify.error(`回滚失败：${(e as Error).message}`)
  } finally {
    rollbackLoading.value = 0
  }
}

// ===== 内置训练集（vulinbox）教程 =====
// 目录数据源 GET /admin/challenges/builtin/tutorials（帐面清单来自平台分发
// 资产，不依赖已导入）；导入/更新走题目管理同一入口 POST /admin/challenges/builtin
//（教程与题目同批写入，一教程对一题）。
const builtin = ref<BuiltinTutorialCatalog | null>(null)
const builtinLoading = ref(true)
const builtinError = ref('')
const builtinImporting = ref(false)
const builtinExpanded = ref(false)

const builtinRoomCount = computed(() =>
  builtin.value?.topics.reduce((sum, topic) => sum + topic.rooms.length, 0) ?? 0,
)

const allBuiltinImported = computed(() =>
  Boolean(builtin.value && builtinRoomCount.value > 0 && builtin.value.imported_rooms === builtinRoomCount.value),
)

const builtinActionLabel = computed(() =>
  builtin.value && builtin.value.imported_rooms > 0 ? '重新导入/更新' : '导入内置训练',
)

async function loadBuiltin() {
  builtinLoading.value = true
  builtinError.value = ''
  try {
    builtin.value = await adminChallengeApi.builtinTutorialCatalog()
  } catch (e) {
    builtinError.value = e instanceof Error ? e.message : '内置训练集目录加载失败，请稍后重试'
  } finally {
    builtinLoading.value = false
  }
}

function roomState(room: BuiltinTutorialRoom) {
  return room.imported ? '已导入' : '未导入'
}

// 内置教程不可删除/编辑（服务端按内置标记拦截覆盖与删除），
// 更新只能重新一键导入（幂等：重复导入 = 更新）。
async function confirmReimport() {
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
    await loadBuiltin()
  } catch (e) {
    notify.error(`内置训练导入失败：${(e as Error).message}`)
  } finally {
    builtinImporting.value = false
  }
}

load()
loadBuiltin()
</script>

<template>
  <PageHeader
    title="教程管理"
    description="教程以内容包（zip）上传：文字教程与配套视频走版本化流程。题目是独立资源，请到「题目管理」上传。"
  >
    <template #actions>
      <Button type="button" variant="outline" @click="specVisible = !specVisible">
        {{ specVisible ? '收起格式规范' : '查看格式规范' }}
      </Button>
      <UiUpload
        :show-file-list="false"
        accept=".zip"
        :before-upload="onUpload"
        :disabled="uploading"
      >
        <UiButton type="primary" :loading="uploading">上传内容包（zip）</UiButton>
      </UiUpload>
    </template>
  </PageHeader>

  <section v-if="specVisible" class="spec-panel">
    <div class="spec-panel__head">
      <h2 class="spec-panel__title">内容包格式</h2>
      <Button type="button" variant="ghost" size="sm" @click="specVisible = false">收起</Button>
    </div>
    <p class="spec-text">zip 根目录（或唯一一层包裹目录）下：</p>
    <pre class="spec-code">package.zip
├── manifest.yaml        # 包元数据（必填）
└── topics/
    └── &lt;专题标识&gt;/
        ├── topic.md     # 专题首页（front matter 必填）
        └── rooms/
            └── &lt;教程标识&gt;/
                └── lesson.md   # 教程正文（front matter 必填，可含视频）</pre>

    <h3 class="spec-subtitle">manifest.yaml（必填）</h3>
    <pre class="spec-code">name: my-course        # 包名（同名再传 = 升级出新版本）
version: 1.0.0         # 语义化版本
language: zh
topics:
  - web-security       # 专题标识列表</pre>

    <h3 class="spec-subtitle">教程 front matter（必填）</h3>
    <pre class="spec-code">---
title: SQL 注入基础
difficulty: apprentice   # 三档：apprentice / practitioner / expert
video: https://cdn.example.com/sqli.mp4   # 可选：http/https 直链或可嵌入页面
flow:                                    # 可选：教程流程（2-12 步，每步 ≤120 字符）
  - 观察正常响应
  - 探测列数
  - 构造 UNION 查询
  - 拖出目标数据
---
正文为 Markdown，知识链接示例：[SQL 注入](#knowledge:sql-injection)</pre>
    <p class="spec-note">
      上传后进入草稿，发布后才对学员可见；同一包可多次上传新版本，旧版本支持归档与回滚。
      题目（flag、容器环境、提示）不随教程包上传。
    </p>
  </section>

  <DataTablePanel
    class="builtin-card"
    title="内置训练集（vulinbox）"
    :count="builtin ? builtinRoomCount : undefined"
  >
    <template #toolbar>
      <Button type="button" :disabled="builtinImporting" @click="builtinConfirmOpen = true">
        {{ builtinActionLabel }}
      </Button>
    </template>

    <div class="builtin-body">
      <EmptyState
        v-if="builtinError"
        compact
        tone="error"
        title="内置训练集目录加载失败"
        :description="builtinError"
        action-label="重试"
        @action="loadBuiltin"
      />

      <div v-else-if="builtinLoading" class="builtin-loading" aria-busy="true" aria-label="正在加载内置训练集">
        <Skeleton class="builtin-skeleton" />
        <Skeleton class="builtin-skeleton builtin-skeleton--short" />
      </div>

      <template v-else-if="builtin">
        <div class="builtin-head">
          <p class="builtin-summary">
            平台自带 {{ builtin.topics.length }} 个专题 / {{ builtinRoomCount }} 篇教程（一教程对一道配套题目，随平台版本分发）。
            内置教程不走内容包版本流程，<strong>不支持删除与编辑</strong>——更新随平台升级由「{{ builtinActionLabel }}」完成，普通内容包也不得覆盖内置教程。
          </p>
          <div class="builtin-status">
            <span class="builtin-version">v{{ builtin.version }}</span>
            <StatusTag
              kind="package"
              :value="allBuiltinImported ? 'published' : 'draft'"
              :label="allBuiltinImported ? '已导入' : '未完整导入'"
            />
          </div>
        </div>

        <div v-if="builtinExpanded" class="builtin-catalog">
          <section v-for="topic in builtin.topics" :key="topic.slug" class="builtin-topic">
            <h3 class="builtin-topic__title">{{ topic.title }}</h3>
            <ul class="builtin-rooms">
              <li v-for="room in topic.rooms" :key="room.slug" :class="{ 'not-imported': !room.imported }">
                <span class="room-title">{{ room.title }}</span>
                <StatusTag
                  kind="package"
                  :value="room.imported ? 'published' : 'draft'"
                  :label="roomState(room)"
                />
              </li>
            </ul>
          </section>
        </div>

        <Button type="button" variant="link" size="sm" @click="builtinExpanded = !builtinExpanded">
          {{ builtinExpanded ? '收起目录' : `展开目录（${builtin.topics.length} 专题 / ${builtinRoomCount} 篇）` }}
        </Button>
      </template>
    </div>
  </DataTablePanel>

  <DataTablePanel
    v-if="!loadError"
    class="packages-panel"
    title="内容包"
    :count="total"
    :description="loading ? '正在加载内容包…' : '上传新版本会生成草稿；发布后学员可见。'"
  >
    <UiTable
      :data="packages"
      :loading="loading"
      :loading-rows="5"
      aria-label="教程内容包"
    >
      <UiTableColumn label="内容包" min-width="220">
        <template #default="{ row }">
          <span class="package-name">{{ row.name }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="最新版本" width="110">
        <template #default="{ row }">
          <span class="package-version">{{ row.latest_version }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="package" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="错误" min-width="200">
        <template #default="{ row }">
          <span v-if="row.error" class="package-error">{{ row.error }}</span>
          <span v-else class="package-muted">—</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="260">
        <template #default="{ row }">
          <div class="row-actions">
            <Button v-if="row.status === 'draft'" type="button" size="sm" @click="publish(row)">发布</Button>
            <Button v-if="row.status === 'published'" type="button" variant="outline" size="sm" @click="archive(row)">归档</Button>
            <Button type="button" variant="ghost" size="sm" @click="openDetail(row)">版本</Button>
            <Button type="button" variant="link" size="sm" class="danger-action" @click="deleteTarget = row">删除</Button>
          </div>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          title="还没有内容包"
          description="上传 zip 后进入草稿，发布后才对学员可见。"
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
    title="内容包列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <UiDialog v-model="detailVisible" :title="`版本历史 - ${detailPkg?.name ?? ''}`" width="560px">
    <EmptyState
      v-if="versionsError"
      compact
      tone="error"
      title="版本历史加载失败"
      :description="versionsError"
      action-label="重试"
      @action="loadVersions"
    />
    <UiTable v-else :data="versions" :loading="versionsLoading" :loading-rows="4" aria-label="版本历史">
      <UiTableColumn label="版本" width="100">
        <template #default="{ row }">
          <span class="package-version">{{ row.version }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="package" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="上传时间" min-width="160">
        <template #default="{ row }">
          <span class="package-muted">{{ formatDateTime(row.created_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="90">
        <template #default="{ row }">
          <Button
            v-if="detailPkg?.status === 'published' && row.status === 'archived'"
            type="button"
            size="sm"
            :disabled="rollbackLoading === row.id"
            @click="rollback(row)"
          >
            {{ rollbackLoading === row.id ? '回滚中…' : '回滚' }}
          </Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState compact title="暂无版本记录" description="上传内容包后会出现版本历史。" />
      </template>
    </UiTable>
  </UiDialog>

  <ConfirmDialog
    :open="!!deleteTarget"
    title="删除内容包"
    :description="deleteTarget ? `将删除内容包「${deleteTarget.name}」及其历代版本写入的全部教程（专题与教程页），学员的完成进度随之清空，事件历史按审计要求保留。此操作不可恢复。` : ''"
    confirm-label="删除"
    tone="danger"
    :loading="deleting"
    @update:open="!$event && (deleteTarget = null)"
    @confirm="confirmRemove"
  />

  <ConfirmDialog
    :open="builtinConfirmOpen"
    title="重新导入内置训练"
    description="将重新导入随平台分发的内置训练集（配套教程与题目，一个教程对应一道题目）。已导入的内置内容会更新到当前平台版本；配置了 Docker 时会同时构建靶场基础镜像。"
    confirm-label="导入/更新"
    :loading="builtinImporting"
    @update:open="builtinConfirmOpen = $event"
    @confirm="confirmReimport"
  />
</template>

<style scoped>
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

.spec-subtitle {
  margin-top: var(--space-4);
  margin-bottom: var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.spec-text,
.spec-note {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.spec-note {
  margin-top: var(--space-4);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.spec-code {
  margin-top: var(--space-2);
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  line-height: var(--leading-relaxed);
  overflow-x: auto;
}

.builtin-body {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-4);
}

.builtin-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.builtin-summary {
  max-width: 72ch;
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.builtin-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

.builtin-version {
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
}

.builtin-loading {
  display: grid;
  gap: var(--space-2);
}

.builtin-skeleton {
  width: 100%;
  height: 12px;
}

.builtin-skeleton--short {
  width: 56%;
}

.builtin-catalog {
  display: grid;
  gap: var(--space-4);
}

.builtin-topic__title {
  margin-bottom: var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.builtin-rooms {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: var(--space-1) var(--space-5);
  margin: 0;
  padding: 0;
  list-style: none;
}

.builtin-rooms li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding-block: var(--space-1);
  border-bottom: 1px solid var(--border);
}

.builtin-rooms li.not-imported .room-title {
  color: var(--text-tertiary);
}

.room-title {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--text-meta);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.packages-panel {
  margin-top: var(--space-4);
}

.package-name {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  overflow-wrap: anywhere;
}

.package-version {
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  white-space: nowrap;
}

.package-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.package-error {
  color: var(--status-danger);
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
</style>