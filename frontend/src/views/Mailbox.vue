<script setup lang="ts">
// 站内邮箱：只收不发——系统投递的竞赛邀请等通知。点开标记已读。
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { notify } from '@/lib/notifications'
import { formatDateTime } from '@/lib/format'
import { profileApi, type MailItem } from '../api'
import PageHeader from '@/components/layout/PageHeader.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import { Button } from '@/components/ui/button'

const router = useRouter()
const mails = ref<MailItem[]>([])
const total = ref(0)
const unread = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(true)
const loadError = ref('')
const dialogVisible = ref(false)
const current = ref<MailItem | null>(null)

async function load() {
  loading.value = true
  loadError.value = ''
  mails.value = []
  total.value = 0
  try {
    const resp = await profileApi.mails(page.value, pageSize)
    mails.value = resp.items
    total.value = resp.total
    unread.value = resp.unread
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '邮件加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

// 打开邮件：标记已读并本地刷新未读数（邀请邮件里的邀请码在此展示）。
// 顶栏未读角标由布局组件持有，读信后广播事件让布局同步刷新。
async function openMail(mail: MailItem) {
  current.value = mail
  dialogVisible.value = true
  if (!mail.read) {
    try {
      await profileApi.markRead(mail.id)
      mail.read = true
      unread.value = Math.max(0, unread.value - 1)
      window.dispatchEvent(new CustomEvent('academy:mails-read'))
    } catch (e) {
      notify.error((e as Error).message)
    }
  }
}

function rowClass({ row }: { row: MailItem }) {
  return row.read ? '' : 'mail-unread'
}

function kindLabel(kind: string) {
  return kind === 'invite' ? '竞赛邀请' : '通知'
}

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
  <PageHeader
    title="站内邮箱"
    description="只收不发：竞赛邀请等系统通知投递到这里。点开邮件即标记已读。"
  >
    <template v-if="!loading" #actions>
      <span class="mail-summary">未读 {{ unread }} 封 · 共 {{ total }} 封</span>
    </template>
  </PageHeader>

  <DataTablePanel
    v-if="!loadError"
    title="邮件"
    :count="total"
    description="竞赛邀请会携带邀请码，可直接进入赛场。"
  >
    <UiTable
      :data="mails"
      :loading="loading"
      :row-class-name="rowClass"
      row-key="id"
      selection-label="选择邮件"
      @row-click="openMail"
    >
      <UiTableColumn label="状态" width="80">
        <template #default="{ row }">
          <StatusTag kind="mail" :value="row.read ? 'read' : 'unread'" />
        </template>
      </UiTableColumn>
      <UiTableColumn prop="title" label="标题" min-width="240">
        <template #default="{ row }">
          <span class="mail-title">{{ row.title }}</span>
        </template>
      </UiTableColumn>
      <UiTableColumn label="类型" width="100">
        <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
      </UiTableColumn>
      <UiTableColumn label="时间" width="170">
        <template #default="{ row }">{{ fmt(row.created_at) }}</template>
      </UiTableColumn>
      <UiTableColumn label="操作" width="88" fixed="right">
        <template #default="{ row }">
          <Button size="sm" variant="outline" @click.stop="openMail(row)">查看</Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          compact
          title="还没有邮件"
          description="竞赛邀请等系统通知会投递到这里；收到邀请后可直接从邮件进入赛场。"
        />
      </template>
    </UiTable>
    <template v-if="total > pageSize" #footer>
      <UiPagination
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        v-model:current-page="page"
        @current-change="load"
      />
    </template>
  </DataTablePanel>

  <EmptyState
    v-else
    tone="error"
    title="邮件加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <UiDialog v-model="dialogVisible" :title="current?.title" width="560px">
    <div v-if="current" class="mail-body">
      <div class="mail-meta">
        <StatusTag kind="mail" :value="current.read ? 'read' : 'unread'" />
        <span>{{ kindLabel(current.kind) }}</span>
        <span>{{ fmt(current.created_at) }}</span>
      </div>
      <pre class="mail-body__text">{{ current.body_md }}</pre>
      <div v-if="current.kind === 'invite' && current.invite_code" class="mail-invite" role="status">
        <span class="mail-invite__label">邀请码</span>
        <code class="mail-invite__code">{{ current.invite_code }}</code>
      </div>
      <Button
        v-if="current.kind === 'invite' && current.competition_id && current.invite_code"
        class="mail-enter"
        @click="goEnter"
      >进入竞赛</Button>
    </div>
  </UiDialog>
</template>

<style scoped>
.mail-summary {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

:deep(.mail-unread) .mail-title {
  font-weight: 600;
}

.mail-title {
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.mail-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.mail-meta {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.mail-body__text {
  margin: 0;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
  white-space: pre-wrap;
}

.mail-invite {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-success) 10%, var(--bg-surface));
}

.mail-invite__label {
  color: var(--status-success);
  font-size: var(--text-meta);
}

.mail-invite__code {
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--text-caption);
  overflow-wrap: anywhere;
}

.mail-enter {
  width: 100%;
  min-height: 44px;
  justify-content: center;
}
</style>