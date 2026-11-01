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
import { Menu, Mail, Award, ArrowUpRight, LogOut } from '@lucide/vue'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
const mobileOpen = ref(false)
const navigation = [{ to: '/learn', title: '教程' }, { to: '/training', title: '题库' }, { to: '/leaderboard', title: '排行榜' }, { to: '/competitions', title: '竞赛' }]
</script>
<template>
  <div class="student-shell">
    <header class="student-header">
      <div class="student-header-inner">
        <Brand to="/learn" />
        <nav class="main-nav" aria-label="学习导航"><RouterLink v-for="item in navigation" :key="item.to" :to="item.to" exact-active-class="main-nav-exact">{{ item.title }}</RouterLink></nav>
        <div class="student-tools">
          <div v-if="xp !== null" class="study-summary"><span class="xp-chip">{{ xp }} <small>XP</small></span><span v-if="completedRooms !== null">{{ completedRooms }} 已完成</span><span v-if="inProgressRooms">{{ inProgressRooms }} 进行中</span></div>
          <RouterLink v-if="canAdmin" to="/admin" class="admin-entry desktop-tool">进入管理端<ArrowUpRight :size="14" /></RouterLink>
          <RouterLink to="/badges" class="header-icon desktop-tool" aria-label="徽章" title="徽章"><Award :size="18" /></RouterLink>
          <RouterLink to="/mailbox" class="header-icon desktop-tool" aria-label="邮箱" title="邮箱"><Mail :size="18" /><span v-if="unreadMails" class="unread-count">{{ unreadMails > 99 ? '99+' : unreadMails }}</span></RouterLink>
          <RouterLink v-if="displayName" to="/profile" class="user-menu desktop-tool" :aria-label="`${displayName}（${roleLabel}）`"><span class="avatar">{{ displayName.slice(0,1).toUpperCase() }}</span><span class="uname">{{ displayName }}（{{ roleLabel }}）</span></RouterLink>
          <ThemeToggle />
          <Button v-if="auth.token" variant="ghost" class="desktop-tool" @click="logout">登出</Button>
          <Button variant="ghost" size="icon" class="mobile-menu-trigger" aria-label="打开学习导航" @click="mobileOpen = true"><Menu :size="20" /></Button>
        </div>
      </div>
    </header>
    <main class="student-main"><RouterView /></main>
    <footer class="shell-footer"><span>YAK ACADEMY · 学以致用，知行合一。</span><RouterLink to="/">学院首页 ↗</RouterLink></footer>
    <Sheet v-model:open="mobileOpen"><SheetContent class="w-[calc(100vw-2rem)] max-w-sm overflow-y-auto"><SheetHeader><SheetTitle>学习导航</SheetTitle><SheetDescription>{{ displayName }}（{{ roleLabel }}）</SheetDescription></SheetHeader><nav class="mobile-navigation" aria-label="更多学习入口"><RouterLink v-for="item in navigation" :key="item.to" :to="item.to" @click="mobileOpen = false">{{ item.title }}</RouterLink><RouterLink to="/profile" @click="mobileOpen = false">账号信息</RouterLink><RouterLink to="/badges" @click="mobileOpen = false">徽章</RouterLink><RouterLink to="/mailbox" @click="mobileOpen = false">邮箱 <span v-if="unreadMails">{{ unreadMails }} 条未读</span></RouterLink><RouterLink v-if="canAdmin" to="/admin" @click="mobileOpen = false">进入管理端</RouterLink><Button variant="outline" @click="mobileOpen = false; logout()"><LogOut :size="16" />登出</Button></nav></SheetContent></Sheet>
  </div>
</template>
