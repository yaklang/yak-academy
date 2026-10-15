<script setup lang="ts">
// 站内邮箱：只收不发——系统投递的竞赛邀请等通知。点开标记已读。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { profileApi, type MailItem } from '../api'

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
    ElMessage.error((e as Error).message)
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
      ElMessage.error((e as Error).message)
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

function fmt(ts: string) {
  return new Date(ts).toLocaleString()
}

onMounted(load)
</script>

<template>
  <h2>站内邮箱</h2>
  <p class="hint">
    只收不发：竞赛邀请等系统通知投递到这里；当前未读 {{ unread }} 封。点开邮件即标记已读。
  </p>

  <el-table
    :data="mails"
    v-loading="loading"
    max-height="560"
    empty-text="暂无邮件"
    :row-class-name="rowClass"
  >
    <el-table-column label="状态" width="80">
      <template #default="{ row }">
        <!-- 单个 el-tag 绑定属性切换，避免 v-if/v-else 双标签的进出过渡在
             el-table 单元格重渲染时停滞、残留两个标签 -->
        <el-tag :type="row.read ? 'info' : 'danger'" size="small">
          {{ row.read ? '已读' : '未读' }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="title" label="标题" min-width="200" />
    <el-table-column label="类型" width="100">
      <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
    </el-table-column>
    <el-table-column label="时间" width="170">
      <template #default="{ row }">{{ fmt(row.created_at) }}</template>
    </el-table-column>
    <el-table-column label="操作" width="90">
      <template #default="{ row }">
        <el-button size="small" @click="openMail(row)">查看</el-button>
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

  <!-- 邮件正文对话框（含邀请码） -->
  <el-dialog v-model="dialogVisible" :title="current?.title" width="560px">
    <div v-if="current" class="mail-body">
      <div class="mail-meta">
        <el-tag size="small">{{ kindLabel(current.kind) }}</el-tag>
        <span>{{ fmt(current.created_at) }}</span>
      </div>
      <pre class="body-text">{{ current.body_md }}</pre>
      <el-alert
        v-if="current.kind === 'invite' && current.invite_code"
        :title="`邀请码：${current.invite_code}`"
        type="success"
        :closable="false"
        class="invite"
      />
    </div>
  </el-dialog>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.pager { margin-top: 1rem; }
:deep(.unread) { font-weight: 600; }
.mail-body { display: flex; flex-direction: column; gap: 0.8rem; }
.mail-meta { display: flex; align-items: center; gap: 0.8rem; color: var(--el-text-color-secondary); font-size: 0.85rem; }
.body-text { white-space: pre-wrap; margin: 0; font-family: inherit; }
.invite { margin-top: 0.5rem; }
</style>