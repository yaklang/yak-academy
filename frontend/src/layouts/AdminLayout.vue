<script setup lang="ts">
// 管理端布局：与学生端同一「顶栏 + 内容」外壳（同头部/同导航样式/同内容宽），
// 管理端不再使用独立侧栏，避免两端观感割裂。
// 顶栏只保留少量入口：2 个学习预览 + 4 个管理项；其中「内容管理」「系统管理」
// 是分组入口，组内兄弟页面（教程/题目、容器/备份/审计/系统设置）在顶栏下方次级 tab 展开。
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Menu, ArrowUpRight, LogOut } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const mobileOpen = ref(false)
watch(() => route.fullPath, () => { mobileOpen.value = false })

// 顶栏主导航；group 项在组内任一页面保持激活。
const nav = [
  { title: '教程', to: '/admin/tutorials' },
  { title: '题库', to: '/admin/training' },
  { title: '内容管理', to: '/admin/panel/tutorials', group: 'content' },
  { title: '用户管理', to: '/admin/panel/users' },
  { title: '竞赛管理', to: '/admin/panel/competitions' },
  { title: '系统管理', to: '/admin/panel/containers', group: 'system' },
]

// 分组次级 tab：仅当前路由命中该组时渲染。
const subnav = computed(() => {
  if (/^\/admin\/panel\/(tutorials|challenges)/.test(route.path)) {
    return [
      { title: '教程管理', to: '/admin/panel/tutorials' },
      { title: '题目管理', to: '/admin/panel/challenges' },
    ]
  }
  if (/^\/admin\/panel\/(containers|backups|audit-logs|settings)/.test(route.path)) {
    return [
      { title: '容器管理', to: '/admin/panel/containers' },
      { title: '备份管理', to: '/admin/panel/backups' },
      { title: '操作审计', to: '/admin/panel/audit-logs' },
      { title: '系统设置', to: '/admin/panel/settings' },
    ]
  }
  return []
})

// RouterLink 只按路径前缀判激活，分组入口需要覆盖组内兄弟页。
function groupActive(item: { to: string; group?: string }) {
  return Boolean(item.group && subnav.value.some((s) => s.to === item.to))
}

const displayName = computed(() => auth.user?.username ?? '')

// 移动端抽屉：分组平铺但保留组标签，管理入口一次全部可见。
const mobileGroups = computed(() => {
  const groups = [
    { label: '学习预览', items: [nav[0], nav[1]] },
    { label: '管理', items: nav.slice(2) },
  ]
  if (subnav.value.length) groups.push({ label: currentGroupLabel.value, items: subnav.value })
  return groups
})

const currentGroupLabel = computed(() =>
  route.path.startsWith('/admin/panel/tutorials') || route.path.startsWith('/admin/panel/challenges')
    ? '内容管理'
    : '系统管理',
)

async function logout() { await auth.logout(); router.push('/login') }
</script>
<template>
  <div class="student-shell">
    <header class="student-header">
      <div class="student-header-inner">
        <Brand to="/admin/tutorials" />
        <nav class="main-nav" aria-label="管理导航">
          <RouterLink
            v-for="item in nav"
            :key="item.to"
            :to="item.to"
            :class="{ 'nav-active': groupActive(item) }"
          >{{ item.title }}</RouterLink>
        </nav>
        <div class="student-tools">
          <RouterLink to="/learn" class="admin-entry desktop-tool">返回学习端<ArrowUpRight :size="14" /></RouterLink>
          <RouterLink v-if="displayName" to="/profile" class="user-menu desktop-tool" :aria-label="`${displayName}（管理员）`">
            <span class="avatar">{{ displayName.slice(0, 1).toUpperCase() }}</span>
            <span class="uname">{{ displayName }}（管理员）</span>
          </RouterLink>
          <ThemeToggle />
          <Button v-if="auth.token" variant="ghost" class="desktop-tool" @click="logout">登出</Button>
          <Button variant="ghost" size="icon" class="mobile-menu-trigger" aria-label="打开管理导航" @click="mobileOpen = true"><Menu :size="20" /></Button>
        </div>
      </div>
      <nav v-if="subnav.length" class="admin-subnav" aria-label="管理分组">
        <div class="admin-subnav-inner">
          <RouterLink v-for="s in subnav" :key="s.to" :to="s.to">{{ s.title }}</RouterLink>
        </div>
      </nav>
    </header>
    <main class="student-main"><RouterView /></main>
    <footer class="shell-footer"><span>YAK ACADEMY · 管理工作台</span><RouterLink to="/learn">返回学习端 ↗</RouterLink></footer>
    <Sheet v-model:open="mobileOpen">
      <SheetContent class="w-[calc(100vw-2rem)] max-w-sm overflow-y-auto">
        <SheetHeader><SheetTitle>管理导航</SheetTitle><SheetDescription>{{ displayName }}（管理员）</SheetDescription></SheetHeader>
        <nav class="mobile-navigation" aria-label="管理入口">
          <template v-for="group in mobileGroups" :key="group.label">
            <p class="mobile-group-label">{{ group.label }}</p>
            <RouterLink v-for="item in group.items" :key="item.to" :to="item.to" @click="mobileOpen = false">{{ item.title }}</RouterLink>
          </template>
          <RouterLink to="/learn" @click="mobileOpen = false">返回学习端</RouterLink>
          <Button variant="outline" @click="mobileOpen = false; logout()"><LogOut :size="16" />登出</Button>
        </nav>
      </SheetContent>
    </Sheet>
  </div>
</template>

<style scoped>
.mobile-group-label {
  margin: 12px 0 0;
  padding: 0 12px;
  color: var(--muted-foreground);
  font-size: 11px;
  letter-spacing: 0.06em;
}
.mobile-navigation > a { margin-top: 4px; }
</style>
