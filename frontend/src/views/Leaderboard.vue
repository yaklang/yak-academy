<script setup lang="ts">
// 排行榜（事件派生只读投影）：全站概览 + 前三名 + 我的名次 + 积分分布 + 分页名次表。
// 数据只有 /leaderboard 一个来源，刷新、分页与错误重试都重新拉取同一份视图；
// 页面不使用假数据，未上榜 / 空榜 / 加载 / 错误分别给出可操作的终态。
import { computed, onMounted, ref } from 'vue'
import { ArrowRight, Inbox, RefreshCw, Trophy } from '@lucide/vue'
import { progressApi, type LeaderboardEntry, type LeaderboardView } from '../api'
import { formatDateTime } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'

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
  <header class="page-head">
    <div>
      <h2 class="page-title">排行<span class="accent">榜</span></h2>
      <p class="page-sub">
        按总分实时派生，同分按通关数排序；积分包含通关得分与提示消耗。
      </p>
    </div>
    <div class="page-actions">
      <span class="sync-note">
        {{ syncLabel }}
      </span>
      <UiButton
        size="small"
        :loading="refreshing"
        :disabled="loading"
        aria-label="刷新排行榜"
        @click="refresh"
      >
        <RefreshCw :size="14" aria-hidden="true" />
        刷新
      </UiButton>
    </div>
  </header>

  <div v-if="error && view" class="inline-error" role="alert">
    <span>{{ error }}</span>
    <UiButton size="small" @click="refresh">重试</UiButton>
  </div>

  <!-- 首屏加载：骨架结构与最终布局一致（概览条 + 主榜 + 侧栏）。 -->
  <section v-if="loading && !view" class="page-skeleton" aria-busy="true" aria-label="排行榜加载中">
    <div class="kpi-grid">
      <div v-for="n in 4" :key="n" class="kpi skeleton-card">
        <span class="skeleton-line short" />
        <span class="skeleton-line value" />
        <span class="skeleton-line" />
      </div>
    </div>
    <div class="board-layout">
      <UiCard class="board-card">
        <div class="table-skeleton">
          <span v-for="n in 6" :key="n" class="skeleton-row" />
        </div>
      </UiCard>
      <div class="side-stack">
        <UiCard class="side-card"><div class="table-skeleton"><span class="skeleton-row" /><span class="skeleton-row" /></div></UiCard>
        <UiCard class="side-card"><div class="table-skeleton"><span v-for="n in 5" :key="n" class="skeleton-row" /></div></UiCard>
      </div>
    </div>
  </section>

  <!-- 首屏失败：错误态独立于空态，明确原因并提供重试。 -->
  <UiCard v-else-if="!view" class="state-card">
    <div class="page-state" role="alert">
      <Trophy class="state-icon" aria-hidden="true" />
      <h3>排行榜加载失败</h3>
      <p>{{ error || '暂时无法获取排行榜数据，请稍后重试。' }}</p>
      <UiButton @click="load(1)">重新加载</UiButton>
    </div>
  </UiCard>

  <template v-else>
    <section class="kpi-grid" aria-label="全站概览">
      <article class="kpi">
        <span class="kpi-label">上榜学员</span>
        <b class="kpi-value mono" data-testid="lb-participants">{{ stats?.participants ?? 0 }}</b>
        <span class="kpi-sub">拥有积分记录的学员</span>
      </article>
      <article class="kpi">
        <span class="kpi-label">累计积分</span>
        <b class="kpi-value mono" data-testid="lb-total-score">{{ stats?.total_score ?? 0 }}</b>
        <span class="kpi-sub">含提示扣分后的净积分</span>
      </article>
      <article class="kpi">
        <span class="kpi-label">累计通关</span>
        <b class="kpi-value mono" data-testid="lb-total-solves">{{ stats?.total_solves ?? 0 }}</b>
        <span class="kpi-sub">全站通关题次</span>
      </article>
      <article class="kpi">
        <span class="kpi-label">平均分</span>
        <b class="kpi-value mono" data-testid="lb-average-score">{{ stats?.average_score ?? 0 }}</b>
        <span class="kpi-sub">榜首 {{ stats?.top_score ?? 0 }} 分</span>
      </article>
    </section>

    <div class="board-layout" :aria-busy="loading">
      <UiCard class="board-card">
        <template #header>
          <div class="card-head">
            <div class="card-heading">
              <span class="card-title">总榜</span>
              <span class="card-meta">共 {{ view.total }} 人</span>
            </div>
            <span v-if="loading" class="card-loading" role="status">同步中…</span>
          </div>
        </template>

        <div v-if="view.total === 0" class="page-state" data-testid="leaderboard-empty" role="status">
          <Inbox class="state-icon" aria-hidden="true" />
          <h3>还没有学员上榜</h3>
          <p>完成任意一道题获得积分后，你的名字会出现在这里。</p>
          <RouterLink :to="practicePath" class="state-link">
            去题库
            <ArrowRight :size="14" aria-hidden="true" />
          </RouterLink>
        </div>

        <template v-else>
          <!-- 前三名：视觉顺序 2-1-3，DOM 顺序仍按名次，便于读屏。 -->
          <section class="podium" aria-label="全场前三名">
            <ol class="podium-list">
              <li
                v-for="(entry, index) in view.top"
                :key="entry.username"
                class="podium-item"
                :class="`podium-place-${index + 1}`"
              >
                <span class="podium-rank mono">#{{ entry.rank }}</span>
                <span class="podium-name">
                  {{ entry.username }}
                  <em v-if="isMe(entry)">我</em>
                </span>
                <span class="podium-score mono">{{ entry.score }}<small> 分</small></span>
                <span class="podium-meta">{{ entry.solved_count }} 题通关</span>
              </li>
            </ol>
          </section>

          <UiTable
            :data="view.items"
            row-key="username"
            :row-class-name="rowClassName"
            aria-label="排行榜名次"
          >
            <UiTableColumn prop="rank" label="名次" width="76">
              <template #default="{ row }">
                <span class="rank-cell mono" :class="{ 'rank-top': row.rank <= 3 }">
                  {{ row.rank }}
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="学员">
              <template #default="{ row }">
                <span class="user-cell">
                  <span class="user-name">{{ row.username }}</span>
                  <span v-if="isMe(row)" class="me-chip">我</span>
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="总分" width="190" align="right">
              <template #default="{ row }">
                <span class="score-cell">
                  <span class="mono">{{ row.score }}</span>
                  <span class="score-track" aria-hidden="true">
                    <i :style="{ width: scoreWidth(row.score) + '%' }" />
                  </span>
                </span>
              </template>
            </UiTableColumn>
            <UiTableColumn prop="solved_count" label="通关" width="88" align="right">
              <template #default="{ row }">
                <span class="mono solved-cell">{{ row.solved_count }} 题</span>
              </template>
            </UiTableColumn>
            <UiTableColumn label="最近活跃" width="176">
              <template #default="{ row }">
                <span class="last-active">{{ formatDateTime(row.last_active_at) }}</span>
              </template>
            </UiTableColumn>
          </UiTable>

          <div class="board-foot">
            <p class="rule-note">积分相同时按通关数排序，再按达成时间先后排序。</p>
            <UiPagination
              v-if="view.total > PAGE_SIZE"
              layout="prev, pager, next"
              :total="view.total"
              :page-size="PAGE_SIZE"
              :current-page="page"
              @current-change="changePage"
            />
          </div>
        </template>
      </UiCard>

      <aside class="side-stack">
        <UiCard class="side-card" data-testid="my-rank">
          <template #header>
            <div class="card-head">
              <span class="card-title">我的排名</span>
              <span class="card-meta">{{ me ? '已上榜' : '未上榜' }}</span>
            </div>
          </template>

          <div v-if="me" class="me-panel">
            <div class="me-head">
              <span class="me-rank mono">#{{ me.rank }}</span>
              <span class="me-identity">
                <b>{{ me.username }}</b>
                <span>{{ me.solved_count }} 题 · {{ me.score }} 分</span>
              </span>
            </div>
            <span class="me-note">
              {{ gapToTop === 0 ? '当前榜首' : `距榜首 ${gapToTop} 分` }}
            </span>
            <span class="me-track" aria-hidden="true">
              <i :style="{ width: scoreWidth(me.score) + '%' }" />
            </span>
          </div>

          <div v-else class="side-empty" role="status">
            <p>完成任意题目获得积分后，这里会显示你的名次。</p>
            <RouterLink :to="practicePath" class="side-link">
              去题库
              <ArrowRight :size="14" aria-hidden="true" />
            </RouterLink>
          </div>
        </UiCard>

        <UiCard class="side-card">
          <template #header>
            <div class="card-head">
              <span class="card-title">积分分布</span>
              <span class="card-meta">共 {{ stats?.participants ?? 0 }} 人</span>
            </div>
          </template>

          <ul v-if="distributionRows.length" class="dist-list">
            <li v-for="band in distributionRows" :key="band.label" class="dist-row">
              <span class="dist-line">
                <span>{{ band.label }}</span>
                <span class="dist-count mono">{{ band.count }} 人 <small>{{ band.share }}%</small></span>
              </span>
              <span class="dist-track" aria-hidden="true">
                <i :style="{ width: band.width + '%' }" />
              </span>
            </li>
          </ul>
          <p v-else class="side-empty">暂无积分分布数据。</p>

          <p class="dist-note">按榜首积分动态分档，负分单独统计。</p>
        </UiCard>
      </aside>
    </div>
  </template>
