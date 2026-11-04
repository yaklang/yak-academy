<script setup lang="ts">
// 竞赛概览看板：KPI + 得分曲线 + 榜单位置 + 分类进度 + 提交结果。
// 数据全部来自 /competitions/{id}/view 的 analytics 与排行榜接口，
// 不造数：没有数据时给出明确空态，而不是画一条假曲线。
import { computed } from 'vue'
import {
  CATEGORY_LABELS,
  DIFFICULTY_LABELS,
  type CompetitionHomeView,
  type TeamLeaderboardEntry,
} from '@/api'
import { formatTime } from '@/lib/format'
import CompetitionScoreChart from './CompetitionScoreChart.vue'

const props = defineProps<{
  view: CompetitionHomeView
  board: TeamLeaderboardEntry[]
  boardLoading: boolean
  boardError: string
  mode: 'team' | 'individual'
  username: string
  countdown: string
  status: 'scheduled' | 'running' | 'ended'
  elapsedPercent: number
}>()

const emit = defineEmits<{ (e: 'open-board'): void; (e: 'retry-board'): void }>()

const a = computed(() => props.view.analytics)
const solvedPercent = computed(() =>
  a.value.total_challenges > 0 ? Math.round((a.value.solved_count / a.value.total_challenges) * 100) : 0,
)
const scorePercent = computed(() =>
  a.value.total_score > 0 ? Math.round((a.value.score / a.value.total_score) * 100) : 0,
)

const titles = computed<Record<number, string>>(() => {
  const map: Record<number, string> = {}
  for (const c of props.view.challenges) map[c.id] = c.title
  return map
})

const categories = computed(() =>
  a.value.categories.map((c) => ({
    ...c,
    label: CATEGORY_LABELS[c.category] ?? c.category,
    percent: c.total > 0 ? Math.round((c.solved / c.total) * 100) : 0,
  })),
)

const difficulties = computed(() =>
  a.value.difficulties.map((d) => ({
    ...d,
    label: DIFFICULTY_LABELS[d.difficulty] ?? d.difficulty,
  })),
)

const myBoardIndex = computed(() =>
  props.board.findIndex((e) => e.members.includes(props.username)),
)
const myEntry = computed(() => (myBoardIndex.value >= 0 ? props.board[myBoardIndex.value] : null))
// 战队榜下名次按队伍聚合，KPI 里的得分仍是个人分；把本队总分放进副行，
// 避免"我的得分 100 / 战队榜第 1"看起来对不上。
const teamScore = computed(() =>
  props.mode === 'team' && myEntry.value ? myEntry.value.score : null,
)

const prevEntry = computed(() => {
  const me = myEntry.value
  if (!me || me.rank <= 1) return null
  return props.board.filter((e) => e.rank < me.rank).at(-1) ?? null
})
const gapToPrev = computed(() => {
  if (!myEntry.value || !prevEntry.value) return null
  return Math.max(0, prevEntry.value.score - myEntry.value.score)
})

const rankValue = computed(() => {
  if (props.boardLoading) return '—'
  if (myEntry.value) return `#${myEntry.value.rank}`
  return props.board.length > 0 ? '未上榜' : '—'
})
const rankSub = computed(() => {
  if (props.boardLoading) return '榜单加载中…'
  if (props.boardError) return '榜单暂不可用'
  if (!myEntry.value) {
    return props.board.length > 0 ? '完成首题即可上榜' : '等待首分解锁'
  }
  const unit = props.mode === 'team' ? '支队伍' : '名选手'
  const base = `${props.mode === 'team' ? '战队榜' : '个人榜'} · 共 ${props.board.length} ${unit}`
  if (myEntry.value.rank === 1) return `${base} · 暂列第一`
  if (gapToPrev.value !== null) return `${base} · 距上一名 ${gapToPrev.value} 分`
  return base
})

const maxBoardScore = computed(() => Math.max(1, ...props.board.map((e) => e.score)))
const rankRows = computed(() => {
  const rows = props.board.slice(0, 5)
  if (myBoardIndex.value >= 5 && myEntry.value) rows.push(myEntry.value)
  return rows.map((e) => ({
    key: `${e.rank}-${e.members.join('/')}`,
    rank: e.rank,
    // 个人榜的 team_name 是"所属队伍"，选手名在 members[0]；战队榜相反。
    name: props.mode === 'team' ? (e.team_name ?? e.members[0] ?? '-') : (e.members[0] ?? e.team_name ?? '-'),
    detail: props.mode === 'team' && e.members.length > 1 ? `${e.members.length} 名队员` : '',
    score: e.score,
    solved: e.solved_count,
    isMe: e === myEntry.value,
    percent: e.score > 0 ? Math.max(3, Math.round((e.score / maxBoardScore.value) * 100)) : 0,
  }))
})

