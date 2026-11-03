<script setup lang="ts">
// 站内邮箱：只收不发——系统投递的竞赛邀请等通知。点开标记已读。
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { profileApi, type MailItem } from '../api'

const router = useRouter()
const mails = ref<MailItem[]>([])
const total = ref(0)
const unread = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const dialogVisible = ref(false)
const current = ref<MailItem | null>(null)

async function load() {
  loading.value = true
  try {
    const resp = await profileApi.mails(page.value, pageSize)
    mails.value = resp.items
    total.value = resp.total
    unread.value = resp.unread
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

// 打开邮件：标记已读并本地刷新未读数（邀请邮件里的邀请码在此展示）。
// 顶栏未读角标由布局组件持有，读信后广播事件让布局同步刷新。
async function openMail(m: MailItem) {
  current.value = m
  dialogVisible.value = true
  if (!m.read) {
    try {
      await profileApi.markRead(m.id)
      m.read = true
      unread.value = Math.max(0, unread.value - 1)
      window.dispatchEvent(new CustomEvent('academy:mails-read'))
    } catch (e) {
      notify.error((e as Error).message)
    }
  }
}

// 未读行加粗（row-class-name 回调）。
function rowClass({ row }: { row: MailItem }) {
  return row.read ? '' : 'unread'
}

function kindLabel(kind: string) {
  return kind === 'invite' ? '竞赛邀请' : '通知'
}

// 邀请邮件 → 入场页（预填邀请码）。
function goEnter() {
  if (!current.value?.competition_id) return
  router.push(`/competitions/enter/${current.value.competition_id}?code=${encodeURIComponent(current.value.invite_code ?? '')}`)
}

function fmt(ts: string) {
  return formatDateTime(ts)
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">站内<span class="accent">邮箱</span></h2>
      <p class="page-sub">
        只收不发：竞赛邀请等系统通知投递到这里；当前未读 {{ unread }} 封。点开邮件即标记已读。
      </p>
    </div>
  </header>

  <UiTable
    :data="mails"
    v-busy="loading"
    max-height="560"
    empty-text="暂无邮件"
    :row-class-name="rowClass"
  >
    <UiTableColumn label="状态" width="80">
      <template #default="{ row }">
        <!-- 单个 el-tag 绑定属性切换，避免 v-if/v-else 双标签的进出过渡在
             el-table 单元格重渲染时停滞、残留两个标签 -->
        <UiTag :type="row.read ? 'info' : 'danger'" size="small">
          {{ row.read ? '已读' : '未读' }}
        </UiTag>
      </template>
    </UiTableColumn>
    <UiTableColumn prop="title" label="标题" min-width="200" />
    <UiTableColumn label="类型" width="100">
      <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
    </UiTableColumn>
    <UiTableColumn label="时间" width="170">
      <template #default="{ row }">{{ fmt(row.created_at) }}</template>
    </UiTableColumn>
    <UiTableColumn label="操作" width="90">
      <template #default="{ row }">
        <UiButton size="small" @click="openMail(row)">查看</UiButton>
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

  <!-- 邮件正文对话框（含邀请码） -->
  <UiDialog v-model="dialogVisible" :title="current?.title" width="560px">
    <div v-if="current" class="mail-body">
      <div class="mail-meta">
        <UiTag size="small">{{ kindLabel(current.kind) }}</UiTag>
        <span>{{ fmt(current.created_at) }}</span>
      </div>
      <pre class="body-text">{{ current.body_md }}</pre>
      <UiAlert
        v-if="current.kind === 'invite' && current.invite_code"
        :title="`邀请码：${current.invite_code}`"
        type="success"
        :closable="false"
        class="invite"
      />
      <!-- 竞赛邀请直达：邮件携带竞赛 ID + 邀请码，一键进入入场页（预填邀请码） -->
      <UiButton
        v-if="current.kind === 'invite' && current.competition_id && current.invite_code"
        type="primary"
        class="enter-btn"
        @click="goEnter"
      >进入竞赛</UiButton>
    </div>
  </UiDialog>
</template>

<style scoped>
.pager { margin-top: var(--space-4); }
:deep(.unread) { font-weight: 600; }
.mail-body { display: flex; flex-direction: column; gap: var(--space-3); }
.mail-meta { display: flex; align-items: center; gap: var(--space-3); color: var(--muted-foreground); font-size: var(--text-sm); }
.body-text { white-space: pre-wrap; margin: 0; font-family: inherit; }
.invite { margin-top: var(--space-2); }
.enter-btn { margin-top: var(--space-3); }
</style>
