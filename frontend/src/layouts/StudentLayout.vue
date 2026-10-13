<script setup lang="ts">
// 普通端布局：导航只有 教程 | 训练 两个板块；头部右侧为 XP、排行榜次级链接与用户区。
// teacher/admin 显示"进入管理端"入口。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { progressApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const displayName = computed(() => auth.user?.username ?? '')
const role = computed(() => auth.user?.global_role ?? '')
const canAdmin = computed(() => role.value === 'teacher' || role.value === 'admin')
const xp = ref<number | null>(null)

onMounted(async () => {
  if (auth.token && !auth.user) {
    auth.fetchMe().catch(() => {})
  }
  if (auth.token) {
    try {
      const p = await progressApi.mine()
      xp.value = p.xp
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
      <span class="brand">yak-academy</span>
      <nav>
        <router-link to="/">教程</router-link>
        <router-link to="/training">训练</router-link>
      </nav>
      <div class="spacer" />
      <span v-if="xp !== null" class="xp">XP: {{ xp }}</span>
      <router-link v-if="canAdmin" to="/admin" class="admin-entry">进入管理端</router-link>
      <router-link to="/leaderboard" class="secondary">排行榜</router-link>
      <span v-if="displayName" class="user">{{ displayName }}（{{ role }}）</span>
      <el-button v-if="auth.token" link @click="logout">登出</el-button>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; }
.nav { display: flex; align-items: center; gap: 2rem; border-bottom: 1px solid #eee; }
.brand { font-weight: 700; }
nav { display: flex; gap: 1.5rem; }
nav a { color: #333; text-decoration: none; }
nav a.router-link-active { color: #409eff; }
.spacer { flex: 1; }
.xp { color: #409eff; font-weight: 600; }
.admin-entry { color: #e6a23c; text-decoration: none; font-weight: 600; }
.secondary { color: #999; text-decoration: none; font-size: 0.9rem; }
.user { color: #666; font-size: 0.9rem; }
</style>