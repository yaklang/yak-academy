<script setup lang="ts">
// 竞赛广场（学生端）：分页列出公开赛（邀请赛定向，不在此展示）。
// 可查看赛程并提前报名（幂等）；已报名者开赛后免邀请码入场。
import { onMounted, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { competitionApi, FORMAT_LABELS, type CompetitionFormat, type PublicCompetitionItem } from '../api'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import { Button } from '@/components/ui/button'

const items = ref<PublicCompetitionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const loadError = ref('')
const registering = ref(0)

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  try {
    const resp = await competitionApi.plaza(page.value, pageSize.value)
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '竞赛列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function register(row: PublicCompetitionItem) {
  registering.value = row.id
  try {
    const updated = await competitionApi.register(row.id)
    const index = items.value.findIndex((item) => item.id === row.id)
    if (index >= 0) items.value[index] = updated
    notify.success('报名成功：开赛后可免邀请码入场')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    registering.value = 0
  }
}

// 报名窗口（可选）：未配置 = 开放到竞赛结束；已配置则按起止时间判断。
function registrationOpen(row: PublicCompetitionItem) {
  const now = Date.now()
  if (row.registration_start && now < Date.parse(row.registration_start)) return false
  if (row.registration_end && now > Date.parse(row.registration_end)) return false
  return true
}

function registrationHint(row: PublicCompetitionItem) {
  if (row.registration_start && Date.now() < Date.parse(row.registration_start)) return '报名未开放'
  return '报名已截止'
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(load)
</script>

<template>
  <PageHeader
    title="竞赛广场"
    description="公开赛在此开放报名：报名后开赛即可免邀请码入场，比赛期间可自由进出。邀请赛凭站内邀请邮件中的邀请码入场并锁定至结束。"
  />

  <DataTablePanel
    v-if="!loadError"
    title="公开竞赛"
    :count="total"
    description="报名是幂等的，重复点击不会重复报名。"
  >
    <UiTable :data="items" :loading="loading" row-key="id" selection-label="选择竞赛">
      <UiTableColumn prop="name" label="竞赛" min-width="200">
        <template #default="{ row }">
          <span class="competition-name">{{ row.name }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="形式" width="90">
        <template #default="{ row }">
          <span class="format-tag">{{ FORMAT_LABELS[row.format as CompetitionFormat] }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="competition" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="开始时间" width="170">
        <template #default="{ row }">{{ fmt(row.start_time) }}</template>
      </UiTableColumn>
      <UiTableColumn label="结束时间" width="170">
        <template #default="{ row }">{{ fmt(row.end_time) }}</template>
      </UiTableColumn>
      <UiTableColumn prop="participants" label="报名人数" width="100" />
      <UiTableColumn label="报名 / 入场" width="200">
        <template #default="{ row }">
          <template v-if="row.registered">
            <StatusTag kind="user" value="active" label="已报名" />
            <Button v-if="row.status === 'running'" size="sm" as-child class="row-action">
              <RouterLink :to="`/competitions/enter/${row.id}`">进入竞赛</RouterLink>
            </Button>
            <span v-else-if="row.status === 'scheduled'" class="competition-dim">开赛后免码入场</span>
          </template>
          <Button
            v-else-if="row.status !== 'ended' && registrationOpen(row)"
            size="sm"
            :loading="registering === row.id"
            @click="register(row)"
          >报名</Button>
          <span v-else-if="row.status !== 'ended'" class="competition-dim">{{ registrationHint(row) }}</span>
          <span v-else class="competition-dim">已结束</span>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          title="暂无公开竞赛"
          description="邀请赛通过站内邮件定向通知；公开赛开放报名后会显示在这里。"
          action-label="查看站内邮箱"
          action-to="/mailbox"
        />
      </template>
    </UiTable>
    <template v-if="total > pageSize" #footer>
      <UiPagination
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        v-model:current-page="page"
        @current-change="load"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-else
    tone="error"
    title="竞赛列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
</template>

<style scoped>
.competition-name {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.format-tag {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.competition-dim {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.row-action {
  margin-left: var(--space-2);
}
</style>