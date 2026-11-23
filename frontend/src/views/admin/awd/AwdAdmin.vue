<script setup lang="ts">
// AWD 管理端控制台：比赛配置、生命周期、服务定义/服务包、靶机与攻击盒部署、
// 回合开启/结算、排名、检查/攻击/告警。CTF 详情的配置表单不在此复用。
import { computed, onMounted, ref } from 'vue'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import {
  adminAwdApi,
  FORMAT_LABELS,
  KIND_LABELS,
  type AwdAdminView,
  type AwdCompetitorType,
  type AwdFinalRankingView,
  type AwdFinalStandingView,
  type AwdService,
  type AwdServiceInput,
  type AwdStandingView,
  type AwdReportView,
  type CompetitionDetail,
  type CompetitionFormat,
} from '../../../api'

const props = defineProps<{ competitionId: number; detail: CompetitionDetail }>()

const consoleData = ref<AwdAdminView | null>(null)
const loading = ref(true)
const loadError = ref('')
const activeTab = ref('config')
const actionLoading = ref('')
const savingConfig = ref(false)
const configForm = ref({
  competitorType: 'team' as AwdCompetitorType,
  roundSeconds: 300,
  scoringConfig: '{}',
})

interface ServiceForm {
  slug: string
  name: string
  image: string
  portsJSON: string
  checkerJSON: string
  baseScore: number | null
  slaWeight: number | null
  attackPoints: number | null
  defenseLoss: number | null
  checkdownPenalty: number | null
  flagLifetimeRounds: number | null
}

const showService = ref(false)
const serviceSaving = ref(false)
const serviceEditingId = ref<number | null>(null)
const serviceForm = ref<ServiceForm>(emptyServiceForm())
const packageUploading = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const reports = ref<AwdReportView[]>([])
const reportsLoading = ref(false)
const reportsError = ref('')
const finalRanking = ref<AwdFinalRankingView | null>(null)
const finalLoading = ref(false)
const finalError = ref('')
const showIncident = ref(false)
const incidentSaving = ref(false)
const incidentForm = ref({ kind: 'manual', detail: '', competitorID: 0, serviceID: 0 })
const showReview = ref(false)
const reviewTarget = ref<AwdReportView | null>(null)
const reviewSaving = ref(false)
const reviewForm = ref({ score: 0, rubricJSON: '{"vulnerability":0,"exploit":0,"defense":0,"report":0}', comment: '' })

function emptyServiceForm(): ServiceForm {
  return {
    slug: '',
    name: '',
    image: '',
    portsJSON: '[{"name":"http","port":80,"protocol":"tcp"}]',
    checkerJSON: '{"type":"http","path":"/"}',
    baseScore: 100,
    slaWeight: 100,
    attackPoints: 100,
    defenseLoss: 50,
    checkdownPenalty: 50,
    flagLifetimeRounds: 1,
  }
}

const gameStatus = computed(() => consoleData.value?.game.status ?? 'pending')
const pending = computed(() => gameStatus.value === 'pending')
const running = computed(() => gameStatus.value === 'running')
const serviceByID = computed(() => {
  const map = new Map<number, AwdService>()
  for (const svc of consoleData.value?.services ?? []) map.set(svc.id, svc)
  return map
})
const nameByCompetitor = computed(() => {
  const map = new Map<string, string>()
  for (const row of consoleData.value?.standings ?? []) {
    map.set(`${row.competitor_type}:${row.competitor_id}`, row.competitor_name || `#${row.competitor_id}`)
  }
  return map
})
const roundByID = computed(() => {
  const map = new Map<number, number>()
  for (const round of consoleData.value?.rounds ?? []) map.set(round.id, round.number)
  return map
})
const checkCountByRound = computed(() => {
  const map = new Map<number, number>()
  for (const check of consoleData.value?.checks ?? []) {
    map.set(check.round_id, (map.get(check.round_id) ?? 0) + 1)
  }
  return map
})

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    pending: '未开始',
    running: '进行中',
    paused: '已暂停',
    ended: '已结束',
    settled: '已结算',
    ok: '正常',
    fail: '失败',
    error: '错误',
    accepted: '已接受',
    rejected: '已拒绝',
    stopped: '已停止',
    failed: '失败',
  }
  return labels[status] ?? status ?? '未知'
}

function statusType(status: string) {
  if (status === 'running' || status === 'ok' || status === 'accepted') return 'success'
  if (status === 'paused' || status === 'pending' || status === 'settled') return 'warning'
  if (status === 'ended' || status === 'rejected' || status === 'failed' || status === 'fail' || status === 'error') return 'danger'
  return 'info'
}

function competitorName(type: AwdCompetitorType, id: number) {
  return nameByCompetitor.value.get(`${type}:${id}`) ?? `#${id}`
}

