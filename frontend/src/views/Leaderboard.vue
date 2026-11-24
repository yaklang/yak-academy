<script setup lang="ts">
// 排行榜（事件派生只读投影）：全站概览 + 前三名 + 我的名次 + 积分分布 + 分页名次表。
// 数据只有 /leaderboard 一个来源，刷新、分页与错误重试都重新拉取同一份视图；
// 页面不使用假数据，未上榜 / 空榜 / 加载 / 错误分别给出可操作的终态。
import { computed, onMounted, ref } from 'vue'
import { ArrowRight, RefreshCw } from '@lucide/vue'
import { progressApi, type LeaderboardEntry, type LeaderboardView } from '../api'
import { formatDateTime } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'
import PageHeader from '@/components/layout/PageHeader.vue'
import MetricStrip from '@/components/layout/MetricStrip.vue'
import StatCard from '@/components/layout/StatCard.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import { Button } from '@/components/ui/button'

const PAGE_SIZE = 20
const auth = useAuthStore()

const view = ref<LeaderboardView | null>(null)
const page = ref(1)
const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
// 连续切页/刷新时只接受最后一次请求的结果，避免旧响应覆盖新页码。
let requestSeq = 0

const stats = computed(() => view.value?.stats ?? null)
const me = computed(() => view.value?.me ?? null)
const updatedLabel = computed(() =>
  stats.value?.updated_at ? formatDateTime(stats.value.updated_at) : '暂无记录',
)
const syncLabel = computed(() => {
  if (loading.value) return '正在同步…'
  if (!view.value) return '数据暂不可用'
  return `更新于 ${updatedLabel.value}`
})
const topScoreBase = computed(() => Math.max(1, stats.value?.top_score ?? 0))
const practicePath = computed(() =>
  auth.user?.global_role === 'admin' ? '/admin/tutorials' : '/training',
)
const distributionRows = computed(() => {
  const rows = view.value?.distribution ?? []
  const max = Math.max(1, ...rows.map((row) => row.count))
  const total = Math.max(1, stats.value?.participants ?? 0)
  return rows.map((row) => ({
    ...row,
    width: Math.round((row.count / max) * 100),
    share: Math.round((row.count / total) * 100),
  }))
})
const gapToTop = computed(() => {
  if (!me.value || !stats.value) return null
  return Math.max(0, stats.value.top_score - me.value.score)
})

function isMe(entry: LeaderboardEntry) {
  return me.value?.username === entry.username
}
function rowClassName({ row }: { row: LeaderboardEntry }) {
  return isMe(row) ? 'is-me' : undefined
}
function scoreWidth(score: number) {
  return Math.max(0, Math.min(100, Math.round((score / topScoreBase.value) * 100)))
}

async function load(target = page.value, options: { silent?: boolean } = {}) {
  const seq = ++requestSeq
  error.value = ''
  if (options.silent) refreshing.value = true
  else loading.value = true
  try {
    const resp = await progressApi.leaderboard(target, PAGE_SIZE)
    if (seq !== requestSeq) return
    view.value = resp
    page.value = resp.page
  } catch (e) {
    if (seq !== requestSeq) return
    // 刷新/翻页失败时保留上一份成功视图，并把页码退回视图对应的页，
    // 避免分页控件显示的目标页与表格内容不一致。
    if (view.value) page.value = view.value.page
    error.value = (e as Error).message
  } finally {
    if (seq === requestSeq) {
      loading.value = false
      refreshing.value = false
    }
  }
}

function refresh() {
  void load(page.value, { silent: true })
}
function changePage(next: number) {
  page.value = next
  void load(next)
}

onMounted(() => void load(1))
</script>

