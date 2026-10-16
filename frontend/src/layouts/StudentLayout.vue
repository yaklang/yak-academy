<script setup lang="ts">
// 普通端布局：导航只有 教程 | 题库；头部右侧 XP、排行榜与用户区。
// 仅管理员显示「进入管理端」；普通人完全看不到管理入口。
// 改密码在账号信息面板（/profile），布局头部只保留登录态操作。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { progressApi, profileApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const displayName = computed(() => auth.user?.username ?? '')
const role = computed(() => auth.user?.global_role ?? '')
const roleLabel = computed(() => (role.value === 'admin' ? '管理员' : '普通人'))
const canAdmin = computed(() => role.value === 'admin')
const xp = ref<number | null>(null)
const completedRooms = ref<number | null>(null)
const inProgressRooms = ref<number | null>(null)
const unreadMails = ref(0)

async function refreshUnread() {
  try {
    const u = await profileApi.unreadCount()
    unreadMails.value = u.unread
  } catch {
    // 未读数拉取失败不打断布局
  }
}

// 邮箱页读信后广播 academy:mails-read，这里同步刷新角标（布局不随子路由重挂载）。
function onMailsRead() {
  refreshUnread()
}

onMounted(async () => {
  // 用户身份由路由守卫统一预取（刷新/直进 URL 不再出现头部空窗），这里只拉进度。
  if (auth.token) {
    try {
      const p = await progressApi.mine()
      xp.value = p.xp
      completedRooms.value = p.completed_rooms
      inProgressRooms.value = p.in_progress_rooms
    } catch {
      // 进度拉取失败不打断布局
    }
    refreshUnread()
  }
  window.addEventListener('academy:mails-read', onMailsRead)
})

onUnmounted(() => {
  window.removeEventListener('academy:mails-read', onMailsRead)
})

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <el-container class="layout">
    <el-header class="nav">
      <span class="brand"><span class="prompt">$</span> yak-academy</span>
      <nav>
        <router-link to="/">教程</router-link>
        <router-link to="/training">题库</router-link>
      </nav>
      <div class="spacer" />
      <span v-if="xp !== null" class="xp">XP: {{ xp }}</span>
      <span v-if="completedRooms !== null" class="stats" title="事件派生的 Progress：已完成 / 进行中的教程 Room">
        已完成 {{ completedRooms }} · 进行中 {{ inProgressRooms }}
      </span>
      <router-link v-if="canAdmin" to="/admin" class="admin-entry">进入管理端</router-link>
      <router-link to="/leaderboard" class="secondary">排行榜</router-link>
      <router-link to="/badges" class="secondary">徽章</router-link>
      <router-link to="/mailbox" class="secondary">
        邮箱
        <el-badge v-if="unreadMails > 0" :value="unreadMails" :max="99" class="mail-badge" />
      </router-link>
      <router-link v-if="displayName" to="/profile" class="user-link">{{ displayName }}（{{ roleLabel }}）</router-link>
      <el-button v-if="auth.token" link @click="logout">登出</el-button>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; }
.nav {
  display: flex; align-items: center; gap: 2rem;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}
.brand { font-weight: 700; font-family: Consolas, monospace; color: var(--el-text-color-primary); }
.brand .prompt { color: var(--el-color-primary); }
nav { display: flex; gap: 1.5rem; }
nav a {
  color: var(--el-text-color-regular); text-decoration: none;
  padding-bottom: 2px; border-bottom: 2px solid transparent;
}
nav a.router-link-active { color: var(--el-color-primary); border-bottom-color: var(--el-color-primary); }
.spacer { flex: 1; }
.xp { color: var(--el-color-primary); font-weight: 600; font-family: Consolas, monospace; }
.stats { color: var(--el-text-color-secondary); font-size: 0.85rem; }
.admin-entry { color: var(--el-color-warning); text-decoration: none; font-weight: 600; }
.secondary { color: var(--el-text-color-secondary); text-decoration: none; font-size: 0.9rem; }
.mail-badge { margin-left: 4px; transform: translateY(-2px); }
.user-link { color: var(--el-text-color-primary); text-decoration: none; font-weight: 600; }
.user { color: var(--el-text-color-secondary); font-size: 0.9rem; }
</style>