</template>

<style scoped>
.sync-note {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  white-space: nowrap;
}
.inline-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid color-mix(in srgb, var(--destructive) 35%, var(--border));
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--destructive) 7%, var(--card));
  color: var(--destructive);
  font-size: var(--text-sm);
}

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.kpi {
  display: grid;
  align-content: start;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.kpi-label { color: var(--muted-foreground); font-size: var(--text-xs); }
.kpi-value {
  color: var(--foreground);
  font-size: var(--text-2xl);
  font-weight: 650;
  line-height: var(--leading-tight);
}
.kpi-sub { color: var(--muted-foreground); font-size: var(--text-xs); line-height: var(--leading-normal); }

.board-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 320px);
  align-items: start;
  gap: var(--space-4);
}
.board-card,
.side-card { min-width: 0; }
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.card-heading { display: flex; align-items: baseline; gap: var(--space-2); min-width: 0; }
.card-title { font-size: var(--text-md); font-weight: 600; }
.card-meta { color: var(--muted-foreground); font-size: var(--text-xs); }
.card-loading { color: var(--muted-foreground); font-size: var(--text-xs); }

.podium { padding-bottom: var(--space-5); }
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
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
}
.podium-place-1 { order: 2; }
.podium-place-2 { order: 1; }
.podium-place-3 { order: 3; }
.podium-place-1 {
  border-color: color-mix(in srgb, var(--primary) 50%, var(--border));
  background: color-mix(in srgb, var(--primary) 6%, var(--panel));
}
.podium-rank { color: var(--muted-foreground); font-size: var(--text-xs); }
.podium-place-1 .podium-rank { color: var(--accent-foreground); font-weight: 600; }
.podium-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--foreground);
  font-size: var(--text-md);
  font-weight: 600;
}
.podium-name em {
  margin-left: var(--space-1);
  padding: 0 var(--space-1);
  border: 1px solid var(--primary);
  border-radius: 999px;
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-style: normal;
  font-weight: 500;
}
.podium-score { color: var(--foreground); font-size: var(--text-lg); font-weight: 650; }
.podium-score small { color: var(--muted-foreground); font-size: var(--text-xs); font-weight: 400; }
.podium-meta { color: var(--muted-foreground); font-size: var(--text-xs); }

