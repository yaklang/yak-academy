<script setup lang="ts">
// 管理面板：操作审计。只追加的管理操作轨迹（上传/发布/回滚/删除/竞赛/账号/
// 备份等），与学员行为事件流分工；可按动作过滤，新→旧分页。
// 表格面向管理员：动作、对象和历史字段都显示中文，原始动作码只用于回退排障。
import { ref, watch } from 'vue'
import { formatDateTime } from '@/lib/format'
import {
  AUDIT_ACTION_OPTIONS,
  auditActionLabel,
  auditTargetLabel,
  statusLabel,
} from '@/lib/labels'
import PageHeader from '@/components/layout/PageHeader.vue'
import FilterBar from '@/components/layout/FilterBar.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { Button } from '@/components/ui/button'
import { auditApi, type AdminAuditLog } from '../../api'

const items = ref<AdminAuditLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const loadError = ref('')
const actionFilter = ref('')

const DETAIL_LABELS: Record<string, string> = {
  name: '名称',
  slug: '标识',
  version: '版本',
  status: '状态',
  title: '标题',
  username: '账号',
  role: '角色',
  count: '数量',
  score: '分值',
  category: '分类',
  difficulty: '难度',
  package: '教程包',
  challenge: '题目',
  competition: '竞赛',
  reason: '原因',
  visible: '可见',
  file: '文件',
  size: '大小',
}

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  try {
    const resp = await auditApi.list({
      page: page.value,
      page_size: pageSize,
      action: actionFilter.value || undefined,
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

watch(actionFilter, applyFilter)

function fmt(ts: string) {
  return formatDateTime(ts)
}

function detailValue(key: string, value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (Array.isArray(value)) return `${value.length} 项`
  if (typeof value === 'object') return '已记录'
  if (typeof value === 'boolean') return value ? '是' : '否'
  const text = String(value)
  if (key === 'status') return statusLabel('package', text)
  if (key === 'role') return statusLabel('role', text)
  if (key === 'difficulty') return statusLabel('difficulty', text)
  return text
}

function detailRows(detail: Record<string, unknown> | null): { label: string; value: string }[] {
  if (!detail) return []
  return Object.entries(detail)
    .slice(0, 4)
    .map(([key, value]) => ({ label: DETAIL_LABELS[key] ?? '信息', value: detailValue(key, value) }))
}

load()
</script>

<template>
  <PageHeader
    title="操作审计"
    description="管理端的写操作（上传、发布、回滚、删除、竞赛与账号调整、备份）都会记录在这里；记录只追加，不可修改。"
  />

  <FilterBar>
    <label class="filter-field">
      <span class="filter-field__label">动作</span>
      <select v-model="actionFilter" class="filter-select" aria-label="动作">
        <option value="">全部动作</option>
        <option v-for="option in AUDIT_ACTION_OPTIONS" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
    </label>
    <template #actions>
      <Button v-if="actionFilter" variant="ghost" type="button" @click="actionFilter = ''">清除筛选</Button>
      <Button variant="outline" type="button" @click="load">刷新</Button>
    </template>
  </FilterBar>

  <DataTablePanel
    v-if="!loadError"
    title="审计记录"
    :count="total"
    :description="loading ? '正在加载审计记录…' : actionFilter ? `按当前动作显示 ${total} 条记录` : undefined"
  >
    <UiTable
      :data="items"
      :loading="loading"
      :loading-rows="7"
      aria-label="操作审计记录"
    >
      <UiTableColumn label="时间" width="160">
        <template #default="{ row }">
          <span class="audit-time">{{ fmt(row.created_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="管理员" width="120">
        <template #default="{ row }">
          <span class="audit-admin">{{ row.admin_name }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="动作" min-width="160">
        <template #default="{ row }">
          <span class="audit-action">{{ auditActionLabel(row.action) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="对象" min-width="200">
        <template #default="{ row }">
          <span class="audit-target">
            <span class="audit-target__type">{{ auditTargetLabel(row.target_type) }}</span>
            <code class="audit-target__id">{{ row.target_id || '—' }}</code>
          </span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="来源 IP" width="130">
        <template #default="{ row }">
          <span class="audit-ip">{{ row.ip || '—' }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="详情" min-width="220">
        <template #default="{ row }">
          <ul v-if="detailRows(row.detail).length" class="audit-detail">
            <li v-for="entry in detailRows(row.detail)" :key="entry.label">
              <span>{{ entry.label }}</span>
              {{ entry.value }}
            </li>
          </ul>
          <span v-else class="audit-muted">—</span>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          :tone="actionFilter ? 'filtered' : 'empty'"
          :title="actionFilter ? '该动作还没有记录' : '暂无审计记录'"
          :description="actionFilter ? '换一个动作，或清除筛选查看全部记录。' : '管理端完成写操作后，记录会立即出现在这里。'"
          :action-label="actionFilter ? '清除筛选' : undefined"
          @action="actionFilter = ''"
        />
      </template>
    </UiTable>
    <template v-if="total > pageSize" #footer>
      <UiPagination
        v-model:current-page="page"
        layout="prev, pager, next, total"
        :page-size="pageSize"
        :total="total"
        @current-change="load"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="审计记录加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
</template>

<style scoped>
.filter-field {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.filter-field__label {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.filter-select {
  min-width: 200px;
  min-height: 36px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.audit-time,
.audit-ip {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.audit-admin {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.audit-action {
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.audit-target {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  min-width: 0;
}

.audit-target__type {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  white-space: nowrap;
}

.audit-target__id {
  overflow: hidden;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audit-detail {
  display: grid;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.audit-detail span {
  color: var(--text-tertiary);
}

.audit-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

@media (max-width: 720px) {
  .filter-field,
  .filter-select {
    width: 100%;
  }

  .filter-select {
    min-height: 44px;
  }
}
</style>