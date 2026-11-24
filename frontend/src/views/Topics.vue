<script setup lang="ts">
// 教程板块首页：页头 → 学习进度指标 → 专题列表 → 训练入口。
// 管理端共用本视图（/admin/tutorials）：只渲染专题列表，不显示学习进度。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowRight } from '@lucide/vue'
import { topicApi, progressApi, BANK_LABELS, type Topic, type ChallengeBank } from '../api'
import { ApiError } from '../api/client'
import { useAuthStore } from '../stores/auth'
import PageHeader from '@/components/layout/PageHeader.vue'
import MetricStrip from '@/components/layout/MetricStrip.vue'
import StatCard from '@/components/layout/StatCard.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const route = useRoute()
// 普通端 /topics/:slug；管理端 /admin/tutorials/:slug。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))
const isAdmin = computed(() => route.path.startsWith('/admin'))
const auth = useAuthStore()

const topics = ref<Topic[]>([])
const loading = ref(true)
const empty = ref(false)
const loadError = ref('')
const totalRooms = computed(() => topics.value.reduce((sum, topic) => sum + topic.room_count, 0))

const progress = ref<{ xp: number; completed_rooms: number; in_progress_rooms: number } | null>(null)

const quickBanks: Array<{ bank: ChallengeBank; desc: string }> = [
  { bank: 'tutorial', desc: '教程配套练习，读完章节即刻上手' },
  { bank: 'ctf', desc: '独立夺旗挑战，按类型与难度筛选' },
  { bank: 'acm', desc: '算法题目训练，在线判题即时反馈' },
]

async function load() {
  loading.value = true
  loadError.value = ''
  empty.value = false
  try {
    const page = await topicApi.list()
    topics.value = page.items
    empty.value = page.items.length === 0
  } catch (e) {
    // 501 = 教程功能未装配，按空态处理；其他错误必须可重试。
    if (e instanceof ApiError && e.status === 501) {
      empty.value = true
    } else {
      loadError.value = e instanceof Error ? e.message : '专题加载失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  load()
  if (!isAdmin.value && auth.token) {
    try {
      progress.value = await progressApi.mine()
    } catch {
      // 进度拉取失败不阻断首页
    }
  }
})
</script>

<template>
  <PageHeader
    title="学习路线"
    :description="isAdmin ? '专题与章节结构同学生端；内容管理入口在左侧导航。' : '按专题循序渐进，挑一个专题，从第一篇章节开始。'"
  >
    <template #actions>
      <span v-if="!loading && topics.length" class="topic-count">
        {{ topics.length }} 个专题 · {{ totalRooms }} 个章节
      </span>
    </template>
  </PageHeader>

  <MetricStrip v-if="!isAdmin && progress" :columns="3">
    <StatCard label="学习积分" :value="progress.xp" suffix="XP" tone="primary" />
    <StatCard label="已完成章节" :value="progress.completed_rooms" />
    <StatCard label="进行中" :value="progress.in_progress_rooms" />
  </MetricStrip>

  <div v-if="loading" class="topic-grid" role="status" aria-label="正在加载学习路线">
    <div v-for="n in 6" :key="n" class="topic-card topic-card--skeleton" aria-hidden="true">
      <span class="sk-line sk-line--short" />
      <span class="sk-line sk-line--heading" />
      <span class="sk-line" />
      <span class="sk-line sk-line--medium" />
    </div>
  </div>
  <EmptyState
    v-else-if="loadError"
    tone="error"
    title="学习路线加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />
  <EmptyState
    v-else-if="empty"
    title="还没有学习内容"
    description="管理员上传教程包后，这里会出现专题与章节。"
    action-label="去题库看看"
    action-to="/training"
  />
  <div v-else class="topic-grid">
    <RouterLink
      v-for="(topic, index) in topics"
      :key="topic.slug"
      :to="`${base}/${topic.slug}`"
      class="topic-card"
    >
      <div class="topic-card__top">
        <span class="topic-card__index">{{ String(index + 1).padStart(2, '0') }}</span>
        <span class="topic-card__kind">专题</span>
      </div>
      <h2 class="topic-card__title">{{ topic.title }}</h2>
      <p v-if="topic.summary" class="topic-card__summary">{{ topic.summary }}</p>
      <div class="topic-card__meta">
        <span>{{ topic.room_count }} 个章节</span>
        <span class="topic-card__go">进入专题 <ArrowRight aria-hidden="true" /></span>
      </div>
    </RouterLink>
  </div>

  <section v-if="!isAdmin && !loading && !loadError" class="quick-section" aria-labelledby="quick-title">
    <header class="quick-section__head">
      <h2 id="quick-title" class="quick-section__title">训练入口</h2>
      <RouterLink to="/training" class="quick-section__more">
        查看全部题库 <ArrowRight aria-hidden="true" />
      </RouterLink>
    </header>
    <div class="quick-grid">
      <RouterLink
        v-for="(item, index) in quickBanks"
        :key="item.bank"
        :to="`/training?bank=${item.bank}`"
        class="quick-card"
      >
        <span class="quick-card__index">{{ String(index + 1).padStart(2, '0') }}</span>
        <h3 class="quick-card__title">{{ BANK_LABELS[item.bank] }}</h3>
        <p class="quick-card__text">{{ item.desc }}</p>
        <span class="quick-card__go">进入题库 <ArrowRight aria-hidden="true" /></span>
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.topic-count {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.topic-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-4);
}

.topic-card {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 180px;
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
  color: inherit;
  transition: border-color 120ms ease;
}

.topic-card:hover {
  border-color: var(--border-strong);
}

.topic-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}

.topic-card__index {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.06em;
}

.topic-card__kind {
  padding: 2px var(--space-2);
  border-radius: var(--radius-pill);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-micro);
  font-weight: 500;
  line-height: 1.5;
}

.topic-card__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  overflow-wrap: anywhere;
}

.topic-card__summary {
  display: -webkit-box;
  margin-top: var(--space-2);
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.topic-card__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-top: auto;
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.topic-card__go {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--action-subtle-fg);
  font-weight: 500;
  white-space: nowrap;
}

.topic-card__go :deep(svg) {
  width: 14px;
  height: 14px;
}

.topic-card--skeleton {
  gap: var(--space-3);
  justify-content: center;
}

.sk-line {
  display: block;
  width: 100%;
  height: 12px;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.sk-line--short {
  width: 24%;
}

.sk-line--medium {
  width: 72%;
}

.sk-line--heading {
  width: 56%;
  height: 18px;
}

.quick-section {
  margin-top: var(--space-10);
}

.quick-section__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}

.quick-section__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  letter-spacing: 0;
}

.quick-section__more {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

.quick-section__more:hover {
  color: var(--text-primary);
}

.quick-section__more :deep(svg) {
  width: 14px;
  height: 14px;
}

.quick-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-4);
}

.quick-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
  color: inherit;
  transition: border-color 120ms ease;
}

.quick-card:hover {
  border-color: var(--border-strong);
}

.quick-card__index {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.06em;
}

.quick-card__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.quick-card__text {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.quick-card__go {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  margin-top: var(--space-2);
  color: var(--action-subtle-fg);
  font-size: var(--text-meta);
  font-weight: 500;
}

.quick-card__go :deep(svg) {
  width: 14px;
  height: 14px;
}

@media (max-width: 1023px) {
  .topic-grid,
  .quick-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .topic-grid,
  .quick-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (prefers-reduced-motion: reduce) {
  .topic-card,
  .quick-card {
    transition: none;
  }
}
</style>
