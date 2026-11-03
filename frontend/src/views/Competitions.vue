<script setup lang="ts">
// 竞赛广场（学生端）：分页列出公开赛（邀请赛定向，不在此展示）。
// 可查看赛程并提前报名（幂等）；已报名者开赛后免邀请码入场
// （入场页 /competitions/enter/:id 支持免码提交）。
import { onMounted, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { competitionApi, type PublicCompetitionItem } from '../api'

const items = ref<PublicCompetitionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const registering = ref(0)

async function load() {
  loading.value = true
  try {
    const resp = await competitionApi.plaza(page.value, pageSize.value)
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function register(row: PublicCompetitionItem) {
  registering.value = row.id
  try {
    const updated = await competitionApi.register(row.id)
    // 原位更新报名状态与人数，不整页刷新。
    const i = items.value.findIndex((it) => it.id === row.id)
    if (i >= 0) items.value[i] = updated
    notify.success('报名成功：开赛后可免邀请码入场')
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    registering.value = 0
  }
}

function statusType(s: PublicCompetitionItem['status']) {
  return s === 'running' ? 'success' : s === 'scheduled' ? 'warning' : 'info'
}
function statusLabel(s: PublicCompetitionItem['status']) {
  return s === 'running' ? '进行中' : s === 'scheduled' ? '未开始' : '已结束'
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">竞赛<span class="accent">广场</span></h2>
      <p class="page-sub">
        公开赛在此开放报名：报名后开赛即可免邀请码入场；邀请赛凭站内邀请邮件中的邀请码入场，不在此展示。
      </p>
    </div>
  </header>

  <UiCard shadow="never">
    <UiTable :data="items" v-busy="loading" empty-text="暂无公开竞赛">
      <UiTableColumn prop="name" label="竞赛" min-width="180" />
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
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
            <UiTag type="success" size="small" class="reg-tag">已报名</UiTag>
            <router-link v-if="row.status === 'running'" :to="`/competitions/enter/${row.id}`">
              <UiButton size="small" type="primary" link>进入竞赛</UiButton>
            </router-link>
            <span v-else-if="row.status === 'scheduled'" class="dim">开赛后免码入场</span>
          </template>
          <UiButton
            v-else-if="row.status !== 'ended'"
            size="small"
            type="primary"
            :loading="registering === row.id"
            @click="register(row)"
          >报名</UiButton>
          <span v-else class="dim">已结束</span>
        </template>
      </UiTableColumn>
    </UiTable>
    <UiPagination
      v-if="total > 0"
      background
      layout="total, prev, pager, next"
      :total="total"
      :page-size="pageSize"
      v-model:current-page="page"
      @current-change="load"
      class="pager"
    />
  </UiCard>
</template>

<style scoped>
.dim { color: var(--muted-foreground); font-size: var(--text-xs); }
.reg-tag { margin-right: var(--space-2); }
.pager { margin-top: var(--space-4); }
</style>