const submission = computed(() => {
  const s = a.value.submission_stats
  const total = s.correct + s.incorrect + s.pending
  return {
    ...s,
    total,
    rate: total > 0 ? Math.round((s.correct / total) * 100) : 0,
    segments: [
      { key: 'correct', label: '正确', count: s.correct, cls: 'correct' },
      { key: 'incorrect', label: '错误', count: s.incorrect, cls: 'incorrect' },
      { key: 'pending', label: '判定中', count: s.pending, cls: 'pending' },
    ].filter((x) => x.count > 0),
  }
})

const recentSubmissions = computed(() => props.view.my_submissions.slice(0, 3))
function statusLabel(status: string) {
  return status === 'correct' ? '正确' : status === 'pending' ? '判定中' : '错误'
}
</script>

<template>
  <section class="overview">
    <div class="kpi-grid">
      <article class="kpi">
        <span class="kpi-label">我的得分</span>
        <b class="kpi-value mono">{{ a.score }}</b>
        <span class="kpi-sub">
          <template v-if="teamScore !== null">本队 {{ teamScore }} 分 · </template>满分 {{ a.total_score }} 分 · 得分率 {{ scorePercent }}%
        </span>
        <span class="kpi-bar" aria-hidden="true"><i :style="{ width: scorePercent + '%' }" /></span>
      </article>

      <article class="kpi">
        <span class="kpi-label">已通关</span>
        <b class="kpi-value mono">{{ a.solved_count }}<span class="kpi-total"> / {{ a.total_challenges }}</span></b>
        <span class="kpi-sub">完成率 {{ solvedPercent }}%</span>
        <span class="kpi-bar" aria-hidden="true"><i :style="{ width: solvedPercent + '%' }" /></span>
      </article>

      <article class="kpi">
        <span class="kpi-label">当前排名</span>
        <b class="kpi-value mono">{{ rankValue }}</b>
        <span class="kpi-sub">{{ rankSub }}</span>
      </article>

      <article class="kpi">
        <span class="kpi-label">{{ status === 'running' ? '剩余时间' : '比赛状态' }}</span>
        <b class="kpi-value mono">{{ status === 'ended' ? '已结束' : countdown }}</b>
        <span class="kpi-sub">{{ status === 'ended' ? '比赛已结束，可提交 Writeup' : `赛程已进行 ${elapsedPercent}%` }}</span>
        <span class="kpi-bar" aria-hidden="true"><i :style="{ width: elapsedPercent + '%' }" /></span>
      </article>
    </div>

    <div class="overview-grid">
      <UiCard class="chart-card">
        <template #header>
          <div class="card-head">
            <span>得分曲线</span>
            <span class="card-meta mono">{{ a.score }} 分</span>
          </div>
        </template>
        <CompetitionScoreChart
          :points="a.score_timeline"
          :start-at="view.competition.start_time"
          :titles="titles"
        />
      </UiCard>

      <UiCard class="chart-card">
        <template #header>
          <div class="card-head">
            <span>排行榜位置</span>
            <button type="button" class="text-action" @click="emit('open-board')">完整榜单</button>
          </div>
        </template>

        <div v-if="boardLoading" class="board-skeleton" aria-busy="true">
          <span v-for="n in 4" :key="n" class="skeleton-row" />
        </div>
        <div v-else-if="boardError" class="board-error" role="alert">
          <p>{{ boardError }}</p>
          <UiButton size="small" @click="emit('retry-board')">重试</UiButton>
        </div>
        <div v-else-if="rankRows.length === 0" class="empty-note">
          还没有队伍得分。提交第一道题后，榜单会实时更新。
        </div>
        <div v-else class="rank-list">
          <div v-for="row in rankRows" :key="row.key" class="rank-row" :class="{ me: row.isMe }">
            <span class="rank-no mono">{{ row.rank }}</span>
            <div class="rank-main">
              <div class="rank-line">
                <span class="rank-name">
                  {{ row.name }}<em v-if="row.detail"> · {{ row.detail }}</em>
                  <span v-if="row.isMe" class="me-tag">我</span>
                </span>
                <span class="rank-score mono">{{ row.score }}</span>
              </div>
              <span class="track" aria-hidden="true"><i :style="{ width: row.percent + '%' }" /></span>
            </div>
          </div>
        </div>
      </UiCard>

      <UiCard class="chart-card">
        <template #header>
          <div class="card-head">
            <span>分类进度</span>
            <span class="card-meta mono">{{ a.solved_count }} / {{ a.total_challenges }} 题</span>
          </div>
        </template>

        <div v-if="categories.length === 0" class="empty-note">
          本竞赛还没有绑定题目。题目公布后，这里会按分类显示通关进度。
        </div>
        <template v-else>
          <div class="cat-list">
            <div v-for="c in categories" :key="c.category" class="cat-row">
              <div class="cat-line">
                <span>{{ c.label }}</span>
                <span class="cat-meta mono">{{ c.solved }}/{{ c.total }} · {{ c.score_earned }}/{{ c.score_total }} 分</span>
              </div>
              <span class="track" aria-hidden="true"><i :style="{ width: c.percent + '%' }" /></span>
            </div>
          </div>
          <div v-if="difficulties.length" class="diff-row">
            <span v-for="d in difficulties" :key="d.difficulty" class="diff-chip">
              {{ d.label }}
              <b class="mono">{{ d.solved }}/{{ d.total }}</b>
            </span>
          </div>
        </template>
      </UiCard>

      <UiCard class="chart-card">
        <template #header>
          <div class="card-head">
            <span>提交结果</span>
            <span class="card-meta mono">{{ submission.total }} 次</span>
          </div>
        </template>

        <div v-if="submission.total === 0" class="empty-note">
          还没有提交记录。提交 flag 后，这里会统计正确 / 错误 / 判定中的分布。
        </div>
        <template v-else>
          <div class="sub-head">
            <b class="mono">{{ submission.rate }}%</b>
            <span class="kpi-sub">正确率</span>
          </div>
          <div
            class="stack"
            role="img"
            :aria-label="`提交结果：正确 ${submission.correct}，错误 ${submission.incorrect}，判定中 ${submission.pending}`"
          >
            <span
              v-for="s in submission.segments"
              :key="s.key"
              :class="s.cls"
              :style="{ width: (s.count / submission.total) * 100 + '%' }"
            />
          </div>
          <ul class="legend">
            <li v-for="s in submission.segments" :key="s.key">
              <i :class="s.cls" aria-hidden="true" />
              <span>{{ s.label }}</span>
              <b class="mono">{{ s.count }}</b>
            </li>
          </ul>

          <div v-if="recentSubmissions.length" class="recent">
            <div class="recent-head">
              <span>最近提交</span>
              <span class="card-meta">最近 {{ recentSubmissions.length }} 条</span>
            </div>
            <ul>
              <li v-for="s in recentSubmissions" :key="s.id">
                <i class="dot" :class="s.status" aria-hidden="true" />
                <span>{{ statusLabel(s.status) }}</span>
                <time class="mono">{{ formatTime(s.created_at) }}</time>
              </li>
            </ul>
          </div>
        </template>
      </UiCard>
    </div>
  </section>
