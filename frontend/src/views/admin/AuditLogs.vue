<script setup lang="ts">
// 管理面板：操作审计。append-only 管理操作轨迹（上传/发布/回滚/删除/竞赛/用户/
// 备份等），与 Event 学生行为流分工；可按动作与管理员过滤，新→旧分页。
import { onMounted, ref } from 'vue'
import { formatDateTime } from '@/lib/format'
import { auditApi, type AdminAuditLog } from '../../api'

const items = ref<AdminAuditLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const loadError = ref('')
const actionFilter = ref('')
const adminFilter = ref<number | null>(null)

// 动作清单与后端 handler 钩子一一对应（content./challenge./user./competition./
// builtin./engine./instance./topic./backup. 前缀）。
const actionOptions = [
  'content.upload', 'content.delete', 'content.publish', 'content.archive', 'content.rollback',
  'challenge.visibility', 'challenge.import', 'challenge.package_import', 'challenge.delete',
  'attachment.upload', 'attachment.delete',
  'builtin.import', 'builtin.image_build', 'engine.download',
  'user.create', 'user.roles', 'user.reset_password',
  'competition.create', 'competition.update', 'competition.end', 'competition.delete',
  'competition.bind', 'competition.unbind', 'competition.import_participants',
  'topic.update', 'instance.destroy', 'backup.create',
]

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  try {
    const resp = await auditApi.list({
      page: page.value, page_size: pageSize,
      action: actionFilter.value || undefined,
      admin_id: adminFilter.value ?? undefined,
    })
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '审计记录加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

function applyFilter() {
  page.value = 1
  load()
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">操作<span class="accent">审计</span></h2>
      <p class="page-sub">
        管理端写操作（内容上传/发布/回滚/删除、题目与附件、竞赛、用户与角色、备份等）的
        append-only 审计轨迹；与学员行为事件流（Event 表）分工，不参与积分重放。
      </p>
    </div>
  </header>

  <UiCard class="block" shadow="never">
    <template #header>
      <div class="toolbar">
        <UiSelect v-model="actionFilter" placeholder="动作（全部）" clearable filterable size="small" style="width: 220px" @change="applyFilter">
          <UiOption v-for="a in actionOptions" :key="a" :label="a" :value="a" />
        </UiSelect>
        <UiInputNumber v-model="adminFilter" :min="1" placeholder="管理员 ID" size="small" clearable style="width: 140px" @change="applyFilter" />
        <UiButton size="small" type="primary" @click="applyFilter">查询</UiButton>
        <UiButton size="small" @click="load">刷新</UiButton>
      </div>
    </template>
    <UiLoadError v-if="loadError" compact :message="loadError" @retry="load" class="list-error" />
    <UiTable v-else :data="items" :loading="loading" empty-text="暂无审计记录">
      <UiTableColumn label="时间" width="180" fixed="left">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </UiTableColumn>
      <UiTableColumn label="管理员" width="140">
        <template #default="{ row }">{{ row.admin_name }}<span class="dim"> #{{ row.admin_id }}</span></template>
      </UiTableColumn>
      <UiTableColumn prop="action" label="动作" width="200">
        <template #default="{ row }"><code>{{ row.action }}</code></template>
      </UiTableColumn>
      <UiTableColumn label="目标" min-width="180">
        <template #default="{ row }"><span class="dim">{{ row.target_type }}/</span>{{ row.target_id }}</template>
      </UiTableColumn>
      <UiTableColumn prop="ip" label="来源 IP" width="140" />
      <UiTableColumn label="详情" min-width="220">
        <template #default="{ row }">
          <code v-if="row.detail" class="detail">{{ JSON.stringify(row.detail) }}</code>
          <span v-else class="dim">-</span>
        </template>
      </UiTableColumn>
    </UiTable>
    <div v-if="total > pageSize" class="pager">
      <UiPagination
        v-model:current-page="page"
        layout="prev, pager, next, total"
        :page-size="pageSize"
        :total="total"
        @current-change="load"
      />
    </div>
  </UiCard>
</template>

<style scoped>
.block { margin-bottom: var(--space-5); }
.toolbar { display: flex; gap: var(--space-2); align-items: center; flex-wrap: wrap; }
.dim { color: var(--muted-foreground); }
.detail { font-size: var(--text-xs); word-break: break-all; }
.pager { margin-top: var(--space-3); display: flex; justify-content: flex-end; }
.list-error { margin-bottom: var(--space-3); }
</style>