<template>
  <PageHeader
    title="排行榜"
    description="按总分实时派生，同分按通关数排序；积分包含通关得分与提示消耗。"
  >
    <template #actions>
      <span class="sync-note">{{ syncLabel }}</span>
      <Button
        variant="outline"
        size="sm"
        :loading="refreshing"
        :disabled="loading"
        aria-label="刷新排行榜"
        @click="refresh"
      >
        <RefreshCw aria-hidden="true" />
        刷新
      </Button>
    </template>
  </PageHeader>

  <div v-if="error && view" class="inline-error" role="alert">
    <span>{{ error }}</span>
    <Button size="sm" variant="outline" @click="refresh">重试</Button>
  </div>

  <div v-if="loading && !view" class="page-skeleton" aria-busy="true" aria-label="排行榜加载中">
    <div class="skeleton-kpis">
      <span v-for="n in 4" :key="n" class="skeleton-card" />
    </div>
    <div class="skeleton-panels">
      <span class="skeleton-panel" />
      <span class="skeleton-panel" />
    </div>
  </div>
  <EmptyState
    v-else-if="!view"
    tone="error"
    title="排行榜加载失败"
    :description="error || '暂时无法获取排行榜数据，请稍后重试。'"
    action-label="重新加载"
    @action="load(1)"
  />
  <template v-else>
    <MetricStrip :columns="4">
      <StatCard label="上榜学员" :value="stats?.participants ?? 0" hint="拥有积分记录的学员" value-test-id="lb-participants" />
      <StatCard label="累计积分" :value="stats?.total_score ?? 0" hint="含提示扣分后的净积分" value-test-id="lb-total-score" />
      <StatCard label="累计通关" :value="stats?.total_solves ?? 0" hint="全站通关题次" value-test-id="lb-total-solves" />
      <StatCard label="平均分" :value="stats?.average_score ?? 0" :hint="`榜首 ${stats?.top_score ?? 0} 分`" value-test-id="lb-average-score" />
    </MetricStrip>

    <div class="board-layout" :aria-busy="loading">
      <DataTablePanel
        title="总榜"
        :count="view.total"
        :description="`共 ${view.total} 人`"
      >
        <template v-if="loading" #toolbar>
          <span class="card-loading" role="status">同步中…</span>
        </template>

        <EmptyState
          v-if="view.total === 0"
          data-testid="leaderboard-empty"
          title="还没有学员上榜"
          description="完成任意一道题获得积分后，你的名字会出现在这里。"
          action-label="去题库"
          :action-to="practicePath"
        />
        <template v-else>
          <section class="podium" aria-label="全场前三名">
            <ol class="podium-list">
              <li
                v-for="(entry, index) in view.top"
                :key="entry.username"
                class="podium-item"
                :data-place="index + 1"
              >
                <span class="podium-rank">#{{ entry.rank }}</span>
                <span class="podium-name">{{ entry.username }}<em v-if="isMe(entry)">我</em></span>
                <span class="podium-score">{{ entry.score }}<small> 分</small></span>
                <span class="podium-meta">{{ entry.solved_count }} 题通关</span>
              </li>
            </ol>
          </section>

          <UiTable
            :data="view.items"
            row-key="username"
            aria-label="排行榜名次"
            :row-class-name="rowClassName"
          >
            <UiTableColumn prop="rank" label="名次" width="76">
              <template #default="{ row }">
                <span class="rank-cell" :class="{ 'rank-top': row.rank <= 3 }">{{ row.rank }}</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="学员" min-width="160">
              <template #default="{ row }">
                <span class="user-cell">
                  <span class="user-name">{{ row.username }}</span>
                  <span v-if="isMe(row)" class="me-chip">我</span>
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="总分" width="170" align="right">
              <template #default="{ row }">
                <span class="score-cell">
                  <span>{{ row.score }}</span>
                  <span class="score-track" aria-hidden="true">
                    <i :style="{ width: scoreWidth(row.score) + '%' }" />
                  </span>
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="solved_count" label="通关" width="88" align="right">
              <template #default="{ row }">
                <span class="solved-cell">{{ row.solved_count }} 题</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="最近活跃" width="176">
              <template #default="{ row }">
                <span class="last-active">{{ formatDateTime(row.last_active_at) }}</span>
              </template>
            </UiTableColumn>
          </UiTable>
        </template>

        <template v-if="view.total > PAGE_SIZE" #footer>
          <p class="rule-note">积分相同时按通关数排序，再按达成时间先后排序。</p>
          <UiPagination
            layout="prev, pager, next"
            :total="view.total"
            :page-size="PAGE_SIZE"
            :current-page="page"
            @current-change="changePage"
          />
        </template>
      </DataTablePanel>

      <aside class="side-stack">
        <section class="side-panel" data-testid="my-rank" aria-labelledby="my-rank-title">
          <header class="side-panel__head">
            <h2 id="my-rank-title" class="side-panel__title">我的排名</h2>
            <span class="side-panel__meta">{{ me ? '已上榜' : '未上榜' }}</span>
          </header>

          <div v-if="me" class="me-panel">
            <div class="me-head">
              <span class="me-rank">#{{ me.rank }}</span>
              <span class="me-identity">
                <b>{{ me.username }}</b>
                <span>{{ me.solved_count }} 题 · {{ me.score }} 分</span>
              </span>
            </div>
            <span class="me-note">{{ gapToTop === 0 ? '当前榜首' : `距榜首 ${gapToTop} 分` }}</span>
            <span class="me-track" aria-hidden="true">
              <i :style="{ width: scoreWidth(me.score) + '%' }" />
            </span>
          </div>

          <div v-else class="side-empty" role="status">
            <p>完成任意题目获得积分后，这里会显示你的名次。</p>
            <RouterLink :to="practicePath" class="side-link">
              去题库
              <ArrowRight aria-hidden="true" />
            </RouterLink>
          </div>
        </section>

        <section class="side-panel" aria-labelledby="distribution-title">
          <header class="side-panel__head">
            <h2 id="distribution-title" class="side-panel__title">积分分布</h2>
            <span class="side-panel__meta">共 {{ stats?.participants ?? 0 }} 人</span>
          </header>

          <ul v-if="distributionRows.length" class="dist-list">
            <li v-for="band in distributionRows" :key="band.label" class="dist-row">
              <span class="dist-line">
                <span>{{ band.label }}</span>
                <span class="dist-count">{{ band.count }} 人 <small>{{ band.share }}%</small></span>
              </span>
              <span class="dist-track" aria-hidden="true">
                <i :style="{ width: band.width + '%' }" />
              </span>
            </li>
          </ul>
          <p v-else class="side-empty">暂无积分分布数据。</p>

          <p class="dist-note">按榜首积分动态分档，负分单独统计。</p>
        </section>
      </aside>
    </div>
  </template>
