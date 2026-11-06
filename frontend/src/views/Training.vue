<script setup lang="ts">
// 题库：CTFd challenges 风格的高密度列表——单行紧凑题目行（难度色条 +
// 标题 + 内联标签 + 通关态），默认平铺不分组，分类用 chip 过滤条点选
// （带每类计数），「按分类分组」开关按需恢复分组视图；是否内置走服务端
// 过滤（bank + builtin 查询参数），标题/分类/配套教程搜索框客户端过滤
//（与分类 chip 叠加）。页头统计：通关进度 + 容器/附件/教程题数 +
// 难度分布条（纯 CSS，颜色仅装饰、语义由文字标签承载）。
// 空态给引导卡片：CTF/ACM tab 无题时说明来源，管理员直接给上传入口。
// 普通端与管理端共用；一次拉全量（page_size=100）。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Inbox, Search, SearchX } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { BANK_LABELS, CATEGORY_LABELS, DIFFICULTY_LABELS, trainingApi, type ChallengeBank, type TrainingItem } from '../api'

const route = useRoute()
const auth = useAuthStore()
const SearchIcon = Search
const banks: ChallengeBank[] = ['tutorial', 'ctf', 'acm']
const activeBank = ref<ChallengeBank>(banks.includes(route.query.bank as ChallengeBank) ? route.query.bank as ChallengeBank : 'tutorial')
// 空态标题（说明与动作按题库状态在模板中区分）。
const emptyHint: Record<ChallengeBank, string> = {
  tutorial: '暂无教程题目',
  ctf: '暂无 CTF 题目',
  acm: '暂无 ACM 题目',
}
const items = ref<TrainingItem[]>([])
const total = ref(0)
const loading = ref(true)
const loadError = ref('')

// ===== 过滤器 =====
// 分类 chip 客户端筛选（选项与计数由当前 tab 数据派生）；来源（是否内置）
// 走服务端查询参数。分组开关只影响展示形态，默认平铺。搜索框客户端
// 关键词过滤（标题/分类/配套教程，大小写不敏感），与分类 chip 叠加。
const categoryFilter = ref('')
const builtinFilter = ref<'' | 'yes' | 'no'>('')
const search = ref('')
const grouped = ref(false)

function categoryLabel(c: string) {
  return CATEGORY_LABELS[c] ?? c
}

const categoryOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const it of items.value) counts.set(it.category, (counts.get(it.category) ?? 0) + 1)
  return [...counts.entries()].map(([category, count]) => ({ category, count })).sort((a, b) => a.category.localeCompare(b.category))
})

const visibleItems = computed(() => {
  let list = items.value
  if (categoryFilter.value) list = list.filter((it) => it.category === categoryFilter.value)
  const q = search.value.trim().toLowerCase()
  if (q) {
    list = list.filter((it) =>
      it.title.toLowerCase().includes(q)
      || it.category.toLowerCase().includes(q)
      || categoryLabel(it.category).toLowerCase().includes(q)
      || (it.tutorial_title ?? '').toLowerCase().includes(q),
    )
  }
  return list
})

// 分组视图（开关打开时）：按 category 聚合，保持后端顺序。
const groups = computed(() => {
  const byCat = new Map<string, TrainingItem[]>()
  for (const it of visibleItems.value) {
    const key = it.category || 'uncategorized'
    if (!byCat.has(key)) byCat.set(key, [])
    byCat.get(key)!.push(it)
  }
  return [...byCat.entries()].map(([category, list]) => ({ category, items: list }))
})

// ===== 页头统计（全部由已拉取的 items 派生，不发额外请求） =====
const solvedCount = computed(() => items.value.filter((it) => it.solved).length)
const solvedPercent = computed(() => (total.value > 0 ? Math.round((solvedCount.value / total.value) * 100) : 0))
const envCount = computed(() => items.value.filter((it) => it.has_env).length)
const fileCount = computed(() => total.value - envCount.value)
const tutorialCount = computed(() => items.value.filter((it) => it.tutorial_title).length)
const difficultyDist = computed(() => {
  const counts = { apprentice: 0, practitioner: 0, expert: 0 } as Record<TrainingItem['difficulty'], number>
  for (const it of items.value) counts[it.difficulty] += 1
  const sum = total.value || 1
  return (['apprentice', 'practitioner', 'expert'] as const).map((d) => ({
    key: d,
    label: DIFFICULTY_LABELS[d],
    count: counts[d],
    percent: Math.round((counts[d] / sum) * 100),
  }))
})

