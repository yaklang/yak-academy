<script setup lang="ts">
// 普通端布局（CTF 平台通行的「顶栏 + 内容」结构，参考 CTFd/picoCTF）：
// 左侧品牌 + 主导航（教程 | 题库 | 排行榜），右侧收拢低频入口（徽章/邮箱）
// 进用户下拉，XP 与通关数做成 chip 徽章紧贴头像区。
// 仅管理员显示「进入管理端」；普通人完全看不到管理入口。
// 改密码在账号信息面板（/profile），布局头部只保留登录态操作。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { progressApi, profileApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
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
    <el-header class="nav" height="auto">
      <div class="nav-inner">
        <router-link to="/" class="brand">
          <span class="logo" aria-hidden="true">&lt;/&gt;</span>
          <span class="name">yak-academy</span>
        </router-link>
        <nav class="main-nav">
          <!-- 「教程」指向根路径 "/"：router-link-active 是前缀匹配，任何路由下都会命中根路径，
               因此各导航项统一改用精确匹配类 router-link-exact-active 来点亮下划线，
               否则进了题库/排行榜后「教程」仍然高亮。 -->
          <router-link to="/" exact-active-class="main-nav-exact">教程</router-link>
          <router-link to="/training" exact-active-class="main-nav-exact">题库</router-link>
          <router-link to="/leaderboard" exact-active-class="main-nav-exact">排行榜</router-link>
        </nav>
        <div class="spacer" />
        <div v-if="xp !== null" class="chips" title="事件派生的 Progress：已完成 / 进行中的教程 Room">
          <span class="chip xp-chip"><span class="chip-label">XP</span>{{ xp }}</span>
          <span v-if="completedRooms !== null" class="chip">
            <span class="dot solved" />{{ completedRooms }}
            <span class="chip-sub">已完成</span>
          </span>
          <span v-if="inProgressRooms !== null && inProgressRooms > 0" class="chip">
            <span class="dot ongoing" />{{ inProgressRooms }}
            <span class="chip-sub">进行中</span>
          </span>
        </div>
        <router-link v-if="canAdmin" to="/admin" class="admin-entry">进入管理端</router-link>
        <router-link to="/badges" class="icon-link">徽章</router-link>
        <router-link to="/mailbox" class="icon-link">
          邮箱
          <el-badge v-if="unreadMails > 0" :value="unreadMails" :max="99" class="mail-badge" />
        </router-link>
        <router-link v-if="displayName" to="/profile" class="user-menu">
          <span class="avatar">{{ displayName.slice(0, 1).toUpperCase() }}</span>
          <span class="uname">{{ displayName }}（{{ roleLabel }}）</span>
        </router-link>
        <el-button v-if="auth.token" link @click="logout">登出</el-button>
      </div>
    </el-header>
    <el-main class="main">
      <div class="container">
        <router-view />
      </div>
    </el-main>
    <footer class="footer">
      <span class="foot-note">$ yak-academy — 安全实训平台</span>
      <span class="foot-dim">教程 · 题库 · 竞赛</span>
    </footer>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; display: flex; flex-direction: column; }
.nav {
  position: sticky; top: 0; z-index: 20;
  padding: 0;
  background: rgba(13, 17, 23, 0.88);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--el-border-color);
}
.nav-inner {
  display: flex; align-items: center; gap: 1.75rem;
  max-width: 1200px; margin: 0 auto; padding: 0.65rem 1.5rem;
}
.brand { display: flex; align-items: center; gap: 0.55rem; text-decoration: none; }
.logo {
  display: inline-flex; align-items: center; justify-content: center;
  width: 30px; height: 30px; border-radius: 7px;
  background: linear-gradient(135deg, rgba(0, 230, 118, 0.22), rgba(0, 230, 118, 0.06));
  border: 1px solid var(--el-color-primary-light-7);
  color: var(--el-color-primary);
  font-family: Consolas, monospace; font-weight: 700; font-size: 0.8rem;
}
.name {
  font-weight: 700; font-size: 1.05rem;
  font-family: Consolas, monospace; color: var(--el-text-color-primary);
}
.main-nav { display: flex; gap: 1.4rem; }
.main-nav a {
  color: var(--el-text-color-secondary); text-decoration: none;
  font-size: 0.95rem; padding: 0.2rem 0;
  border-bottom: 2px solid transparent;
  transition: color 0.15s ease;
}
.main-nav a:hover { color: var(--el-text-color-primary); }
.main-nav a.main-nav-exact {
  color: var(--el-color-primary);
  border-bottom-color: var(--el-color-primary);
  font-weight: 600;
}
.spacer { flex: 1; }
.chips { display: flex; gap: 0.5rem; align-items: center; }
.chip {
  display: inline-flex; align-items: center; gap: 0.35rem;
  font-family: Consolas, monospace; font-size: 0.85rem;
  color: var(--el-text-color-primary);
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 999px; padding: 0.2rem 0.7rem;
}
.xp-chip { color: var(--el-color-primary); font-weight: 700; }
.chip-label { color: var(--el-color-primary); font-size: 0.72rem; font-weight: 700; }
.chip-sub { color: var(--el-text-color-secondary); font-size: 0.72rem; }
.dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
.dot.solved { background: var(--el-color-primary); }
.dot.ongoing { background: #d29922; }
.admin-entry {
  color: var(--el-color-warning); text-decoration: none; font-weight: 600; font-size: 0.9rem;
}
.icon-link {
  color: var(--el-text-color-secondary); text-decoration: none; font-size: 0.9rem;
}
.icon-link:hover { color: var(--el-text-color-primary); }
.mail-badge { margin-left: 4px; transform: translateY(-2px); }
.user-menu {
  display: flex; align-items: center; gap: 0.5rem;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 999px; padding: 0.22rem 0.8rem 0.22rem 0.25rem;
  text-decoration: none;
}
.user-menu:hover { border-color: var(--el-color-primary-light-5); }
.avatar {
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 26px; border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary); font-weight: 700; font-size: 0.8rem;
}
.uname { font-weight: 600; font-size: 0.9rem; color: var(--el-text-color-primary); }
.main { flex: 1; padding: 1.75rem 0 3rem; }
.container { max-width: 1200px; margin: 0 auto; padding: 0 1.5rem; }
.footer {
  border-top: 1px solid var(--el-border-color-lighter);
  box-sizing: border-box;
  display: flex; align-items: center; justify-content: space-between;
  max-width: 1200px; margin: 0 auto; width: 100%;
  padding: 0.9rem 1.5rem;
  color: var(--el-text-color-secondary); font-size: 0.8rem;
}
.foot-note { font-family: Consolas, monospace; }
.foot-dim { opacity: 0.7; }
/* 窄屏：先收起信息性内容（XP chips / 用户名文字，头像保留），再让主导航折到第二行 */
@media (max-width: 960px) {
  .nav-inner { gap: 1rem; }
  .chips, .uname { display: none; }
}
@media (max-width: 640px) {
  .nav-inner { flex-wrap: wrap; row-gap: 0.5rem; padding: 0.55rem 1rem; }
  .main-nav, .admin-entry { order: 5; }
  .main-nav { flex-basis: 100%; gap: 1.1rem; }
}
</style>