function serviceName(id: number) {
  const svc = serviceByID.value.get(id)
  return svc ? svc.name || svc.slug : `#${id}`
}

function roundNumber(id: number) {
  return roundByID.value.get(id) ?? id
}

function portsLabel(json: string) {
  try {
    const parsed = JSON.parse(json)
    if (!Array.isArray(parsed) || parsed.length === 0) return '—'
    return parsed.map((p) => `${p.name ?? 'port'} ${p.port ?? '?'}/${p.protocol ?? 'tcp'}`).join(' · ')
  } catch {
    return json || '—'
  }
}

function errorMessage(e: unknown, fallback: string) {
  return e instanceof Error ? e.message : fallback
}

async function refresh(silent = true) {
  try {
    consoleData.value = await adminAwdApi.console(props.competitionId)
    syncConfig()
  } catch (e) {
    if (!silent) loadError.value = errorMessage(e, 'AWD 控制台加载失败')
    else notify.error(errorMessage(e, '刷新失败'))
  }
}

function syncConfig() {
  const game = consoleData.value?.game
  if (!game) return
  configForm.value = {
    competitorType: game.competitor_type,
    roundSeconds: game.round_seconds,
    scoringConfig: game.scoring_config || '{}',
  }
}

async function loadConsole() {
  loading.value = true
  loadError.value = ''
  try {
    consoleData.value = await adminAwdApi.console(props.competitionId)
    syncConfig()
  } catch (e) {
    loadError.value = errorMessage(e, 'AWD 控制台加载失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

function parseJSON(raw: string, label: string): unknown | null {
  const value = raw.trim()
  if (!value) return {}
  try {
    return JSON.parse(value)
  } catch {
    notify.error(`${label} 不是合法 JSON`)
    return null
  }
}

async function saveConfig() {
  const scoring = parseJSON(configForm.value.scoringConfig, '计分配置')
  if (scoring === null || typeof scoring !== 'object' || Array.isArray(scoring)) {
    if (scoring !== null) notify.error('计分配置必须是 JSON 对象')
    return
  }
  savingConfig.value = true
  try {
    await adminAwdApi.updateGame(props.competitionId, {
      competitor_type: configForm.value.competitorType,
      round_seconds: configForm.value.roundSeconds ?? 300,
      scoring_config: scoring as Record<string, unknown>,
    })
    notify.success('AWD 配置已保存')
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '保存失败'))
  } finally {
    savingConfig.value = false
  }
}

async function lifecycle(action: 'start' | 'pause' | 'resume' | 'end') {
  if (action === 'end') {
    await dialogs.confirm('结束 AWD 比赛？结束后不可恢复，已部署容器需另行清理。', '结束比赛')
  }
  actionLoading.value = action
  try {
    await adminAwdApi[action](props.competitionId)
    notify.success('操作成功')
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '操作失败'))
  } finally {
    actionLoading.value = ''
  }
}

async function deployTargets() {
  actionLoading.value = 'deploy'
  try {
    const result = await adminAwdApi.deployTargets(props.competitionId)
    notify.success(`靶机部署完成：新建 ${result.created}，复用 ${result.reused}`)
    if (result.failed.length) notify.warning(`部分失败：${result.failed.join('；')}`)
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '靶机部署失败'))
  } finally {
    actionLoading.value = ''
  }
}

async function deployAttackBoxes() {
  actionLoading.value = 'boxes'
  try {
    const result = await adminAwdApi.deployAttackBoxes(props.competitionId)
    notify.success(`攻击盒部署完成：新建 ${result.created}，复用 ${result.reused}`)
    if (result.failed.length) notify.warning(`部分失败：${result.failed.join('；')}`)
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '攻击盒部署失败'))
  } finally {
    actionLoading.value = ''
  }
}

async function startRound() {
  actionLoading.value = 'round'
  try {
    const result = await adminAwdApi.startRound(props.competitionId)
    notify.success(`第 ${result.round.number} 回合已开启（flag ${result.flags} 个）`)
    if (result.failed.length) notify.warning(`部分靶机检查失败：${result.failed.join('；')}`)
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '回合开启失败'))
  } finally {
    actionLoading.value = ''
  }
}

async function settleRound(roundID: number) {
  actionLoading.value = `settle-${roundID}`
  try {
    const view = await adminAwdApi.settleRound(props.competitionId, roundID)
    notify.success(`回合已结算：新增 ${view.events} 条得分事件`)
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '结算失败'))
  } finally {
    actionLoading.value = ''
  }
}