const base = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))
const isAdmin = computed(() => auth.user?.global_role === 'admin')
const emptyDescription = computed(() => {
  if (isAdmin.value) {
    return activeBank.value === 'tutorial'
      ? '在「题目管理」一键导入内置训练（vulinbox）后即可查看。'
      : '在「题目管理」上传题目包后，会自动出现在这里。'
  }
  return activeBank.value === 'tutorial'
    ? '内置题集尚未导入，请联系管理员在管理端一键导入内置训练（vulinbox）。'
    : '题目包由管理员上传后会自动出现在这里。'
})

let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  loading.value = true
  loadError.value = ''
  // 切 tab / 来源时先清空旧行：否则新条件的加载期会继续展示上一个 tab 的题目。
  items.value = []
  total.value = 0
  categoryFilter.value = '' // 切 tab/来源后分类集合变化，重置已失效的选择
  try {
    const filter: { bank: ChallengeBank; builtin?: boolean } = { bank: activeBank.value }
    if (builtinFilter.value === 'yes') filter.builtin = true
    if (builtinFilter.value === 'no') filter.builtin = false
    const resp = await trainingApi.list(1, 100, filter)
    if (seq !== loadSeq) return
    items.value = resp.items
    total.value = resp.total
    // 超过 100 条时分页拉余下页。
    if (resp.total > resp.items.length) await loadRemaining(filter, resp.items.length, seq)
  } catch (e) {
    if (seq !== loadSeq) return
    loadError.value = e instanceof Error ? e.message : '题库加载失败，请稍后重试'
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function loadRemaining(filter: { bank: ChallengeBank; builtin?: boolean }, loaded: number, seq: number) {
  let offset = loaded
  while (offset < total.value) {
    const page = Math.floor(offset / 100) + 1
    const resp = await trainingApi.list(page, 100, filter)
    if (seq !== loadSeq) return
    items.value.push(...resp.items)
    offset += resp.items.length
    if (resp.items.length === 0) break
  }
}

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

// 过滤结果为空时保留筛选栏，用户可以直接清空条件；题库本身为空时筛选栏不渲染。
function clearFilters() {
  categoryFilter.value = ''
  search.value = ''
  builtinFilter.value = ''
}

watch([activeBank, builtinFilter], load)
watch(() => route.query.bank, value => {
  if (banks.includes(value as ChallengeBank)) activeBank.value = value as ChallengeBank
})
onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">题<span class="accent">库</span></h2>
      <p class="page-sub">边学边练：题目可启动容器实战或下载附件离线作答，通关即计分。</p>
    </div>
    <div v-if="total" class="solve-progress">
      <span class="label">已通关 {{ solvedCount }}/{{ total }}</span>
      <UiProgress :percentage="solvedPercent" :stroke-width="8" :show-text="false" class="bar" />
      <span class="label">{{ solvedPercent }}%</span>
    </div>
  </header>

  <!-- 页头统计：有题时展示容器/附件/配套教程计数 + 难度分布；空题库不渲染零值统计。 -->
  <div v-if="!loading && total > 0" class="stats-strip" role="status">
    <span class="stat">容器题 <b class="mono">{{ envCount }}</b></span>
    <span class="stat">附件题 <b class="mono">{{ fileCount }}</b></span>
    <span v-if="tutorialCount" class="stat">配套教程 <b class="mono">{{ tutorialCount }}</b></span>
    <div class="diff-bar" aria-hidden="true">
      <span
        v-for="seg in difficultyDist"
        :key="seg.key"
        class="seg"
        :class="`seg-${seg.key}`"
        :style="{ width: `${seg.percent}%` }"
      />
    </div>
    <span class="stat diff-legend">
      <template v-for="(seg, i) in difficultyDist" :key="seg.key">
        <i class="dot" :class="`seg-${seg.key}`" aria-hidden="true" /><b class="mono">{{ seg.count }}</b> {{ seg.label }}{{ i < difficultyDist.length - 1 ? ' · ' : '' }}
      </template>
    </span>
  </div>

  <UiTabs v-model="activeBank" class="bank-tabs">
    <UiTabPane v-for="b in banks" :key="b" :name="b" :label="BANK_LABELS[b]" />
  </UiTabs>

  <!-- 过滤条只在题库有内容时保留；过滤后结果为空也要能直接清除条件。 -->
  <div v-if="!loading && items.length > 0" class="filters">
    <div v-if="categoryOptions.length" class="chip-bar" role="group" aria-label="按分类筛选">
      <button
        type="button"
        class="cat-chip"
        :class="{ active: categoryFilter === '' }"
        :aria-pressed="categoryFilter === ''"
        @click="categoryFilter = ''"
      >
        全部 <span class="chip-count mono">{{ total }}</span>
      </button>
      <button
        v-for="opt in categoryOptions"
        :key="opt.category"
        type="button"
        class="cat-chip"
        :class="{ active: categoryFilter === opt.category }"
        :aria-pressed="categoryFilter === opt.category"
        @click="categoryFilter = categoryFilter === opt.category ? '' : opt.category"
      >
        {{ categoryLabel(opt.category) }} <span class="chip-count mono">{{ opt.count }}</span>
      </button>
    </div>
    <div class="filters-tools">
      <UiInput
        v-model="search"
        class="search"
        placeholder="搜索标题 / 分类 / 配套教程"
        clearable
        :prefix-icon="SearchIcon"
      />
      <label v-if="categoryOptions.length > 1" class="group-toggle">
        <UiSwitch v-model="grouped" size="small" aria-label="按分类分组" />
        <span>按分类分组</span>
      </label>
      <UiSelect v-model="builtinFilter" class="filter">
        <UiOption label="来源：全部" value="" />
        <UiOption label="来源：内置题集" value="yes" />
        <UiOption label="来源：管理员上传" value="no" />
      </UiSelect>
    </div>
  </div>

  <div v-if="loading" class="training-skeleton" role="status" aria-label="正在加载题目">
    <div class="sk-filter-row" aria-hidden="true">
      <span v-for="n in 4" :key="n" class="sk-chip" />
      <span class="sk-search" />
    </div>
    <div class="sk-list" aria-hidden="true">
      <div v-for="n in 7" :key="n" class="sk-row">
        <span class="sk-diff" />
        <span class="sk-main">
          <span class="sk-title" />
          <span class="sk-sub" />
        </span>
        <span class="sk-score" />
        <span class="sk-tag" />
      </div>
    </div>
  </div>
  <UiLoadError v-else-if="loadError" :message="loadError" @retry="load" />
  <template v-else-if="visibleItems.length === 0">
    <div class="empty-guide" data-testid="training-empty" role="status">
      <span class="empty-icon" aria-hidden="true">
        <component :is="items.length === 0 ? Inbox : SearchX" :size="20" />
      </span>
      <p class="empty-title">{{ items.length === 0 ? emptyHint[activeBank] : '没有匹配的题目' }}</p>
      <p class="empty-sub">
        {{ items.length === 0 ? emptyDescription : '试试清除关键词或分类筛选，或切换到其它题库。' }}
      </p>
      <UiButton v-if="items.length === 0 && isAdmin" type="primary" @click="$router.push('/admin/panel/challenges')">
        前往题目管理上传
      </UiButton>
      <UiButton v-else-if="items.length > 0" size="small" @click="clearFilters">清除筛选</UiButton>
    </div>
  </template>
  <template v-else-if="grouped">
    <section v-for="g in groups" :key="g.category" class="group">
      <header class="cat-head">
        <h3 class="cat">{{ categoryLabel(g.category) }}</h3>
        <span class="cat-count">{{ g.items.length }} 题</span>
      </header>
      <div class="chal-list">
        <router-link
          v-for="it in g.items"
          :key="it.id"
          class="row chal-link"
          :to="`${base}/training/${it.slug}`"
        >
          <span class="diff-dot" :class="`diff-${it.difficulty}`" aria-hidden="true" />
          <div class="chal-main">
            <div class="chal-title">
              <span class="title-text">{{ it.title }}</span>
              <UiTag :type="difficultyType(it.difficulty)" size="small" effect="plain">
                {{ DIFFICULTY_LABELS[it.difficulty] }}
              </UiTag>
            </div>
            <span v-if="it.tutorial_title" class="tutorial-line">配套教程：{{ it.tutorial_title }}</span>
          </div>
          <span class="score mono">{{ it.score }} 分</span>
          <span class="row-tags">
            <UiTag v-if="it.solved" type="success" size="small">已通关</UiTag>
            <UiTag v-else-if="it.has_env" type="primary" size="small" effect="plain">容器</UiTag>
            <UiTag v-if="it.is_builtin" size="small" effect="plain">内置</UiTag>
          </span>
          <span v-if="it.solved" class="solved-check" title="已通关">✓</span>
        </router-link>
      </div>
    </section>
  </template>
  <div v-else class="chal-list">
    <router-link
      v-for="it in visibleItems"
      :key="it.id"
      class="row chal-link"
      :to="`${base}/training/${it.slug}`"
    >
      <span class="diff-dot" :class="`diff-${it.difficulty}`" aria-hidden="true" />
      <div class="chal-main">
        <div class="chal-title">
          <span class="title-text">{{ it.title }}</span>
          <UiTag :type="difficultyType(it.difficulty)" size="small" effect="plain">
            {{ DIFFICULTY_LABELS[it.difficulty] }}
          </UiTag>
        </div>
        <span v-if="it.tutorial_title" class="tutorial-line">配套教程：{{ it.tutorial_title }}</span>
      </div>
      <span class="score mono">{{ it.score }} 分</span>
      <span class="row-tags">
        <UiTag v-if="it.solved" type="success" size="small">已通关</UiTag>
        <UiTag v-else-if="it.has_env" type="primary" size="small" effect="plain">容器</UiTag>
        <UiTag v-if="it.is_builtin" size="small" effect="plain">内置</UiTag>
      </span>
      <span v-if="it.solved" class="solved-check" title="已通关">✓</span>
    </router-link>
  </div>
</template>

<style scoped>
.accent { color: var(--accent-foreground); }
.solve-progress {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 220px;
  max-width: 380px;
}
.solve-progress .bar { flex: 1; }
.solve-progress .label {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.stats-strip {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-bottom: var(--space-5);
  font-size: var(--text-xs);
  color: var(--muted-foreground);
}
.stat b { font-weight: 600; color: var(--foreground); }
.diff-bar {
  display: flex;
  height: 6px;
  min-width: 140px;
  flex: 1;
  max-width: 260px;
  border-radius: 999px;
  overflow: hidden;
  background: var(--muted);
}
.diff-bar .seg { height: 100%; }
.seg-apprentice { background: var(--success); }
.seg-practitioner { background: var(--warning); }
.seg-expert { background: var(--destructive); }
.diff-legend .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 4px; vertical-align: 0; }

.bank-tabs { margin-bottom: var(--space-1); }
.filters {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}
.chip-bar { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; min-width: 0; }
.cat-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  min-height: 32px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--card);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: border-color 150ms ease, color 150ms ease, background-color 150ms ease;
}
.cat-chip:hover { border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); color: var(--foreground); }
.cat-chip:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.cat-chip.active {
  border-color: color-mix(in srgb, var(--primary) 55%, var(--border));
  background: var(--accent);
  color: var(--accent-foreground);
}
.chip-count { opacity: 0.75; }
.filters-tools { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; }
.search {
  width: 220px;
  min-width: 0;
  flex: none;
}
@media (max-width: 1024px) {
  .search { width: 160px; }
}
@media (max-width: 720px) {
  .search { width: 100%; flex: 1 1 100%; order: 2; }
}
.group-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-xs);
  color: var(--muted-foreground);
  cursor: pointer;
  white-space: nowrap;
}
.filter { width: 180px; }

