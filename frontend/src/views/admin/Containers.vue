<script setup lang="ts">
// 管理面板：容器管理。整体视图 = 主机性能面板（宿主 CPU/内存/容器与镜像计数/
// 数据卷用量 + 全容器资源占用表，可自动刷新）+ 当前活跃容器概览（含题目信息，
// 可强制销毁）+ 下方容器生命周期日志（append-only，记录每次启动/停止及失败原因）。
// 后两个表格均为固定最大高度的内滚动；日志表分页常驻，支持切换每页条数。
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  adminInstanceApi, performanceApi,
  type AdminLabInstance, type InstanceLog, type PerformanceReport,
} from '../../api'

const perf = ref<PerformanceReport | null>(null)
const perfLoading = ref(false)
const autoRefresh = ref(true)
let perfTimer: number | undefined

const active = ref<AdminLabInstance[]>([])
const activeLoading = ref(false)
const destroying = ref(0)

const logs = ref<InstanceLog[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(20)
const logLoading = ref(false)

const REFRESH_MS = 10000

async function loadPerf(silent = true) {
  if (!silent) perfLoading.value = true
  try {
    perf.value = await performanceApi.get()
  } catch (e) {
    // 自动刷新失败不打断页面（瞬时抖动）；手动刷新显式提示。
    if (!silent) ElMessage.error((e as Error).message)
  } finally {
    perfLoading.value = false
  }
}

function schedulePerf() {
  stopPerf()
  if (autoRefresh.value) perfTimer = window.setInterval(() => loadPerf(), REFRESH_MS)
}

function stopPerf() {
  if (perfTimer !== undefined) {
    window.clearInterval(perfTimer)
    perfTimer = undefined
  }
}

function onAutoRefreshChange() {
  schedulePerf()
  if (autoRefresh.value) loadPerf()
}

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
    const resp = await adminInstanceApi.logs(logPage.value, logPageSize.value)
    logs.value = resp.items
    logTotal.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    logLoading.value = false
  }
}

// 换页容量后回到第 1 页：page>1 时改值会触发 current-change 拉取一次，
// 已在第 1 页时 current-change 不会触发，需显式拉取，避免双请求。
async function onLogSizeChange() {
  if (logPage.value !== 1) {
    logPage.value = 1
  } else {
    await loadLogs()
  }
}

async function load() {
  await Promise.all([loadPerf(false), loadActive(), loadLogs()])
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

function fmtBytes(bytes: number | null | undefined) {
  if (bytes === null || bytes === undefined) return '—'
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(2)} GB`
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KB`
  return `${bytes} B`
}

// 资源占用水位着色：<60% 健康、60–85% 警告、>85% 危险（进度条与数字标签共用）。
function usageColor(pct: number) {
  return pct > 85 ? '#f56c6c' : pct > 60 ? '#e6a23c' : '#67c23a'
}

function memPercentOf(rep: PerformanceReport) {
  if (!rep.host) return null
  const used = rep.host.mem_used_bytes
  if (used === null || !rep.host.mem_total_bytes) return null
  return (used / rep.host.mem_total_bytes) * 100
}

function volPercent() {
  const v = perf.value?.data_volume
  if (!v || !v.total_bytes) return 0
  return (v.used_bytes / v.total_bytes) * 100
}

onMounted(() => {
  load()
  schedulePerf()
})

onUnmounted(stopPerf)
</script>

