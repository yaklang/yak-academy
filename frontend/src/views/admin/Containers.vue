<script setup lang="ts">
// 管理面板：容器管理。整体视图 = 当前活跃容器概览（含题目信息，可强制销毁）
// + 下方容器生命周期日志（append-only，记录每次启动/停止及失败原因）。
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminInstanceApi, type AdminLabInstance, type InstanceLog } from '../../api'

const active = ref<AdminLabInstance[]>([])
const activeLoading = ref(false)
const destroying = ref(0)

const logs = ref<InstanceLog[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logLoading = ref(false)

async function loadActive() {
  activeLoading.value = true
  try {
    const resp = await adminInstanceApi.active()
    active.value = resp.items
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    activeLoading.value = false
  }
}

async function loadLogs() {
  logLoading.value = true
  try {
    const resp = await adminInstanceApi.logs(logPage.value)
    logs.value = resp.items
    logTotal.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    logLoading.value = false
  }
}

async function load() {
  await Promise.all([loadActive(), loadLogs()])
}

async function forceDestroy(inst: AdminLabInstance) {
  try {
    await ElMessageBox.confirm(
      `将强制销毁用户 ${inst.username} 的实例「${inst.challenge_title}」#${inst.id}，容器会被真实回收，该用户环境将不可用。`,
      '强制销毁确认',
      { type: 'warning', confirmButtonText: '销毁', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  destroying.value = inst.id
  try {
    await adminInstanceApi.destroy(inst.id)
    ElMessage.success(`实例 #${inst.id} 已销毁`)
    await load()
  } catch (e) {
    ElMessage.error(`销毁失败：${(e as Error).message}`)
  } finally {
    destroying.value = 0
  }
}

function statusType(s: AdminLabInstance['status']) {
  return s === 'running' ? 'success' : s === 'creating' ? 'warning' : s === 'destroyed' ? 'info' : 'danger'
}

function sourceLabel(source: string) {
  if (source === 'admin') return '管理员'
  if (source === 'reaper') return '超时回收'
  return '用户'
}

function fmt(ts: string) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

function fmtTime(ts: string) {
  return ts ? new Date(ts).toLocaleTimeString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>容器管理</h2>
  <p class="hint">
    当前打开的容器与操作日志。强制销毁会回收真实容器资源；每次启动/停止均记录日志（含来源与失败原因）。
  </p>

  <!-- 当前活跃容器概览 -->
  <el-card class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>当前打开的容器</span>
        <el-tag size="small" type="primary">{{ active.length }}</el-tag>
      </div>
    </template>
    <el-table :data="active" v-loading="activeLoading" empty-text="当前没有运行中的容器">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="username" label="用户" width="120" />
      <el-table-column label="题目" min-width="180">
        <template #default="{ row }">
          <span>{{ row.challenge_title || `题目 #${row.challenge_id}` }}</span>
          <div v-if="row.challenge_slug" class="dim">{{ row.challenge_slug }}</div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="endpoint" label="Endpoint" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <code v-if="row.endpoint">{{ row.endpoint }}</code>
          <span v-else class="dim">-</span>
        </template>
      </el-table-column>
      <el-table-column label="到期时间" width="100">
        <template #default="{ row }">{{ fmtTime(row.expire_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="110">
        <template #default="{ row }">
          <el-button
            size="small"
            type="danger"
            :loading="destroying === row.id"
            @click="forceDestroy(row)"
          >强制销毁</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-card>

  <!-- 容器生命周期日志 -->
  <el-card class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>容器日志</span>
        <el-button size="small" @click="load">刷新</el-button>
      </div>
    </template>
    <el-table :data="logs" v-loading="logLoading" empty-text="暂无容器操作记录">
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="instance_id" label="实例" width="80" />
      <el-table-column prop="username" label="用户" width="120" />
      <el-table-column label="题目" min-width="160">
        <template #default="{ row }">
          <span>{{ row.challenge_title || `题目 #${row.challenge_id}` }}</span>
        </template>
      </el-table-column>
      <el-table-column label="动作" width="90">
        <template #default="{ row }">
          <el-tag :type="row.action === 'started' ? 'success' : 'info'" size="small">
            {{ row.action === 'started' ? '启动' : '停止' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="来源" width="90">
        <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="90" />
      <el-table-column prop="detail" label="详情" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.detail">{{ row.detail }}</span>
          <span v-else class="dim">-</span>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-if="logTotal > 20"
      layout="prev, pager, next"
      :total="logTotal"
      :page-size="20"
      v-model:current-page="logPage"
      @current-change="loadLogs"
      class="pager"
    />
  </el-card>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.block { margin-bottom: 1.2rem; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.dim { color: var(--el-text-color-placeholder); font-size: 0.8rem; }
.pager { margin-top: 1rem; }
</style>