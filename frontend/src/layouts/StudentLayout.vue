<script setup lang="ts">
// 学生端布局：顶部导航（主题/路径/排行榜）+ 用户区（含当前 XP）。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { progressApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const displayName = computed(() => auth.user?.username ?? '')
const role = computed(() => auth.user?.global_role ?? '')
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
        <router-link to="/">主题</router-link>
        <router-link to="/paths">学习路径</router-link>
        <router-link to="/leaderboard">排行榜</router-link>
      </nav>
      <div class="spacer" />
      <span v-if="xp !== null" class="xp">XP: {{ xp }}</span>
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
nav a { margin-right: 1rem; color: #333; text-decoration: none; }
nav a.router-link-active { color: #409eff; }
.spacer { flex: 1; }
.xp { color: #409eff; font-weight: 600; }
.user { color: #666; font-size: 0.9rem; }
</style>