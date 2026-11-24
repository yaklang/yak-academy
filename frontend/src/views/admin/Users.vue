<script setup lang="ts">
// 管理面板：用户管理。表格 + 过滤/搜索：
//   q 对用户名/账号编码/邮箱/手机号子串匹配；role 按角色；
//   active=false = 6 个月未登录（最近登录或注册超 6 个月）的不活跃账号；
//   no_solves=true = 从未完成过题目（无判定通过的提交）。
// 过滤条件与表格内标记（活跃/完成过题目）同口径，由服务端统一判定。
// 行内操作「重置密码」：管理员直接设新密码（不校验旧密码），目标用户全部
// 已登录会话立即失效——未绑定邮箱/手机号的竞赛批量账号走此入口找回。
import { computed, ref, watch } from 'vue'
import { Search } from '@lucide/vue'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import PageHeader from '@/components/layout/PageHeader.vue'
import FilterBar from '@/components/layout/FilterBar.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import FormField from '@/components/layout/FormField.vue'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { adminApi, type AdminUserItem } from '../../api'

const items = ref<AdminUserItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const loadError = ref('')

// 重置密码对话框：目标用户 + 新密码（预填平台批量建号同款初始密码 123456，可改）。
const resetVisible = ref(false)
const resetTarget = ref<AdminUserItem | null>(null)
const resetPassword = ref('123456')
const resetError = ref('')
const resetting = ref(false)

// 过滤状态：q/role/active/noSolves。active/noSolves 用 '' | 'true' | 'false' 三态。
const q = ref('')
const role = ref<'' | 'student' | 'admin'>('')
const active = ref<'' | 'true' | 'false'>('')
const noSolves = ref<'' | 'true' | 'false'>('')

const hasFilters = computed(() =>
  Boolean(q.value.trim() || role.value || active.value || noSolves.value),
)

const resetDescription = computed(() => {
  if (!resetTarget.value) return ''
  const code = resetTarget.value.account_code ? ` · ${resetTarget.value.account_code}` : ''
  return `目标账号：${resetTarget.value.username}${code}`
})

