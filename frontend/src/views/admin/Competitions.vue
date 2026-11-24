<script setup lang="ts">
// 管理面板：竞赛管理（列表 + 创建）。竞赛详情、题目、参赛者、容器、警告、
// 解题报告与榜单在详情页按标签页组织（/admin/panel/competitions/:id）。
// 状态由起止时间派生；进行中的竞赛可手动结束（结束时间置为当前），
// 已结束的竞赛可在详情页重新配置起止时间再次开赛。
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { Button } from '@/components/ui/button'
import {
  adminCompetitionApi,
  FORMAT_LABELS,
  KIND_LABELS,
  SCORING_MODE_LABELS,
  type Competition,
  type CompetitionFormat,
  type CompetitionKind,
  type CompetitionScoringMode,
} from '../../api'

const items = ref<Competition[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const loadError = ref('')

const showCreate = ref(false)
const creating = ref(false)
const createError = ref('')
const form = ref({
  name: '',
  description: '',
  kind: 'invitational' as CompetitionKind,
  format: 'ctf' as CompetitionFormat,
  scoringMode: 'dynamic' as CompetitionScoringMode,
  range: null as [Date, Date] | null,
  registrationRange: null as [Date, Date] | null,
})

function openCreate() {
  form.value = {
    name: '',
    description: '',
    kind: 'invitational',
    format: 'ctf',
    scoringMode: 'dynamic',
    range: null,
    registrationRange: null,
  }
  createError.value = ''
  showCreate.value = true
}

// 日期选择器 → RFC3339（toISOString 即 UTC 标准形式）。
function toRfc3339(date: Date | undefined): string | null {
  return date ? date.toISOString() : null
}

async function submitCreate() {
  createError.value = ''
  if (!form.value.name.trim()) {
    createError.value = '竞赛名称必填'
    return
  }
  const [start, end] = form.value.range ?? []
  const startTime = toRfc3339(start)
  const endTime = toRfc3339(end)
  if (!startTime || !endTime) {
    createError.value = '请选择起止时间'
    return
  }
  const [regStart, regEnd] = form.value.registrationRange ?? []
  const regStartTime = toRfc3339(regStart)
  const regEndTime = toRfc3339(regEnd)
  if ((regStartTime && !regEndTime) || (!regStartTime && regEndTime)) {
    createError.value = '报名窗口需同时选择开始与截止时间'
    return
  }
  if (regEndTime && new Date(regEndTime) > new Date(endTime)) {
    createError.value = '报名截止时间不能晚于竞赛结束时间'
    return
  }
  creating.value = true
  try {
    await adminCompetitionApi.create({
      name: form.value.name.trim(),
      description: form.value.description,
      start_time: startTime,
      end_time: endTime,
      kind: form.value.kind,
      format: form.value.format,
      scoring_mode: form.value.format === 'ctf' ? form.value.scoringMode : 'dynamic',
      registration_start: regStartTime,
      registration_end: regEndTime,
    })
    notify.success('竞赛已创建')
    showCreate.value = false
    await load()
  } catch (e) {
    createError.value = e instanceof Error ? e.message : '创建失败，请稍后重试'
  } finally {
    creating.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  try {
    const resp = await adminCompetitionApi.list(page.value, pageSize.value)
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '竞赛列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

const deleting = ref('')
const ending = ref('')
const endTarget = ref<Competition | null>(null)
const deleteTarget = ref<Competition | null>(null)

// 手动结束竞赛（仅进行中）：立即解锁参赛者、停止提交与容器，解题报告开放提交。
async function confirmEnd() {
  const row = endTarget.value
  if (!row) return
  ending.value = String(row.id)
  try {
    await adminCompetitionApi.end(row.id)
    notify.success(`竞赛「${row.name}」已结束`)
    endTarget.value = null
    await load()
  } catch (e) {
    notify.error((e as Error).message || '操作失败')
  } finally {
    ending.value = ''
  }
}

// 删除竞赛：进行中的竞赛后端拒绝，前端提示先手动结束再删。
async function confirmDelete() {
  const row = deleteTarget.value
  if (!row) return
  deleting.value = String(row.id)
  try {
    await adminCompetitionApi.delete(row.id)
    notify.success(`竞赛「${row.name}」已删除`)
    deleteTarget.value = null
    // 当前页删空时回退一页（列表不出现空页）。
    if (items.value.length === 1 && page.value > 1) page.value--
    await load()
  } catch (e) {
    const message = (e as Error).message || '删除失败'
    notify.error(
      row.status === 'running' ? `${message}（进行中的竞赛不可删除，可先手动结束再删除）` : message,
    )
  } finally {
    deleting.value = ''
  }
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

load()
</script>

<template>
  <PageHeader
    title="竞赛管理"
    description="起止时间决定竞赛状态与邀请赛锁定；详情页内管理题目、参赛人员、容器、反作弊警告、解题报告与排行榜。"
  >
    <template #actions>
      <Button type="button" @click="openCreate">创建竞赛</Button>
    </template>
  </PageHeader>

  <DataTablePanel
    v-if="!loadError"
    title="竞赛列表"
    :count="total"
    :description="loading ? '正在加载竞赛…' : undefined"
  >
    <UiTable
      :data="items"
      :loading="loading"
      :loading-rows="6"
      aria-label="竞赛列表"
    >
      <UiTableColumn label="竞赛" min-width="220">
        <template #default="{ row }">
          <span class="competition-name">{{ row.name }}</span>
          <span class="competition-window">{{ fmt(row.start_time) }} — {{ fmt(row.end_time) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="形式" width="100">
        <template #default="{ row }">
          <span class="meta-chip">{{ FORMAT_LABELS[row.format as CompetitionFormat] }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="报名模式" width="110">
        <template #default="{ row }">
          <span class="meta-chip">{{ KIND_LABELS[row.kind as CompetitionKind] }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <StatusTag kind="competition" :value="row.status" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="开始时间" width="170">
        <template #default="{ row }">
          <span class="competition-time">{{ fmt(row.start_time) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="结束时间" width="170">
        <template #default="{ row }">
          <span class="competition-time">{{ fmt(row.end_time) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="210">
        <template #default="{ row }">
          <div class="row-actions">
            <Button variant="outline" size="sm" as-child>
              <RouterLink :to="`/admin/panel/competitions/${row.id}`">详情与配置</RouterLink>
            </Button>
            <Button
              v-if="row.status === 'running'"
              type="button"
              variant="ghost"
              size="sm"
              :disabled="ending === String(row.id)"
              @click="endTarget = row"
            >
              {{ ending === String(row.id) ? '结束中…' : '结束' }}
            </Button>
            <Button
              type="button"
              variant="link"
              size="sm"
              class="danger-action"
              :disabled="deleting === String(row.id)"
              @click="deleteTarget = row"
            >
              删除
            </Button>
          </div>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          title="还没有竞赛"
          description="创建竞赛后，可在详情页配置题目、参赛人员与容器。"
          action-label="创建竞赛"
          @action="openCreate"
        />
      </template>
    </UiTable>
    <template v-if="total > 0" #footer>
      <UiPagination
        v-model:current-page="page"
        background
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        @current-change="load"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="竞赛列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <UiDialog v-model="showCreate" title="创建竞赛" width="640px">
    <div v-if="createError" class="create-error" role="alert">{{ createError }}</div>
    <UiForm label-width="90px">
      <UiFormItem label="竞赛名称" required>
        <UiInput v-model="form.name" placeholder="如：2026 春季新生赛" maxlength="100" />
      </UiFormItem>
      <UiFormItem label="竞赛说明">
        <UiInput v-model="form.description" type="textarea" :rows="3" placeholder="展示给参赛者的说明（Markdown，可选）" />
      </UiFormItem>
      <UiFormItem label="竞赛形式" required>
        <UiRadioGroup v-model="form.format">
          <UiRadio value="ctf">CTF 解题赛</UiRadio>
          <UiRadio value="awd">AWD 攻防赛</UiRadio>
          <UiRadio value="exam">在线考试</UiRadio>
        </UiRadioGroup>
        <div class="form-hint">竞赛形式创建后不可更改。</div>
      </UiFormItem>
      <UiFormItem v-if="form.format === 'ctf'" label="计分模式">
        <UiRadioGroup v-model="form.scoringMode">
          <UiRadio value="dynamic">动态计分（按通过人数衰减）</UiRadio>
          <UiRadio value="fixed">固定计分（按题目分值）</UiRadio>
        </UiRadioGroup>
        <div class="form-hint">{{ SCORING_MODE_LABELS[form.scoringMode] }}；开赛后不可切换。</div>
      </UiFormItem>
      <UiFormItem label="报名模式" required>
        <UiRadioGroup v-model="form.kind">
          <UiRadio value="invitational">邀请赛（定向导入或生成参赛者，凭邀请码入场）</UiRadio>
          <UiRadio value="public">公开赛（竞赛广场自助报名，已报名者免码入场）</UiRadio>
        </UiRadioGroup>
        <div class="form-hint">报名模式创建后不可更改。</div>
      </UiFormItem>
      <UiFormItem label="起止时间" required>
        <UiDatePicker
          v-model="form.range"
          class="date-range"
          type="datetimerange"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
        />
      </UiFormItem>
      <UiFormItem label="报名窗口">
        <UiDatePicker
          v-model="form.registrationRange"
          class="date-range"
          type="datetimerange"
          start-placeholder="报名开始（可选）"
          end-placeholder="报名截止（可选）"
        />
        <div class="form-hint">公开赛可选；不填则报名开放到竞赛结束。截止不得晚于竞赛结束。</div>
      </UiFormItem>
    </UiForm>
    <template #footer>
      <Button type="button" variant="outline" @click="showCreate = false">取消</Button>
      <Button type="button" :disabled="creating" @click="submitCreate">
        {{ creating ? '创建中…' : '创建' }}
      </Button>
    </template>
  </UiDialog>

  <ConfirmDialog
    :open="!!endTarget"
    title="手动结束竞赛"
    :description="endTarget ? `立即结束竞赛「${endTarget.name}」：邀请赛参赛者解除锁定，停止提交与容器，解题报告开放提交。结束后仍可在详情页重新配置起止时间再次开赛。` : ''"
    confirm-label="结束竞赛"
    :loading="!!ending"
    @update:open="!$event && (endTarget = null)"
    @confirm="confirmEnd"
  />

  <ConfirmDialog
    :open="!!deleteTarget"
    title="删除竞赛"
    :description="deleteTarget ? `删除竞赛「${deleteTarget.name}」？参赛者、题目绑定、提交、解题报告与容器记录将一并清除，不可恢复。` : ''"
    confirm-label="删除"
    tone="danger"
    :loading="!!deleting"
    @update:open="!$event && (deleteTarget = null)"
    @confirm="confirmDelete"
  />
</template>

<style scoped>
.competition-name {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  overflow-wrap: anywhere;
}

.competition-window {
  display: block;
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  font-variant-numeric: tabular-nums;
}

.competition-time {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.meta-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  white-space: nowrap;
}

.row-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.danger-action {
  color: var(--status-danger);
}

.form-hint {
  margin-top: var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.date-range {
  width: 100%;
}

.create-error {
  margin-bottom: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border: 1px solid color-mix(in srgb, var(--status-danger) 30%, transparent);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 6%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-meta);
}
</style>