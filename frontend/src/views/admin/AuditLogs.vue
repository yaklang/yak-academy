<script setup lang="ts">
// 管理面板：操作审计。append-only 管理操作轨迹（上传/发布/回滚/删除/竞赛/用户/
// 备份等），与 Event 学生行为流分工；可按动作与管理员过滤，新→旧分页。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { auditApi, type AdminAuditLog } from '../../api'

const items = ref<AdminAuditLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const actionFilter = ref('')
const adminFilter = ref<number | null>(null)

// 动作清单与后端 handler 钩子一一对应（content./challenge./user./competition./
// builtin./engine./instance./topic./backup. 前缀）。
const actionOptions = [
  'content.upload', 'content.delete', 'content.publish', 'content.archive', 'content.rollback',
  'challenge.visibility', 'challenge.import', 'challenge.package_import', 'challenge.delete',
  'attachment.upload', 'attachment.delete',
  'builtin.import', 'builtin.image_build', 'engine.download',
  'user.create', 'user.roles', 'user.reset_password',
  'competition.create', 'competition.update', 'competition.end', 'competition.delete',
  'competition.bind', 'competition.unbind', 'competition.import_participants',
  'topic.update', 'instance.destroy', 'backup.create',
]

async function load() {
  loading.value = true
  try {
    const resp = await auditApi.list({
      page: page.value, page_size: pageSize,
      action: actionFilter.value || undefined,
      admin_id: adminFilter.value ?? undefined,
    })
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function applyFilter() {
  page.value = 1
  load()
}

function fmt(ts: string) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

onMounted(load)
</script>

<template>
  <h2>操作审计</h2>
  <p class="hint">
    管理端写操作（内容上传/发布/回滚/删除、题目与附件、竞赛、用户与角色、备份等）的
    append-only 审计轨迹；与学员行为事件流（Event 表）分工，不参与积分重放。
  </p>

  <el-card class="block" shadow="never">
    <template #header>
      <div class="toolbar">
        <el-select v-model="actionFilter" placeholder="动作（全部）" clearable filterable size="small" style="width: 220px" @change="applyFilter">
          <el-option v-for="a in actionOptions" :key="a" :label="a" :value="a" />
        </el-select>
        <el-input-number v-model="adminFilter" :min="1" placeholder="管理员 ID" size="small" clearable style="width: 140px" @change="applyFilter" />
        <el-button size="small" type="primary" @click="applyFilter">查询</el-button>
        <el-button size="small" @click="load">刷新</el-button>
      </div>
    </template>
    <el-table :data="items" v-loading="loading" empty-text="暂无审计记录">
      <el-table-column label="时间" width="180">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="管理员" width="140">
        <template #default="{ row }">{{ row.admin_name }}<span class="dim"> #{{ row.admin_id }}</span></template>
      </el-table-column>
      <el-table-column prop="action" label="动作" width="200">
        <template #default="{ row }"><code>{{ row.action }}</code></template>
      </el-table-column>
      <el-table-column label="目标" min-width="180">
        <template #default="{ row }"><span class="dim">{{ row.target_type }}/</span>{{ row.target_id }}</template>
      </el-table-column>
      <el-table-column prop="ip" label="来源 IP" width="140" />
      <el-table-column label="详情" min-width="220">
        <template #default="{ row }">
          <code v-if="row.detail" class="detail">{{ JSON.stringify(row.detail) }}</code>
          <span v-else class="dim">-</span>
        </template>
      </el-table-column>
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="page"
        layout="prev, pager, next, total"
        :page-size="pageSize"
        :total="total"
        @current-change="load"
      />
    </div>
  </el-card>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.block { margin-bottom: 1.2rem; }
.toolbar { display: flex; gap: 0.6rem; align-items: center; }
.dim { color: var(--el-text-color-secondary); }
.detail { font-size: 0.8rem; word-break: break-all; }
.pager { margin-top: 0.8rem; display: flex; justify-content: flex-end; }
</style>