</template>

<style scoped>
.sync-note {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  white-space: nowrap;
}

.inline-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid color-mix(in srgb, var(--status-danger) 32%, var(--border));
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 7%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-caption);
}

.board-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 320px);
  align-items: start;
  gap: var(--space-4);
}

.card-loading {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.podium {
  padding: var(--space-4) var(--space-4) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.podium-list {
  display: flex;
  align-items: stretch;
  justify-content: center;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.podium-item {
  display: grid;
  align-content: start;
  gap: var(--space-1);
  flex: 1 1 0;
  min-width: 0;
  max-width: 220px;
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.podium-item[data-place='1'] {
  order: 2;
  border-color: color-mix(in srgb, var(--action-primary) 40%, var(--border));
  background: var(--action-subtle);
}

.podium-item[data-place='2'] {
  order: 1;
}

.podium-item[data-place='3'] {
  order: 3;
}

.podium-rank {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.podium-item[data-place='1'] .podium-rank {
  color: var(--action-subtle-fg);
  font-weight: 600;
}

.podium-name {
  overflow: hidden;
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.podium-name em {
  margin-left: var(--space-1);
  padding: 0 var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--action-primary);
  color: var(--action-primary-fg);
  font-size: var(--text-micro);
  font-style: normal;
  font-weight: 500;
}

.podium-score {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.podium-score small {
  color: var(--text-tertiary);
  font-size: var(--text-micro);
  font-weight: 400;
}

.podium-meta {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.rank-cell {
  color: var(--text-tertiary);
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}

.rank-top {
  color: var(--action-subtle-fg);
  font-weight: 600;
}

.user-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  max-width: 100%;
}

.user-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.me-chip {
  flex: none;
  padding: 0 var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
  font-size: var(--text-micro);
  line-height: var(--leading-normal);
}

.score-cell {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
  min-width: 0;
  font-variant-numeric: tabular-nums;
}

.score-track,
.me-track,
.dist-track {
  display: block;
  overflow: hidden;
  border-radius: var(--radius-pill);
  background: var(--bg-subtle);
}

.score-track {
  width: 72px;
  height: 4px;
  flex: none;
}

.score-track i,
.me-track i,
.dist-track i {
  display: block;
  height: 100%;
  border-radius: var(--radius-control);
  background: var(--action-primary);
  transition: width 180ms ease-out;
}

.solved-cell,
.last-active {
  color: var(--text-secondary);
  font-size: var(--text-meta);
  white-space: nowrap;
}

:deep(.ui-data-row.is-me td) {
  background: var(--action-subtle);
}

:deep(.ui-data-row.is-me td:first-child) {
  box-shadow: inset 2px 0 0 var(--action-primary);
}

.rule-note {
  margin: 0;
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.side-stack {
  display: grid;
  gap: var(--space-4);
  min-width: 0;
}

.side-panel {
  min-width: 0;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.side-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.side-panel__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.side-panel__meta {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.me-panel {
  display: grid;
  gap: var(--space-3);
}

.me-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.me-rank {
  color: var(--action-subtle-fg);
  font-size: var(--text-metric);
  font-weight: 600;
  line-height: var(--leading-tight);
  font-variant-numeric: tabular-nums;
}

.me-identity {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.me-identity b {
  overflow: hidden;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.me-identity span {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.me-note {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.me-track {
  height: 6px;
}

.side-empty {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.side-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  margin-top: var(--space-2);
  color: var(--action-subtle-fg);
  font-size: var(--text-caption);
  font-weight: 500;
}

.side-link :deep(svg) {
  width: 14px;
  height: 14px;
}

.dist-list {
  display: grid;
  gap: var(--space-3);
  margin: 0;
  padding: 0;
  list-style: none;
}

.dist-row {
  display: grid;
  gap: var(--space-1);
}

.dist-line {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.dist-count {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.dist-count small {
  margin-left: var(--space-1);
}

.dist-track {
  height: 6px;
}

.dist-note {
  margin-top: var(--space-4);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-normal);
}

.page-skeleton {
  display: grid;
  gap: var(--space-4);
}

.skeleton-kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
}

.skeleton-card {
  height: 104px;
  border-radius: var(--radius-card);
  background: var(--bg-subtle);
}

.skeleton-panels {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: var(--space-4);
}

.skeleton-panel {
  display: block;
  height: 360px;
  border-radius: var(--radius-card);
  background: var(--bg-subtle);
}

@media (max-width: 1100px) {
  .board-layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .side-stack {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
  }

  .skeleton-panels {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 720px) {
  .side-stack {
    grid-template-columns: minmax(0, 1fr);
  }

  .podium-list {
    gap: var(--space-1);
  }

  .podium-item {
    padding: var(--space-2);
  }

  .podium-name {
    font-size: var(--text-meta);
  }

  .podium-score {
    font-size: var(--text-body);
  }

  .score-track {
    display: none;
  }

  .skeleton-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .skeleton-kpis {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (prefers-reduced-motion: reduce) {
  .score-track i,
  .me-track i,
  .dist-track i {
    transition: none;
  }
}
</style>