</template>

<style scoped>
.overview { display: grid; gap: var(--space-4); min-width: 0; }

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
}
.kpi {
  display: grid;
  gap: 6px;
  align-content: start;
  min-width: 0;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.kpi-label { color: var(--muted-foreground); font-size: 12px; }
.kpi-value { color: var(--foreground); font-size: 26px; font-weight: 650; line-height: 1.2; }
.kpi-total { color: var(--muted-foreground); font-size: 16px; font-weight: 500; }
.kpi-sub { color: var(--muted-foreground); font-size: 12px; line-height: 1.5; }
.kpi-bar,
.track {
  display: block;
  height: 5px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--muted);
}
.kpi-bar { margin-top: 2px; }
.kpi-bar i,
.track i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--primary);
  transition: width 180ms ease-out;
}

.overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.65fr) minmax(0, 1fr);
  gap: var(--space-3);
}
.chart-card { min-width: 0; }
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: 14px;
  font-weight: 550;
}
.card-meta { color: var(--muted-foreground); font-size: 12px; font-weight: 400; }
.text-action {
  padding: 2px 0;
  border: 0;
  background: none;
  color: var(--accent-foreground);
  font-size: 12px;
  cursor: pointer;
}
.text-action:hover { color: var(--primary); text-decoration: underline; }
.text-action:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; border-radius: 2px; }

