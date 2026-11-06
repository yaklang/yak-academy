<script setup lang="ts">
// 徽章页：目录全集 + 当前用户获得状态（计算型读模型，进度/积分/路径的投影）。
import { computed, onMounted, ref } from 'vue'
import { Award, Lock } from '@lucide/vue'
import { badgeApi, type Badge } from '../api'

const badges = ref<Badge[]>([])
const loading = ref(true)
const loadError = ref('')

const earnedCount = computed(() => badges.value.filter((b) => b.earned).length)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    badges.value = await badgeApi.list()
  } catch (e) {
    badges.value = []
    loadError.value = e instanceof Error ? e.message : '徽章加载失败，请稍后重试'
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
      <p class="page-sub">
        <template v-if="loading">正在同步徽章…</template>
        <template v-else-if="!loadError">已获得 {{ earnedCount }} / {{ badges.length }} 枚 —— 完成题目、学习教程与积分里程碑自动解锁。</template>
        <template v-else>完成题目、学习教程与积分里程碑自动解锁。</template>
      </p>
    </div>
  </header>
  <div v-if="loading" class="grid badge-skeleton" role="status" aria-label="正在加载徽章">
    <div v-for="n in 6" :key="n" class="badge sk-badge" aria-hidden="true">
      <span class="sk-medal" />
      <span class="sk-copy">
        <i class="sk-title" />
        <i class="sk-desc" />
      </span>
    </div>
  </div>
  <UiLoadError v-else-if="loadError" :message="loadError" @retry="load" />
  <UiEmpty v-else-if="badges.length === 0" description="暂无徽章：完成题目或教程后自动解锁" />
  <div v-else class="grid">
    <div v-for="b in badges" :key="b.code" class="badge" :class="{ earned: b.earned }">
      <span class="medal">
        <Award v-if="b.earned" :size="22" aria-hidden="true" />
        <Lock v-else :size="18" aria-hidden="true" />
      </span>
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
.badge.earned .medal { opacity: 1; color: var(--accent-foreground); }
.badge.earned .title { color: var(--foreground); }
.medal {
  display: inline-grid;
  width: 40px;
  height: 40px;
  flex: none;
  place-items: center;
  border-radius: 999px;
  background: var(--card);
  color: var(--muted-foreground);
}
.meta { display: flex; min-width: 0; flex-direction: column; gap: var(--space-1); }
.title { color: var(--foreground); font-size: var(--text-sm); font-weight: 600; }
.desc { color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-normal); }
.sk-badge { border-style: solid; }
.sk-medal { width: 40px; height: 40px; flex: none; border-radius: 999px; background: var(--muted); }
.sk-copy { display: grid; flex: 1; gap: var(--space-2); min-width: 0; }
.sk-title { display: block; width: 42%; height: 13px; border-radius: 4px; background: var(--muted); }
.sk-desc { display: block; width: 78%; height: 10px; border-radius: 4px; background: var(--muted); }
.badge-skeleton .sk-medal,
.badge-skeleton .sk-title,
.badge-skeleton .sk-desc { animation: badge-skeleton-pulse 1.4s ease-in-out infinite; }
@keyframes badge-skeleton-pulse { 50% { opacity: .45; } }
</style>
