<script setup lang="ts">
// 应用外壳：左侧栏 + 顶栏 + 内容区。学生端与管理端共用同一结构，
// 只切换导航配置；移动端侧栏收进 Sheet。页面只提供内容，不再各自画 header。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogOut, Mail } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { progressApi, profileApi } from '@/api'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import SidebarNav from './SidebarNav.vue'
import TopBar from './TopBar.vue'
import { activeSection, isNavItemActive, type NavSection } from './navigation'

const props = withDefaults(defineProps<{
  nav: NavSection[]
  brandTo: string
  mode: 'student' | 'admin'
  footerNote: string
}>(), {
  footerNote: 'YAK ACADEMY',
})

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const adminMode = computed(() => props.mode === 'admin')
const displayName = computed(() => auth.user?.username ?? '')
const roleLabel = computed(() => (auth.user?.global_role === 'admin' ? '管理员' : '学员'))
const canAdmin = computed(() => auth.user?.global_role === 'admin')
const modeLabel = computed(() => (adminMode.value ? '管理端' : '学习端'))
const sectionLabel = computed(() => activeSection(props.nav, route.path)?.label ?? props.nav[0]?.label ?? '')

const xp = ref<number | null>(null)
const completedRooms = ref<number | null>(null)
const unreadMails = ref(0)
const mobileOpen = ref(false)
const collapsed = ref(readCollapsed())

function readCollapsed() {
  try {
    return localStorage.getItem('academy_sidebar_collapsed') === '1'
  } catch {
    return false
  }
}

watch(collapsed, (value) => {
  try {
    localStorage.setItem('academy_sidebar_collapsed', value ? '1' : '0')
  } catch {
    // 隐私模式下无法持久化，忽略。
  }
})

async function refreshUnread() {
  if (adminMode.value) return
  try {
    const result = await profileApi.unreadCount()
    unreadMails.value = result.unread
  } catch {
    // 未读数失败不阻断布局
  }
}

function onMailsRead() {
  refreshUnread()
}

onMounted(async () => {
  if (!auth.token) return
  if (!adminMode.value) {
    try {
      const progress = await progressApi.mine()
      xp.value = progress.xp
      completedRooms.value = progress.completed_rooms
    } catch {
      // 进度失败不阻断布局
    }
    await refreshUnread()
    window.addEventListener('academy:mails-read', onMailsRead)
  }
})

onUnmounted(() => {
  window.removeEventListener('academy:mails-read', onMailsRead)
})

watch(() => route.fullPath, () => {
  mobileOpen.value = false
})

async function logout() {
  await auth.logout()
  router.push('/login')
}
</script>