.group { margin-bottom: var(--space-6); }
.cat-head {
  display: flex; align-items: baseline; gap: var(--space-3);
  margin-bottom: var(--space-2);
  border-bottom: 1px solid var(--border);
  padding-bottom: var(--space-2);
}
.cat {
  font-size: var(--text-sm); color: var(--accent-foreground); margin: 0;
  font-weight: 600;
}
.cat::before { content: '# '; opacity: 0.7; }
.cat-count { color: var(--muted-foreground); font-size: var(--text-xs); font-family: var(--font-mono); }

.chal-list { display: flex; flex-direction: column; }
.chal-link {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 10px 14px 10px 10px;
  border-bottom: 1px solid var(--border);
  text-decoration: none;
  color: inherit;
  transition: background-color 150ms ease;
}
.chal-list .chal-link:first-child { border-top: 1px solid var(--border); }
.chal-link:hover { background: var(--accent); }
.chal-link:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.diff-dot { width: 4px; align-self: stretch; border-radius: 2px; flex: none; }
.diff-apprentice { background: var(--success); }
.diff-practitioner { background: var(--warning); }
.diff-expert { background: var(--destructive); }
.chal-main { min-width: 0; flex: 1; }
.chal-title { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; }
.title-text {
  color: var(--foreground);
  font-size: var(--text-sm);
  font-weight: 600;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.tutorial-line {
  display: block;
  margin-top: 2px;
  font-size: var(--text-xs);
  color: var(--muted-foreground);
  overflow-wrap: anywhere;
}
.score { color: var(--muted-foreground); font-size: var(--text-xs); white-space: nowrap; }
.row-tags { display: flex; align-items: center; gap: 6px; flex: none; }
.solved-check {
  color: var(--accent-foreground); font-weight: 700;
  border: 1px solid var(--border);
  background: var(--accent);
  border-radius: 50%; width: 20px; height: 20px;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: var(--text-xs); flex: none;
}

/* ===== 结构骨架：与 tabs / 过滤条 / 单行题目列表同形 ===== */
.training-skeleton { min-width: 0; }
.sk-filter-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}
.sk-chip { width: 76px; height: 32px; border-radius: 999px; background: var(--muted); }
.sk-search { width: 220px; height: 36px; margin-left: auto; border-radius: var(--radius-control); background: var(--muted); }
.sk-list { border-top: 1px solid var(--border); }
.sk-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.sk-diff { width: 4px; height: 32px; flex: none; border-radius: 2px; background: var(--muted); }
.sk-main { display: grid; flex: 1; gap: var(--space-2); min-width: 0; }
.sk-title { width: min(46%, 280px); height: 14px; border-radius: 4px; background: var(--muted); }
.sk-sub { width: min(30%, 180px); height: 10px; border-radius: 4px; background: var(--muted); }
.sk-score { width: 44px; height: 12px; border-radius: 4px; background: var(--muted); }
.sk-tag { width: 52px; height: 20px; border-radius: 999px; background: var(--muted); }
.training-skeleton .sk-chip,
.training-skeleton .sk-search,
.training-skeleton .sk-row span { animation: training-skeleton-pulse 1.4s ease-in-out infinite; }
@keyframes training-skeleton-pulse { 50% { opacity: .45; } }