.rank-list { display: grid; }
.rank-row {
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr);
  gap: var(--space-2);
  align-items: center;
  padding: 9px 0;
  border-top: 1px solid var(--border);
}
.rank-row:first-child { border-top: 0; padding-top: 2px; }
.rank-row.me .rank-main { padding: 4px 8px; margin: -4px -8px; border-radius: 8px; background: color-mix(in srgb, var(--primary) 9%, transparent); }
.rank-no { color: var(--muted-foreground); font-size: 12px; text-align: right; }
.rank-main { display: grid; gap: 5px; min-width: 0; }
.rank-line { display: flex; align-items: baseline; justify-content: space-between; gap: var(--space-2); min-width: 0; }
.rank-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.rank-name em { color: var(--muted-foreground); font-size: 11px; font-style: normal; }
.rank-score { font-size: 12px; color: var(--muted-foreground); }
.me-tag {
  display: inline-block;
  margin-left: 6px;
  padding: 0 5px;
  border: 1px solid var(--primary);
  border-radius: 999px;
  color: var(--accent-foreground);
  font-size: 10px;
  line-height: 15px;
}
.track i { background: var(--primary); }

.board-skeleton { display: grid; gap: 14px; padding: 4px 0; }
.skeleton-row { height: 14px; border-radius: 4px; background: var(--muted); opacity: 0.7; }
.skeleton-row:nth-child(2) { width: 82%; }
.skeleton-row:nth-child(3) { width: 92%; }
.skeleton-row:nth-child(4) { width: 70%; }
.board-error { display: grid; justify-items: start; gap: var(--space-3); padding: 16px 0; color: var(--destructive); font-size: 13px; }

.cat-list { display: grid; }
.cat-row { display: grid; gap: 6px; padding: 9px 0; border-top: 1px solid var(--border); }
.cat-row:first-child { border-top: 0; padding-top: 0; }
.cat-line { display: flex; align-items: baseline; justify-content: space-between; gap: var(--space-3); font-size: 13px; }
.cat-meta { color: var(--muted-foreground); font-size: 11px; white-space: nowrap; }
.diff-row { display: flex; flex-wrap: wrap; gap: 8px; margin-top: var(--space-3); padding-top: var(--space-3); border-top: 1px solid var(--border); }
.diff-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--muted-foreground);
  font-size: 11px;
}
.diff-chip b { color: var(--foreground); font-weight: 550; }

.sub-head { display: flex; align-items: baseline; gap: 6px; margin-bottom: var(--space-3); }
.sub-head b { font-size: 26px; font-weight: 650; }
.stack { display: flex; height: 10px; overflow: hidden; border-radius: 999px; background: var(--muted); }
.stack span { height: 100%; }
.stack .correct, .legend i.correct { background: var(--success); }
.stack .incorrect, .legend i.incorrect { background: var(--destructive); }
.stack .pending, .legend i.pending { background: var(--warning); }
.legend { display: flex; flex-wrap: wrap; gap: 8px 16px; margin: var(--space-4) 0 0; padding: 0; list-style: none; }
.legend li { display: inline-flex; align-items: center; gap: 6px; color: var(--muted-foreground); font-size: 12px; }
.legend i { width: 7px; height: 7px; border-radius: 50%; }
.legend b { color: var(--foreground); font-weight: 550; }

.recent { display: grid; gap: 8px; margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--border); }
.recent-head { display: flex; justify-content: space-between; gap: 12px; color: var(--muted-foreground); font-size: 12px; }
.recent ul { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.recent li { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.recent .dot { width: 6px; height: 6px; border-radius: 50%; }
.recent .dot.correct { background: var(--success); }
.recent .dot.incorrect { background: var(--destructive); }
.recent .dot.pending { background: var(--warning); }
.recent time { margin-left: auto; color: var(--muted-foreground); font-size: 11px; }

.empty-note {
  display: grid;
  place-items: center;
  min-height: 120px;
  padding: var(--space-4);
  color: var(--muted-foreground);
  font-size: 13px;
  line-height: 1.6;
  text-align: center;
}

@media (max-width: 1180px) {
  .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .overview-grid { grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 560px) {
  .kpi-grid { grid-template-columns: minmax(0, 1fr); }
  .kpi-value { font-size: 23px; }
}
</style>
