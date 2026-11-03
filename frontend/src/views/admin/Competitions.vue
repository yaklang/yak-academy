<script setup lang="ts">
// 管理面板：竞赛管理（列表 + 创建）。竞赛详情/题目/参赛者/容器/警告/WP/榜单
// 在详情页按标签页组织（/admin/panel/competitions/:id）。
// 状态由起止时间派生（scheduled/running/ended）：进行中的竞赛可手动结束
// （end_time 置为当前时刻），已结束的竞赛可在详情页重新配置再次开赛。
import { onMounted, ref } from 'vue'
import { notify, dialogs } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { adminCompetitionApi, KIND_LABELS, type Competition, type CompetitionKind } from '../../api'

const items = ref<Competition[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const form = ref({
  name: '',
  description: '',
  kind: 'invitational' as CompetitionKind,
  range: null as [Date, Date] | null,
})

function openCreate() {
  form.value = { name: '', description: '', kind: 'invitational', range: null }
  showCreate.value = true
}

// el-date-picker datetime → RFC3339（toISOString 即 UTC 标准形式）。
function toRfc3339(d: Date | undefined): string | null {
  return d ? d.toISOString() : null
}

async function submitCreate() {
  if (!form.value.name.trim()) {
    notify.warning('竞赛名称必填')
    return
  }
  const [start, end] = form.value.range ?? []
  const startTime = toRfc3339(start)
  const endTime = toRfc3339(end)
  if (!startTime || !endTime) {
    notify.warning('请选择起止时间')
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
    })
    notify.success('竞赛已创建')
    showCreate.value = false
    await load()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    creating.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const resp = await adminCompetitionApi.list(page.value, pageSize.value)
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function statusType(s: Competition['status']) {
  return s === 'running' ? 'success' : s === 'scheduled' ? 'warning' : 'info'
}

function statusLabel(s: Competition['status']) {
  return s === 'running' ? '进行中' : s === 'scheduled' ? '未开始' : '已结束'
}

const deleting = ref('')
const ending = ref('')

// 手动结束竞赛（仅 running）：end_time 置为当前时刻，参赛者解锁、WP 开放提交。
async function end(row: Competition) {
  try {
    await dialogs.confirm(
      `手动结束竞赛「${row.name}」？立即生效：参赛者解除锁定、停止提交与容器，WP 转为可提交。结束后仍可在详情页重新配置起止时间再次开赛。`,
      '手动结束竞赛',
      { type: 'warning', confirmButtonText: '结束竞赛', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  ending.value = String(row.id)
  try {
    await adminCompetitionApi.end(row.id)
    notify.success(`竞赛「${row.name}」已结束`)
    await load()
  } catch (e) {
    notify.error((e as Error).message || '操作失败')
  } finally {
    ending.value = ''
  }
}

// 删除竞赛：running 期间后端 422 拉回提示，前端直接给出可操作提示
// （先手动结束再删）。
async function remove(row: Competition) {
  try {
    await dialogs.confirm(
      `删除竞赛「${row.name}」？参赛者、题目绑定、提交、WP 与容器记录将一并清除，不可恢复。`,
      '删除竞赛',
      { type: 'warning', confirmButtonText: '删除', confirmButtonClass: 'el-button--danger' },
    )
  } catch {
    return
  }
  deleting.value = String(row.id)
  try {
    await adminCompetitionApi.delete(row.id)
    notify.success(`竞赛「${row.name}」已删除`)
    // 当前页删空时回退一页（列表不出现空页）。
    if (items.value.length === 1 && page.value > 1) page.value--
    await load()
  } catch (e) {
    const msg = (e as Error).message || '删除失败'
    notify.error(
      row.status === 'running' ? `${msg}（进行中的竞赛不可删除，可先手动结束再删除）` : msg,
    )
  } finally {
    deleting.value = ''
  }
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">竞赛<span class="accent">管理</span></h2>
      <p class="page-sub">
        创建与配置竞赛（起止时间决定状态与参赛锁定）；详情页内管理题目、参赛人员、容器、反作弊警告、WP 与排行榜。
      </p>
    </div>
  </header>

  <UiCard shadow="never" class="block">
    <template #header>
      <div class="card-head">
        <span>竞赛列表</span>
        <UiButton type="primary" size="small" @click="openCreate">创建竞赛</UiButton>
      </div>
    </template>
    <UiTable :data="items" v-busy="loading" empty-text="暂无竞赛">
      <UiTableColumn prop="id" label="ID" width="70" />
      <UiTableColumn prop="name" label="名称" min-width="160" />
      <UiTableColumn label="赛制" width="100">
        <template #default="{ row }">
          <UiTag :type="row.kind === 'public' ? 'success' : 'warning'" size="small">
            {{ KIND_LABELS[row.kind as CompetitionKind] }}
          </UiTag>
        </template>
      </UiTableColumn>
      <UiTableColumn label="状态" width="100">
        <template #default="{ row }">
          <UiTag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</UiTag>
        </template>
      </UiTableColumn>
      <UiTableColumn label="开始时间" width="170">
        <template #default="{ row }">{{ fmt(row.start_time) }}</template>
      </UiTableColumn>
      <UiTableColumn label="结束时间" width="170">
        <template #default="{ row }">{{ fmt(row.end_time) }}</template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="220">
        <template #default="{ row }">
          <router-link :to="`/admin/panel/competitions/${row.id}`">
            <UiButton size="small" type="primary" link>详情与配置</UiButton>
          </router-link>
          <UiButton
            v-if="row.status === 'running'"
            size="small"
            type="danger"
            link
            :loading="ending === String(row.id)"
            @click="end(row)"
          >结束</UiButton>
          <UiButton
            size="small"
            type="danger"
            link
            :loading="deleting === String(row.id)"
            @click="remove(row)"
          >删除</UiButton>
        </template>
      </UiTableColumn>
    </UiTable>
    <UiPagination
      v-if="total > 0"
      background
      layout="total, prev, pager, next"
      :total="total"
      :page-size="pageSize"
      v-model:current-page="page"
      @current-change="load"
      class="pager"
    />
  </UiCard>

  <UiDialog v-model="showCreate" title="创建竞赛" width="560px">
    <UiForm label-width="90px">
      <UiFormItem label="竞赛名称" required>
        <UiInput v-model="form.name" placeholder="如：2026 春季新生赛" maxlength="100" />
      </UiFormItem>
      <UiFormItem label="竞赛说明">
        <UiInput v-model="form.description" type="textarea" :rows="3" placeholder="展示给参赛者的说明（Markdown，可选）" />
      </UiFormItem>
      <UiFormItem label="赛制" required>
        <UiRadioGroup v-model="form.kind">
          <UiRadio value="invitational">邀请赛（定向导入/生成参赛者，凭邀请码入场）</UiRadio>
          <UiRadio value="public">公开赛（竞赛广场自助报名，已报名者免码入场）</UiRadio>
        </UiRadioGroup>
        <div class="dim">赛制创建后不可更改。</div>
      </UiFormItem>
      <UiFormItem label="起止时间" required>
        <UiDatePicker
          v-model="form.range"
          type="datetimerange"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          style="width: 100%"
        />
      </UiFormItem>
    </UiForm>
    <template #footer>
      <UiButton @click="showCreate = false">取消</UiButton>
      <UiButton type="primary" :loading="creating" @click="submitCreate">创建</UiButton>
    </template>
  </UiDialog>
</template>

<style scoped>
.dim { color: var(--muted-foreground); font-size: 0.8rem; margin-top: 4px; }
.block { margin-bottom: 1.2rem; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.pager { margin-top: 1rem; }
</style>
