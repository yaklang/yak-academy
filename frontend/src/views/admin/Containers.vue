<script setup lang="ts">
// 管理面板：容器管理。全量实例列表（跨用户），支持状态筛选与强制销毁
//（销毁会真实回收驱动容器并写 lab.instance.stopped 事件）。
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminInstanceApi, type AdminLabInstance } from '../../api'

const instances = ref<AdminLabInstance[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const statusFilter = ref<string>('')
const destroying = ref(0)

const filtered = computed(() =>
  statusFilter.value
    ? instances.value.filter((i) => i.status === statusFilter.value)
    : instances.value,
)

async function load() {
  loading.value = true
  try {
    const resp = await adminInstanceApi.list(page.value)
    instances.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
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

function fmt(ts: string) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>容器管理</h2>
  <p class="hint">全部用户的 Lab 容器实例。强制销毁会回收真实容器资源并记录事件（source=admin）。</p>
  <div class="toolbar">
    <el-select v-model="statusFilter" placeholder="状态筛选（全部）" clearable size="small" style="width: 180px">
      <el-option label="creating" value="creating" />
      <el-option label="running" value="running" />
      <el-option label="stopped" value="stopped" />
      <el-option label="destroyed" value="destroyed" />
    </el-select>
    <el-button size="small" @click="load">刷新</el-button>
  </div>
  <el-table :data="filtered" v-loading="loading">
    <el-table-column prop="id" label="ID" width="70" />
    <el-table-column prop="username" label="用户" width="120" />
    <el-table-column prop="challenge_title" label="题目" />
    <el-table-column prop="room_slug" label="Room" width="140" show-overflow-tooltip />
    <el-table-column label="状态" width="100">
      <template #default="{ row }">
        <el-tag :type="statusType(row.status)" size="small">{{ row.status }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="endpoint" label="Endpoint" show-overflow-tooltip>
      <template #default="{ row }">
        <code v-if="row.endpoint">{{ row.endpoint }}</code>
        <span v-else class="dim">-</span>
      </template>
    </el-table-column>
    <el-table-column label="到期时间" width="170">
      <template #default="{ row }">{{ fmt(row.expire_at) }}</template>
    </el-table-column>
    <el-table-column label="创建时间" width="170">
      <template #default="{ row }">{{ fmt(row.created_at) }}</template>
    </el-table-column>
    <el-table-column label="操作" width="110">
      <template #default="{ row }">
        <el-button
          v-if="row.status === 'running' || row.status === 'creating'"
          size="small"
          type="danger"
          :loading="destroying === row.id"
          @click="forceDestroy(row)"
        >强制销毁</el-button>
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
    class="pager"
  />
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.toolbar { display: flex; gap: 0.5rem; margin-bottom: 1rem; }
.dim { color: var(--el-text-color-placeholder); }
.pager { margin-top: 1rem; }
</style>