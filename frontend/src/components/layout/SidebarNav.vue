<script setup lang="ts">
// 应用侧栏：48px 图标栏（分区）+ 224px 菜单（页面），两者共用同一份导航配置。
// 收起时只保留图标栏；移动端整体隐藏，由顶栏按钮打开 Sheet。
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { LogOut } from '@lucide/vue'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { Button } from '@/components/ui/button'
import { activeSection, isNavItemActive, type NavSection } from './navigation'

const props = defineProps<{
  nav: NavSection[]
  brandTo: string
  displayName: string
  roleLabel: string
  unreadMails: number
  canAdmin: boolean
  adminMode: boolean
  collapsed: boolean
}>()

const emit = defineEmits<{ navigate: []; logout: [] }>()

const route = useRoute()
const current = computed(() => activeSection(props.nav, route.path))
const initial = computed(() => props.displayName.slice(0, 1).toUpperCase())
</script>

<template>
  <aside class="app-sidebar" :data-collapsed="collapsed ? 'true' : undefined">
    <div class="app-rail">
      <Brand :to="brandTo" compact class="app-rail__brand" />
      <nav class="app-rail__nav" aria-label="主导航">
        <button
          v-for="section in nav"
          :key="section.label"
          type="button"
          class="app-rail__item"
          :class="{ 'is-active': current?.label === section.label }"
          :aria-label="section.label"
          :title="section.label"
          @click="emit('navigate'); $router.push(section.items[0].to)"
        >
          <component :is="section.icon" aria-hidden="true" />
        </button>
      </nav>
      <div class="app-rail__foot">
        <ThemeToggle />
      </div>
    </div>

    <div class="app-menu" :aria-hidden="collapsed || undefined" :inert="collapsed ? true : undefined">
      <div class="app-menu__brand">
        <Brand :to="brandTo" />
      </div>
      <nav class="app-menu__nav" aria-label="页面导航">
        <section
          v-for="section in nav"
          :key="section.label"
          class="app-nav-section"
        >
          <p class="app-nav-section__label">{{ section.label }}</p>
          <RouterLink
            v-for="item in section.items"
            :key="item.to"
            :to="item.to"
            class="app-nav-item"
            :class="{ 'is-active': isNavItemActive(item, route.path) }"
            @click="emit('navigate')"
          >
            <component :is="item.icon" class="app-nav-item__icon" aria-hidden="true" />
            <span class="app-nav-item__label">{{ item.label }}</span>
            <span v-if="item.badge === 'unread' && unreadMails" class="app-nav-item__badge">
              {{ unreadMails > 99 ? '99+' : unreadMails }}
            </span>
          </RouterLink>
        </section>
      </nav>
      <div class="app-menu__foot">
        <div class="app-menu__user">
          <span class="app-menu__avatar" aria-hidden="true">{{ initial }}</span>
          <span class="app-menu__identity">
            <span class="app-menu__name">{{ displayName }}</span>
            <span class="app-menu__role">{{ roleLabel }}</span>
          </span>
        </div>
        <Button
          v-if="canAdmin"
          variant="outline"
          size="sm"
          as-child
          class="app-menu__action"
        >
          <RouterLink :to="adminMode ? '/learn' : '/admin'">
            {{ adminMode ? '返回学习端' : '进入管理端' }}
          </RouterLink>
        </Button>
        <Button variant="ghost" size="sm" class="app-menu__action" @click="emit('logout')">
          <LogOut aria-hidden="true" />
          登出
        </Button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.app-sidebar {
  position: fixed;
  inset-block: 0;
  left: 0;
  z-index: 40;
  display: flex;
}

.app-rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-1);
  width: 48px;
  padding: var(--space-2) 0;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
}

.app-rail__brand {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  margin-bottom: var(--space-2);
}

.app-rail__brand :deep(span) {
  display: none;
}

.app-rail__nav {
  display: grid;
  gap: var(--space-1);
  width: 100%;
  justify-items: center;
}

.app-rail__item {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: var(--radius-control);
  color: var(--text-secondary);
  transition: background-color 120ms ease, color 120ms ease;
}

.app-rail__item :deep(svg) {
  width: 18px;
  height: 18px;
}

.app-rail__item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.app-rail__item.is-active {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

.app-rail__foot {
  margin-top: auto;
}

.app-menu {
  display: flex;
  flex-direction: column;
  width: 224px;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
  transition: transform 160ms ease;
}

.app-sidebar[data-collapsed='true'] .app-menu {
  transform: translateX(-100%);
}

.app-menu__brand {
  display: flex;
  align-items: center;
  height: 48px;
  padding-inline: var(--space-4);
  border-bottom: 1px solid var(--border);
}

.app-menu__nav {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-3) var(--space-2);
}

.app-nav-section + .app-nav-section {
  margin-top: var(--space-4);
}

.app-nav-section__label {
  padding: 0 var(--space-2) var(--space-1);
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.app-nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 36px;
  padding-inline: var(--space-2);
  border-radius: var(--radius-control);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  font-weight: 500;
  transition: background-color 120ms ease, color 120ms ease;
}

.app-nav-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.app-nav-item.is-active {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-weight: 600;
}

.app-nav-item__icon {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.app-nav-item__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-nav-item__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  margin-left: auto;
  padding-inline: 4px;
  border-radius: var(--radius-pill);
  background: var(--action-primary);
  color: var(--action-primary-fg);
  font-size: var(--text-micro);
  font-variant-numeric: tabular-nums;
}

.app-menu__foot {
  display: grid;
  gap: var(--space-2);
  padding: var(--space-3);
  border-top: 1px solid var(--border);
}

.app-menu__user {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-1) var(--space-2);
}

.app-menu__avatar {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-meta);
  font-weight: 600;
}

.app-menu__identity {
  display: grid;
  min-width: 0;
}

.app-menu__name {
  overflow: hidden;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-menu__role {
  color: var(--text-tertiary);
  font-size: var(--text-micro);
}

.app-menu__action {
  justify-content: flex-start;
  width: 100%;
}

@media (prefers-reduced-motion: reduce) {
  .app-menu,
  .app-nav-item,
  .app-rail__item {
    transition: none;
  }
}
</style>
