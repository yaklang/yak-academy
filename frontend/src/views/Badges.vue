<script setup lang="ts">
// 徽章页：目录全集 + 当前用户获得状态（计算型读模型）。
import { computed, onMounted, ref } from 'vue'
import { Award, Lock } from '@lucide/vue'
import { badgeApi, type Badge } from '../api'
import PageHeader from '@/components/layout/PageHeader.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const badges = ref<Badge[]>([])
const loading = ref(true)
const loadError = ref('')

const earnedCount = computed(() => badges.value.filter((badge) => badge.earned).length)

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
  <PageHeader
    title="徽章"
    description="完成题目、学习教程与积分里程碑自动解锁。"
  >
    <template v-if="!loading && !loadError" #actions>
      <span class="badge-count">已获得 {{ earnedCount }} / {{ badges.length }} 枚</span>
    </template>
  </PageHeader>

  <div v-if="loading" class="badge-grid" role="status" aria-label="正在加载徽章">
    <div v-for="n in 6" :key="n" class="badge-card" aria-hidden="true">
      <span class="badge-medal badge-medal--empty" />
      <span class="badge-copy">
        <span class="sk-line sk-line--title" />
        <span class="sk-line" />
      </span>
    </div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="徽章加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
  <EmptyState
    v-else-if="badges.length === 0"
    title="还没有可解锁的徽章"
    description="完成题目或教程后，徽章会自动出现在这里。"
    action-label="去题库"
    action-to="/training"
  />
  <div v-else class="badge-grid">
    <div
      v-for="badge in badges"
      :key="badge.code"
      class="badge-card"
      :data-earned="badge.earned ? 'true' : 'false'"
    >
      <span class="badge-medal" aria-hidden="true">
        <Award v-if="badge.earned" />
        <Lock v-else />
      </span>
      <span class="badge-copy">
        <span class="badge-title">{{ badge.title }}</span>
        <span class="badge-desc">{{ badge.description }}</span>
      </span>
      <span v-if="badge.earned" class="badge-state">已获得</span>
      <span v-else class="badge-state">未解锁</span>
    </div>
  </div>
</template>

<style scoped>
.badge-count {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.badge-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: var(--space-3);
}

.badge-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.badge-card[data-earned='true'] {
  border-color: color-mix(in srgb, var(--action-primary) 32%, var(--border));
}

.badge-medal {
  display: inline-grid;
  place-items: center;
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
  color: var(--text-tertiary);
}

.badge-medal :deep(svg) {
  width: 20px;
  height: 20px;
}

.badge-card[data-earned='true'] .badge-medal {
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

.badge-medal--empty {
  background: var(--bg-subtle);
}

.badge-copy {
  display: grid;
  flex: 1;
  gap: var(--space-1);
  min-width: 0;
}

.badge-title {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.badge-desc {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.badge-state {
  flex-shrink: 0;
  color: var(--text-tertiary);
  font-size: var(--text-micro);
}

.badge-card[data-earned='true'] .badge-state {
  color: var(--status-success);
}

.sk-line {
  display: block;
  width: 78%;
  height: 10px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-line--title {
  width: 42%;
  height: 12px;
}

@media (max-width: 560px) {
  .badge-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>