function openService(row?: AwdService) {
  serviceEditingId.value = row?.id ?? null
  serviceForm.value = row
    ? {
        slug: row.slug,
        name: row.name,
        image: row.image,
        portsJSON: row.ports_json || '[]',
        checkerJSON: row.checker_config || '{}',
        baseScore: row.base_score,
        slaWeight: row.sla_weight,
        attackPoints: row.attack_points,
        defenseLoss: row.defense_loss,
        checkdownPenalty: row.checkdown_penalty,
        flagLifetimeRounds: row.flag_lifetime_rounds,
      }
    : emptyServiceForm()
  showService.value = true
}

async function saveService() {
  const form = serviceForm.value
  if (!form.slug.trim() || !form.image.trim()) {
    notify.warning('请填写 slug 与镜像')
    return
  }
  const ports = parseJSON(form.portsJSON, '端口配置')
  if (ports === null) return
  if (!Array.isArray(ports) || ports.length === 0) {
    notify.error('端口配置必须是非空数组')
    return
  }
  const checker = parseJSON(form.checkerJSON, 'checker 配置')
  if (checker === null) return
  if (typeof checker !== 'object' || Array.isArray(checker)) {
    notify.error('checker 配置必须是 JSON 对象')
    return
  }
  const body: AwdServiceInput = {
    slug: form.slug.trim(),
    name: form.name.trim() || form.slug.trim(),
    image: form.image.trim(),
    ports: ports as AwdServiceInput['ports'],
    checker_config: checker as Record<string, unknown>,
    base_score: form.baseScore ?? 100,
    sla_weight: form.slaWeight ?? 100,
    attack_points: form.attackPoints ?? 100,
    defense_loss: form.defenseLoss ?? 50,
    checkdown_penalty: form.checkdownPenalty ?? 50,
    flag_lifetime_rounds: form.flagLifetimeRounds ?? 1,
  }
  serviceSaving.value = true
  try {
    if (serviceEditingId.value) {
      await adminAwdApi.updateService(props.competitionId, serviceEditingId.value, body)
      notify.success('服务定义已更新')
    } else {
      await adminAwdApi.createService(props.competitionId, body)
      notify.success('服务定义已创建')
    }
    showService.value = false
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '保存失败'))
  } finally {
    serviceSaving.value = false
  }
}

async function deleteService(row: AwdService) {
  await dialogs.confirm(`删除服务定义「${row.name || row.slug}」？仅未部署靶机时可删除。`, '删除服务')
  try {
    await adminAwdApi.deleteService(props.competitionId, row.id)
    notify.success('服务定义已删除')
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '删除失败'))
  }
}

async function onPackageFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  packageUploading.value = true
  try {
    const svc = await adminAwdApi.uploadServicePackage(props.competitionId, file)
    notify.success(`服务包已上传：${svc.slug}`)
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '服务包上传失败'))
  } finally {
    packageUploading.value = false
    input.value = ''
  }
}

function standingName(row: AwdStandingView) {
  return row.competitor_name || `#${row.competitor_id}`
}

function reportName(row: AwdReportView) {
  return row.competitor_name || `#${row.competitor_id}`
}

function finalName(row: AwdFinalStandingView) {
  return row.competitor_name || `#${row.competitor_id}`
}

const finalFormula = computed(() => {
  const weights = finalRanking.value?.weights
  const awd = weights?.awd_weight ?? 70
  const report = weights?.report_weight ?? 30
  return `最终分 = (${awd} × AWD 分 / 榜首 AWD 分 × 100 + ${report} × 战报分) / ${awd + report}；并列依次比较战报分、最后有效攻击时间。`
})

async function loadFinalRanking() {
  finalLoading.value = true
  finalError.value = ''
  try {
    finalRanking.value = await adminAwdApi.finalRanking(props.competitionId)
  } catch (e) {
    finalError.value = errorMessage(e, '最终排名加载失败')
  } finally {
    finalLoading.value = false
  }
}

async function downloadCSV(kind: 'final' | 'attacks' | 'incidents') {
  const loaders = {
    final: adminAwdApi.finalRankingCSV,
    attacks: adminAwdApi.attacksCSV,
    incidents: adminAwdApi.incidentsCSV,
  }
  try {
    const blob = await loaders[kind](props.competitionId)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `competition-${props.competitionId}-awd-${kind}.csv`
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    notify.error(errorMessage(e, '导出失败'))
  }
}

function openIncident() {
  incidentForm.value = { kind: 'manual', detail: '', competitorID: 0, serviceID: 0 }
  showIncident.value = true
}

async function saveIncident() {
  const kind = incidentForm.value.kind.trim()
  const detail = incidentForm.value.detail.trim()
  if (!kind || !detail) {
    notify.warning('请填写告警类型与详情')
    return
  }
  incidentSaving.value = true
  try {
    await adminAwdApi.createIncident(props.competitionId, {
      kind,
      detail,
      competitor_id: incidentForm.value.competitorID || 0,
      service_id: incidentForm.value.serviceID || 0,
    })
    notify.success('运营告警已登记')
    showIncident.value = false
    await refresh()
  } catch (e) {
    notify.error(errorMessage(e, '登记失败'))
  } finally {
    incidentSaving.value = false
  }
}

