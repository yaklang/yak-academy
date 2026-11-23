<script setup lang="ts">
// 证书签发面板（共享）：赛后按排名/分数预览接收人、签发、导出与验证码展示。
// CTF 详情页与 AWD 控制台共用；发证资格由父级按竞赛状态派生。
import { onMounted, ref } from 'vue'
import { dialogs, notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import {
  adminCompetitionApi,
  type CertificateAdminView,
  type CertificatePreview,
  type CertificateRuleInput,
  type QualificationMode,
} from '../../api'

const props = defineProps<{
  competitionId: number
  allowed: boolean
}>()

const data = ref<CertificateAdminView | null>(null)
const loading = ref(false)
const error = ref('')
const preview = ref<CertificatePreview | null>(null)
const issuing = ref(false)
const form = ref<CertificateRuleInput>({
  title: '',
  level: '',
  mode: 'top_n',
  threshold: 1,
})

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

function modeLabel(mode: QualificationMode) {
  if (mode === 'min_score') return '最低分'
  if (mode === 'top_percent') return '前百分比'
  return '前 N 名'
}

function thresholdLabel(mode: QualificationMode) {
  if (mode === 'min_score') return '最低分'
  if (mode === 'top_percent') return '百分比'
  return 'N 名'
}

async function load() {
  loading.value = true
  error.value = ''
  preview.value = null
  try {
    data.value = await adminCompetitionApi.certificates(props.competitionId)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '证书数据加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function previewCandidates() {
  if (!form.value.title.trim()) {
    notify.warning('请填写证书名称')
    return
  }
  if (!form.value.level.trim()) {
    notify.warning('请填写证书级别')
    return
  }
  if (!Number.isInteger(form.value.threshold) || form.value.threshold < 0) {
    notify.warning('请填写有效的筛选阈值')
    return
  }
  preview.value = null
  try {
    preview.value = await adminCompetitionApi.previewCertificates(props.competitionId, {
      title: form.value.title.trim(),
      level: form.value.level.trim(),
      mode: form.value.mode,
      threshold: form.value.threshold,
    })
    if (!preview.value.candidates.length) notify.warning('没有符合该规则的参赛者')
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '证书预览失败')
  }
}

async function issue() {
  if (!preview.value) return
  try {
    await dialogs.confirm(
      `将按「${preview.value.title}」签发 ${preview.value.candidates.length} 张证书？签发后不可撤销。`,
      '签发证书',
    )
  } catch {
    return
  }
  issuing.value = true
  try {
    const result = await adminCompetitionApi.issueCertificates(props.competitionId, {
      title: form.value.title.trim(),
      level: form.value.level.trim(),
      mode: form.value.mode,
      threshold: form.value.threshold,
    })
    notify.success(`已签发 ${result.certificates.length} 张证书`)
    preview.value = null
    await load()
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '证书签发失败')
  } finally {
    issuing.value = false
  }
}

async function exportCSV(kind: 'certificates' | 'awards') {
  try {
    const blob = kind === 'certificates'
      ? await adminCompetitionApi.certificatesCSV(props.competitionId)
      : await adminCompetitionApi.awardsCSV(props.competitionId)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `competition-${props.competitionId}-${kind}.csv`
    link.click()
    URL.revokeObjectURL(url)
    notify.success(kind === 'certificates' ? '证书 CSV 已导出' : '获奖名单 CSV 已导出')
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '导出失败')
  }
}

onMounted(load)
</script>

