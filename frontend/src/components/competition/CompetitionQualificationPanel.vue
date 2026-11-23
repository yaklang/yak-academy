<script setup lang="ts">
// 晋级管理面板（共享）：来源竞赛榜单（CTF/AWD/考试）→ 本邀请赛参赛名单。
// CTF 详情页与 AWD 控制台共用；只在目标竞赛未开始时允许应用。
import { onMounted, ref } from 'vue'
import { dialogs, notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import {
  adminCompetitionApi,
  FORMAT_LABELS,
  type Competition,
  type QualificationMode,
  type QualificationPreview,
  type QualificationRule,
} from '../../api'

const props = defineProps<{
  competitionId: number
  disabled: boolean
}>()

const emit = defineEmits<{ applied: [] }>()

const sources = ref<Competition[]>([])
const history = ref<QualificationRule[]>([])
const loading = ref(true)
const error = ref('')
const preview = ref<QualificationPreview | null>(null)
const applying = ref(false)
const form = ref({
  sourceId: null as number | null,
  mode: 'min_score' as QualificationMode,
  threshold: 70,
})

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

async function load() {
  loading.value = true
  error.value = ''
  preview.value = null
  try {
    const [list, rules] = await Promise.all([
      adminCompetitionApi.list(1, 100),
      adminCompetitionApi.qualifications(props.competitionId),
    ])
    sources.value = list.items.filter(
      (c) => c.id !== props.competitionId && c.status !== 'scheduled',
    )
    history.value = rules.items
  } catch (e) {
    error.value = e instanceof Error ? e.message : '晋级数据加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function previewCandidates() {
  if (!form.value.sourceId) {
    notify.warning('请选择来源竞赛')
    return
  }
  preview.value = null
  try {
    preview.value = await adminCompetitionApi.previewQualification(props.competitionId, {
      source_competition_id: form.value.sourceId,
      mode: form.value.mode,
      threshold: form.value.threshold,
    })
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '晋级预览失败')
  }
}

async function apply() {
  if (!preview.value) return
  try {
    await dialogs.confirm(
      `将 ${preview.value.candidates.length} 名中选者导入本场邀请赛并生成邀请码？同一来源竞赛只能应用一次。`,
      '应用晋级',
    )
  } catch {
    return
  }
  applying.value = true
  try {
    const result = await adminCompetitionApi.applyQualification(props.competitionId, {
      source_competition_id: form.value.sourceId!,
      mode: form.value.mode,
      threshold: form.value.threshold,
    })
    notify.success(`已导入 ${result.imported} 名参赛者`)
    preview.value = null
    await load()
    emit('applied')
  } catch (e) {
    notify.error(e instanceof Error ? e.message : '晋级应用失败')
  } finally {
    applying.value = false
  }
}

onMounted(load)
</script>

<template>
  <UiCard shadow="never">
    <template #header>
      <div class="card-head">
        <span>晋级名单</span>
        <UiTag v-if="disabled" type="info" size="small">仅未开始时可应用</UiTag>
      </div>
    </template>
    <UiLoadError v-if="error" compact :message="error" @retry="load" class="block" />
    <template v-else>
      <UiForm label-width="110px" style="max-width: 620px">
        <UiFormItem label="来源竞赛">
          <UiSelect v-model="form.sourceId" filterable placeholder="选择已开赛的竞赛（CTF/AWD/考试）" style="width: 100%">
            <UiOption
              v-for="c in sources"
              :key="c.id"
              :label="`${c.name}（${FORMAT_LABELS[c.format]}·${c.status === 'ended' ? '已结束' : '进行中'}）`"
              :value="c.id"
            />
          </UiSelect>
        </UiFormItem>
        <UiFormItem label="筛选方式">
          <UiSelect v-model="form.mode" style="width: 100%">
            <UiOption label="最低总分" value="min_score" />
            <UiOption label="前 N 名" value="top_n" />
            <UiOption label="前百分之 N" value="top_percent" />
          </UiSelect>
        </UiFormItem>
        <UiFormItem :label="form.mode === 'min_score' ? '最低分' : form.mode === 'top_n' ? 'N 名' : '百分比'">
          <UiInputNumber v-model="form.threshold" :min="0" :max="1000000" :step="1" />
        </UiFormItem>
        <UiFormItem>
          <UiButton type="primary" :disabled="disabled" :loading="loading" @click="previewCandidates">
            预览晋级名单
          </UiButton>
        </UiFormItem>
      </UiForm>
      <template v-if="preview">
        <p class="dim">
          {{ preview.source_competition_name }} 共 {{ preview.total_participants }} 名参赛者，
          符合条件 {{ preview.candidates.length }} 人（并列边界一并入选）。
        </p>
        <UiTable :data="preview.candidates" size="small" empty-text="无符合条件选手">
          <UiTableColumn prop="rank" label="名次" width="80" />
          <UiTableColumn prop="username" label="用户名" min-width="130" />
          <UiTableColumn prop="account_code" label="账号编码" width="130" />
          <UiTableColumn label="队伍" min-width="120">
            <template #default="{ row }">{{ row.team_name ?? '-' }}</template>
          </UiTableColumn>
          <UiTableColumn prop="score" label="得分" width="90" />
        </UiTable>
        <div class="qualification-actions">
          <UiButton
            type="primary"
            :disabled="!preview.candidates.length || disabled"
            :loading="applying"
            @click="apply"
          >应用晋级（导入参赛者）</UiButton>
        </div>
      </template>

      <div class="qualification-history">
        <h4>历史晋级规则</h4>
        <UiTable :data="history" :loading="loading" size="small" empty-text="尚无晋级记录">
          <UiTableColumn prop="source_name" label="来源竞赛" min-width="160" />
          <UiTableColumn label="规则" min-width="130">
            <template #default="{ row }">
              {{ row.mode === 'min_score' ? `≥ ${row.threshold} 分` : row.mode === 'top_n' ? `前 ${row.threshold} 名` : `前 ${row.threshold}%` }}
            </template>
          </UiTableColumn>
          <UiTableColumn label="中选人数" width="100">
            <template #default="{ row }">{{ row.usernames.length }}</template>
          </UiTableColumn>
          <UiTableColumn label="应用时间" width="170">
            <template #default="{ row }">{{ fmt(row.applied_at) }}</template>
          </UiTableColumn>
        </UiTable>
      </div>
    </template>
  </UiCard>
</template>

<style scoped>
.card-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; }
.qualification-actions { margin-top: var(--space-3); }
.qualification-history { margin-top: var(--space-5); }
.qualification-history h4 { margin: 0 0 var(--space-2); font-size: var(--text-sm); }
.block { margin-bottom: var(--space-5); }
.dim { color: var(--muted-foreground); font-size: var(--text-xs); }
</style>
