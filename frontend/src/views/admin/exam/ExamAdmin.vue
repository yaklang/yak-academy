<script setup lang="ts">
// 考试管理端：试卷配置 + 试题 CRUD（开赛前可编辑，开赛后锁定）。
// 与 AWD 控制台/CTF 详情完全独立，仅复用竞赛入口元数据。
import { computed, onMounted, ref } from 'vue'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import CompetitionCertificatePanel from '@/components/competition/CompetitionCertificatePanel.vue'
import CompetitionQualificationPanel from '@/components/competition/CompetitionQualificationPanel.vue'
import {
  adminExamApi,
  FORMAT_LABELS,
  KIND_LABELS,
  type CompetitionDetail,
  type CompetitionFormat,
  type ExamOption,
  type ExamPaperView,
  type ExamQuestionAdmin,
  type ExamQuestionInput,
  type ExamQuestionType,
  type ExamResultStatus,
  type ExamResultsView,
} from '../../../api'

const props = defineProps<{ competitionId: number; detail: CompetitionDetail }>()

const data = ref<ExamPaperView | null>(null)
const loading = ref(true)
const loadError = ref('')
const savingPaper = ref(false)
const results = ref<ExamResultsView | null>(null)
const resultsLoading = ref(false)
const resultsError = ref('')
const exporting = ref(false)
const paperForm = ref({ title: '', description: '', durationSeconds: 5400, passScore: 60 })

const showQuestion = ref(false)
const questionSaving = ref(false)
const editingId = ref<number | null>(null)
const questionForm = ref({
  seq: 1,
  type: 'single' as ExamQuestionType,
  title: '',
  content: '',
  optionsJSON: '[{"key":"A","text":""},{"key":"B","text":""}]',
  answerText: '',
  explanation: '',
  score: 5,
  challengeID: 0,
})

const TYPE_LABELS: Record<ExamQuestionType, string> = {
  single: '单选',
  multiple: '多选',
  truefalse: '判断',
  code: '编程',
}

const locked = computed(() => props.detail.status !== 'scheduled')
const qualificationDisabled = computed(() => props.detail.status !== 'scheduled')
const certificateAllowed = computed(() => props.detail.status === 'ended')
const questions = computed(() => data.value?.questions ?? [])
const totalScore = computed(() => data.value?.paper.total_score ?? 0)
const resultRows = computed(() => results.value?.results ?? [])
const summary = computed(() => results.value?.summary ?? null)
const RESULT_STATUS_LABELS: Record<ExamResultStatus, string> = {
  not_started: '未开始',
  in_progress: '作答中',
  submitted: '已交卷',
  expired: '超时交卷',
}
const RESULT_STATUS_TYPES: Record<ExamResultStatus, 'info' | 'warning' | 'success'> = {
  not_started: 'info',
  in_progress: 'warning',
  submitted: 'success',
  expired: 'warning',
}
const answerHint = computed(() => {
  switch (questionForm.value.type) {
    case 'single':
      return '填写选项 key，如 A'
    case 'multiple':
      return '多个选项用逗号分隔，如 A,C（全对才得分）'
    case 'truefalse':
      return '填写 true 或 false'
    default:
      return '编程题无需标准答案，绑定 Challenge 后由判题回写分数'
  }
})

function fmt(value: string | null | undefined) {
  return value ? formatDateTime(value) : '—'
}

