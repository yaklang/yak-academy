<script setup lang="ts">
// 管理面板：用户管理。表格 + 过滤/搜索：
//   q 对用户名/账号编码/手机号子串匹配；role 按角色；
//   active=false = 6 个月未登录（最近登录或注册超 6 个月）的不活跃账号；
//   no_solves=true = 从未完成过题目（无判定通过的提交）。
// 过滤条件与表格内标记（活跃/完成过题目）同口径，由服务端统一判定。
// 行内操作「重置密码」：管理员直接设新密码（不校验旧密码），目标用户全部
// 已登录会话立即失效——未绑定手机号的竞赛批量账号走此入口找回。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminApi, type AdminUserItem } from '../../api'

const items = ref<AdminUserItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)

// 重置密码对话框：目标用户 + 新密码（预填平台批量建号同款初始密码 123456，可改）。
const resetVisible = ref(false)
const resetTarget = ref<AdminUserItem | null>(null)
const resetPassword = ref('123456')
const resetting = ref(false)

// 过滤状态：q/role/active/noSolves。active/noSolves 用 '' | 'true' | 'false' 三态。
const q = ref('')
const role = ref<'' | 'student' | 'admin'>('')
const active = ref<'' | 'true' | 'false'>('')
const noSolves = ref<'' | 'true' | 'false'>('')

async function load() {
  loading.value = true
  try {
    const resp = await adminApi.listUsers({
      page: page.value,
      q: q.value.trim() || undefined,
      role: role.value || undefined,
      active: active.value === '' ? undefined : active.value === 'true',
      no_solves: noSolves.value === '' ? undefined : noSolves.value === 'true',
    })
    items.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

// 组合常用画像过滤：不活跃 + 从未完成题目。
function filterStaleNoSolves() {
  active.value = 'false'
  noSolves.value = 'true'
  search()
}

function fmt(ts: string | null | undefined) {
  return ts ? new Date(ts).toLocaleString() : '-'
}

function openReset(row: AdminUserItem) {
  resetTarget.value = row
  resetPassword.value = '123456'
  resetVisible.value = true
}

async function confirmReset() {
  if (!resetTarget.value) return
  if (resetPassword.value.length < 6) {
    ElMessage.warning('新密码至少 6 位')
    return
  }
  resetting.value = true
  try {
    await adminApi.resetPassword(resetTarget.value.id, resetPassword.value)
    ElMessage.success(`已重置 ${resetTarget.value.username} 的密码，其登录状态已全部失效`)
    resetVisible.value = false
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    resetting.value = false
  }
}

onMounted(load)
</script>

<template>
  <h2>用户管理</h2>
  <p class="hint">
    账号编码（YA{YY}-{NNNNN}）在建号时按年自动分配；6 个月未登录的账号标记为「不活跃」
    （从未登录的账号按注册时间起算）。
  </p>

  <el-card shadow="never">
    <div class="filters">
      <el-input
        v-model="q"
        placeholder="搜索：用户名 / 账号编码 / 手机号"
        clearable
        class="q-input"
        @keyup.enter="search"
        @clear="search"
      />
      <el-select v-model="role" placeholder="角色（全部）" clearable class="sel" @change="search">
        <el-option label="普通人" value="student" />
        <el-option label="管理员" value="admin" />
      </el-select>
      <el-select v-model="active" placeholder="活跃状态（全部）" clearable class="sel" @change="search">
        <el-option label="活跃" value="true" />
        <el-option label="不活跃（6 个月未登录）" value="false" />
      </el-select>
      <el-select v-model="noSolves" placeholder="解题情况（全部）" clearable class="sel" @change="search">
        <el-option label="从未完成题目" value="true" />
        <el-option label="完成过题目" value="false" />
      </el-select>
      <el-button type="primary" @click="search">搜索</el-button>
      <el-button @click="filterStaleNoSolves">不活跃且从未解题</el-button>
    </div>

    <el-table :data="items" v-loading="loading" empty-text="暂无用户">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="username" label="用户名" min-width="130" />
      <el-table-column label="账号编码" width="130">
        <template #default="{ row }"><code>{{ row.account_code || '-' }}</code></template>
      </el-table-column>
      <el-table-column label="角色" width="90">
        <template #default="{ row }">
          <el-tag :type="row.global_role === 'admin' ? 'danger' : 'info'" size="small">
            {{ row.global_role === 'admin' ? '管理员' : '普通人' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="活跃" width="100">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'warning'" size="small">{{ row.active ? '活跃' : '不活跃' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="完成题目" width="100">
        <template #default="{ row }">{{ row.solved_any ? '有' : '从未' }}</template>
      </el-table-column>
      <el-table-column label="最近登录" width="160">
        <template #default="{ row }">{{ fmt(row.last_login_at) }}</template>
      </el-table-column>
      <el-table-column label="注册时间" width="160">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="openReset(row)">重置密码</el-button>
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

  <!-- 管理员重置密码：不校验旧密码，重置后该用户全部已登录会话立即失效。
       默认填平台批量建号同款初始密码 123456，可改。 -->
  <el-dialog v-model="resetVisible" title="重置密码" width="420px">
    <template v-if="resetTarget">
      <p class="reset-who">
        目标账号：<b>{{ resetTarget.username }}</b>
        <code v-if="resetTarget.account_code">{{ resetTarget.account_code }}</code>
      </p>
      <el-form @submit.prevent="confirmReset">
        <el-form-item label="新密码">
          <el-input v-model="resetPassword" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
        <p class="hint">重置后该用户的全部登录状态立即失效，需用新密码重新登录。</p>
      </el-form>
    </template>
    <template #footer>
      <el-button @click="resetVisible = false">取消</el-button>
      <el-button type="primary" :loading="resetting" @click="confirmReset">重置</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.filters { display: flex; gap: 0.6rem; flex-wrap: wrap; margin-bottom: 1rem; align-items: center; }
.q-input { width: 260px; }
.sel { width: 200px; }
.pager { margin-top: 1rem; }
.reset-who { font-size: 0.92rem; }
.reset-who code { margin-left: 0.5rem; color: var(--el-text-color-secondary); }
</style>