<template>
  <UiCard shadow="never">
    <template #header>
      <div class="card-head">
        <span>证书签发</span>
        <UiTag :type="allowed ? 'success' : 'warning'" size="small">
          {{ allowed ? '可签发' : '竞赛结束后可签发' }}
        </UiTag>
      </div>
    </template>
    <UiAlert
      v-if="!allowed"
      type="info"
      :closable="false"
      class="block"
      title="证书在竞赛结束后签发"
      description="签发时的名次与分数会写入证书快照，后续榜单变化不会改写已签发证书。"
    />
    <UiForm label-width="110px" style="max-width: 680px">
      <UiFormItem label="证书名称" required>
        <UiInput v-model="form.title" maxlength="128" placeholder="如：初赛荣誉证书 / 决赛一等奖" />
      </UiFormItem>
      <UiFormItem label="证书级别" required>
        <UiInput v-model="form.level" maxlength="64" placeholder="如：一等奖 / NISP1 / AD 初级" />
      </UiFormItem>
      <UiFormItem label="筛选方式">
        <UiSelect v-model="form.mode" style="width: 100%">
          <UiOption label="最低总分" value="min_score" />
          <UiOption label="前 N 名" value="top_n" />
          <UiOption label="前百分之 N" value="top_percent" />
        </UiSelect>
      </UiFormItem>
      <UiFormItem :label="thresholdLabel(form.mode)">
        <UiInputNumber v-model="form.threshold" :min="0" :max="1000000" />
        <div class="dim">
          {{ form.mode === 'top_percent' ? '1-100，按向上取整计算名额。' : form.mode === 'top_n' ? '仅正分参赛者进入排名。' : '分数不低于该值即可签发。' }}
        </div>
      </UiFormItem>
      <UiFormItem>
        <UiButton type="primary" :disabled="!allowed" @click="previewCandidates">
          预览接收人
        </UiButton>
      </UiFormItem>
    </UiForm>
    <template v-if="preview">
      <p class="dim">
        {{ preview.competition_name }} 共 {{ preview.total_ranked }} 个排名单位，
        符合「{{ preview.title }}」{{ preview.candidates.length }} 人/队。
      </p>
      <UiTable :data="preview.candidates" size="small" max-height="300" row-key="user_id" empty-text="无符合条件的接收人">
        <UiTableColumn label="类型" width="80">
          <template #default="{ row }">{{ row.recipient_kind === 'team' ? '队伍' : '个人' }}</template>
        </UiTableColumn>
        <UiTableColumn prop="recipient_name" label="接收人" min-width="140" />
        <UiTableColumn prop="username" label="用户名" width="130" />
        <UiTableColumn prop="rank" label="名次" width="80" align="right" />
        <UiTableColumn prop="score" label="分数" width="80" align="right" />
      </UiTable>
      <div class="qualification-actions">
        <UiButton
          type="primary"
          :loading="issuing"
          :disabled="issuing || !preview.candidates.length"
          @click="issue"
        >
          签发证书（{{ preview.candidates.length }}）
        </UiButton>
      </div>
    </template>
  </UiCard>

  <UiCard shadow="never">
    <template #header>
      <div class="card-head">
        <span>已签发证书（{{ data?.total ?? 0 }}）</span>
        <div class="actions">
          <UiButton size="small" :disabled="!data?.total" @click="exportCSV('awards')">
            导出获奖名单 CSV
          </UiButton>
          <UiButton size="small" :disabled="!data?.total" @click="exportCSV('certificates')">
            导出证书 CSV
          </UiButton>
          <UiButton size="small" :loading="loading" @click="load">刷新</UiButton>
        </div>
      </div>
    </template>
    <UiLoadError v-if="error" compact :message="error" @retry="load" class="block" />
    <template v-else>
      <h4 class="cert-subtitle">签发批次（{{ data?.rules.length ?? 0 }}）</h4>
      <UiTable :data="data?.rules ?? []" :loading="loading" size="small" empty-text="尚未签发证书">
        <UiTableColumn prop="title" label="证书名称" min-width="150" />
        <UiTableColumn prop="level" label="级别" width="120" />
        <UiTableColumn label="规则" width="160">
          <template #default="{ row }">{{ modeLabel(row.mode) }} {{ row.threshold }}</template>
        </UiTableColumn>
        <UiTableColumn prop="issued_count" label="数量" width="80" align="right" />
        <UiTableColumn label="签发时间" width="170">
          <template #default="{ row }">{{ fmt(row.issued_at) }}</template>
        </UiTableColumn>
      </UiTable>
      <h4 class="cert-subtitle">证书明细（{{ data?.certificates.length ?? 0 }}）</h4>
      <UiTable :data="data?.certificates ?? []" size="small" max-height="360" empty-text="暂无证书">
        <UiTableColumn label="类型" width="70">
          <template #default="{ row }">{{ row.recipient_kind === 'team' ? '队伍' : '个人' }}</template>
        </UiTableColumn>
        <UiTableColumn prop="recipient_name" label="接收人" min-width="130" />
        <UiTableColumn prop="level" label="级别" width="110" />
        <UiTableColumn prop="rank" label="名次" width="70" align="right" />
        <UiTableColumn prop="score" label="分数" width="70" align="right" />
        <UiTableColumn label="验证码" width="160">
          <template #default="{ row }">
            <router-link :to="`/certificates/${row.code}`" target="_blank" class="cert-code-link">
              <code>{{ row.code }}</code>
            </router-link>
          </template>
        </UiTableColumn>
        <UiTableColumn label="签发时间" width="170">
          <template #default="{ row }">{{ fmt(row.created_at) }}</template>
        </UiTableColumn>
      </UiTable>
    </template>
  </UiCard>
</template>

<style scoped>
.card-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; }
.actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.qualification-actions { margin-top: var(--space-3); }
.cert-subtitle { margin: var(--space-4) 0 var(--space-2); font-size: var(--text-sm); font-weight: 600; }
.cert-code-link { color: var(--accent-foreground); text-underline-offset: 3px; text-decoration: underline; }
.cert-subtitle:first-child { margin-top: 0; }
.block { margin-bottom: var(--space-5); }
.dim { color: var(--muted-foreground); font-size: var(--text-xs); }
</style>