<template>
  <h2>容器管理</h2>
  <p class="hint">
    主机性能、当前打开的容器与操作日志。强制销毁会回收真实容器资源；每次启动/停止均记录日志（含来源与失败原因）。
  </p>

  <!-- 主机性能面板 -->
  <el-card class="block" shadow="never" v-loading="perfLoading">
    <template #header>
      <div class="card-head">
        <span>主机性能</span>
        <div class="head-actions">
          <span v-if="autoRefresh" class="dim">每 {{ REFRESH_MS / 1000 }}s 自动刷新</span>
          <el-switch v-model="autoRefresh" size="small" @change="onAutoRefreshChange" />
          <el-button size="small" @click="loadPerf(false)">刷新</el-button>
        </div>
      </div>
    </template>
    <template v-if="perf">
      <div class="host-grid">
        <div class="metric">
          <div class="metric-title">CPU（{{ perf.ncpu }} 核）</div>
          <div v-if="perf.host?.cpu_percent !== null && perf.host?.cpu_percent !== undefined">
            <el-progress
              :percentage="Math.min(100, Math.round(perf.host.cpu_percent))"
              :color="usageColor(perf.host.cpu_percent)"
            />
            <div class="dim" :style="{ color: usageColor(perf.host.cpu_percent) }">
              宿主占用 {{ perf.host.cpu_percent.toFixed(1) }}%（多核可超 100%）
            </div>
          </div>
          <div v-else class="dim">宿主占用采样不可用（非 Linux 部署）</div>
        </div>
        <div class="metric">
          <div class="metric-title">内存</div>
          <template v-if="memPercentOf(perf) !== null">
            <el-progress
              :percentage="Math.min(100, Math.round(memPercentOf(perf)!))"
              :color="usageColor(memPercentOf(perf)!)"
            />
            <div class="dim">
              {{ fmtBytes(perf.host!.mem_used_bytes) }} / {{ fmtBytes(perf.host!.mem_total_bytes) }}
            </div>
          </template>
          <div v-else class="dim">总量 {{ fmtBytes(perf.mem_total_bytes) }}；占用采样不可用</div>
        </div>
        <div class="metric">
          <div class="metric-title">容器 / 镜像</div>
          <div class="metric-line">
            共 {{ perf.containers_total }}：
            <el-tag size="small" type="success">运行 {{ perf.containers_running }}</el-tag>
            <el-tag size="small" type="warning" v-if="perf.containers_paused">暂停 {{ perf.containers_paused }}</el-tag>
            <el-tag size="small" type="info">停止 {{ perf.containers_stopped }}</el-tag>
          </div>
          <div class="dim">镜像 {{ perf.images }} 个</div>
        </div>
        <div class="metric">
          <div class="metric-title">数据卷</div>
          <template v-if="perf.data_volume">
            <el-progress :percentage="Math.min(100, Math.round(volPercent()))" :color="usageColor(volPercent())" />
            <div class="dim">
              {{ fmtBytes(perf.data_volume.used_bytes) }} / {{ fmtBytes(perf.data_volume.total_bytes) }}
              （可用 {{ fmtBytes(perf.data_volume.avail_bytes) }}）
            </div>
          </template>
          <div v-else class="dim">数据卷用量统计不可用（非 Linux 部署）</div>
        </div>
      </div>

      <el-table :data="perf.containers" max-height="320" empty-text="当前没有容器" class="res-table">
        <el-table-column prop="name" label="容器" min-width="180">
          <template #default="{ row }">
            <span>{{ row.name }}</span>
            <div class="dim">{{ row.id }}</div>
          </template>
        </el-table-column>
        <el-table-column label="CPU" width="140">
          <template #default="{ row }">
            <el-tag size="small" :color="usageColor(row.cpu_percent / (perf!.ncpu || 1))" effect="dark">
              {{ row.cpu_percent.toFixed(2) }}%
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="内存" min-width="200">
          <template #default="{ row }">
            <div class="mem-line">
              <span>{{ fmtBytes(row.mem_used_bytes) }} / {{ fmtBytes(row.mem_limit_bytes) }}</span>
              <el-tag size="small" :type="row.mem_percent > 85 ? 'danger' : row.mem_percent > 60 ? 'warning' : 'success'">
                {{ row.mem_percent.toFixed(1) }}%
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="网络 ↓/↑" width="170">
          <template #default="{ row }">{{ fmtBytes(row.net_io_bytes[0]) }} / {{ fmtBytes(row.net_io_bytes[1]) }}</template>
        </el-table-column>
        <el-table-column label="磁盘读/写" width="170">
          <template #default="{ row }">{{ fmtBytes(row.block_io_bytes[0]) }} / {{ fmtBytes(row.block_io_bytes[1]) }}</template>
        </el-table-column>
        <el-table-column prop="pids" label="PIDs" width="70" />
      </el-table>
    </template>
    <div v-else-if="!perfLoading" class="dim">
      性能数据不可用：未配置容器运行时（Docker）或守护进程不可达。
    </div>
  </el-card>

  <!-- 当前活跃容器概览 -->
  <el-card class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>当前打开的容器</span>
        <el-tag size="small" type="primary">{{ active.length }}</el-tag>
      </div>
    </template>
    <el-table
      :data="active"
      v-loading="activeLoading"
      max-height="360"
      empty-text="当前没有运行中的容器"
    >
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
    <el-table
      :data="logs"
      v-loading="logLoading"
      max-height="480"
      empty-text="暂无容器操作记录"
    >
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
    <!-- 分页常驻（含总数与每页条数）：日志 append-only 只增不减，total 短暂为 0 也应保留翻页入口 -->
    <el-pagination
      background
      layout="total, sizes, prev, pager, next, jumper"
      :total="logTotal"
      :page-sizes="[20, 50, 100]"
      v-model:page-size="logPageSize"
      v-model:current-page="logPage"
      @current-change="loadLogs"
      @size-change="onLogSizeChange"
      class="pager"
    />
  </el-card>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.block { margin-bottom: 1.2rem; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.head-actions { display: flex; align-items: center; gap: 0.6rem; }
.dim { color: var(--el-text-color-placeholder); font-size: 0.8rem; }
.pager { margin-top: 1rem; }
.host-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1.2rem; margin-bottom: 1rem; }
.metric-title { font-weight: 600; margin-bottom: 0.5rem; }
.metric-line { display: flex; gap: 0.4rem; align-items: center; flex-wrap: wrap; }
.res-table { width: 100%; }
.mem-line { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; }
</style>