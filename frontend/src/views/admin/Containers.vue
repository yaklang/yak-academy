<script setup lang="ts">
// 管理面板：容器管理。主机资源概览（CPU/内存/容器与镜像/数据卷 + 全容器资源）
// 可自动刷新；下方是当前打开的容器与生命周期日志。强制销毁会真实回收容器。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { formatBytes, formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  adminInstanceApi,
  performanceApi,
  type AdminLabInstance,
  type InstanceLog,
  type PerformanceReport,
} from '../../api'

const perf = ref<PerformanceReport | null>(null)
const perfLoading = ref(true)
const perfError = ref('')
const autoRefresh = ref(true)
let perfTimer: number | undefined

const active = ref<AdminLabInstance[]>([])
const activeLoading = ref(true)
const activeError = ref('')
const destroying = ref(0)
const destroyTarget = ref<AdminLabInstance | null>(null)

const logs = ref<InstanceLog[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(20)
const logLoading = ref(true)
const logError = ref('')

const REFRESH_MS = 10000

const cpuPercent = computed(() => {
  const value = perf.value?.host?.cpu_percent
  return value === null || value === undefined ? null : Math.min(100, value)
})

const memPercent = computed(() => {
  const host = perf.value?.host
  if (!host || host.mem_used_bytes === null || !host.mem_total_bytes) return null
  return Math.min(100, (host.mem_used_bytes / host.mem_total_bytes) * 100)
})

const volumePercent = computed(() => {
  const volume = perf.value?.data_volume
  if (!volume || !volume.total_bytes) return null
  return Math.min(100, (volume.used_bytes / volume.total_bytes) * 100)
})

function usageTone(percent: number | null) {
  if (percent === null) return 'neutral'
  if (percent > 85) return 'danger'
  if (percent > 60) return 'warning'
  return 'success'
}

function barWidth(percent: number | null) {
  return `${Math.round((percent ?? 0) * 10) / 10}%`
}

async function loadPerf(silent = true) {
  if (!silent) perfLoading.value = true
  try {
    perf.value = await performanceApi.get()
    perfError.value = ''
  } catch (e) {
    // 自动刷新失败不打断页面，但必须把失败原因留在面板里。
    perfError.value = e instanceof Error ? e.message : '性能数据加载失败，请稍后重试'
    if (!silent) notify.error(perfError.value)
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

function askDestroy(instance: AdminLabInstance) {
  destroyTarget.value = instance
}

async function confirmDestroy() {
  const target = destroyTarget.value
  if (!target) return
  destroying.value = target.id
  try {
    await adminInstanceApi.destroy(target.id)
    notify.success(`已销毁 ${target.username} 的容器`)
    destroyTarget.value = null
    await load()
  } catch (e) {
    notify.error(`销毁失败：${(e as Error).message}`)
  } finally {
    destroying.value = 0
  }
}

function sourceLabel(source: string) {
  if (source === 'admin') return '管理员'
  if (source === 'reaper') return '超时回收'
  return '用户'
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(() => {
  load()
  schedulePerf()
})

onUnmounted(stopPerf)
</script>

<template>
  <PageHeader
    title="容器管理"
    description="查看主机资源、当前打开的容器与生命周期日志。强制销毁会立即回收真实容器，学员的实验环境随之失效。"
  />

  <DataTablePanel
    title="主机性能"
    :description="perfError ? undefined : `每 ${REFRESH_MS / 1000} 秒自动刷新；数据来自容器运行时。`"
  >
    <template #toolbar>
      <label class="auto-refresh">
        <Switch v-model="autoRefresh" aria-label="自动刷新主机性能" @update:model-value="onAutoRefreshChange" />
        <span>自动刷新</span>
      </label>
      <Button type="button" variant="outline" @click="loadPerf(false)">刷新</Button>
    </template>

    <EmptyState
      v-if="perfError"
      compact
      tone="error"
      title="主机性能加载失败"
      :description="perfError"
      action-label="重试"
      @action="loadPerf(false)"
    />

    <div v-else-if="!perf && perfLoading" class="host-metrics host-metrics--loading" aria-busy="true" aria-label="正在加载主机性能">
      <div v-for="n in 4" :key="n" class="host-metric">
        <Skeleton class="host-skeleton host-skeleton--label" />
        <Skeleton class="host-skeleton host-skeleton--value" />
        <Skeleton class="host-skeleton" />
      </div>
    </div>

    <template v-else-if="perf">
      <div class="host-metrics">
        <div class="host-metric">
          <p class="host-metric__label">CPU（{{ perf.ncpu }} 核）</p>
          <p class="host-metric__value">{{ cpuPercent === null ? '—' : `${cpuPercent.toFixed(1)}%` }}</p>
          <div class="usage-track" role="presentation">
            <span class="usage-fill" :data-tone="usageTone(cpuPercent)" :style="{ width: barWidth(cpuPercent) }" />
          </div>
          <p class="host-metric__hint">{{ cpuPercent === null ? '宿主占用采样不可用' : '宿主占用（多核可超 100%）' }}</p>
        </div>

        <div class="host-metric">
          <p class="host-metric__label">内存</p>
          <p class="host-metric__value">{{ memPercent === null ? '—' : `${memPercent.toFixed(1)}%` }}</p>
          <div class="usage-track" role="presentation">
            <span class="usage-fill" :data-tone="usageTone(memPercent)" :style="{ width: barWidth(memPercent) }" />
          </div>
          <p class="host-metric__hint">
            {{ memPercent === null
              ? `总量 ${formatBytes(perf.mem_total_bytes)}；占用采样不可用`
              : `${formatBytes(perf.host!.mem_used_bytes)} / ${formatBytes(perf.host!.mem_total_bytes)}` }}
          </p>
        </div>

        <div class="host-metric">
          <p class="host-metric__label">容器 / 镜像</p>
          <p class="host-metric__value">{{ perf.containers_total }}</p>
          <div class="host-metric__chips">
            <StatusTag kind="container" value="running" :label="`运行 ${perf.containers_running}`" />
            <StatusTag v-if="perf.containers_paused" kind="awd" value="paused" :label="`暂停 ${perf.containers_paused}`" />
            <StatusTag kind="container" value="stopped" :label="`停止 ${perf.containers_stopped}`" />
          </div>
          <p class="host-metric__hint">镜像 {{ perf.images }} 个</p>
        </div>

        <div class="host-metric">
          <p class="host-metric__label">数据卷</p>
          <p class="host-metric__value">{{ volumePercent === null ? '—' : `${volumePercent.toFixed(1)}%` }}</p>
          <div class="usage-track" role="presentation">
            <span class="usage-fill" :data-tone="usageTone(volumePercent)" :style="{ width: barWidth(volumePercent) }" />
          </div>
          <p class="host-metric__hint">
            {{ volumePercent === null
              ? '数据卷用量统计不可用'
              : `${formatBytes(perf.data_volume!.used_bytes)} / ${formatBytes(perf.data_volume!.total_bytes)} · 可用 ${formatBytes(perf.data_volume!.avail_bytes)}` }}
          </p>
        </div>
      </div>

      <div class="host-resources">
        <h3 class="host-resources__title">全部容器资源</h3>
        <UiTable
          :data="perf.containers"
          max-height="320"
          aria-label="全部容器资源"
        >
          <UiTableColumn label="容器" min-width="180">
            <template #default="{ row }">
              <span class="container-name">{{ row.name }}</span>
              <span class="container-id">{{ row.id }}</span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="CPU" width="100">
            <template #default="{ row }">
              <span class="resource-value">{{ row.cpu_percent.toFixed(2) }}%</span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="内存" min-width="200">
            <template #default="{ row }">
              <span class="resource-value">
                {{ formatBytes(row.mem_used_bytes) }} / {{ formatBytes(row.mem_limit_bytes) }}
                <span class="resource-percent" :data-tone="usageTone(row.mem_percent)">{{ row.mem_percent.toFixed(1) }}%</span>
              </span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="网络 下行 / 上行" width="180">
            <template #default="{ row }">
              <span class="resource-value">{{ formatBytes(row.net_io_bytes[0]) }} / {{ formatBytes(row.net_io_bytes[1]) }}</span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="磁盘 读 / 写" width="180">
            <template #default="{ row }">
              <span class="resource-value">{{ formatBytes(row.block_io_bytes[0]) }} / {{ formatBytes(row.block_io_bytes[1]) }}</span>
            </template>
          </UiTableColumn>
          <UiTableColumn prop="pids" label="进程数" width="90" />
          <template #empty>
            <EmptyState compact title="当前没有容器" description="学员打开实验环境后，容器会出现在这里。" />
          </template>
        </UiTable>
      </div>
    </template>

    <EmptyState
      v-else
      compact
      title="容器运行时不可用"
      description="未配置容器运行时（Docker）或守护进程不可达，暂时无法查看主机资源。"
      action-label="重试"
      @action="loadPerf(false)"
    />
  </DataTablePanel>

  <DataTablePanel
    v-if="!activeError"
    class="containers-panel"
    title="当前打开的容器"
    :count="active.length"
    description="强制销毁会立即回收容器；学员需要重新打开题目才能获得新环境。"
  >
    <UiTable
      :data="active"
      :loading="activeLoading"
      max-height="360"
      aria-label="当前打开的容器"
    >
      <UiTableColumn label="学员" width="140">
        <template #default="{ row }">
          <span class="container-user">{{ row.username }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="题目" min-width="220">
        <template #default="{ row }">
          <span class="container-title">{{ row.challenge_title || '未命名题目' }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="container" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="访问地址" min-width="200">
        <template #default="{ row }">
          <code v-if="row.endpoint" class="container-endpoint">{{ row.endpoint }}</code>
          <span v-else class="container-muted">尚未分配</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="到期时间" width="170">
        <template #default="{ row }">
          <span class="container-time">{{ fmt(row.expire_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="110">
        <template #default="{ row }">
          <Button variant="link" size="sm" type="button" class="destroy-action" @click="askDestroy(row)">
            强制销毁
          </Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState compact title="当前没有打开的容器" description="学员打开实验环境后会出现在这里。" />
      </template>
    </UiTable>
  </DataTablePanel>

  <EmptyState
    v-if="activeError"
    tone="error"
    title="活跃容器加载失败"
    :description="activeError"
    action-label="重试"
    @action="loadActive"
  />

  <DataTablePanel
    v-if="!logError"
    class="containers-panel"
    title="容器日志"
    :count="logTotal"
    description="记录每次启动与停止，含来源和失败原因。"
  >
    <template #toolbar>
      <Button type="button" variant="outline" @click="loadLogs">刷新</Button>
    </template>

    <UiTable
      :data="logs"
      :loading="logLoading"
      max-height="480"
      aria-label="容器生命周期日志"
    >
      <UiTableColumn label="时间" width="170">
        <template #default="{ row }">
          <span class="container-time">{{ fmt(row.created_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="学员" width="140">
        <template #default="{ row }">
          <span class="container-user">{{ row.username }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="题目" min-width="200">
        <template #default="{ row }">
          <span class="container-title">{{ row.challenge_title || '未命名题目' }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="动作" width="90">
        <template #default="{ row }">
          <StatusTag kind="container" :value="row.action === 'started' ? 'running' : 'stopped'" :label="row.action === 'started' ? '启动' : '停止'" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="来源" width="100">
        <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="container" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="详情" min-width="180">
        <template #default="{ row }">
          <span v-if="row.detail" class="container-detail">{{ row.detail }}</span>
          <span v-else class="container-muted">—</span>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState compact title="暂无容器记录" description="学员启动实验环境后，这里会记录启动与停止。" />
      </template>
    </UiTable>
    <template v-if="logTotal > 0" #footer>
      <UiPagination
        v-model:page-size="logPageSize"
        v-model:current-page="logPage"
        background
        layout="total, sizes, prev, pager, next"
        :total="logTotal"
        :page-sizes="[20, 50, 100]"
        @current-change="loadLogs"
        @size-change="onLogSizeChange"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-if="logError"
    tone="error"
    title="容器日志加载失败"
    :description="logError"
    action-label="重试"
    @action="loadLogs"
  />

  <ConfirmDialog
    :open="!!destroyTarget"
    title="强制销毁容器"
    :description="destroyTarget ? `将回收 ${destroyTarget.username} 的容器「${destroyTarget.challenge_title || '未命名题目'}」，该学员的实验环境会立即失效。` : ''"
    confirm-label="销毁"
    tone="danger"
    :loading="destroying > 0"
    @update:open="!$event && (destroyTarget = null)"
    @confirm="confirmDestroy"
  />
</template>

<style scoped>
.containers-panel {
  margin-top: var(--space-4);
}

.auto-refresh {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--text-meta);
}

.host-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0;
}

.host-metric {
  min-width: 0;
  padding: var(--space-4);
  border-right: 1px solid var(--border);
}

.host-metric:last-child {
  border-right: 0;
}

.host-metric__label {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.host-metric__value {
  margin-top: var(--space-1);
  color: var(--text-primary);
  font-size: var(--text-metric);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: var(--leading-tight);
}

.usage-track {
  height: 6px;
  margin-top: var(--space-2);
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  overflow: hidden;
}

.usage-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--status-success);
}

.usage-fill[data-tone='warning'] {
  background: var(--status-warning);
}

.usage-fill[data-tone='danger'] {
  background: var(--status-danger);
}

.usage-fill[data-tone='neutral'] {
  background: var(--border-strong);
}

.host-metric__hint {
  margin-top: var(--space-2);
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  line-height: var(--leading-normal);
}

.host-metric__chips {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
  margin-top: var(--space-2);
}

.host-metrics--loading .host-metric {
  display: grid;
  gap: var(--space-2);
}

.host-skeleton {
  width: 100%;
  height: 12px;
}

.host-skeleton--label {
  width: 40%;
}

.host-skeleton--value {
  width: 60%;
  height: 24px;
}

.host-resources {
  border-top: 1px solid var(--border);
}

.host-resources__title {
  padding: var(--space-3) var(--space-4) var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.container-name,
.container-user,
.container-title {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 500;
  overflow-wrap: anywhere;
}

.container-id,
.container-time,
.container-detail {
  display: block;
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.container-id {
  font-family: var(--font-mono);
  font-size: var(--text-micro);
}

.container-endpoint {
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  overflow-wrap: anywhere;
}

.container-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.resource-value {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.resource-percent {
  margin-left: var(--space-1);
  color: var(--status-success);
  font-weight: 600;
}

.resource-percent[data-tone='warning'] {
  color: var(--status-warning);
}

.resource-percent[data-tone='danger'] {
  color: var(--status-danger);
}

.destroy-action {
  color: var(--status-danger);
}

@media (max-width: 1023px) {
  .host-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .host-metric:nth-child(2) {
    border-right: 0;
  }

  .host-metric:nth-child(-n + 2) {
    border-bottom: 1px solid var(--border);
  }
}

@media (max-width: 560px) {
  .host-metrics {
    grid-template-columns: 1fr;
  }

  .host-metric {
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .host-metric:last-child {
    border-bottom: 0;
  }
}
</style>