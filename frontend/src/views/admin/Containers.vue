<script setup lang="ts">
// 管理面板：容器管理。整体视图 = 主机性能面板（宿主 CPU/内存/容器与镜像计数/
// 数据卷用量 + 全容器资源占用表，可自动刷新）+ 当前活跃容器概览（含题目信息，
// 可强制销毁）+ 下方容器生命周期日志（append-only，记录每次启动/停止及失败原因）。
// 三张表均分页，默认 20 条/页；性能与活跃表在前端分页，日志表走服务端分页。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime, formatTime } from '@/lib/format'
import { useClientPagination } from '@/composables/useClientPagination'
import {
  adminInstanceApi, performanceApi,
  type AdminLabInstance, type InstanceLog, type PerformanceReport,
} from '../../api'

const props = defineProps<{ embedded?: boolean }>()

const perf = ref<PerformanceReport | null>(null)
const perfLoading = ref(true)
const perfError = ref('')
const autoRefresh = ref(true)
let perfTimer: number | undefined
let perfCatchUpTimer: number | undefined
let disposed = false

const active = ref<AdminLabInstance[]>([])
const activeLoading = ref(true)
const activeError = ref('')
const destroying = ref(0)

const logs = ref<InstanceLog[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(20)
const logLoading = ref(true)
const logError = ref('')

const perfContainers = computed(() => perf.value?.containers ?? [])
const {
  page: perfPage,
  pageSize: perfPageSize,
  total: perfTotal,
  items: perfItems,
} = useClientPagination(perfContainers)
const {
  page: activePage,
  pageSize: activePageSize,
  total: activeTotal,
  items: activeItems,
} = useClientPagination(active)

const REFRESH_MS = 10000
// 服务端返回缓存快照并后台刷新时，短暂轮询追平；docker stats 采样约 1-2s，
// 1.5s 一次不会造成请求堆积，也不会让旧 CPU/内存数字停留到下一个 10s 周期。
const PERF_CATCHUP_MS = 1500

async function loadPerf(silent = true) {
  if (!silent) perfLoading.value = true
  try {
    perf.value = await performanceApi.get()
    perfError.value = ''
    schedulePerfCatchUp()
  } catch (e) {
    // 自动刷新失败不打断页面，但必须把失败原因留在卡片里；
    // 否则会退化成“未配置容器运行时”的误导性空态。
    perfError.value = e instanceof Error ? e.message : '性能数据加载失败，请稍后重试'
    if (!silent) notify.error(perfError.value)
  } finally {
    perfLoading.value = false
  }
}

function schedulePerfCatchUp() {
  if (perfCatchUpTimer !== undefined) {
    window.clearTimeout(perfCatchUpTimer)
    perfCatchUpTimer = undefined
  }
  if (disposed || !perf.value?.refreshing) return
  perfCatchUpTimer = window.setTimeout(() => {
    perfCatchUpTimer = undefined
    loadPerf()
  }, PERF_CATCHUP_MS)
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
  if (perfCatchUpTimer !== undefined) {
    window.clearTimeout(perfCatchUpTimer)
    perfCatchUpTimer = undefined
  }
}

function onAutoRefreshChange() {
  schedulePerf()
  if (autoRefresh.value) loadPerf()
}

async function loadActive() {
  activeLoading.value = true
  activeError.value = ''
  active.value = []
  try {
    const resp = await adminInstanceApi.active()
    active.value = resp.items
  } catch (e) {
    activeError.value = e instanceof Error ? e.message : '活跃容器加载失败，请稍后重试'
  } finally {
    activeLoading.value = false
  }
}

async function loadLogs() {
  logLoading.value = true
  logError.value = ''
  logs.value = []
  logTotal.value = 0
  try {
    const resp = await adminInstanceApi.logs(logPage.value, logPageSize.value)
    logs.value = resp.items
    logTotal.value = resp.total
  } catch (e) {
    logError.value = e instanceof Error ? e.message : '容器日志加载失败，请稍后重试'
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
    await dialogs.confirm(
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
    notify.success(`实例 #${inst.id} 已销毁`)
    await load()
  } catch (e) {
    notify.error(`销毁失败：${(e as Error).message}`)
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
  return formatDateTime(ts)
}

function fmtTime(ts: string) {
  return formatTime(ts)
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

onUnmounted(() => {
  disposed = true
  stopPerf()
})
</script>

<template>
  <header v-if="!props.embedded" class="page-head">
    <div>
      <h2 class="page-title">容器<span class="accent">管理</span></h2>
      <p class="page-sub">
        主机性能、当前打开的容器与操作日志。强制销毁会回收真实容器资源；每次启动/停止均记录日志（含来源与失败原因）。
      </p>
    </div>
  </header>

  <!-- 主机性能面板 -->
  <UiCard class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>主机性能</span>
        <div class="head-actions">
          <span v-if="perf?.sampled_at" class="dim sample-time" role="status">
            采样于 {{ formatTime(perf.sampled_at) }}<template v-if="perf.refreshing"> · 刷新中…</template>
          </span>
          <span v-if="autoRefresh" class="dim">每 {{ REFRESH_MS / 1000 }}s 自动刷新</span>
          <UiSwitch v-model="autoRefresh" size="small" @change="onAutoRefreshChange" />
          <UiButton size="small" @click="loadPerf(false)">刷新</UiButton>
        </div>
      </div>
    </template>
    <UiLoadError v-if="perfError" compact :message="perfError" @retry="loadPerf(false)" class="card-error" />
    <UiLoadError
      v-else-if="perf?.warning"
      compact
      :message="perf.warning"
      @retry="loadPerf(false)"
      class="card-error"
    />
    <div v-if="!perf && perfLoading" class="perf-skeleton" role="status" aria-label="正在加载主机性能">
      <div v-for="n in 4" :key="n" class="sk-metric">
        <i class="sk-line short" />
        <i class="sk-line" />
        <i class="sk-line medium" />
      </div>
    </div>
    <template v-else-if="perf">
      <div class="host-grid">
        <div class="metric">
          <div class="metric-title">CPU（{{ perf.ncpu }} 核）</div>
          <div v-if="perf.host?.cpu_percent !== null && perf.host?.cpu_percent !== undefined">
            <UiProgress
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
            <UiProgress
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
            <UiTag size="small" type="success">运行 {{ perf.containers_running }}</UiTag>
            <UiTag size="small" type="warning" v-if="perf.containers_paused">暂停 {{ perf.containers_paused }}</UiTag>
            <UiTag size="small" type="info">停止 {{ perf.containers_stopped }}</UiTag>
          </div>
          <div class="dim">镜像 {{ perf.images }} 个</div>
        </div>
        <div class="metric">
          <div class="metric-title">数据卷</div>
          <template v-if="perf.data_volume">
            <UiProgress :percentage="Math.min(100, Math.round(volPercent()))" :color="usageColor(volPercent())" />
            <div class="dim">
              {{ fmtBytes(perf.data_volume.used_bytes) }} / {{ fmtBytes(perf.data_volume.total_bytes) }}
              （可用 {{ fmtBytes(perf.data_volume.avail_bytes) }}）
            </div>
          </template>
          <div v-else class="dim">数据卷用量统计不可用（非 Linux 部署）</div>
        </div>
      </div>

      <UiTable :data="perfItems" max-height="320" empty-text="当前没有容器" class="res-table">
        <UiTableColumn prop="name" label="容器" min-width="180">
          <template #default="{ row }">
            <span>{{ row.name }}</span>
            <div class="dim">{{ row.id }}</div>
          </template>
        </UiTableColumn>
        <UiTableColumn label="CPU" width="140">
          <template #default="{ row }">
            <UiTag size="small" :color="usageColor(row.cpu_percent / (perf!.ncpu || 1))" effect="dark">
              {{ row.cpu_percent.toFixed(2) }}%
            </UiTag>
          </template>
        </UiTableColumn>
        <UiTableColumn label="内存" min-width="200">
          <template #default="{ row }">
            <div class="mem-line">
              <span>{{ fmtBytes(row.mem_used_bytes) }} / {{ fmtBytes(row.mem_limit_bytes) }}</span>
              <UiTag size="small" :type="row.mem_percent > 85 ? 'danger' : row.mem_percent > 60 ? 'warning' : 'success'">
                {{ row.mem_percent.toFixed(1) }}%
              </UiTag>
            </div>
          </template>
        </UiTableColumn>
        <UiTableColumn label="网络 ↓/↑" width="170">
          <template #default="{ row }">{{ fmtBytes(row.net_io_bytes[0]) }} / {{ fmtBytes(row.net_io_bytes[1]) }}</template>
        </UiTableColumn>
        <UiTableColumn label="磁盘读/写" width="170">
          <template #default="{ row }">{{ fmtBytes(row.block_io_bytes[0]) }} / {{ fmtBytes(row.block_io_bytes[1]) }}</template>
        </UiTableColumn>
        <UiTableColumn prop="pids" label="PIDs" width="70" />
      </UiTable>
      <UiPagination
        background
        layout="total, sizes, prev, pager, next, jumper"
        :total="perfTotal"
        :page-sizes="[20, 50, 100]"
        v-model:page-size="perfPageSize"
        v-model:current-page="perfPage"
        show-single-page
        class="pager"
      />
    </template>
    <div v-else-if="!perfLoading && !perfError" class="dim">
      性能数据不可用：未配置容器运行时（Docker）或守护进程不可达。
    </div>
  </UiCard>

  <!-- 当前活跃容器概览 -->
  <UiCard class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>当前打开的容器</span>
        <UiTag size="small" type="primary">{{ activeTotal }}</UiTag>
      </div>
    </template>
    <UiLoadError v-if="activeError" compact :message="activeError" @retry="loadActive" class="card-error" />
    <UiTable
      v-else
      :data="activeItems"
      :loading="activeLoading"
      max-height="360"
      empty-text="当前没有运行中的容器"
    >
      <UiTableColumn prop="id" label="ID" width="70" fixed="left" />
      <UiTableColumn prop="username" label="用户" width="120" />
      <UiTableColumn label="题目" min-width="180">
        <template #default="{ row }">
          <span>{{ row.challenge_title || `题目 #${row.challenge_id}` }}</span>
          <div v-if="row.challenge_slug" class="dim">{{ row.challenge_slug }}</div>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <UiTag :type="statusType(row.status)" size="small">{{ row.status }}</UiTag>
        </template>
      </UiTableColumn>
      <UiTableColumn prop="endpoint" label="Endpoint" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <code v-if="row.endpoint">{{ row.endpoint }}</code>
          <span v-else class="dim">-</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="到期时间" width="100">
        <template #default="{ row }">{{ fmtTime(row.expire_at) }}</template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <UiButton
            size="small"
            type="danger"
            :loading="destroying === row.id"
            @click="forceDestroy(row)"
          >强制销毁</UiButton>
        </template>
      </UiTableColumn>
    </UiTable>
    <UiPagination
      background
      layout="total, sizes, prev, pager, next, jumper"
      :total="activeTotal"
      :page-sizes="[20, 50, 100]"
      v-model:page-size="activePageSize"
      v-model:current-page="activePage"
      show-single-page
      class="pager"
    />
  </UiCard>

  <!-- 容器生命周期日志 -->
  <UiCard class="block" shadow="never">
    <template #header>
      <div class="card-head">
        <span>容器日志</span>
        <UiButton size="small" @click="load">刷新</UiButton>
      </div>
    </template>
    <UiLoadError v-if="logError" compact :message="logError" @retry="loadLogs" class="card-error" />
    <UiTable
      v-else
      :data="logs"
      :loading="logLoading"
      max-height="480"
      empty-text="暂无容器操作记录"
    >
      <UiTableColumn label="时间" width="170" fixed="left">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </UiTableColumn>
      <UiTableColumn prop="instance_id" label="实例" width="80" />
      <UiTableColumn prop="username" label="用户" width="120" />
      <UiTableColumn label="题目" min-width="160">
        <template #default="{ row }">
          <span>{{ row.challenge_title || `题目 #${row.challenge_id}` }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="动作" width="90">
        <template #default="{ row }">
          <UiTag :type="row.action === 'started' ? 'success' : 'info'" size="small">
            {{ row.action === 'started' ? '启动' : '停止' }}
          </UiTag>
        </template>
      </UiTableColumn>
      <UiTableColumn label="来源" width="90">
        <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
      </UiTableColumn>
      <UiTableColumn prop="status" label="状态" width="90" />
      <UiTableColumn prop="detail" label="详情" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.detail">{{ row.detail }}</span>
          <span v-else class="dim">-</span>
        </template>
      </UiTableColumn>
    </UiTable>
    <!-- 分页器常驻：默认 20 条/页，有数据即可见总数与每页条数。 -->
    <UiPagination
      background
      layout="total, sizes, prev, pager, next, jumper"
      :total="logTotal"
      :page-sizes="[20, 50, 100]"
      v-model:page-size="logPageSize"
      v-model:current-page="logPage"
      @current-change="loadLogs"
      @size-change="onLogSizeChange"
      show-single-page
      class="pager"
    />
  </UiCard>
</template>

<style scoped>
.block { margin-bottom: var(--space-5); }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.head-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: var(--space-2); }
.sample-time { white-space: nowrap; }
@media (max-width: 640px) {
  .card-head { align-items: flex-start; flex-wrap: wrap; row-gap: var(--space-2); }
  .head-actions { flex-basis: 100%; justify-content: flex-start; }
}
.dim { color: var(--muted-foreground); font-size: var(--text-xs); }
.pager { margin-top: var(--space-4); }
.card-error { margin-bottom: var(--space-4); }
.host-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: var(--space-5); margin-bottom: var(--space-4); }
.metric-title { font-weight: 600; margin-bottom: var(--space-2); }
.metric-line { display: flex; gap: var(--space-1); align-items: center; flex-wrap: wrap; }
.res-table { width: 100%; }
.mem-line { display: flex; justify-content: space-between; align-items: center; gap: var(--space-2); }
.perf-skeleton { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: var(--space-5); }
.sk-metric { display: grid; gap: var(--space-2); padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); }
.sk-line { display: block; width: 100%; height: 12px; border-radius: 4px; background: var(--muted); }
.sk-line.short { width: 32%; }
.sk-line.medium { width: 68%; }
.perf-skeleton .sk-line { animation: perf-skeleton-pulse 1.4s ease-in-out infinite; }
@keyframes perf-skeleton-pulse { 50% { opacity: .45; } }
</style>