<template>
  <div class="app-shell" :class="{ 'is-collapsed': collapsed }">
    <a class="app-skip" href="#main-content">跳到主要内容</a>

    <SidebarNav
      :nav="nav"
      :brand-to="brandTo"
      :display-name="displayName"
      :role-label="roleLabel"
      :unread-mails="unreadMails"
      :can-admin="canAdmin"
      :admin-mode="adminMode"
      :collapsed="collapsed"
      @navigate="mobileOpen = false"
      @logout="logout"
    />

    <div class="app-body">
      <TopBar
        :section-label="sectionLabel"
        :mode-label="modeLabel"
        :display-name="displayName"
        :role-label="roleLabel"
        :xp="adminMode ? null : xp"
        :completed-rooms="adminMode ? null : completedRooms"
        :unread-mails="unreadMails"
        :can-admin="canAdmin"
        :admin-mode="adminMode"
        :collapsed="collapsed"
        @toggle="collapsed = !collapsed"
        @open-mobile="mobileOpen = true"
        @logout="logout"
      />

      <main id="main-content" class="app-content" tabindex="-1">
        <slot />
      </main>

      <footer class="app-footer">
        <span>{{ footerNote }}</span>
        <RouterLink to="/">学院首页 ↗</RouterLink>
      </footer>
    </div>

    <Sheet v-model:open="mobileOpen">
      <SheetContent class="w-[calc(100vw-2rem)] max-w-sm overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{{ modeLabel }}导航</SheetTitle>
          <SheetDescription>{{ displayName }}（{{ roleLabel }}）</SheetDescription>
        </SheetHeader>
        <nav class="app-mobile-nav" aria-label="移动端导航">
          <template v-for="section in nav" :key="section.label">
            <p class="app-mobile-nav__label">{{ section.label }}</p>
            <RouterLink
              v-for="item in section.items"
              :key="item.to"
              :to="item.to"
              class="app-mobile-nav__item"
              :class="{ 'is-active': isNavItemActive(item, route.path) }"
              @click="mobileOpen = false"
            >
              <component :is="item.icon" aria-hidden="true" />
              {{ item.label }}
              <span v-if="item.badge === 'unread' && unreadMails" class="app-mobile-nav__badge">
                <Mail aria-hidden="true" />
                {{ unreadMails > 99 ? '99+' : unreadMails }}
              </span>
            </RouterLink>
          </template>
          <div class="app-mobile-nav__foot">
            <ThemeToggle />
            <Button
              v-if="canAdmin"
              variant="outline"
              as-child
            >
              <RouterLink :to="adminMode ? '/learn' : '/admin'" @click="mobileOpen = false">
                {{ adminMode ? '返回学习端' : '进入管理端' }}
              </RouterLink>
            </Button>
            <Button variant="outline" @click="logout">
              <LogOut aria-hidden="true" />
              登出
            </Button>
          </div>
        </nav>
      </SheetContent>
    </Sheet>
  </div>
</template>

<style scoped>
.app-shell {
  min-height: 100dvh;
  background: var(--bg-canvas);
}

.app-skip {
  position: fixed;
  top: var(--space-2);
  left: var(--space-2);
  z-index: 60;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-control);
  background: var(--action-primary);
  color: var(--action-primary-fg);
  font-size: var(--text-caption);
  transform: translateY(-200%);
}

.app-skip:focus-visible {
  transform: translateY(0);
}

.app-body {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
  margin-left: 272px;
  transition: margin-left 160ms ease;
}

.app-shell.is-collapsed .app-body {
  margin-left: 48px;
}

.app-content {
  flex: 1;
  width: 100%;
  max-width: var(--layout-max-width);
  margin-inline: auto;
  padding: var(--space-5) var(--space-8) var(--space-12);
  min-width: 0;
}

.app-content:focus {
  outline: none;
}

.app-content > * {
  min-width: 0;
  max-width: 100%;
}

.app-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  width: 100%;
  max-width: var(--layout-max-width);
  margin-inline: auto;
  padding: var(--space-4) var(--space-8);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.app-mobile-nav {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-4);
}

.app-mobile-nav__label {
  margin-top: var(--space-3);
  padding-inline: var(--space-2);
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.app-mobile-nav__item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: 44px;
  padding-inline: var(--space-2);
  border-radius: var(--radius-control);
  color: var(--text-secondary);
  font-size: var(--text-body);
}

.app-mobile-nav__item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.app-mobile-nav__item.is-active {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-weight: 600;
}

.app-mobile-nav__item :deep(svg) {
  width: 16px;
  height: 16px;
}

.app-mobile-nav__badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
  color: var(--action-subtle-fg);
  font-size: var(--text-meta);
}

.app-mobile-nav__foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  flex-wrap: wrap;
}

@media (max-width: 1023px) {
  .app-body,
  .app-shell.is-collapsed .app-body {
    margin-left: 0;
  }

  .app-content {
    padding: var(--space-4) var(--space-4) var(--space-10);
  }

  .app-footer {
    padding-inline: var(--space-4);
  }
}

@media (prefers-reduced-motion: reduce) {
  .app-body {
    transition: none;
  }
}
</style>
