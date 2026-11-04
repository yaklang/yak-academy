<script setup lang="ts">
// 徽章页：目录全集 + 当前用户获得状态（计算型读模型，进度/积分/路径的投影）。
import { computed, onMounted, ref } from 'vue'
import { notify } from '@/lib/notifications'
import { badgeApi, type Badge } from '../api'

const badges = ref<Badge[]>([])
const loading = ref(false)

const earnedCount = computed(() => badges.value.filter((b) => b.earned).length)

async function load() {
  loading.value = true
  try {
    badges.value = await badgeApi.list()
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">徽<span class="accent">章</span></h2>
      <p class="page-sub">已获得 {{ earnedCount }} / {{ badges.length }} 枚 —— 完成题目、学习教程与积分里程碑自动解锁。</p>
    </div>
  </header>
  <UiSkeleton v-if="loading" :rows="4" animated />
  <div v-else class="grid">
    <div v-for="b in badges" :key="b.code" class="badge" :class="{ earned: b.earned }">
      <span class="medal">{{ b.earned ? '🏅' : '🔒' }}</span>
      <div class="meta">
        <span class="title">{{ b.title }}</span>
        <span class="desc">{{ b.description }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--space-3);
}
.badge {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 1px dashed var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.badge .medal { opacity: 0.55; }
.badge .title { color: var(--muted-foreground); }
.badge.earned {
  border-style: solid;
  border-color: color-mix(in srgb, var(--primary) 35%, var(--border));
  background: var(--accent);
}
.badge.earned .medal { opacity: 1; }
.badge.earned .title { color: var(--foreground); }
.medal { font-size: var(--text-2xl); line-height: 1; }
.meta { display: flex; min-width: 0; flex-direction: column; gap: var(--space-1); }
.title { color: var(--foreground); font-size: var(--text-sm); font-weight: 600; }
.desc { color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-normal); }
</style>
