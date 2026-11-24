<script setup lang="ts">
// 题库：页头指标 → 题库切换 → 筛选条 → 题目表格。保留分类筛选、搜索、
// 来源过滤与通关状态；表格行可键盘激活，窄屏横向滚动。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Search } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'
import { BANK_LABELS, CATEGORY_LABELS, DIFFICULTY_LABELS, trainingApi, type ChallengeBank, type TrainingItem } from '../api'
import PageHeader from '@/components/layout/PageHeader.vue'
import PageTabs from '@/components/layout/PageTabs.vue'
import MetricStrip from '@/components/layout/MetricStrip.vue'
import StatCard from '@/components/layout/StatCard.vue'
import FilterBar from '@/components/layout/FilterBar.vue'
import DataTablePanel from '@/components/layout/DataTablePanel.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import StatusTag from '@/components/layout/StatusTag.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const banks: ChallengeBank[] = ['tutorial', 'ctf', 'acm']
const activeBank = ref<ChallengeBank>(banks.includes(route.query.bank as ChallengeBank) ? route.query.bank as ChallengeBank : 'tutorial')

const items = ref<TrainingItem[]>([])
const total = ref(0)
const loading = ref(true)
const loadError = ref('')

const categoryFilter = ref('')
const builtinFilter = ref<'' | 'yes' | 'no'>('')
const search = ref('')
const sort = ref<{ prop: string; order: 'asc' | 'desc' } | null>(null)

const emptyHint: Record<ChallengeBank, string> = {
  tutorial: '暂无教程题目',
  ctf: '暂无 CTF 题目',
  acm: '暂无 ACM 题目',
}

function categoryLabel(category: string) {
  return CATEGORY_LABELS[category] ?? category
}

const categoryOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const item of items.value) counts.set(item.category, (counts.get(item.category) ?? 0) + 1)
  return [...counts.entries()]
    .map(([category, count]) => ({ category, count }))
    .sort((a, b) => a.category.localeCompare(b.category))
})

const visibleItems = computed(() => {
  let list = items.value
  if (categoryFilter.value) list = list.filter((item) => item.category === categoryFilter.value)
  const keyword = search.value.trim().toLowerCase()
  if (keyword) {
    list = list.filter((item) =>
      item.title.toLowerCase().includes(keyword)
      || item.category.toLowerCase().includes(keyword)
      || categoryLabel(item.category).toLowerCase().includes(keyword)
      || (item.tutorial_title ?? '').toLowerCase().includes(keyword),
    )
  }
  return list
})

const difficultyRank: Record<TrainingItem['difficulty'], number> = { apprentice: 0, practitioner: 1, expert: 2 }

const sortedItems = computed(() => {
  if (!sort.value) return visibleItems.value
  const list = [...visibleItems.value]
  const direction = sort.value.order === 'asc' ? 1 : -1
  list.sort((a, b) => {
    if (sort.value?.prop === 'score') return (a.score - b.score) * direction
    return (difficultyRank[a.difficulty] - difficultyRank[b.difficulty]) * direction
  })
  return list
})

const solvedCount = computed(() => items.value.filter((item) => item.solved).length)
const envCount = computed(() => items.value.filter((item) => item.has_env).length)
const fileCount = computed(() => total.value - envCount.value)
const tutorialCount = computed(() => items.value.filter((item) => item.tutorial_title).length)

const base = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))
const isAdmin = computed(() => auth.user?.global_role === 'admin')
const emptyDescription = computed(() => {
  if (isAdmin.value) {
    return activeBank.value === 'tutorial'
      ? '在「题目管理」一键导入内置训练后即可查看。'
      : '在「题目管理」上传题目包后，会自动出现在这里。'
  }
  return activeBank.value === 'tutorial'
    ? '内置题集尚未导入，请联系管理员在管理端一键导入内置训练。'
    : '题目包由管理员上传后会自动出现在这里。'
})