.empty-guide {
  display: grid;
  justify-items: center;
  gap: var(--space-2);
  width: min(100%, 620px);
  margin: var(--space-6) auto 0;
  padding: var(--space-6);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
  text-align: center;
}
.empty-icon {
  display: inline-grid;
  width: 44px;
  height: 44px;
  place-items: center;
  margin-bottom: var(--space-2);
  border-radius: 50%;
  background: var(--accent);
  color: var(--accent-foreground);
}
.empty-title { margin: 0; font-size: var(--text-md); font-weight: 600; color: var(--foreground); }
.empty-sub { max-width: 56ch; margin: 0 0 var(--space-2); font-size: var(--text-sm); color: var(--muted-foreground); line-height: var(--leading-relaxed); }

@media (max-width: 720px) {
  /* 移动端保留分数：改为两行栅格（标题 / 分数 + 状态），不再直接隐藏信息。 */
  .chal-link {
    display: grid;
    grid-template-columns: 4px minmax(0, 1fr) auto;
    grid-template-areas:
      "diff main check"
      "diff meta check";
    align-items: center;
    column-gap: var(--space-3);
    row-gap: var(--space-1);
  }
  .diff-dot { grid-area: diff; align-self: stretch; }
  .chal-main { grid-area: main; }
  .score { grid-area: meta; justify-self: start; font-size: var(--text-xs); }
  .row-tags { grid-area: meta; justify-self: end; }
  .solved-check { grid-area: check; }
  .row-tags .ui-tag[data-tone='default'] { display: none; }
  .chal-link { min-height: 44px; }
  .stats-strip { gap: var(--space-2) var(--space-3); }
  .diff-bar { display: none; }
  .filters { flex-direction: column; align-items: stretch; gap: var(--space-3); }
  .filters-tools { width: 100%; }
  .search { width: 100%; order: 0; flex: 1 1 100%; }
  .filter { width: auto; flex: 1 1 180px; }
  .empty-guide { margin-top: var(--space-4); padding: var(--space-6) var(--space-5); }
  .sk-search { width: 100%; margin-left: 0; }
  .sk-score { display: none; }
  .sk-tag { display: none; }
}
</style>
