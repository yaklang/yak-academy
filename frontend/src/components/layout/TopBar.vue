<script setup lang="ts">
// 顶栏：48px 高，左侧是移动端菜单/侧栏折叠与当前分区，右侧是学习进度、
// 徽章/邮箱入口、主题与账号菜单。低频入口收进下拉，避免横向堆叠。
import { RouterLink } from 'vue-router'
import { Award, Menu, PanelLeftClose, PanelLeftOpen, LogOut, Mail } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import ThemeToggle from '@/components/ThemeToggle.vue'

const props = defineProps<{
  sectionLabel: string
  modeLabel: string
  displayName: string
  roleLabel: string
  xp: number | null
  completedRooms: number | null
  unreadMails: number
  canAdmin: boolean
  adminMode: boolean
  collapsed: boolean
}>()

const emit = defineEmits<{
  toggle: []
  'open-mobile': []
  logout: []
}>()

const initial = () => props.displayName.slice(0, 1).toUpperCase()
</script>

<template>
  <header class="app-topbar">
    <Button
      variant="ghost"
      size="icon"
      class="app-topbar__mobile"
      aria-label="打开导航"
      @click="emit('open-mobile')"
    >
      <Menu />
    </Button>
    <Button
      variant="ghost"
      size="icon"
      class="app-topbar__collapse"
      :aria-label="collapsed ? '展开侧栏' : '收起侧栏'"
      :title="collapsed ? '展开侧栏' : '收起侧栏'"
      @click="emit('toggle')"
    >
      <PanelLeftOpen v-if="collapsed" />
      <PanelLeftClose v-else />
    </Button>

    <div class="app-topbar__crumbs">
      <span class="app-topbar__mode">{{ modeLabel }}</span>
      <span class="app-topbar__sep" aria-hidden="true">/</span>
      <strong class="app-topbar__section">{{ sectionLabel }}</strong>
    </div>

    <div class="app-topbar__tools">
      <span v-if="xp !== null" class="app-topbar__xp">
        {{ xp }} <small>XP</small>
        <span v-if="completedRooms !== null" class="app-topbar__xp-detail">{{ completedRooms }} 已完成</span>
      </span>
      <Button variant="ghost" size="icon" as-child aria-label="徽章" title="徽章">
        <RouterLink to="/badges">
          <Award />
        </RouterLink>
      </Button>
      <Button variant="ghost" size="icon" as-child class="app-topbar__mail" aria-label="邮箱" title="邮箱">
        <RouterLink to="/mailbox">
          <Mail />
          <span v-if="unreadMails" class="app-topbar__badge">{{ unreadMails > 99 ? '99+' : unreadMails }}</span>
        </RouterLink>
      </Button>
      <ThemeToggle />
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button variant="ghost" class="app-topbar__user" :aria-label="`${displayName}（${roleLabel}）`">
            <span class="app-topbar__avatar" aria-hidden="true">{{ initial() }}</span>
            <span class="app-topbar__name">{{ displayName }}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-56">
          <DropdownMenuLabel>{{ displayName }}（{{ roleLabel }}）</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem as-child>
            <RouterLink to="/profile">账号信息</RouterLink>
          </DropdownMenuItem>
          <DropdownMenuItem v-if="canAdmin" as-child>
            <RouterLink :to="adminMode ? '/learn' : '/admin'">
              {{ adminMode ? '返回学习端' : '进入管理端' }}
            </RouterLink>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem @select="emit('logout')">
            <LogOut />
            登出
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  </header>
</template>

<style scoped>
.app-topbar {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 48px;
  padding-inline: var(--space-3);
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-canvas) 92%, transparent);
  backdrop-filter: blur(8px);
}

.app-topbar__mobile {
  display: none;
}

.app-topbar__crumbs {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  font-size: var(--text-caption);
}

.app-topbar__mode {
  color: var(--text-tertiary);
  white-space: nowrap;
}

.app-topbar__sep {
  color: var(--text-tertiary);
}

.app-topbar__section {
  overflow: hidden;
  color: var(--text-primary);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-topbar__tools {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  margin-left: auto;
  min-width: 0;
}

.app-topbar__xp {
  display: inline-flex;
  align-items: baseline;
  gap: var(--space-1);
  padding-inline: var(--space-2);
  color: var(--action-subtle-fg);
  font-size: var(--text-caption);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.app-topbar__xp small {
  font-size: var(--text-micro);
}

.app-topbar__xp-detail {
  color: var(--text-tertiary);
  font-size: var(--text-micro);
}

.app-topbar__mail {
  position: relative;
}

.app-topbar__badge {
  position: absolute;
  top: -2px;
  right: -2px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 16px;
  height: 16px;
  padding-inline: var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--action-primary);
  color: var(--action-primary-fg);
  font-size: var(--text-micro);
  line-height: 1;
}

.app-topbar__user {
  gap: var(--space-2);
  padding-inline: var(--space-1);
}

.app-topbar__avatar {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-meta);
  font-weight: 600;
}

.app-topbar__name {
  max-width: 120px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--text-meta);
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1023px) {
  .app-topbar__mobile {
    display: inline-flex;
  }

  .app-topbar__collapse {
    display: none;
  }

  .app-topbar__tools :deep([data-slot='button']),
  .app-topbar__user {
    min-height: 44px;
    min-width: 44px;
  }

  .app-topbar__name {
    display: none;
  }

  .app-topbar__xp-detail {
    display: none;
  }
}
</style>