let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  loading.value = true
  loadError.value = ''
  items.value = []
  total.value = 0
  categoryFilter.value = ''
  try {
    const filter: { bank: ChallengeBank; builtin?: boolean } = { bank: activeBank.value }
    if (builtinFilter.value === 'yes') filter.builtin = true
    if (builtinFilter.value === 'no') filter.builtin = false
    const resp = await trainingApi.list(1, 100, filter)
    if (seq !== loadSeq) return
    items.value = resp.items
    total.value = resp.total
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

function clearFilters() {
  categoryFilter.value = ''
  search.value = ''
  builtinFilter.value = ''
  sort.value = null
}

function openChallenge(item: TrainingItem) {
  router.push(`${base.value}/training/${item.slug}`)
}

function onSortChange(value: { prop: string; order: 'asc' | 'desc' }) {
  sort.value = value
}

watch([activeBank, builtinFilter], load)
watch(() => route.query.bank, (value) => {
  if (banks.includes(value as ChallengeBank)) activeBank.value = value as ChallengeBank
})
onMounted(load)
</script>

<template>
  <PageHeader
    title="题库"
    description="边学边练：题目可启动容器实战或下载附件离线作答，通关即计分。"
  >
    <template v-if="!loading && total > 0" #actions>
      <span class="training-progress">已通关 {{ solvedCount }}/{{ total }}</span>
    </template>
  </PageHeader>

  <MetricStrip v-if="!loading && total > 0" :columns="4">
    <StatCard label="题目总数" :value="total" />
    <StatCard label="已通关" :value="solvedCount" tone="success" />
    <StatCard label="容器题" :value="envCount" />
    <StatCard label="附件题" :value="fileCount" />
  </MetricStrip>

  <PageTabs
    v-model="activeBank"
    :tabs="banks.map((bank) => ({ label: BANK_LABELS[bank], value: bank }))"
    aria-label="题库切换"
  />

  <FilterBar v-if="!loading && items.length > 0" class="training-filters">
    <label class="filter-field">
      <span class="filter-field__label">搜索</span>
      <span class="filter-field__control">
        <Search class="filter-field__icon" aria-hidden="true" />
        <Input v-model="search" class="filter-field__input" placeholder="搜索标题 / 分类 / 配套教程" />
      </span>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">分类</span>
      <select v-model="categoryFilter" class="filter-select">
        <option value="">全部分类（{{ total }}）</option>
        <option v-for="option in categoryOptions" :key="option.category" :value="option.category">
          {{ categoryLabel(option.category) }}（{{ option.count }}）
        </option>
      </select>
    </label>
    <label class="filter-field">
      <span class="filter-field__label">来源</span>
      <select v-model="builtinFilter" class="filter-select">
        <option value="">全部来源</option>
        <option value="yes">内置题集</option>
        <option value="no">管理员上传</option>
      </select>
    </label>
    <template #actions>
      <span v-if="tutorialCount" class="filter-hint">{{ tutorialCount }} 题有配套教程</span>
      <Button v-if="search || categoryFilter || builtinFilter || sort" variant="ghost" size="sm" @click="clearFilters">
        清除筛选
      </Button>
    </template>
  </FilterBar>

  <DataTablePanel
    v-if="!loadError"
    title="题目列表"
    :count="sortedItems.length"
    :description="loading ? '正在加载题目…' : sortedItems.length ? `按当前条件显示 ${sortedItems.length} 题` : undefined"
  >
    <UiTable
      :data="sortedItems"
      row-key="id"
      :loading="loading"
      :loading-rows="7"
      row-clickable
      :sort="sort"
      selection-label="选择题目"
      @row-click="openChallenge"
      @sort-change="onSortChange"
    >
      <UiTableColumn prop="title" label="题目" min-width="280">
        <template #default="{ row }">
          <span class="challenge-title">{{ row.title }}</span>
          <span v-if="row.tutorial_title" class="challenge-tutorial">配套教程：{{ row.tutorial_title }}</span>
          <span v-if="row.is_builtin" class="challenge-builtin">内置</span>
        </template>
      </UiTableColumn>
      <UiTableColumn prop="category" label="分类" min-width="120">
        <template #default="{ row }">{{ categoryLabel(row.category) }}</template>
      </UiTableColumn>
      <UiTableColumn prop="difficulty" label="难度" width="100" sortable>
        <template #default="{ row }">
          <StatusTag kind="difficulty" :value="row.difficulty" />
        </template>
      </UiTableColumn>
      <UiTableColumn prop="score" label="分值" width="88" sortable>
        <template #default="{ row }">
          <span class="challenge-score">{{ row.score }} 分</span>
        </template>
      </UiTableColumn>
      <UiTableColumn prop="status" label="状态" min-width="120">
        <template #default="{ row }">
          <StatusTag v-if="row.solved" kind="submission" value="correct" />
          <StatusTag v-else-if="row.has_env" kind="container" value="running" label="容器" />
          <span v-else class="challenge-muted">未作答</span>
        </template>
      </UiTableColumn>
      <UiTableColumn prop="actions" label="操作" width="88">
        <template #default="{ row }">
          <Button variant="link" size="sm" as-child>
            <RouterLink :to="`${base}/training/${row.slug}`">进入</RouterLink>
          </Button>
        </template>
      </UiTableColumn>
      <template #empty>
        <EmptyState
          v-if="!loadError && items.length === 0"
          compact
          data-testid="training-empty"
          :title="emptyHint[activeBank]"
          :description="emptyDescription"
        >
          <Button v-if="isAdmin" @click="router.push('/admin/panel/challenges')">前往题目管理上传</Button>
        </EmptyState>
        <EmptyState
          v-else
          compact
          tone="filtered"
          title="没有匹配的题目"
          description="试试清除关键词或分类筛选，或切换到其它题库。"
          action-label="清除筛选"
          @action="clearFilters"
        />
      </template>
    </UiTable>
    <template #footer>
      <span class="table-footer-note">共 {{ sortedItems.length }} 题 · 第 {{ activeBank ? BANK_LABELS[activeBank] : '' }}</span>
    </template>
  </DataTablePanel>

  <EmptyState
    v-if="loadError"
    tone="error"
    title="题库加载失败"
    :description="loadError"
    action-label="重试"
    @action="load"
  />

  <p v-else-if="!loading && total > 0 && tutorialCount" class="training-note">
    {{ tutorialCount }} 题配有教程章节，可先阅读教程再作答。
  </p>
</template>

<style scoped>
.training-progress {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-variant-numeric: tabular-nums;
}

.training-filters {
  margin-top: var(--space-4);
}

.filter-field {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.filter-field__label {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
}

.filter-field__control {
  position: relative;
  display: block;
}

.filter-field__icon {
  position: absolute;
  top: 50%;
  left: var(--space-3);
  width: 16px;
  height: 16px;
  color: var(--text-tertiary);
  transform: translateY(-50%);
  pointer-events: none;
}

.filter-field__input {
  width: 240px;
  padding-left: var(--space-8);
}

.filter-select {
  min-height: 36px;
  padding-inline: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: var(--text-caption);
}

.filter-hint {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.challenge-title {
  display: block;
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
  line-height: var(--leading-normal);
}

.challenge-tutorial {
  display: block;
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  overflow-wrap: anywhere;
}

.challenge-builtin {
  display: inline-block;
  margin-top: var(--space-1);
  padding-inline: var(--space-1);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-micro);
}

.challenge-score {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.challenge-muted {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.table-footer-note {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.training-note {
  margin-top: var(--space-3);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

@media (max-width: 720px) {
  .filter-field,
  .filter-field__input,
  .filter-select {
    width: 100%;
  }

  .filter-select {
    min-height: 44px;
  }
}
</style>