.rank-cell { color: var(--muted-foreground); font-weight: 500; }
.rank-top { color: var(--accent-foreground); font-weight: 700; }
.user-cell { display: inline-flex; align-items: center; gap: var(--space-2); min-width: 0; max-width: 100%; }
.user-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
.me-chip {
  flex: none;
  padding: 0 var(--space-1);
  border: 1px solid var(--primary);
  border-radius: 999px;
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-normal);
}
.score-cell {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
  min-width: 0;
}
.score-track,
.me-track,
.dist-track {
  display: block;
  overflow: hidden;
  border-radius: 999px;
  background: var(--muted);
}
.score-track { width: 72px; height: 4px; flex: none; }
.score-track i,
.me-track i,
.dist-track i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--primary);
  transition: width 180ms ease-out;
}
.solved-cell { color: var(--muted-foreground); white-space: nowrap; }
.last-active { color: var(--muted-foreground); font-size: var(--text-xs); white-space: nowrap; }

/* 当前用户整行高亮：只在派生数据命中本人时出现，不改变其他行权重。 */
:deep(.ui-data-row.is-me td) {
  background: color-mix(in srgb, var(--primary) 8%, transparent);
}
:deep(.ui-data-row.is-me td:first-child) {
  box-shadow: inset 2px 0 0 var(--primary);
}

.board-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  margin-top: var(--space-4);
}
.rule-note { margin: 0; color: var(--muted-foreground); font-size: var(--text-xs); }
.board-foot :deep(.ui-pagination) { margin-top: 0; }

