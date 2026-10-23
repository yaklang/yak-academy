<script setup lang="ts">
// 竞赛广场（学生端）：分页列出公开赛（邀请赛定向，不在此展示）。
// 可查看赛程并提前报名（幂等）；已报名者开赛后免邀请码入场
// （入场页 /competitions/enter/:id 支持免码提交）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
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
    ElMessage.error((e as Error).message)
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
    ElMessage.success('报名成功：开赛后可免邀请码入场')
  } catch (e) {
    ElMessage.error((e as Error).message)
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
  return ts ? new Date(ts).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>竞赛广场</h2>
  <p class="hint">
    公开赛在此开放报名：报名后开赛即可免邀请码入场；邀请赛凭站内邀请邮件中的邀请码入场，不在此展示。
  </p>

  <el-card shadow="never">
    <el-table :data="items" v-loading="loading" empty-text="暂无公开竞赛">
      <el-table-column prop="name" label="竞赛" min-width="180" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="开始时间" width="170">
        <template #default="{ row }">{{ fmt(row.start_time) }}</template>
      </el-table-column>
      <el-table-column label="结束时间" width="170">
        <template #default="{ row }">{{ fmt(row.end_time) }}</template>
      </el-table-column>
      <el-table-column prop="participants" label="报名人数" width="100" />
      <el-table-column label="报名 / 入场" width="200">
        <template #default="{ row }">
          <template v-if="row.registered">
            <el-tag type="success" size="small" class="reg-tag">已报名</el-tag>
            <router-link v-if="row.status === 'running'" :to="`/competitions/enter/${row.id}`">
              <el-button size="small" type="primary" link>进入竞赛</el-button>
            </router-link>
            <span v-else-if="row.status === 'scheduled'" class="dim">开赛后免码入场</span>
          </template>
          <el-button
            v-else-if="row.status !== 'ended'"
            size="small"
            type="primary"
            :loading="registering === row.id"
            @click="register(row)"
          >报名</el-button>
          <span v-else class="dim">已结束</span>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      background
      layout="total, prev, pager, next"
      :total="total"
      :page-size="pageSize"
      v-model:current-page="page"
      @current-change="load"
      class="pager"
    />
  </el-card>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.dim { color: var(--el-text-color-placeholder); font-size: 0.8rem; }
.reg-tag { margin-right: 0.6rem; }
.pager { margin-top: 1rem; }
</style>
