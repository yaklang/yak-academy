<script setup lang="ts">
// 管理面板：备份管理。平台数据快照（SQLite 一致性快照 + 附件/头像/WP 数据目录）
// 打包为单文件 tar.gz，按保留份数自动清理；支持立即备份与下载离机保存。
// PostgreSQL 模式应用内备份不支持（创建返回 422，数据库走外部 pg_dump）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminBackupApi, type BackupInfo } from '../../api'

const items = ref<BackupInfo[]>([])
const loading = ref(false)
const creating = ref(false)
const downloading = ref('')

async function load() {
  loading.value = true
  try {
    const resp = await adminBackupApi.list()
    items.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function createBackup() {
  creating.value = true
  try {
    const info = await adminBackupApi.create()
    ElMessage.success(`备份完成：${info.name}（${fmtSize(info.size_bytes)}）`)
    await load()
  } catch (e) {
    ElMessage.error(`备份失败：${(e as Error).message}`)
  } finally {
    creating.value = false
  }
}

async function download(item: BackupInfo) {
  downloading.value = item.name
  try {
    const blob = await adminBackupApi.download(item.name)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = item.name
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(`下载失败：${(e as Error).message}`)
  } finally {
    downloading.value = ''
  }
}

function fmtSize(bytes: number) {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(2)} GB`
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(2)} MB`
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`
  return `${bytes} B`
}

function fmt(ts: string) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>备份管理</h2>
  <p class="hint">
    每份备份 = SQLite 一致性快照 + 附件/头像/WP 数据目录的单文件 tar.gz 归档，超出保留份数自动清理。
    归档落在数据卷的 backups/ 目录——数据卷损坏时备份同样丢失，请定期下载离机保存（异地容灾）。
  </p>

  <el-card class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>归档列表</span>
        <div>
          <el-button size="small" :loading="creating" type="primary" @click="createBackup">立即备份</el-button>
          <el-button size="small" @click="load">刷新</el-button>
        </div>
      </div>
    </template>
    <el-table :data="items" v-loading="loading" empty-text="暂无备份；点击「立即备份」生成第一份">
      <el-table-column prop="name" label="归档" min-width="240">
        <template #default="{ row }">
          <code>{{ row.name }}</code>
        </template>
      </el-table-column>
      <el-table-column label="大小" width="110">
        <template #default="{ row }">{{ fmtSize(row.size_bytes) }}</template>
      </el-table-column>
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="WP 存储" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.store_kinds === 's3' ? 'warning' : 'info'">
            {{ row.store_kinds === 's3' ? 's3（zip 不在归档）' : '本地' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="110">
        <template #default="{ row }">
          <el-button size="small" :loading="downloading === row.name" @click="download(row)">下载</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-card>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.block { margin-bottom: 1.2rem; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
</style>
