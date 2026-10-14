<script setup lang="ts">
// 普通端布局：导航只有 教程 | 训练；头部右侧 XP、排行榜与用户区。
// 仅管理员显示「进入管理端」；普通人完全看不到管理入口。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { progressApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const displayName = computed(() => auth.user?.username ?? '')
const role = computed(() => auth.user?.global_role ?? '')
const roleLabel = computed(() => (role.value === 'admin' ? '管理员' : '普通人'))
const canAdmin = computed(() => role.value === 'admin')
const xp = ref<number | null>(null)
const completedRooms = ref<number | null>(null)
const inProgressRooms = ref<number | null>(null)

onMounted(async () => {
  if (auth.token && !auth.user) {
    auth.fetchMe().catch(() => {})
  }
  if (auth.token) {
    try {
      const p = await progressApi.mine()
      xp.value = p.xp
      completedRooms.value = p.completed_rooms
      inProgressRooms.value = p.in_progress_rooms
    } catch {
      // 进度拉取失败不打断布局
    }
  }
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
        <router-link to="/training">训练</router-link>
        <router-link to="/paths">路径</router-link>
      </nav>
      <div class="spacer" />
      <span v-if="xp !== null" class="xp">XP: {{ xp }}</span>
      <span v-if="completedRooms !== null" class="stats" title="事件派生的 Progress：已完成 / 进行中的教程 Room">
        已完成 {{ completedRooms }} · 进行中 {{ inProgressRooms }}
      </span>
      <router-link v-if="canAdmin" to="/admin" class="admin-entry">进入管理端</router-link>
      <router-link to="/leaderboard" class="secondary">排行榜</router-link>
      <span v-if="displayName" class="user">{{ displayName }}（{{ roleLabel }}）</span>
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
.user { color: var(--el-text-color-secondary); font-size: 0.9rem; }
</style>