.side-stack { display: grid; gap: var(--space-4); min-width: 0; }
.me-panel { display: grid; gap: var(--space-3); }
.me-head { display: flex; align-items: center; gap: var(--space-3); min-width: 0; }
.me-rank {
  color: var(--accent-foreground);
  font-size: var(--text-2xl);
  font-weight: 700;
  line-height: var(--leading-tight);
}
.me-identity { display: grid; gap: var(--space-1); min-width: 0; }
.me-identity b {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--foreground);
  font-size: var(--text-base);
  font-weight: 600;
}
.me-identity span { color: var(--muted-foreground); font-size: var(--text-xs); }
.me-note { color: var(--muted-foreground); font-size: var(--text-xs); }
.me-track { height: 6px; }

.side-empty {
  display: grid;
  justify-items: start;
  gap: var(--space-3);
  margin: 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.side-link,
.state-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 500;
  text-decoration: none;
}
.side-link:hover,
.state-link:hover { text-decoration: underline; }
.side-link:focus-visible,
.state-link:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
  border-radius: var(--radius-control);
}

.dist-list { display: grid; gap: var(--space-3); margin: 0; padding: 0; list-style: none; }
.dist-row { display: grid; gap: var(--space-1); }
.dist-line {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2);
  color: var(--foreground);
  font-size: var(--text-sm);
}
.dist-count { color: var(--muted-foreground); font-size: var(--text-xs); }
.dist-count small { margin-left: var(--space-1); }
.dist-track { height: 6px; }
.dist-note {
  margin: var(--space-4) 0 0;
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-normal);
}

.page-state {
  display: grid;
  justify-items: center;
  gap: var(--space-3);
  padding: var(--space-10) var(--space-5);
  text-align: center;
}
.state-card { min-width: 0; }
.state-icon { width: 32px; height: 32px; color: var(--muted-foreground); }
.page-state h3 { margin: 0; color: var(--foreground); font-size: var(--text-md); font-weight: 600; }
.page-state p {
  max-width: 48ch;
  margin: 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}

.page-skeleton { display: grid; gap: var(--space-4); }
.skeleton-card { align-content: start; }
.skeleton-line,
.skeleton-row {
  display: block;
  border-radius: var(--radius-control);
  background: var(--muted);
  opacity: 0.65;
}
.skeleton-line { height: 12px; width: 100%; }
.skeleton-line.short { width: 42%; }
.skeleton-line.value { height: 24px; width: 60%; }
.table-skeleton { display: grid; gap: var(--space-3); padding: var(--space-2) 0; }
.skeleton-row { height: 40px; }

@media (max-width: 1100px) {
  .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .board-layout { grid-template-columns: minmax(0, 1fr); }
  .side-stack { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
}
@media (max-width: 720px) {
  .side-stack { grid-template-columns: minmax(0, 1fr); }
  .podium-list { gap: var(--space-1); }
  .podium-item { padding: var(--space-3) var(--space-2); }
  .podium-name { font-size: var(--text-sm); }
  .podium-score { font-size: var(--text-md); }
  .score-track { display: none; }
  /* 移动端只保留名次/学员/总分/通关四列；最近活跃在窄屏隐藏，
     避免表格内部横向滚动把关键分数挤出首屏。 */
  :deep(.ui-data-table table) { table-layout: fixed; }
  :deep(.ui-data-table th),
  :deep(.ui-data-table td) { min-width: 0 !important; padding-inline: var(--space-2); }
  :deep(.ui-data-table th:nth-child(3)),
  :deep(.ui-data-table td:nth-child(3)) { width: 96px !important; }
  :deep(.ui-data-table th:nth-child(4)),
  :deep(.ui-data-table td:nth-child(4)) { width: 64px !important; }
  :deep(.ui-data-table th:nth-child(5)),
  :deep(.ui-data-table td:nth-child(5)) { display: none; }
}
@media (max-width: 560px) {
  .kpi-grid { grid-template-columns: minmax(0, 1fr); }
  .kpi-value { font-size: var(--text-xl); }
  .page-state { padding: var(--space-8) var(--space-4); }
  :deep(.ui-data-table th:nth-child(1)),
  :deep(.ui-data-table td:nth-child(1)) { width: 40px !important; }
  :deep(.ui-data-table th:nth-child(3)),
  :deep(.ui-data-table td:nth-child(3)) { width: 64px !important; }
  :deep(.ui-data-table th:nth-child(4)),
  :deep(.ui-data-table td:nth-child(4)) { width: 52px !important; }
}
@media (prefers-reduced-motion: reduce) {
  .score-track i,
  .me-track i,
  .dist-track i { transition: none; }
}
</style>
