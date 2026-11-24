<script setup lang="ts">
// 管理面板：备份管理。平台数据快照（数据库 + 附件/头像/解题报告数据目录）
// 打包为单文件归档，按保留份数自动清理；支持立即备份与下载离机保存。
// 数据库使用外部 PostgreSQL 时应用内备份不可用（后端返回说明）。
import { ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatBytes, formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { Button } from '@/components/ui/button'
import { adminBackupApi, type BackupInfo } from '../../api'

const items = ref<BackupInfo[]>([])
const loading = ref(true)
const loadError = ref('')
const creating = ref(false)
const downloading = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  try {
    const resp = await adminBackupApi.list()
    items.value = resp.items
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '备份列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function createBackup() {
  creating.value = true
  try {
    const info = await adminBackupApi.create()
    notify.success(`备份完成：${info.name}（${formatBytes(info.size_bytes)}）`)
    await load()
  } catch (e) {
    notify.error(`备份失败：${(e as Error).message}`)
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
    notify.error(`下载失败：${(e as Error).message}`)
  } finally {
    downloading.value = ''
  }
}

function storeLabel(kind: string) {
  return kind === 's3' ? '对象存储（报告未打包）' : '本地数据目录'
}
</script>

<template>
  <PageHeader
    title="备份管理"
    description="每份备份包含平台数据库与上传的附件；超出保留份数时自动清理最旧的一份。备份文件存放在服务器数据目录，建议定期下载到其它设备。"
  />

  <DataTablePanel
    v-if="!loadError"
    title="备份记录"
    :count="items.length"
    :description="loading ? '正在加载备份…' : items.length ? '下载后的归档可脱离服务器保存。' : undefined"
  >
    <template #toolbar>
      <Button type="button" variant="outline" @click="load">刷新</Button>
      <Button type="button" :disabled="creating" @click="createBackup">
        {{ creating ? '正在备份…' : '立即备份' }}
      </Button>
    </template>

    <UiTable
      :data="items"
      :loading="loading"
      :loading-rows="5"
      aria-label="备份记录"
    >
      <UiTableColumn label="备份文件" min-width="240">
        <template #default="{ row }">
          <code class="backup-name">{{ row.name }}</code>
        </template>
      </UiTableColumn>
      <UiTableColumn label="大小" width="110">
        <template #default="{ row }">
          <span class="backup-meta">{{ formatBytes(row.size_bytes) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="创建时间" width="180">
        <template #default="{ row }">
          <span class="backup-meta">{{ formatDateTime(row.created_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="解题报告存储" min-width="180">
        <template #default="{ row }">
          <span class="backup-store" :data-s3="row.store_kinds === 's3' ? 'true' : undefined">
            {{ storeLabel(row.store_kinds) }}
          </span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="110">
        <template #default="{ row }">
          <Button
            type="button"
            variant="link"
            size="sm"
            :disabled="downloading === row.name"
            @click="download(row)"
          >
            {{ downloading === row.name ? '下载中…' : '下载' }}
          </Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          title="还没有备份"
          description="点击「立即备份」生成第一份，下载后请保存在其它设备。"
        >
          <Button type="button" variant="outline" :disabled="creating" @click="createBackup">
            立即备份
          </Button>
        </EmptyState>
      </template>
    </UiTable>
  </DataTablePanel>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="备份列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <p class="backup-note">
    备份保存在服务器本地时，服务器磁盘故障会导致备份一并丢失；请把重要归档下载到其它设备。
  </p>
</template>

<style scoped>
.backup-name {
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.backup-meta {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.backup-store {
  color: var(--text-secondary);
  font-size: var(--text-meta);
}

.backup-store[data-s3='true'] {
  color: var(--status-warning);
}

.backup-note {
  margin-top: var(--space-3);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

@media (max-width: 720px) {
  .backup-note {
    margin-top: var(--space-2);
  }
}
</style>