function errorMessage(e: unknown, fallback: string) {
  return e instanceof Error ? e.message : fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    data.value = await adminExamApi.paper(props.competitionId)
    paperForm.value = {
      title: data.value.paper.title,
      description: data.value.paper.description,
      durationSeconds: data.value.paper.duration_seconds,
      passScore: data.value.paper.pass_score,
    }
  } catch (e) {
    loadError.value = errorMessage(e, '考试试卷加载失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

async function loadResults() {
  resultsLoading.value = true
  resultsError.value = ''
  try {
    results.value = await adminExamApi.results(props.competitionId)
  } catch (e) {
    resultsError.value = errorMessage(e, '成绩单加载失败，请稍后重试')
  } finally {
    resultsLoading.value = false
  }
}

function fmtDuration(seconds: number) {
  if (!seconds) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`
}

function resultStatusLabel(status: ExamResultStatus) {
  return RESULT_STATUS_LABELS[status] ?? status
}

function resultStatusType(status: ExamResultStatus) {
  return RESULT_STATUS_TYPES[status] ?? 'info'
}

async function exportResults() {
  if (exporting.value) return
  exporting.value = true
  try {
    const blob = await adminExamApi.resultsCSV(props.competitionId)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `competition-${props.competitionId}-exam-results.csv`
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
    notify.success('成绩单 CSV 已导出')
  } catch (e) {
    notify.error(errorMessage(e, '导出失败'))
  } finally {
    exporting.value = false
  }
}

async function savePaper() {
  if (!paperForm.value.title.trim()) {
    notify.warning('请填写试卷标题')
    return
  }
  savingPaper.value = true
  try {
    data.value = await adminExamApi.updatePaper(props.competitionId, {
      title: paperForm.value.title.trim(),
      description: paperForm.value.description.trim(),
      duration_seconds: paperForm.value.durationSeconds,
      pass_score: paperForm.value.passScore,
    })
    notify.success('试卷配置已保存')
  } catch (e) {
    notify.error(errorMessage(e, '保存失败'))
  } finally {
    savingPaper.value = false
  }
}

function emptyQuestionForm() {
  const nextSeq = questions.value.reduce((max, q) => Math.max(max, q.seq), 0) + 1
  return {
    seq: nextSeq,
    type: 'single' as ExamQuestionType,
    title: '',
    content: '',
    optionsJSON: '[{"key":"A","text":""},{"key":"B","text":""}]',
    answerText: '',
    explanation: '',
    score: 5,
    challengeID: 0,
  }
}

function openCreate() {
  editingId.value = null
  questionForm.value = emptyQuestionForm()
  showQuestion.value = true
}

function openEdit(row: ExamQuestionAdmin) {
  editingId.value = row.id
  questionForm.value = {
    seq: row.seq,
    type: row.type,
    title: row.title,
    content: row.content,
    optionsJSON: JSON.stringify(row.options, null, 2),
    answerText: Array.isArray(row.answer) ? row.answer.join(',') : row.answer === null ? '' : String(row.answer),
    explanation: row.explanation,
    score: row.score,
    challengeID: row.challenge_id,
  }
  showQuestion.value = true
}

function parseOptions(raw: string): ExamOption[] | null {
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) throw new Error('not array')
    const options = parsed.map((item) => ({
      key: String(item?.key ?? '').trim(),
      text: String(item?.text ?? '').trim(),
    }))
    if (options.some((item) => !item.key || !item.text)) throw new Error('blank')
    return options
  } catch {
    notify.error('选项必须是 JSON 数组，如 [{"key":"A","text":"选项内容"}]')
    return null
  }
}

function buildAnswer(): unknown {
  switch (questionForm.value.type) {
    case 'single':
      return questionForm.value.answerText.trim()
    case 'multiple':
      return questionForm.value.answerText
        .split(/[,，]/)
        .map((item) => item.trim())
        .filter(Boolean)
    case 'truefalse':
      return questionForm.value.answerText.trim() === 'true'
    default:
      return null
  }
}

async function saveQuestion() {
  const form = questionForm.value
  if (!form.title.trim()) {
    notify.warning('请填写题目标题')
    return
  }
  const body: ExamQuestionInput = {
    seq: form.seq,
    type: form.type,
    title: form.title.trim(),
    content: form.content,
    explanation: form.explanation,
    score: form.score,
  }
  if (form.type === 'single' || form.type === 'multiple') {
    const options = parseOptions(form.optionsJSON)
    if (!options) return
    body.options = options
  }
  if (form.type === 'code') {
    if (!form.challengeID) {
      notify.warning('编程题需要绑定 Challenge ID')
      return
    }
    body.challenge_id = form.challengeID
    body.answer = null
  } else {
    body.answer = buildAnswer()
  }
  questionSaving.value = true
  try {
    if (editingId.value) {
      await adminExamApi.updateQuestion(props.competitionId, editingId.value, body)
      notify.success('试题已更新')
    } else {
      await adminExamApi.createQuestion(props.competitionId, body)
      notify.success('试题已创建')
    }
    showQuestion.value = false
    await load()
  } catch (e) {
    notify.error(errorMessage(e, '试题保存失败'))
  } finally {
    questionSaving.value = false
  }
}

async function removeQuestion(row: ExamQuestionAdmin) {
  await dialogs.confirm(`删除第 ${row.seq} 题「${row.title}」？试卷总分将同步更新。`, '删除试题')
  try {
    await adminExamApi.deleteQuestion(props.competitionId, row.id)
    notify.success('试题已删除')
    await load()
  } catch (e) {
    notify.error(errorMessage(e, '删除失败'))
  }
}

function answerLabel(row: ExamQuestionAdmin) {
  if (row.type === 'code') return `Challenge #${row.challenge_id}`
  if (row.type === 'truefalse') return row.answer ? '正确' : '错误'
  if (Array.isArray(row.answer)) return row.answer.join('、') || '—'
  return row.answer === null || row.answer === '' ? '—' : String(row.answer)
}

onMounted(async () => {
  await load()
  await loadResults()
})
</script>

<template>
  <div class="exam-admin">
    <div class="head">
      <h2>
        {{ detail.name }}
        <UiTag size="small" :type="detail.kind === 'public' ? 'success' : 'warning'">
          {{ KIND_LABELS[detail.kind] }}
        </UiTag>
        <UiTag size="small" type="info">{{ FORMAT_LABELS[detail.format as CompetitionFormat] }}</UiTag>
        <UiTag size="small" :type="locked ? 'warning' : 'success'">{{ locked ? '已锁定' : '可编辑' }}</UiTag>
      </h2>
      <router-link to="/admin/panel/competitions"><UiButton size="small">← 返回列表</UiButton></router-link>
    </div>
    <p class="hint">
      {{ fmt(detail.start_time) }} ~ {{ fmt(detail.end_time) }}
      <span v-if="detail.description"> · {{ detail.description }}</span>
    </p>

    <UiAlert v-if="loadError" type="error" :closable="false" :title="loadError" class="load-error">
      <UiButton size="small" @click="load">重试</UiButton>
    </UiAlert>

    <UiSkeleton v-if="loading && !data" :rows="7" />
    <template v-else-if="data">
      <UiCard header="试卷配置">
        <UiAlert
          v-if="locked"
          type="warning"
          :closable="false"
          title="试卷已锁定"
          description="竞赛开始后不可修改试卷与试题；如需调整请先取消或重新创建竞赛。"
          class="paper-tip"
        />
        <UiForm label-width="120px" class="paper-form">
          <UiFormItem label="试卷标题">
            <UiInput v-model="paperForm.title" :disabled="locked" maxlength="200" />
          </UiFormItem>
          <UiFormItem label="说明">
            <UiInput v-model="paperForm.description" type="textarea" :rows="3" :disabled="locked" maxlength="2000" />
          </UiFormItem>
          <UiFormItem label="时长（秒）">
            <UiInputNumber v-model="paperForm.durationSeconds" :min="300" :max="86400" :disabled="locked" />
            <div class="dim">服务端计时：min(开始 + 时长, 竞赛结束时间)。</div>
          </UiFormItem>
          <UiFormItem label="通过分">
            <UiInputNumber v-model="paperForm.passScore" :min="0" :max="100000" :disabled="locked" />
            <div class="dim">0 = 不设通过线。</div>
          </UiFormItem>
          <UiFormItem label="试卷总分">
            <span class="mono total">{{ totalScore }}</span>
          </UiFormItem>
        </UiForm>
        <div class="actions">
          <UiButton type="primary" :loading="savingPaper" :disabled="locked || savingPaper" @click="savePaper">
            保存试卷
          </UiButton>
        </div>
      </UiCard>

      <UiCard header="试题">
        <div class="toolbar">
          <span class="hint">共 {{ questions.length }} 题 · 总分 {{ totalScore }}</span>
          <UiButton type="primary" :disabled="locked" @click="openCreate">新增试题</UiButton>
        </div>
        <UiTable :data="questions" row-key="id" empty-text="暂无试题，点击「新增试题」开始组卷">
          <UiTableColumn prop="seq" label="题序" width="80" align="center" />
          <UiTableColumn label="题型" width="90" align="center">
            <template #default="{ row }">
              <UiTag size="small" type="info">{{ TYPE_LABELS[row.type as ExamQuestionType] }}</UiTag>
            </template>
          </UiTableColumn>
          <UiTableColumn prop="title" label="题目" min-width="220" align="left" />
          <UiTableColumn prop="score" label="分值" width="90" align="right" />
          <UiTableColumn label="答案" min-width="160" align="left">
            <template #default="{ row }">{{ answerLabel(row) }}</template>
          </UiTableColumn>
          <UiTableColumn label="操作" width="170" align="right">
            <template #default="{ row }">
              <UiButton size="small" :disabled="locked" @click="openEdit(row)">编辑</UiButton>
              <UiButton size="small" variant="ghost" :disabled="locked" @click="removeQuestion(row)">删除</UiButton>
            </template>
          </UiTableColumn>
        </UiTable>
      </UiCard>

      <UiCard header="成绩单">
        <div class="toolbar">
          <div class="result-head">
            <span class="hint">按总分降序、用时升序排名；未交卷不参与排名。</span>
            <span v-if="summary" class="dim">
              参赛 {{ summary.participants }} · 已开始 {{ summary.started }} · 已交卷
              {{ summary.submitted }} · 超时 {{ summary.expired }} · 通过 {{ summary.passed }}
            </span>
          </div>
          <div class="actions">
            <UiButton size="small" :loading="resultsLoading" :disabled="resultsLoading" @click="loadResults">
              刷新
            </UiButton>
            <UiButton size="small" type="primary" :loading="exporting" :disabled="exporting || !results" @click="exportResults">
              导出成绩 CSV
            </UiButton>
          </div>
        </div>
        <div v-if="summary" class="result-stats">
          <div class="result-stat">
            <span>参赛人数</span>
            <b>{{ summary.participants }}</b>
          </div>
          <div class="result-stat">
            <span>已交卷</span>
            <b>{{ summary.submitted + summary.expired }}</b>
          </div>
          <div class="result-stat">
            <span>通过人数</span>
            <b>{{ summary.passed }}</b>
          </div>
          <div class="result-stat">
            <span>平均分</span>
            <b>{{ summary.average_score || 0 }}</b>
          </div>
          <div class="result-stat">
            <span>最高分</span>
            <b>{{ summary.highest_score }}</b>
          </div>
        </div>
        <UiAlert v-if="resultsError" type="error" :closable="false" :title="resultsError" class="results-error">
          <UiButton size="small" @click="loadResults">重试</UiButton>
        </UiAlert>
        <UiSkeleton v-if="resultsLoading && !results" :rows="5" />
        <UiTable v-else :data="resultRows" row-key="user_id" empty-text="暂无参赛者成绩">
          <UiTableColumn prop="rank" label="排名" width="70" align="center">
            <template #default="{ row }">{{ row.rank || '—' }}</template>
          </UiTableColumn>
          <UiTableColumn prop="username" label="参赛者" min-width="140" align="left" />
          <UiTableColumn label="状态" width="110" align="center">
            <template #default="{ row }">
              <UiTag size="small" :type="resultStatusType(row.status as ExamResultStatus)">
                {{ resultStatusLabel(row.status as ExamResultStatus) }}
              </UiTag>
            </template>
          </UiTableColumn>
          <UiTableColumn prop="total_score" label="总分" width="80" align="right" />
          <UiTableColumn prop="objective_score" label="客观题" width="80" align="right" />
          <UiTableColumn prop="code_score" label="编程题" width="80" align="right" />
          <UiTableColumn label="是否通过" width="90" align="center">
            <template #default="{ row }">
              <UiTag
                v-if="row.status === 'submitted' || row.status === 'expired'"
                size="small"
                :type="row.passed ? 'success' : 'warning'"
              >
                {{ row.passed ? '通过' : '未通过' }}
              </UiTag>
              <span v-else class="dim">—</span>
            </template>
          </UiTableColumn>
          <UiTableColumn label="用时" width="90" align="right">
            <template #default="{ row }">{{ fmtDuration(row.duration_seconds) }}</template>
          </UiTableColumn>
          <UiTableColumn label="交卷时间" min-width="170" align="left">
            <template #default="{ row }">{{ fmt(row.submitted_at) }}</template>
          </UiTableColumn>
        </UiTable>
      </UiCard>
    </template>

    <CompetitionQualificationPanel
      v-if="detail.kind === 'invitational'"
      :competition-id="competitionId"
      :disabled="qualificationDisabled"
    />
    <CompetitionCertificatePanel :competition-id="competitionId" :allowed="certificateAllowed" />

    <UiDialog v-model="showQuestion" :title="editingId ? '编辑试题' : '新增试题'" width="760px">
      <UiForm label-width="120px">
        <UiFormItem label="题型">
          <UiSelect v-model="questionForm.type">
            <UiOption label="单选" value="single" />
            <UiOption label="多选" value="multiple" />
            <UiOption label="判断" value="truefalse" />
            <UiOption label="编程" value="code" />
          </UiSelect>
        </UiFormItem>
        <UiFormItem label="题序">
          <UiInputNumber v-model="questionForm.seq" :min="1" :max="999" />
          <div class="dim">试卷内唯一，决定答题顺序。</div>
        </UiFormItem>
        <UiFormItem label="题目标题">
          <UiInput v-model="questionForm.title" maxlength="200" />
        </UiFormItem>
        <UiFormItem label="题干">
          <UiInput v-model="questionForm.content" type="textarea" :rows="4" placeholder="支持 Markdown" />
        </UiFormItem>
        <template v-if="questionForm.type === 'single' || questionForm.type === 'multiple'">
          <UiFormItem label="选项 JSON">
            <UiInput v-model="questionForm.optionsJSON" type="textarea" :rows="6" class="mono" />
            <div class="dim">格式：[{&quot;key&quot;:&quot;A&quot;,&quot;text&quot;:&quot;选项内容&quot;}]，key 为大写字母/数字。</div>
          </UiFormItem>
        </template>
        <UiFormItem v-if="questionForm.type === 'code'" label="Challenge ID">
          <UiInputNumber v-model="questionForm.challengeID" :min="0" />
          <div class="dim">绑定 type=code 的题目；选手提交走判题服务的 exam scope。</div>
        </UiFormItem>
        <UiFormItem v-else label="答案">
          <UiInput v-model="questionForm.answerText" :placeholder="answerHint" />
          <div class="dim">{{ answerHint }}</div>
        </UiFormItem>
        <UiFormItem label="解析">
          <UiInput v-model="questionForm.explanation" type="textarea" :rows="3" placeholder="可选；交卷后展示" />
        </UiFormItem>
        <UiFormItem label="分值">
          <UiInputNumber v-model="questionForm.score" :min="1" :max="1000" />
        </UiFormItem>
      </UiForm>
      <template #footer>
        <UiButton @click="showQuestion = false">取消</UiButton>
        <UiButton type="primary" :loading="questionSaving" :disabled="questionSaving" @click="saveQuestion">
          保存试题
        </UiButton>
      </template>
    </UiDialog>
  </div>
</template>

<style scoped>
.exam-admin { display: grid; gap: var(--space-5); }
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); flex-wrap: wrap; }
.head h2 { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: 0; font-size: var(--text-xl); font-weight: 600; }
.hint { margin: 0; color: var(--muted-foreground); font-size: var(--text-sm); line-height: 1.6; }
.load-error { max-width: 720px; }
.paper-tip { margin-bottom: var(--space-4); }
.paper-form { max-width: 720px; }
.total { font-size: var(--text-xl); font-weight: 600; }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-3); }
.actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.dim { color: var(--muted-foreground); font-size: var(--text-xs); }
.result-head { display: grid; gap: var(--space-1); }
.result-stats {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.result-stat {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--muted);
}
.result-stat span { font-size: var(--text-xs); color: var(--muted-foreground); }
.result-stat b { font-size: var(--text-lg); font-weight: 600; font-variant-numeric: tabular-nums; }
.results-error { margin-bottom: var(--space-3); }
@media (max-width: 900px) {
  .result-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
