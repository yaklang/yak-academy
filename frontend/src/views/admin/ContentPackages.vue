<script setup lang="ts">
// 管理端：内容包上传与发布（五步管线入口）+ 版本回滚/归档。
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
  <h2>内容包管理</h2>
  <div class="toolbar">
    <el-upload
      :show-file-list="false"
      accept=".zip"
      :before-upload="onUpload"
      :disabled="uploading"
    >
      <el-button type="primary" :loading="uploading">上传内容包（zip）</el-button>
    </el-upload>
  </div>
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
.toolbar { margin-bottom: 1rem; }
</style>