async function load() {
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
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
    loadError.value = e instanceof Error ? e.message : '用户列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

// 下拉筛选即时生效（v-model 更新后再查询，避免读到旧值）。
watch([role, active, noSolves], search)

// 组合常用画像过滤：不活跃 + 从未完成题目。
function filterStaleNoSolves() {
  active.value = 'false'
  noSolves.value = 'true'
  page.value = 1
  load()
}

function clearFilters() {
  const selectsChanged = Boolean(role.value || active.value || noSolves.value)
  q.value = ''
  role.value = ''
  active.value = ''
  noSolves.value = ''
  page.value = 1
  // 下拉变化会触发 watch；只有搜索词变化时 watch 不动作，这里补一次查询。
  if (!selectsChanged) load()
}

function fmt(ts: string | null | undefined) {
  return formatDateTime(ts)
}

function openReset(row: AdminUserItem) {
  resetTarget.value = row
  resetPassword.value = '123456'
  resetError.value = ''
  resetVisible.value = true
}

async function confirmReset() {
  if (!resetTarget.value) return
  if (resetPassword.value.length < 6) {
    resetError.value = '新密码至少 6 位'
    return
  }
  resetError.value = ''
  resetting.value = true
  try {
    await adminApi.resetPassword(resetTarget.value.id, resetPassword.value)
    notify.success(`已重置 ${resetTarget.value.username} 的密码，其登录状态已全部失效`)
    resetVisible.value = false
  } catch (e) {
    resetError.value = e instanceof Error ? e.message : '重置失败，请稍后重试'
  } finally {
    resetting.value = false
  }
}

load()
</script>

<template>
  <PageHeader
    title="用户管理"
    description="账号编码（YA{YY}-{NNNNN}）在建号时按年自动分配；6 个月未登录的账号标记为「不活跃」（从未登录的账号按注册时间起算）。"
  />

  <FilterBar>
    <label class="filter-field">
      <span class="filter-field__label">搜索</span>
      <span class="filter-field__control">
        <Search class="filter-field__icon" aria-hidden="true" />
        <Input
          v-model="q"
          class="filter-field__input"
          placeholder="搜索：用户名 / 账号编码 / 邮箱 / 手机号"
          @keyup.enter="search"
        />
      </span>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">角色</span>
      <select v-model="role" class="filter-select" aria-label="角色">
        <option value="">全部角色</option>
        <option value="student">学员</option>
        <option value="admin">管理员</option>
      </select>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">活跃状态</span>
      <select v-model="active" class="filter-select" aria-label="活跃状态">
        <option value="">全部状态</option>
        <option value="true">活跃</option>
        <option value="false">不活跃（6 个月未登录）</option>
      </select>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">解题情况</span>
      <select v-model="noSolves" class="filter-select" aria-label="解题情况">
        <option value="">全部情况</option>
        <option value="true">从未完成题目</option>
        <option value="false">完成过题目</option>
      </select>
    </label>
    <template #actions>
      <Button variant="ghost" type="button" @click="filterStaleNoSolves">不活跃且从未解题</Button>
      <Button v-if="hasFilters" variant="ghost" type="button" @click="clearFilters">清除筛选</Button>
      <Button type="button" @click="search">搜索</Button>
    </template>
  </FilterBar>

  <DataTablePanel
    v-if="!loadError"
    title="用户列表"
    :count="total"
    :description="loading ? '正在加载用户…' : hasFilters ? `按当前条件显示 ${total} 个账号` : undefined"
  >
    <UiTable
      :data="items"
      :loading="loading"
      :loading-rows="7"
      aria-label="用户列表"
    >
      <UiTableColumn label="用户" min-width="200">
        <template #default="{ row }">
          <span class="user-name">{{ row.username }}</span>
          <code class="user-code">{{ row.account_code || '—' }}</code>
        </template>
      </UiTableColumn>
      <UiTableColumn label="联系方式" min-width="200">
        <template #default="{ row }">
          <span v-if="row.email" class="user-contact">{{ row.email }}</span>
          <span v-else-if="row.phone" class="user-contact">{{ row.phone }}</span>
          <span v-else class="user-muted">未绑定</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="角色" width="96">
        <template #default="{ row }">
          <StatusTag kind="role" :value="row.global_role" />
        </template>
      </UiTableColumn>
      <UiTableColumn label="活跃" width="96">
        <template #default="{ row }">
          <StatusTag
            kind="user"
            :value="row.active ? 'active' : 'inactive'"
            :label="row.active ? '活跃' : '不活跃'"
          />
        </template>
      </UiTableColumn>
      <UiTableColumn label="完成题目" width="100">
        <template #default="{ row }">
          <span :class="row.solved_any ? 'user-contact' : 'user-muted'">
            {{ row.solved_any ? '完成过' : '从未' }}
          </span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="最近登录" width="160">
        <template #default="{ row }">
          <span class="user-time">{{ fmt(row.last_login_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="注册时间" width="160">
        <template #default="{ row }">
          <span class="user-time">{{ fmt(row.created_at) }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="104">
        <template #default="{ row }">
          <Button variant="link" size="sm" type="button" @click="openReset(row)">重置密码</Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          v-if="hasFilters"
          compact
          tone="filtered"
          title="没有匹配的用户"
          description="试试更换关键词，或清除角色与状态筛选。"
          action-label="清除筛选"
          @action="clearFilters"
        />
        <EmptyState
          v-else
          compact
          title="暂无用户"
          description="学员通过微信登录自动建号，管理员可在竞赛中批量导入账号。"
        />
      </template>
    </UiTable>
    <template #footer>
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
    title="用户列表加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <!-- 管理员重置密码：不校验旧密码，重置后该用户全部已登录会话立即失效。
       默认填平台批量建号同款初始密码 123456，可改。 -->
  <ConfirmDialog
    :open="resetVisible"
    title="重置密码"
    :description="resetDescription"
    confirm-label="重置"
    :loading="resetting"
    @update:open="resetVisible = $event"
    @confirm="confirmReset"
  >
    <FormField
      label="新密码"
      html-for="reset-password"
      hint="重置后该用户的全部登录状态立即失效，需用新密码重新登录。"
      :error="resetError"
    >
      <template #default="{ describedby, invalid }">
        <Input
          id="reset-password"
          v-model="resetPassword"
          type="password"
          placeholder="至少 6 位"
          autocomplete="new-password"
          :aria-describedby="describedby"
          :aria-invalid="invalid"
        />
      </template>
    </FormField>
  </ConfirmDialog>
</template>

<style scoped>
.filter-field {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.filter-field__label {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.filter-field__control {
  position: relative;
  display: block;
}

.filter-field__icon {
  position: absolute;
  top: 50%;
  left: var(--space-3);
  width: 16px;
  height: 16px;
  color: var(--text-tertiary);
  transform: translateY(-50%);
  pointer-events: none;
}

.filter-field__input {
  width: 280px;
  padding-left: var(--space-8);
}

.filter-select {
  min-height: 36px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.user-name {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  line-height: var(--leading-normal);
  overflow-wrap: anywhere;
}

.user-code {
  display: block;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  line-height: var(--leading-normal);
}

.user-contact {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  overflow-wrap: anywhere;
}

.user-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.user-time {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

@media (max-width: 720px) {
  .filter-field,
  .filter-field__input,
  .filter-select {
    width: 100%;
  }

  .filter-select {
    min-height: 44px;
  }
}
</style>