async function loadReports() {
  reportsLoading.value = true
  reportsError.value = ''
  try {
    const resp = await adminAwdApi.reports(props.competitionId)
    reports.value = resp.items
  } catch (e) {
    reportsError.value = errorMessage(e, '战报列表加载失败')
  } finally {
    reportsLoading.value = false
  }
}

function openReview(row: AwdReportView) {
  reviewTarget.value = row
  reviewForm.value = {
    score: row.score || 0,
    rubricJSON: '{"vulnerability":0,"exploit":0,"defense":0,"report":0}',
    comment: '',
  }
  showReview.value = true
}

async function saveReview() {
  if (!reviewTarget.value) return
  const rubric = parseJSON(reviewForm.value.rubricJSON, 'rubric')
  if (rubric === null || typeof rubric !== 'object' || Array.isArray(rubric)) {
    if (rubric !== null) notify.error('rubric 必须是 JSON 对象')
    return
  }
  reviewSaving.value = true
  try {
    await adminAwdApi.reviewReport(props.competitionId, reviewTarget.value.id, {
      score: reviewForm.value.score ?? 0,
      rubric: rubric as Record<string, unknown>,
      comment: reviewForm.value.comment.trim(),
    })
    notify.success('战报评审已提交')
    showReview.value = false
    await loadReports()
    if (finalRanking.value) await loadFinalRanking()
  } catch (e) {
    notify.error(errorMessage(e, '评审提交失败'))
  } finally {
    reviewSaving.value = false
  }
}

async function downloadReportZip(row: AwdReportView) {
  try {
    const blob = await adminAwdApi.downloadReportZip(props.competitionId, row.id)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = row.zip_name || `report-${row.id}.zip`
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    notify.error(errorMessage(e, '下载失败'))
  }
}

function onTab(name: string | number) {
  if (name === 'reports') void loadReports()
  if (name === 'final') void loadFinalRanking()
}

onMounted(loadConsole)
</script>

