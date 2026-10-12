<script setup lang="ts">
// 管理端：内容包上传与发布（五步管线入口）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { contentApi, type ContentPackage } from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const packages = ref<ContentPackage[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const uploading = ref(false)

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
    <el-table-column label="操作" width="120">
      <template #default="{ row }">
        <el-button
          v-if="auth.user?.global_role === 'admin' && row.status === 'draft'"
          size="small"
          type="success"
          @click="publish(row)"
        >发布</el-button>
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
.toolbar { margin-bottom: 1rem; }
</style>