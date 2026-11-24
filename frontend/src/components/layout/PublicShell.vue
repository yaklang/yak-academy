<script setup lang="ts">
// 公开页外壳：学院首页与登录页共用同一页头/页脚，内容宽度与内边距统一。
// 公开页不进入应用侧栏，也不加载任何装饰动画。
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'

withDefaults(defineProps<{
  footerNote?: string
}>(), {
  footerNote: '学以致用，知行合一。',
})

const year = new Date().getFullYear()
</script>

<template>
  <div class="public-shell">
    <div class="public-shell__inner">
      <header class="public-shell__header">
        <Brand />
        <nav v-if="$slots.nav" class="public-shell__nav" aria-label="页面导航">
          <slot name="nav" />
        </nav>
        <div class="public-shell__actions">
          <slot name="actions" />
          <ThemeToggle />
        </div>
      </header>
      <main class="public-shell__main">
        <slot />
      </main>
      <footer class="public-shell__footer">
        <Brand />
        <span>{{ footerNote }}</span>
        <span>© {{ year }} Yak Academy</span>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.public-shell {
  min-height: 100dvh;
  background: var(--bg-canvas);
}

.public-shell__inner {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
  width: 100%;
  max-width: var(--public-max-width);
  margin-inline: auto;
  padding-inline: clamp(20px, 4vw, 48px);
}

.public-shell__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-6);
  height: 72px;
  border-bottom: 1px solid var(--border);
}

.public-shell__nav {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

.public-shell__nav :deep(a) {
  padding-block: var(--space-2);
}

.public-shell__nav :deep(a:hover) {
  color: var(--text-primary);
}

.public-shell__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

.public-shell__main {
  flex: 1;
  min-width: 0;
  padding-block: var(--space-10) var(--space-16);
}

.public-shell__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-4);
  padding-block: var(--space-5);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

@media (max-width: 767px) {
  .public-shell__header {
    height: auto;
    min-height: 64px;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-3);
    padding-block: var(--space-2);
  }

  .public-shell__actions {
    margin-left: auto;
  }

  .public-shell__nav {
    display: none;
  }

  .public-shell__main {
    padding-block: var(--space-6) var(--space-10);
  }

  .public-shell__footer {
    justify-content: flex-start;
  }
}
</style>
