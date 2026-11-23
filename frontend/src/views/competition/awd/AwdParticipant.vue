<script setup lang="ts">
// AWD 参赛端（满屏独立布局）：回合倒计时、我的服务、目标目录、攻击盒凭据、
// 提交 flag、排行榜与运营事件。与 CTF 首页完全独立，只共享竞赛入口元数据。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import {
  competitionAwdApi,
  type AwdFinalRankingView,
  type AwdFinalStandingView,
  type AwdParticipantView,
  type AwdReportView,
  type AwdStandingView,
} from '../../../api'
import { ApiError } from '../../../api/client'
import { useAuthStore } from '../../../stores/auth'
import { useCompetitionStore } from '../../../stores/competition'

const props = defineProps<{ competitionId: number }>()
const router = useRouter()
const auth = useAuthStore()
const compStore = useCompetitionStore()

const view = ref<AwdParticipantView | null>(null)
const loading = ref(true)
const loadError = ref('')
const activeTab = ref('overview')
const flagInput = ref('')
const submitting = ref(false)
const report = ref<AwdReportView | null>(null)
const reportContent = ref('')
const reportLoading = ref(false)
const reportSaving = ref(false)
const reportFileInput = ref<HTMLInputElement | null>(null)
const finalRanking = ref<AwdFinalRankingView | null>(null)
const finalLoading = ref(false)
const finalError = ref('')
const nowTs = ref(Date.now())
let tick: number | undefined
let poll: number | undefined

const comp = computed(() => view.value?.competition ?? null)
const game = computed(() => view.value?.game ?? null)
const round = computed(() => view.value?.round ?? null)
const running = computed(() => comp.value?.status === 'running' && game.value?.status === 'running')
const reportEditable = computed(() => game.value?.status === 'ended')
const isAdmin = computed(() => auth.user?.global_role === 'admin')
const lockedHere = computed(() => running.value && comp.value?.kind === 'invitational' && !isAdmin.value)
const platformHome = computed(() => (isAdmin.value ? '/admin/tutorials' : '/learn'))
const platformHomeLabel = computed(() => (isAdmin.value ? '返回管理端' : '返回平台'))
const roundRemaining = computed(() => {
  if (!round.value) return 0
  return Math.max(0, Math.floor((Date.parse(round.value.ends_at) - nowTs.value) / 1000))
})
const roundCountdown = computed(() => formatDuration(roundRemaining.value))
const roundPercent = computed(() => {
  if (!round.value) return 0
  const start = Date.parse(round.value.starts_at)
  const end = Date.parse(round.value.ends_at)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return 0
  return Math.min(100, Math.max(0, Math.round(((nowTs.value - start) / (end - start)) * 100)))
})
const myStanding = computed<AwdStandingView | null>(() => {
  const current = view.value
  if (!current) return null
  return current.standings.find(
    (row: AwdStandingView) =>
      row.competitor_type === current.competitor_type && row.competitor_id === current.competitor_id,
  ) ?? null
})
const statusLabel = computed(() => {
  switch (game.value?.status) {
    case 'running':
      return '进行中'
    case 'paused':
      return '已暂停'
    case 'ended':
      return '已结束'
    default:
      return '未开始'
  }
})
const statusType = computed(() => {
  switch (game.value?.status) {
    case 'running':
      return 'success'
    case 'paused':
      return 'warning'
    default:
      return 'info'
  }
})
const kindLabel = computed(() => (comp.value?.kind === 'public' ? '公开赛' : '邀请赛'))
const finalFormula = computed(() => {
  const weights = finalRanking.value?.weights
  const awd = weights?.awd_weight ?? 70
  const report = weights?.report_weight ?? 30
  return `最终分 = (${awd} × AWD 分 / 榜首 AWD 分 × 100 + ${report} × 战报分) / ${awd + report}；同分依次比较战报分、最后有效攻击时间。`
})