<template>
  <div class="awd-admin">
    <div class="head">
      <h2>
        {{ detail.name }}
        <UiTag size="small" :type="detail.kind === 'public' ? 'success' : 'warning'">
          {{ KIND_LABELS[detail.kind] }}
        </UiTag>
        <UiTag size="small" type="info">{{ FORMAT_LABELS[detail.format as CompetitionFormat] }}</UiTag>
        <UiTag size="small" :type="statusType(gameStatus)">{{ statusLabel(gameStatus) }}</UiTag>
      </h2>
      <router-link to="/admin/panel/competitions"><UiButton size="small">← 返回列表</UiButton></router-link>
    </div>
    <p class="hint">
      {{ fmt(detail.start_time) }} ~ {{ fmt(detail.end_time) }}
      <span v-if="detail.description"> · {{ detail.description }}</span>
    </p>

    <UiAlert v-if="loadError" type="error" :closable="false" :title="loadError" class="load-error">
      <UiButton size="small" @click="loadConsole">重试</UiButton>
    </UiAlert>

    <UiSkeleton v-if="loading && !consoleData" :rows="7" />
    <template v-else-if="consoleData">
      <div class="stats">
        <div class="stat"><b>{{ consoleData.services.length }}</b><span>服务定义</span></div>
        <div class="stat"><b>{{ consoleData.rounds.length }}</b><span>回合</span></div>
        <div class="stat"><b>{{ consoleData.targets.length }}</b><span>靶机</span></div>
        <div class="stat"><b>{{ consoleData.attack_boxes.length }}</b><span>攻击盒</span></div>
        <div class="stat"><b>{{ consoleData.standings.length }}</b><span>竞域单位</span></div>
        <div class="stat warn"><b>{{ consoleData.incidents.length }}</b><span>运营告警</span></div>
      </div>

      <div class="actions">
        <UiButton
          v-if="pending"
          type="primary"
          :loading="actionLoading === 'start'"
          :disabled="!!actionLoading"
          @click="lifecycle('start')"
        >
          开始比赛
        </UiButton>
        <UiButton
          v-if="running"
          :loading="actionLoading === 'pause'"
          :disabled="!!actionLoading"
          @click="lifecycle('pause')"
        >
          暂停
        </UiButton>
        <UiButton
          v-if="gameStatus === 'paused'"
          type="primary"
          :loading="actionLoading === 'resume'"
          :disabled="!!actionLoading"
          @click="lifecycle('resume')"
        >
          继续
        </UiButton>
        <UiButton
          v-if="gameStatus === 'running' || gameStatus === 'paused'"
          type="danger"
          :loading="actionLoading === 'end'"
          :disabled="!!actionLoading"
          @click="lifecycle('end')"
        >
          结束比赛
        </UiButton>
        <UiButton :loading="actionLoading === 'deploy'" :disabled="!running || !!actionLoading" @click="deployTargets">
          部署靶机
        </UiButton>
        <UiButton :loading="actionLoading === 'boxes'" :disabled="!running || !!actionLoading" @click="deployAttackBoxes">
          部署攻击盒
        </UiButton>
        <UiButton :loading="actionLoading === 'round'" :disabled="!running || !!actionLoading" @click="startRound">
          开启下一回合
        </UiButton>
        <UiButton :disabled="!!actionLoading" @click="refresh(false)">刷新</UiButton>
      </div>

      <UiTabs v-model="activeTab" @tab-change="onTab">
        <UiTabPane label="配置" name="config">
          <UiCard header="AWD 比赛配置">
            <UiForm label-width="120px" class="config-form">
              <UiFormItem label="竞域单位">
                <UiSelect v-model="configForm.competitorType" :disabled="!pending" style="width: 100%">
                  <UiOption value="team" label="战队（team）" />
                  <UiOption value="user" label="个人（user）" />
                </UiSelect>
                <div class="dim">创建后未开赛前可修改；开赛后锁定。</div>
              </UiFormItem>
              <UiFormItem label="回合秒数">
                <UiInputNumber v-model="configForm.roundSeconds" :min="30" :max="86400" />
                <div class="dim">每回合 30..86400 秒；开启下一回合时按此生成 AwdRound。</div>
              </UiFormItem>
              <UiFormItem label="计分配置">
                <UiInput v-model="configForm.scoringConfig" type="textarea" :rows="5" class="mono" />
                <div class="dim">
                  JSON 对象，支持 sla_weight / attack_weight / defense_weight / checkdown_weight，缺省 1。
                </div>
              </UiFormItem>
              <UiFormItem>
                <UiButton type="primary" :loading="savingConfig" :disabled="!pending || savingConfig" @click="saveConfig">
                  保存配置
                </UiButton>
              </UiFormItem>
            </UiForm>
          </UiCard>
        </UiTabPane>

        <UiTabPane label="服务定义" name="services">
          <div class="toolbar">
            <UiButton type="primary" :disabled="!pending" @click="openService()">新增服务</UiButton>
            <UiButton :loading="packageUploading" :disabled="!pending || packageUploading" @click="fileInput?.click()">
              上传服务包
            </UiButton>
            <input ref="fileInput" type="file" accept=".zip" class="file-input" @change="onPackageFile" />
            <span class="dim">服务包 = awd-service.json + Dockerfile（+ 可选 checker 文件），≤32MB。</span>
          </div>
          <UiTable :data="consoleData.services" row-key="id" empty-text="尚未定义 AWD 服务">
            <UiTableColumn prop="slug" label="Slug" min-width="120" align="left" />
            <UiTableColumn prop="name" label="名称" min-width="140" align="left" />
            <UiTableColumn prop="image" label="镜像" min-width="180" align="left" />
            <UiTableColumn label="端口" min-width="180" align="left">
              <template #default="{ row }">{{ portsLabel(row.ports_json) }}</template>
            </UiTableColumn>
            <UiTableColumn prop="base_score" label="基础分" width="90" align="right" />
            <UiTableColumn prop="attack_points" label="攻击池" width="90" align="right" />
            <UiTableColumn prop="defense_loss" label="防守扣分" width="100" align="right" />
            <UiTableColumn label="操作" width="150" align="right">
              <template #default="{ row }">
                <UiButton size="small" variant="ghost" :disabled="!pending" @click="openService(row)">编辑</UiButton>
                <UiButton size="small" variant="ghost" type="danger" :disabled="!pending" @click="deleteService(row)">删除</UiButton>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="回合" name="rounds">
          <UiTable :data="consoleData.rounds" row-key="id" empty-text="尚未开启回合">
            <UiTableColumn prop="number" label="回合" width="80" align="center" />
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="开始" min-width="160" align="left">
              <template #default="{ row }">{{ fmt(row.starts_at) }}</template>
            </UiTableColumn>
            <UiTableColumn label="结束" min-width="160" align="left">
              <template #default="{ row }">{{ fmt(row.ends_at) }}</template>
            </UiTableColumn>
            <UiTableColumn label="检查数" width="90" align="right">
              <template #default="{ row }">{{ checkCountByRound.get(row.id) ?? 0 }}</template>
            </UiTableColumn>
            <UiTableColumn label="操作" width="120" align="right">
              <template #default="{ row }">
                <UiButton
                  size="small"
                  :loading="actionLoading === `settle-${row.id}`"
                  :disabled="row.status === 'settled' || !!actionLoading"
                  @click="settleRound(row.id)"
                >
                  结算
                </UiButton>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="靶机" name="targets">
          <UiTable :data="consoleData.targets" row-key="id" empty-text="尚未部署靶机">
            <UiTableColumn label="竞域单位" min-width="150" align="left">
              <template #default="{ row }">{{ competitorName(row.competitor_type, row.competitor_id) }}</template>
            </UiTableColumn>
            <UiTableColumn label="服务" min-width="130" align="left">
              <template #default="{ row }">{{ serviceName(row.service_id) }}</template>
            </UiTableColumn>
            <UiTableColumn prop="internal_host" label="内部地址" min-width="160" align="left" />
            <UiTableColumn label="端口" min-width="170" align="left">
              <template #default="{ row }">{{ portsLabel(row.ports_json) }}</template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn label="最近检查" min-width="160" align="left">
              <template #default="{ row }">{{ fmt(row.last_check_at) }}</template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="攻击盒" name="boxes">
          <UiTable :data="consoleData.attack_boxes" row-key="id" empty-text="尚未部署攻击盒">
            <UiTableColumn label="竞域单位" min-width="150" align="left">
              <template #default="{ row }">{{ competitorName(row.competitor_type, row.competitor_id) }}</template>
            </UiTableColumn>
            <UiTableColumn label="SSH 地址" min-width="180" align="left">
              <template #default="{ row }">{{ row.ssh_host }}:{{ row.ssh_port }}</template>
            </UiTableColumn>
            <UiTableColumn prop="ssh_user" label="用户" min-width="120" align="left" />
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="排名" name="standings">
          <UiTable :data="consoleData.standings" row-key="competitor_id" empty-text="暂无排名">
            <UiTableColumn prop="rank" label="名次" width="80" align="center" />
            <UiTableColumn label="竞域单位" min-width="180" align="left">
              <template #default="{ row }">{{ standingName(row) }}</template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="总分" width="100" align="right" />
            <UiTableColumn prop="attack_score" label="攻击分" width="100" align="right" />
            <UiTableColumn prop="defense_loss" label="防守扣分" width="110" align="right" />
            <UiTableColumn prop="sla_score" label="SLA 分" width="100" align="right" />
          </UiTable>
        </UiTabPane>

        <UiTabPane label="检查 / 攻击 / 告警" name="events">
          <div class="toolbar">
            <UiButton size="small" @click="downloadCSV('attacks')">导出攻击 CSV</UiButton>
            <UiButton size="small" @click="downloadCSV('incidents')">导出告警 CSV</UiButton>
            <UiButton size="small" type="primary" @click="openIncident">登记告警</UiButton>
          </div>
          <div class="events">
            <UiCard header="最近检查">
              <UiTable :data="consoleData.checks" row-key="id" empty-text="暂无检查记录">
                <UiTableColumn label="时间" min-width="160" align="left">
                  <template #default="{ row }">{{ fmt(row.created_at) }}</template>
                </UiTableColumn>
                <UiTableColumn label="回合" width="80" align="right">
                  <template #default="{ row }">#{{ roundNumber(row.round_id) }}</template>
                </UiTableColumn>
                <UiTableColumn label="靶机" min-width="150" align="left">
                  <template #default="{ row }">#{{ row.team_service_id }}</template>
                </UiTableColumn>
                <UiTableColumn prop="action" label="动作" width="80" align="center" />
                <UiTableColumn label="状态" width="90" align="center">
                  <template #default="{ row }">
                    <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
                  </template>
                </UiTableColumn>
                <UiTableColumn prop="latency_ms" label="延迟(ms)" width="100" align="right" />
              </UiTable>
            </UiCard>
            <UiCard header="最近攻击">
              <UiTable :data="consoleData.attacks" row-key="id" empty-text="暂无攻击记录">
                <UiTableColumn label="时间" min-width="160" align="left">
                  <template #default="{ row }">{{ fmt(row.submitted_at) }}</template>
                </UiTableColumn>
                <UiTableColumn label="攻击方" min-width="140" align="left">
                  <template #default="{ row }">{{ competitorName(row.attacker_type, row.attacker_id) }}</template>
                </UiTableColumn>
                <UiTableColumn label="目标靶机" min-width="120" align="left">
                  <template #default="{ row }">#{{ row.victim_team_service_id }}</template>
                </UiTableColumn>
                <UiTableColumn label="状态" width="100" align="center">
                  <template #default="{ row }">
                    <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
                  </template>
                </UiTableColumn>
                <UiTableColumn prop="reason" label="原因" min-width="150" align="left" />
              </UiTable>
            </UiCard>
            <UiCard header="运营告警">
              <UiTable :data="consoleData.incidents" row-key="id" empty-text="暂无运营告警">
                <UiTableColumn label="时间" min-width="160" align="left">
                  <template #default="{ row }">{{ fmt(row.created_at) }}</template>
                </UiTableColumn>
                <UiTableColumn prop="kind" label="类型" width="120" align="left" />
                <UiTableColumn label="竞域单位" min-width="140" align="left">
                  <template #default="{ row }">{{ competitorName(row.competitor_type, row.competitor_id) }}</template>
                </UiTableColumn>
                <UiTableColumn prop="detail" label="详情" min-width="220" align="left" />
              </UiTable>
            </UiCard>
          </div>
        </UiTabPane>

        <UiTabPane label="战报评审" name="reports">
          <UiAlert v-if="reportsError" type="error" :closable="false" :title="reportsError" class="load-error">
            <UiButton size="small" @click="loadReports">重试</UiButton>
          </UiAlert>
          <UiTable :data="reports" row-key="id" empty-text="暂无战报提交" :loading="reportsLoading">
            <UiTableColumn label="竞域单位" min-width="150" align="left">
              <template #default="{ row }">{{ reportName(row) }}</template>
            </UiTableColumn>
            <UiTableColumn label="状态" width="100" align="center">
              <template #default="{ row }">
                <UiTag :type="row.status === 'reviewed' ? 'success' : 'warning'" size="small">
                  {{ row.status === 'reviewed' ? '已评审' : '待评审' }}
                </UiTag>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="score" label="分数" width="90" align="right" />
            <UiTableColumn label="附件" width="110" align="center">
              <template #default="{ row }">
                <UiButton v-if="row.has_zip" size="small" variant="ghost" @click="downloadReportZip(row)">下载 zip</UiButton>
                <span v-else class="dim">—</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="提交时间" min-width="160" align="left">
              <template #default="{ row }">{{ fmt(row.updated_at) }}</template>
            </UiTableColumn>
            <UiTableColumn label="操作" width="110" align="right">
              <template #default="{ row }">
                <UiButton size="small" type="primary" @click="openReview(row)">评审</UiButton>
              </template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>

        <UiTabPane label="最终排名" name="final">
          <div class="toolbar">
            <UiButton size="small" @click="downloadCSV('final')">导出最终排名 CSV</UiButton>
          </div>
          <UiAlert
            type="info"
            :closable="false"
            title="最终计分"
            :description="finalFormula"
            class="final-tip"
          />
          <UiAlert
            v-if="finalRanking && !finalRanking.finalized"
            type="warning"
            :closable="false"
            title="比赛未结束"
            description="当前为预览；结束比赛并完成战报评审后即为最终结果。"
            class="final-tip"
          />
          <UiAlert v-if="finalError" type="error" :closable="false" :title="finalError" class="load-error">
            <UiButton size="small" @click="loadFinalRanking">重试</UiButton>
          </UiAlert>
          <UiTable
            :data="finalRanking?.items ?? []"
            row-key="competitor_id"
            empty-text="暂无最终排名"
            :loading="finalLoading"
          >
            <UiTableColumn prop="rank" label="名次" width="80" align="center" />
            <UiTableColumn label="竞域单位" min-width="180" align="left">
              <template #default="{ row }">{{ finalName(row) }}</template>
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
            <UiTableColumn label="最后得分事件" min-width="170" align="left">
              <template #default="{ row }">{{ fmt(row.last_event_at) }}</template>
            </UiTableColumn>
          </UiTable>
        </UiTabPane>
      </UiTabs>
    </template>

    <UiDialog v-model="showService" :title="serviceEditingId ? '编辑服务定义' : '新增服务定义'" width="760px">
      <UiForm label-width="120px">
        <UiFormItem label="Slug">
          <UiInput v-model="serviceForm.slug" :disabled="!!serviceEditingId" placeholder="web" />
        </UiFormItem>
        <UiFormItem label="名称">
          <UiInput v-model="serviceForm.name" placeholder="Web 服务" />
        </UiFormItem>
        <UiFormItem label="镜像">
          <UiInput v-model="serviceForm.image" placeholder="awd/web:latest" />
        </UiFormItem>
        <UiFormItem label="端口 JSON">
          <UiInput v-model="serviceForm.portsJSON" type="textarea" :rows="3" class="mono" />
          <div class="dim">示例：[{&quot;name&quot;:&quot;http&quot;,&quot;port&quot;:80,&quot;protocol&quot;:&quot;tcp&quot;}]</div>
        </UiFormItem>
        <UiFormItem label="Checker JSON">
          <UiInput v-model="serviceForm.checkerJSON" type="textarea" :rows="3" class="mono" />
        </UiFormItem>
        <div class="score-grid">
          <UiFormItem label="基础分">
            <UiInputNumber v-model="serviceForm.baseScore" :min="0" />
          </UiFormItem>
          <UiFormItem label="SLA 权重%">
            <UiInputNumber v-model="serviceForm.slaWeight" :min="0" :max="100" />
          </UiFormItem>
          <UiFormItem label="攻击池">
            <UiInputNumber v-model="serviceForm.attackPoints" :min="0" />
          </UiFormItem>
          <UiFormItem label="防守扣分">
            <UiInputNumber v-model="serviceForm.defenseLoss" :min="0" />
          </UiFormItem>
          <UiFormItem label="checkdown 扣分">
            <UiInputNumber v-model="serviceForm.checkdownPenalty" :min="0" />
          </UiFormItem>
          <UiFormItem label="flag 生命周期">
            <UiInputNumber v-model="serviceForm.flagLifetimeRounds" :min="1" :max="20" />
          </UiFormItem>
        </div>
      </UiForm>
      <template #footer>
        <UiButton @click="showService = false">取消</UiButton>
        <UiButton type="primary" :loading="serviceSaving" :disabled="serviceSaving" @click="saveService">保存</UiButton>
      </template>
    </UiDialog>

    <UiDialog v-model="showReview" title="战报评审" width="680px">
      <UiForm label-width="100px">
        <UiFormItem label="竞域单位">
          <UiInput :model-value="reviewTarget ? reportName(reviewTarget) : ''" disabled />
        </UiFormItem>
        <UiFormItem label="得分">
          <UiInputNumber v-model="reviewForm.score" :min="0" :max="100" />
          <div class="dim">0..100；保留每次评审，最终分取最后一条。</div>
        </UiFormItem>
        <UiFormItem label="rubric JSON">
          <UiInput v-model="reviewForm.rubricJSON" type="textarea" :rows="5" class="mono" />
          <div class="dim">示例：{&quot;vulnerability&quot;:30,&quot;exploit&quot;:20,&quot;defense&quot;:20,&quot;report&quot;:18}</div>
        </UiFormItem>
        <UiFormItem label="评审意见">
          <UiInput v-model="reviewForm.comment" type="textarea" :rows="3" maxlength="2000" />
        </UiFormItem>
      </UiForm>
      <template #footer>
        <UiButton @click="showReview = false">取消</UiButton>
        <UiButton type="primary" :loading="reviewSaving" :disabled="reviewSaving" @click="saveReview">提交评审</UiButton>
      </template>
    </UiDialog>

    <UiDialog v-model="showIncident" title="登记运营告警" width="640px">
      <UiForm label-width="120px">
        <UiFormItem label="类型">
          <UiInput v-model="incidentForm.kind" placeholder="manual" maxlength="32" />
          <div class="dim">小写字母/数字/下划线/连字符，如 manual、flag_leak。</div>
        </UiFormItem>
        <UiFormItem label="详情">
          <UiInput v-model="incidentForm.detail" type="textarea" :rows="4" maxlength="2000" />
        </UiFormItem>
        <UiFormItem label="竞域单位 ID">
          <UiInputNumber v-model="incidentForm.competitorID" :min="0" />
          <div class="dim">0 = 竞赛级告警（不关联具体竞域单位）。</div>
        </UiFormItem>
        <UiFormItem label="服务 ID">
          <UiInputNumber v-model="incidentForm.serviceID" :min="0" />
          <div class="dim">0 = 不关联服务；否则必须属于本场比赛。</div>
        </UiFormItem>
      </UiForm>
      <template #footer>
        <UiButton @click="showIncident = false">取消</UiButton>
        <UiButton type="primary" :loading="incidentSaving" :disabled="incidentSaving" @click="saveIncident">
          登记
        </UiButton>
      </template>
    </UiDialog>
  </div>
</template>

<style scoped>
.awd-admin { display: grid; gap: var(--space-5); }
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); flex-wrap: wrap; }
.head h2 { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: 0; font-size: var(--text-xl); font-weight: 600; }
.hint { margin: 0; color: var(--muted-foreground); font-size: var(--text-sm); line-height: 1.6; }
.load-error { max-width: 720px; }
.final-tip { margin-bottom: var(--space-4); }
.stats { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: var(--space-3); }
.stat {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.stat b { font-size: var(--text-xl); font-variant-numeric: tabular-nums; }
.stat span { color: var(--muted-foreground); font-size: var(--text-xs); }
.stat.warn b { color: var(--destructive); }
.actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin-bottom: var(--space-3); }
.file-input { display: none; }
.config-form { max-width: 720px; }
.dim { color: var(--muted-foreground); font-size: var(--text-xs); line-height: 1.6; }
.mono { font-family: ui-monospace, 'SFMono-Regular', Consolas, monospace; }
.events { display: grid; gap: var(--space-4); }
.score-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--space-3); }
@media (max-width: 1024px) {
  .stats { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .score-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 640px) {
  .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .score-grid { grid-template-columns: 1fr; }
}
</style>
