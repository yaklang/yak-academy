<script setup lang="ts">
// 管理面板：竞赛管理（列表 + 创建）。竞赛详情/题目/参赛者/容器/警告/WP/榜单
// 在详情页按标签页组织（/admin/panel/competitions/:id）。
// 状态由起止时间派生（scheduled/running/ended），无"手动开始/结束"。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminCompetitionApi, type Competition } from '../../api'

const items = ref<Competition[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)

const showCreate = ref(false)
const creating = ref(false)
const form = ref({ name: '', description: '', range: null as [Date, Date] | null })

function openCreate() {
  form.value = { name: '', description: '', range: null }
  showCreate.value = true
}

// el-date-picker datetime → RFC3339（toISOString 即 UTC 标准形式）。
function toRfc3339(d: Date | undefined): string | null {
  return d ? d.toISOString() : null
}

async function submitCreate() {
  if (!form.value.name.trim()) {
    ElMessage.warning('竞赛名称必填')
    return
  }
  const [start, end] = form.value.range ?? []
  const startTime = toRfc3339(start)
  const endTime = toRfc3339(end)
  if (!startTime || !endTime) {
    ElMessage.warning('请选择起止时间')
    return
  }
  creating.value = true
  try {
    await adminCompetitionApi.create({
      name: form.value.name.trim(),
      description: form.value.description,
      start_time: startTime,
      end_time: endTime,
    })
    ElMessage.success('竞赛已创建')
    showCreate.value = false
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
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
    ElMessage.error((e as Error).message)
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

function fmt(ts: string) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>竞赛管理</h2>
  <p class="hint">
    创建与配置竞赛（起止时间决定状态与参赛锁定）；详情页内管理题目、参赛人员、容器、反作弊警告、WP 与排行榜。
  </p>

  <el-card shadow="never" class="block">
    <template #header>
      <div class="card-head">
        <span>竞赛列表</span>
        <el-button type="primary" size="small" @click="openCreate">创建竞赛</el-button>
      </div>
    </template>
    <el-table :data="items" v-loading="loading" empty-text="暂无竞赛">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="开始时间" width="170">
        <template #default="{ row }">{{ fmt(row.start_time) }}</template>
      </el-table-column>
      <el-table-column label="结束时间" width="170">
        <template #default="{ row }">{{ fmt(row.end_time) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="110">
        <template #default="{ row }">
          <router-link :to="`/admin/panel/competitions/${row.id}`">
            <el-button size="small" type="primary" link>详情与配置</el-button>
          </router-link>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      background
      layout="total, prev, pager, next"
      :total="total"
      :page-size="pageSize"
      v-model:current-page="page"
      @current-change="load"
      class="pager"
    />
  </el-card>

  <el-dialog v-model="showCreate" title="创建竞赛" width="560px">
    <el-form label-width="90px">
      <el-form-item label="竞赛名称" required>
        <el-input v-model="form.name" placeholder="如：2026 春季新生赛" maxlength="100" />
      </el-form-item>
      <el-form-item label="竞赛说明">
        <el-input v-model="form.description" type="textarea" :rows="3" placeholder="展示给参赛者的说明（Markdown，可选）" />
      </el-form-item>
      <el-form-item label="起止时间" required>
        <el-date-picker
          v-model="form.range"
          type="datetimerange"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          style="width: 100%"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showCreate = false">取消</el-button>
      <el-button type="primary" :loading="creating" @click="submitCreate">创建</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.block { margin-bottom: 1.2rem; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.pager { margin-top: 1rem; }
</style>