function formatDuration(seconds: number) {
  if (seconds <= 0) return '00:00'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  const mm = String(m).padStart(2, '0')
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

function shortDateTime(value: string) {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

async function loadView(silent = false) {
  if (!silent) {
    loading.value = true
    loadError.value = ''
  }
  try {
    view.value = await competitionAwdApi.view(props.competitionId)
    if (lockedHere.value) compStore.lock(props.competitionId)
    if (view.value.game.status === 'ended' && !finalRanking.value && !finalLoading.value) {
      void loadFinalRanking()
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return
    if (!silent || !view.value) {
      loadError.value = e instanceof Error ? e.message : 'AWD 数据加载失败，请稍后重试'
    }
    if (!view.value && compStore.lockedCompetitionId === props.competitionId) compStore.unlock()
  } finally {
    if (!silent) loading.value = false
  }
}

async function submitFlag() {
  const value = flagInput.value.trim()
  if (!value || submitting.value) return
  submitting.value = true
  try {
    const receipt = await competitionAwdApi.submitAttack(props.competitionId, value)
    if (receipt.status === 'rejected') {
      notify.error(`提交被拒绝：${reasonLabel(receipt.reason)}`)
    } else {
      notify.success('flag 已提交，等待判定')
    }
    flagInput.value = ''
    await loadView(true)
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

function reasonLabel(reason?: string) {
  const labels: Record<string, string> = {
    'empty flag': 'flag 不能为空',
    'flag not found': 'flag 不存在',
    'flag expired': 'flag 已过期',
    'flag is not active yet': 'flag 尚未生效',
    'flag already captured': '该 flag 已被首杀',
    'cannot attack your own service': '不能攻击自己的服务',
    'victim service not found': '目标服务不存在',
    'game is not running': '比赛未进行中',
  }
  return (reason && labels[reason]) || reason || '未知原因'
}

async function copy(text: string, message: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    notify.success(message)
  } catch {
    notify.error('复制失败，请手动选择文本')
  }
}

async function loadReport() {
  reportLoading.value = true
  try {
    const resp = await competitionAwdApi.myReport(props.competitionId)
    report.value = resp.report
    reportContent.value = resp.report?.content_md ?? ''
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    reportLoading.value = false
  }
}

async function loadFinalRanking() {
  finalLoading.value = true
  finalError.value = ''
  try {
    finalRanking.value = await competitionAwdApi.finalRanking(props.competitionId)
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) {
      finalError.value = e instanceof Error ? e.message : '最终排名加载失败'
    }
  } finally {
    finalLoading.value = false
  }
}

async function saveReport() {
  if (!reportContent.value.trim() || reportSaving.value) {
    if (!reportContent.value.trim()) notify.warning('请填写战报内容')
    return
  }
  reportSaving.value = true
  try {
    report.value = await competitionAwdApi.saveReport(props.competitionId, reportContent.value)
    reportContent.value = report.value.content_md
    notify.success('战报已保存')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    reportSaving.value = false
  }
}

async function onReportZip(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || reportSaving.value) return
  reportSaving.value = true
  try {
    report.value = await competitionAwdApi.uploadReportZip(props.competitionId, file)
    notify.success('战报附件已上传')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    reportSaving.value = false
    input.value = ''
  }
}

function onTab(name: string | number) {
  if (name === 'report' && !report.value && !reportLoading.value) void loadReport()
  if (name === 'final' && !finalLoading.value) void loadFinalRanking()
}

interface AwdPortView {
  name: string
  port: number
  protocol: string
}

function parsePorts(json: string): AwdPortView[] {
  try {
    const parsed = JSON.parse(json)
    if (!Array.isArray(parsed)) return []
    return parsed.map((item) => ({
      name: String(item?.name ?? 'port'),
      port: Number(item?.port) || 0,
      protocol: String(item?.protocol ?? 'tcp'),
    }))
  } catch {
    return []
  }
}

function portsLabel(json: string) {
  const ports = parsePorts(json)
  return ports.length ? ports.map((p) => `${p.name} ${p.port}/${p.protocol}`).join(' · ') : '—'
}

function targetAddress(host: string, json: string) {
  const port = parsePorts(json)[0]
  return port && port.port > 0 ? `${host}:${port.port}` : host
}

function serviceStatusLabel(status: string) {
  const labels: Record<string, string> = {
    running: '运行中',
    pending: '部署中',
    stopped: '已停止',
    failed: '失败',
  }
  return labels[status] ?? status ?? '未知'
}

function incidentKindLabel(kind: string) {
  const labels: Record<string, string> = {
    check_fail: '检查失败',
    put_fail: 'PUT 失败',
    flag_leak: 'flag 泄漏告警',
    manual: '人工告警',
  }
  return labels[kind] ?? kind
}

function attackStatusLabel(status: string) {
  const labels: Record<string, string> = {
    pending: '判定中',
    accepted: '已接受',
    rejected: '已拒绝',
  }
  return labels[status] ?? status
}

function standingName(row: AwdStandingView) {
  return row.competitor_name || `#${row.competitor_id}`
}

function isMe(row: AwdStandingView) {
  return row.competitor_type === view.value?.competitor_type && row.competitor_id === view.value?.competitor_id
}

function finalName(row: AwdFinalStandingView) {
  return row.competitor_name || `#${row.competitor_id}`
}

function isMeFinal(row: AwdFinalStandingView) {
  return row.competitor_type === view.value?.competitor_type && row.competitor_id === view.value?.competitor_id
}

async function logout() {
  await auth.logout()
  router.replace('/login')
}

function returnToPlatform() {
  router.push(platformHome.value)
}

onMounted(async () => {
  await loadView()
  tick = window.setInterval(() => (nowTs.value = Date.now()), 1000)
  poll = window.setInterval(() => loadView(true), 10000)
})

onUnmounted(() => {
  if (tick) window.clearInterval(tick)
  if (poll) window.clearInterval(poll)
})
</script>

<template>
  <div class="awd-home">
    <header class="awd-header">
      <div class="awd-header-inner">
        <div class="awd-identity">
          <span class="awd-mark" aria-hidden="true">
            <svg width="22" height="24" viewBox="0 0 27 30" fill="none">
              <path
                d="M3 4L13.5 12.5L24 4M13.5 12.5V27M3 12L9 17M24 12L18 17"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="square"
              />
            </svg>
          </span>
          <div class="awd-title">
            <p class="awd-eyebrow">YAK ACADEMY · AWD</p>
            <div class="awd-name-row">
              <h1>{{ comp?.name ?? 'AWD 攻防赛' }}</h1>
              <UiTag v-if="game" :type="statusType" size="small">{{ statusLabel }}</UiTag>
              <UiTag v-if="comp" size="small" type="info">{{ kindLabel }}</UiTag>
              <UiTag v-if="view" size="small" type="warning">AWD 攻防赛</UiTag>
            </div>
            <p v-if="comp" class="awd-window mono">
              {{ shortDateTime(comp.start_time) }} → {{ shortDateTime(comp.end_time) }}
            </p>
          </div>
        </div>

        <div class="awd-clock">
          <div class="awd-clock-line">
            <span>回合倒计时</span>
            <b class="mono">{{ round ? roundCountdown : '—' }}</b>
          </div>
          <UiProgress :percentage="roundPercent" :show-text="false" />
          <span class="awd-clock-sub">
            {{ round ? `第 ${round.number} 回合` : '尚未开启回合' }}
            <template v-if="view?.team_name"> · {{ view.team_name }}</template>
          </span>
        </div>

        <div class="awd-user">
          <span class="awd-user-name">{{ auth.user?.username }}</span>
          <UiButton v-if="!lockedHere" size="small" variant="ghost" @click="returnToPlatform">
            {{ platformHomeLabel }}
          </UiButton>
          <UiButton size="small" variant="ghost" @click="logout">登出</UiButton>
        </div>
      </div>
    </header>

    <UiAlert
      v-if="loadError"
      type="error"
      :closable="false"
      :title="loadError"
      description="当前显示上一次成功加载的数据。"
      class="awd-refresh-error"
    >
      <UiButton size="small" @click="loadView()">重试</UiButton>
    </UiAlert>

    <main v-if="loading && !view" class="awd-body">
      <UiSkeleton :rows="7" />
    </main>
    <main v-else-if="loadError && !view" class="awd-body awd-center">
      <UiLoadError title="无法加载 AWD 比赛" :message="loadError" @retry="loadView()" />
    </main>
    <main v-else-if="view" class="awd-body">
      <section class="awd-kpis" aria-label="AWD 关键指标">
        <div class="awd-kpi">
          <p>当前排名</p>
          <b class="mono">{{ myStanding ? `#${myStanding.rank}` : '—' }}</b>
          <span>{{ view.competitor_name }}</span>
        </div>
        <div class="awd-kpi">
          <p>总分</p>
          <b class="mono">{{ myStanding?.score ?? 0 }}</b>
          <span>攻击 + SLA − 防守 − checkdown</span>
        </div>
        <div class="awd-kpi">
          <p>攻击分</p>
          <b class="mono">{{ myStanding?.attack_score ?? 0 }}</b>
          <span>首杀 flag 后按攻击池结算</span>
        </div>
        <div class="awd-kpi">
          <p>防守 / SLA</p>
          <b class="mono">−{{ myStanding?.defense_loss ?? 0 }} / +{{ myStanding?.sla_score ?? 0 }}</b>
          <span>服务可用性与被攻破扣分</span>
        </div>
      </section>

      <div class="awd-grid">
        <UiCard header="提交 flag">
          <div class="awd-submit">
            <UiInput
              v-model="flagInput"
              placeholder="flag{...}"
              :disabled="!running || submitting"
              @keyup.enter="submitFlag"
            />
            <UiButton
              type="primary"
              :loading="submitting"
              :disabled="!running || !flagInput.trim()"
              @click="submitFlag"
            >
              提交
            </UiButton>
          </div>
          <p class="awd-hint">
            {{
              running
                ? '提交其他竞域单位服务上的 flag；同一 flag 只记首个有效攻击。'
                : '比赛未进行中，暂不可提交 flag。'
            }}
          </p>
        </UiCard>

        <UiCard header="SSH 攻击盒">
          <template v-if="view.attack_box">
            <dl class="awd-pairs">
              <div>
                <dt>地址</dt>
                <dd class="mono">{{ view.attack_box.ssh_host }}:{{ view.attack_box.ssh_port }}</dd>
              </div>
              <div>
                <dt>用户</dt>
                <dd class="mono">{{ view.attack_box.ssh_user }}</dd>
              </div>
              <div>
                <dt>状态</dt>
                <dd>{{ serviceStatusLabel(view.attack_box.status) }}</dd>
              </div>
            </dl>
            <div class="awd-inline">
              <code class="awd-command">{{ view.attack_box.ssh_command || '—' }}</code>
              <UiButton
                size="small"
                :disabled="!view.attack_box.ssh_command"
                @click="copy(view.attack_box?.ssh_command ?? '', 'SSH 命令已复制')"
              >
                复制命令
              </UiButton>
            </div>
            <div class="awd-inline">
              <code class="awd-command">{{ view.attack_box.ssh_password || '未配置密码' }}</code>
              <UiButton
                size="small"
                :disabled="!view.attack_box.ssh_password"
                @click="copy(view.attack_box?.ssh_password ?? '', '密码已复制')"
              >
                复制密码
              </UiButton>
            </div>
            <p class="awd-hint">登录后经竞赛内部网络访问目标目录中的服务地址。</p>
          </template>
          <UiEmpty v-else description="攻击盒尚未部署；裁判部署后刷新即可获得 SSH 凭据。" />
        </UiCard>
      </div>

      <UiTabs v-model="activeTab" @tab-change="onTab">
        <UiTabPane label="概览" name="overview">
          <div class="awd-overview">
            <UiCard header="我的服务">
              <UiTable :data="view.my_services" row-key="team_service_id" empty-text="尚未部署靶机服务">
                <UiTableColumn prop="name" label="服务" min-width="140" align="left">
                  <template #default="{ row }">{{ row.name || row.slug }}</template>
                </UiTableColumn>
                <UiTableColumn prop="internal_host" label="内部地址" min-width="160" align="left" />
                <UiTableColumn label="端口" min-width="170" align="left">
                  <template #default="{ row }">{{ portsLabel(row.ports_json) }}</template>
                </UiTableColumn>
                <UiTableColumn label="状态" width="100" align="center">
                  <template #default="{ row }">
                    <UiTag :type="row.status === 'running' ? 'success' : row.status === 'failed' ? 'danger' : 'info'" size="small">
                      {{ serviceStatusLabel(row.status) }}
                    </UiTag>
                  </template>
                </UiTableColumn>
                <UiTableColumn label="最近检查" min-width="150" align="left">
                  <template #default="{ row }">
                    {{ row.last_check ? `${row.last_check.action} · ${row.last_check.status}` : '—' }}
                  </template>
                </UiTableColumn>
              </UiTable>
            </UiCard>
            <UiCard header="最近运营事件">
              <UiEmpty v-if="!view.incidents.length" description="暂无运营事件" />
              <ul v-else class="awd-list">
                <li v-for="incident in view.incidents" :key="incident.id" class="awd-list-item">
                  <div class="awd-list-main">
                    <UiTag size="small" type="warning">{{ incidentKindLabel(incident.kind) }}</UiTag>
                    <span class="awd-list-detail">{{ incident.detail }}</span>
                  </div>
                  <span class="awd-dim mono">{{ fmt(incident.created_at) }}</span>
                </li>
              </ul>
            </UiCard>
          </div>
        </UiTabPane>

        <UiTabPane label="我的服务" name="services">
          <UiTable :data="view.my_services" row-key="team_service_id" empty-text="尚未部署靶机服务">
            <UiTableColumn prop="name" label="服务" min-width="140" align="left">
              <template #default="{ row }">{{ row.name || row.slug }}</template>
            </UiTableColumn>
            <UiTableColumn prop="internal_host" label="内部地址" min-width="160" align="left" />
            <UiTableColumn label="端口" min-width="180" align="left">
              <template #default="{ row }">{{ portsLabel(row.ports_json) }}</template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="row.status === 'running' ? 'success' : row.status === 'failed' ? 'danger' : 'info'" size="small">
                  {{ serviceStatusLabel(row.status) }}
                </UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="最近检查" min-width="160" align="left">
              <template #default="{ row }">{{ row.last_check ? fmt(row.last_check.created_at) : '—' }}</template>
            </UiTableColumn>
            <UiTableColumn label="操作" width="120" align="right">
              <template #default="{ row }">
                <UiButton
                  size="small"
                  variant="ghost"
                  @click="copy(targetAddress(row.internal_host, row.ports_json), '服务地址已复制')"
                >
                  复制地址
                </UiButton>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="目标目录" name="targets">
          <UiTable :data="view.targets" row-key="team_service_id" empty-text="暂无其他竞域单位的服务">
            <UiTableColumn prop="service_name" label="服务" min-width="140" align="left">
              <template #default="{ row }">{{ row.service_name || row.service_slug }}</template>
            </UiTableColumn>
            <UiTableColumn prop="competitor_name" label="竞域单位" min-width="150" align="left" />
            <UiTableColumn prop="internal_host" label="内部地址" min-width="160" align="left" />
            <UiTableColumn label="端口" min-width="180" align="left">
              <template #default="{ row }">{{ portsLabel(row.ports_json) }}</template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="row.status === 'running' ? 'success' : row.status === 'failed' ? 'danger' : 'info'" size="small">
                  {{ serviceStatusLabel(row.status) }}
                </UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="操作" width="120" align="right">
              <template #default="{ row }">
                <UiButton
                  size="small"
                  variant="ghost"
                  @click="copy(targetAddress(row.internal_host, row.ports_json), '目标地址已复制')"
                >
                  复制地址
                </UiButton>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="排行榜" name="board">
          <UiTable :data="view.standings" row-key="competitor_id" empty-text="暂无排名">
            <UiTableColumn prop="rank" label="名次" width="80" align="center" />
            <UiTableColumn label="竞域单位" min-width="180" align="left">
              <template #default="{ row }">
                <span>{{ standingName(row) }}</span>
                <UiTag v-if="isMe(row)" size="small" type="success" class="awd-self-tag">我</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="总分" width="100" align="right" />
            <UiTableColumn prop="attack_score" label="攻击分" width="100" align="right" />
            <UiTableColumn prop="defense_loss" label="防守扣分" width="110" align="right" />
            <UiTableColumn prop="sla_score" label="SLA 分" width="100" align="right" />
          </UiTable>
        </UiTabPane>

        <UiTabPane label="事件" name="events">
          <div class="awd-events">
            <UiCard header="运营事件">
              <UiEmpty v-if="!view.incidents.length" description="暂无运营事件" />
              <ul v-else class="awd-list">
                <li v-for="incident in view.incidents" :key="incident.id" class="awd-list-item">
                  <div class="awd-list-main">
                    <UiTag size="small" type="warning">{{ incidentKindLabel(incident.kind) }}</UiTag>
                    <span class="awd-list-detail">{{ incident.detail }}</span>
                  </div>
                  <span class="awd-dim mono">{{ fmt(incident.created_at) }}</span>
                </li>
              </ul>
            </UiCard>
            <UiCard header="我的攻击">
              <UiTable :data="view.attacks" row-key="id" empty-text="尚未提交 flag">
                <UiTableColumn label="时间" min-width="160" align="left">
                  <template #default="{ row }">{{ fmt(row.submitted_at) }}</template>
                </UiTableColumn>
                <UiTableColumn label="状态" width="100" align="center">
                  <template #default="{ row }">
                    <UiTag
                      :type="row.status === 'accepted' ? 'success' : row.status === 'rejected' ? 'danger' : 'info'"
                      size="small"
                    >
                      {{ attackStatusLabel(row.status) }}
                    </UiTag>
                  </template>
                </UiTableColumn>
                <UiTableColumn label="目标服务" width="110" align="right">
                  <template #default="{ row }">#{{ row.victim_team_service_id }}</template>
                </UiTableColumn>
                <UiTableColumn label="原因" min-width="180" align="left">
                  <template #default="{ row }">{{ row.reason ? reasonLabel(row.reason) : '—' }}</template>
                </UiTableColumn>
                <UiTableColumn label="判定时间" min-width="160" align="left">
                  <template #default="{ row }">{{ fmt(row.judged_at) }}</template>
                </UiTableColumn>
              </UiTable>
            </UiCard>
          </div>
        </UiTabPane>

        <UiTabPane label="战报" name="report">
          <UiCard header="赛后代战报">
            <UiAlert
              v-if="!reportEditable"
              type="info"
              :closable="false"
              title="赛后开放"
              description="比赛结束后可提交 Markdown 战报与 zip 附件；管理员按 rubric 评审后显示得分。"
              class="awd-report-tip"
            />
            <UiSkeleton v-if="reportLoading" :rows="4" />
            <template v-else>
              <UiInput
                v-model="reportContent"
                type="textarea"
                :rows="12"
                :disabled="!reportEditable || reportSaving"
                placeholder="# 战报&#10;&#10;漏洞发现 / 利用 / 修复 / 防守过程…"
              />
              <div class="awd-inline awd-report-actions">
                <UiButton
                  type="primary"
                  :loading="reportSaving"
                  :disabled="!reportEditable || reportSaving"
                  @click="saveReport"
                >
                  保存战报
                </UiButton>
                <UiButton
                  :loading="reportSaving"
                  :disabled="!reportEditable || reportSaving"
                  @click="reportFileInput?.click()"
                >
                  上传 zip
                </UiButton>
                <input ref="reportFileInput" type="file" accept=".zip" class="awd-file" @change="onReportZip" />
                <span v-if="report?.zip_name" class="awd-dim">附件：{{ report.zip_name }}</span>
              </div>
              <p class="awd-hint">支持 Markdown；zip ≤32MB。重新保存会把评审状态重置为待评审。</p>
              <div v-if="report" class="awd-report-status">
                <UiTag :type="report.status === 'reviewed' ? 'success' : 'warning'" size="small">
                  {{ report.status === 'reviewed' ? '已评审' : '待评审' }}
                </UiTag>
                <b class="mono">{{ report.score }}</b>
                <span class="awd-dim">分</span>
              </div>
              <div v-if="report?.reviews?.length" class="awd-reviews">
                <h4>评审历史</h4>
                <ul class="awd-list">
                  <li v-for="review in report.reviews" :key="review.id" class="awd-list-item">
                    <div class="awd-list-main">
                      <UiTag size="small" type="info">
                        {{ review.reviewer_name || `#${review.reviewer_id}` }}
                      </UiTag>
                      <span class="awd-list-detail">{{ review.comment || '—' }}</span>
                    </div>
                    <span class="awd-dim mono">{{ review.score }} 分 · {{ fmt(review.created_at) }}</span>
                  </li>
                </ul>
              </div>
            </template>
          </UiCard>
        </UiTabPane>

        <UiTabPane v-if="game?.status === 'ended'" label="最终排名" name="final">
          <UiAlert
            type="info"
            :closable="false"
            title="最终计分"
            :description="finalFormula"
            class="awd-final-tip"
          />
          <UiAlert
            v-if="finalError"
            type="error"
            :closable="false"
            :title="finalError"
            class="awd-final-error"
          >
            <UiButton size="small" @click="loadFinalRanking">重试</UiButton>
          </UiAlert>
          <UiTable :data="finalRanking?.items ?? []" row-key="competitor_id" empty-text="暂无最终排名" :loading="finalLoading">
            <UiTableColumn prop="rank" label="名次" width="80" align="center" />
            <UiTableColumn label="竞域单位" min-width="180" align="left">
              <template #default="{ row }">
                <span>{{ finalName(row) }}</span>
                <UiTag v-if="isMeFinal(row)" size="small" type="success" class="awd-self-tag">我</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="awd_score" label="AWD 分" width="100" align="right" />
            <UiTableColumn prop="report_score" label="战报分" width="100" align="right" />
            <UiTableColumn label="最终分" width="110" align="right">
              <template #default="{ row }">
                <b class="mono">{{ row.final_score.toFixed(2) }}</b>
              </template>
            </UiTableColumn>
            <UiTableColumn label="最后有效攻击" min-width="170" align="left">
              <template #default="{ row }">{{ fmt(row.last_attack_at) }}</template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>
      </UiTabs>
    </main>
  </div>
</template>

<style scoped>
.awd-home { min-height: 100vh; background: var(--background); }
.awd-header {
  position: sticky;
  top: 0;
  z-index: 20;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--background) 94%, transparent);
  backdrop-filter: blur(12px);
}
.awd-header-inner {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  max-width: 1280px;
  margin-inline: auto;
  padding: var(--space-4) var(--space-8);
}
.awd-identity { display: flex; flex: 1; align-items: center; gap: var(--space-3); min-width: 0; }
.awd-mark { display: grid; flex-shrink: 0; place-items: center; color: var(--accent-foreground); }
.awd-title { min-width: 0; }
.awd-eyebrow {
  margin: 0 0 var(--space-1);
  color: var(--muted-foreground);
  font-size: 10px;
  letter-spacing: 0;
}
.awd-name-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.awd-name-row h1 { margin: 0; font-size: var(--text-xl); font-weight: 600; line-height: 1.3; }
.awd-window { margin: var(--space-1) 0 0; color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-clock { display: grid; gap: var(--space-2); width: 210px; }
.awd-clock-line { display: flex; align-items: baseline; justify-content: space-between; gap: var(--space-2); }
.awd-clock-line span { color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-clock-line b { font-size: var(--text-lg); font-variant-numeric: tabular-nums; }
.awd-clock-sub { color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-user { display: flex; align-items: center; gap: var(--space-2); }
.awd-user-name { color: var(--muted-foreground); font-size: var(--text-sm); }
.awd-refresh-error { max-width: 1280px; margin: var(--space-4) auto 0; }
.awd-body {
  display: grid;
  gap: var(--space-6);
  max-width: 1280px;
  margin-inline: auto;
  padding: var(--space-6) var(--space-8) var(--space-12);
}
.awd-center { justify-items: center; }
.awd-kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-4); }
.awd-kpi {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.awd-kpi p { margin: 0; color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-kpi b { font-size: var(--text-2xl); font-weight: 600; font-variant-numeric: tabular-nums; }
.awd-kpi span { color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 0.9fr); gap: var(--space-4); }
.awd-submit { display: flex; gap: var(--space-2); align-items: center; }
.awd-submit > :first-child { flex: 1; min-width: 0; }
.awd-hint { margin: var(--space-2) 0 0; color: var(--muted-foreground); font-size: var(--text-xs); line-height: 1.6; }
.awd-pairs { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--space-3); margin: 0 0 var(--space-3); }
.awd-pairs dt { color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-pairs dd { margin: var(--space-1) 0 0; font-size: var(--text-sm); }
.awd-inline { display: flex; align-items: center; gap: var(--space-2); min-width: 0; margin-top: var(--space-2); }
.awd-command {
  flex: 1;
  min-width: 0;
  padding: var(--space-2) var(--space-3);
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--card);
  font-family: ui-monospace, 'SFMono-Regular', Consolas, monospace;
  font-size: var(--text-xs);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.awd-overview { display: grid; grid-template-columns: minmax(0, 1.35fr) minmax(0, 0.65fr); gap: var(--space-4); }
.awd-events { display: grid; gap: var(--space-4); }
.awd-list { display: grid; gap: 0; margin: 0; padding: 0; list-style: none; }
.awd-list-item {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--border);
}
.awd-list-item:last-child { border-bottom: 0; }
.awd-list-main { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); min-width: 0; }
.awd-list-detail { color: var(--foreground); font-size: var(--text-sm); overflow-wrap: anywhere; }
.awd-dim { color: var(--muted-foreground); font-size: var(--text-xs); }
.awd-self-tag { margin-left: var(--space-2); }
.awd-file { display: none; }
.awd-report-tip { margin-bottom: var(--space-4); }
.awd-final-tip { margin-bottom: var(--space-4); }
.awd-final-error { margin-bottom: var(--space-4); }
.awd-report-actions { margin-top: var(--space-3); }
.awd-report-status { display: flex; align-items: center; gap: var(--space-2); margin-top: var(--space-4); }
.awd-report-status b { font-size: var(--text-xl); }
.awd-reviews { margin-top: var(--space-5); }
.awd-reviews h4 { margin: 0 0 var(--space-2); font-size: var(--text-sm); font-weight: 600; }

@media (max-width: 1024px) {
  .awd-header-inner { flex-wrap: wrap; }
  .awd-clock { order: 3; width: 100%; }
  .awd-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .awd-grid,
  .awd-overview { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .awd-header-inner { padding: var(--space-3) var(--space-4); }
  .awd-user-name { display: none; }
  .awd-body { padding: var(--space-4); }
  .awd-kpis { grid-template-columns: 1fr; }
  .awd-pairs { grid-template-columns: 1fr; }
  .awd-inline { align-items: stretch; }
  .awd-command { white-space: normal; overflow-wrap: anywhere; }
}
</style>
