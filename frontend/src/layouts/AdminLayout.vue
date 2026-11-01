<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Menu, BookOpen, Flag, Package, Braces, Server, Database, ScrollText, Trophy, Users, ArrowLeft, LogOut } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const mobileOpen = ref(false)
const sections = [
  { label: '学习预览', items: [{ title:'教程',to:'/admin/tutorials',icon:BookOpen },{title:'题库',to:'/admin/training',icon:Flag}] },
  { label: '资源管理', items: [{title:'教程管理',to:'/admin/panel/tutorials',icon:Package},{title:'题目管理',to:'/admin/panel/challenges',icon:Braces},{title:'容器管理',to:'/admin/panel/containers',icon:Server},{title:'备份管理',to:'/admin/panel/backups',icon:Database},{title:'操作审计',to:'/admin/panel/audit-logs',icon:ScrollText},{title:'竞赛管理',to:'/admin/panel/competitions',icon:Trophy},{title:'用户管理',to:'/admin/panel/users',icon:Users}] },
]
const currentTitle = computed(() => sections.flatMap(section => section.items).find(item => route.path.startsWith(item.to))?.title || '管理工作台')
watch(() => route.fullPath, () => { mobileOpen.value = false })
async function logout() { await auth.logout(); router.push('/login') }
</script>
<template>
  <div class="admin-shell">
    <aside class="admin-sidebar">
      <div class="admin-brand"><Brand to="/admin/tutorials" /><span class="workspace-label">管理工作台</span></div>
      <nav class="admin-navigation" aria-label="管理导航"><section v-for="section in sections" :key="section.label"><p>{{ section.label }}</p><RouterLink v-for="item in section.items" :key="item.to" :to="item.to"><component :is="item.icon" :size="17" :stroke-width="1.6" />{{ item.title }}</RouterLink></section></nav>
      <div class="admin-side-footer"><RouterLink to="/learn"><ArrowLeft :size="16" />返回普通端</RouterLink><div class="admin-identity"><span class="avatar">{{ auth.user?.username.slice(0,1).toUpperCase() }}</span><span>{{ auth.user?.username }}<small>管理员</small></span></div></div>
    </aside>
    <div class="admin-workspace">
      <header class="admin-header"><div class="admin-header-title"><Button variant="ghost" size="icon" class="mobile-admin-trigger" aria-label="打开管理导航" @click="mobileOpen = true"><Menu :size="20" /></Button><span class="admin-desktop-label">工作台 /</span><span>{{ currentTitle }}</span></div><div class="public-actions"><ThemeToggle /><Button variant="ghost" @click="logout"><LogOut :size="15" /><span>登出</span></Button></div></header>
      <main class="admin-main"><RouterView /></main>
    </div>
    <Sheet v-model:open="mobileOpen"><SheetContent side="left" class="w-[calc(100vw-2rem)] max-w-xs overflow-y-auto"><SheetHeader><SheetTitle>管理导航</SheetTitle><SheetDescription>Yak Academy 管理工作台</SheetDescription></SheetHeader><nav class="admin-navigation" aria-label="移动管理导航"><section v-for="section in sections" :key="section.label"><p>{{ section.label }}</p><RouterLink v-for="item in section.items" :key="item.to" :to="item.to" @click="mobileOpen = false"><component :is="item.icon" :size="17" />{{ item.title }}</RouterLink></section><RouterLink to="/learn" @click="mobileOpen = false"><ArrowLeft :size="16" />返回普通端</RouterLink></nav></SheetContent></Sheet>
  </div>
</template>
