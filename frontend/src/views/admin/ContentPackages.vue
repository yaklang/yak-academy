<script setup lang="ts">
// 管理面板：教程 / 题目上传（内容包管理，五步管线入口）+ 版本回滚/归档。
// 附标准内容包格式规范（可折叠）：教程与题目走同一个 zip 包（教题一体）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
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
  <h2>教程 / 题目上传</h2>
  <p class="hint">教程与题目通过同一个标准内容包（zip）上传——教题一体：每个 Room 是一篇教程，至多内嵌一个题目。</p>
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
      <h3>标准内容包格式</h3>
      <p>zip 根目录（或唯一一层包裹目录）下：</p>
      <pre class="tree">package.zip
├── manifest.yaml        # 包元数据（必填）
└── topics/
    └── &lt;topic-slug&gt;/
        ├── topic.md     # topic 首页（front matter 必填）
        └── rooms/
            └── &lt;room-slug&gt;/
                ├── lesson.md   # 教程正文（front matter 必填）
                └── lab.yaml    # 内嵌题目定义（Room 可选有一个）</pre>

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
---
正文为 Markdown……</pre>

      <h4>lab.yaml（题目定义；title/challenge/score 必填）</h4>
      <pre class="yaml">title: 找出注入点
challenge:
  type: static          # static（明文 flag）/ regex（正则匹配）
  flag: flag{answer}
score: 100              # 初始分值（动态计分按通过人数衰减）
env:                    # 可选：容器环境（开启容器后部署题目）
  image: nginx:alpine   # 与 port 要么都填要么都不填
  port: 80
hints:                  # 可选：扣分解锁的提示列表
  - title: 提示一
    content: 看 UNION
    cost: 10</pre>
      <p class="dim">上传后进入 draft，由 admin 发布才对学生可见；同一包可多次上传新版本，旧版本支持归档 / 回滚。</p>
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
    <el-table-column label="操作" width="220">
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
.hint { color: #666; font-size: 0.9rem; margin-top: 0; }
.toolbar { margin-bottom: 1rem; display: flex; gap: 0.5rem; }
.spec h3 { margin-top: 0; }
.spec pre { background: #f5f7fa; padding: 0.8rem; border-radius: 4px; font-size: 0.82rem; overflow: auto; }
.spec .tree { line-height: 1.5; }
.spec .yaml { line-height: 1.6; }
.dim { color: #999; font-size: 0.85rem; }
</style>