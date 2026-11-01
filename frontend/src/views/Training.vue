<script setup lang="ts">
// 题库：页顶按题库 tab 切换（教程题目/CTF 题目/ACM 题目），
// tab 内过滤器支持按题目类型（category，客户端）与是否内置（服务端）筛选，
// 按 category 分组的卡片网格（CTFd challenges 风格），卡片展示
// 标题/难度/分值/容器/通关状态；点题目进详情页（启动容器、提交 flag
// 都在详情完成，列表保持纯浏览）。一次拉全量（page_size=100）。
// 普通端与管理端共用。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { notify } from '@/lib/notifications'
import { BANK_LABELS, trainingApi, type ChallengeBank, type TrainingItem } from '../api'

const route = useRoute()
const banks: ChallengeBank[] = ['tutorial', 'ctf', 'acm']
const activeBank = ref<ChallengeBank>(banks.includes(route.query.bank as ChallengeBank) ? route.query.bank as ChallengeBank : 'tutorial')
// 教程题目 tab 专属内置题集，空态文案与上传型 tab 区分。
const emptyHint: Record<ChallengeBank, string> = {
  tutorial: '暂无教程题目：内置题集尚未导入，请联系管理员在管理端一键导入内置训练（vulinbox）',
  ctf: '暂无CTF题目：等待管理员上传',
  acm: '暂无ACM题目：等待管理员上传',
}
const items = ref<TrainingItem[]>([])
const total = ref(0)
const loading = ref(true)

// ===== 过滤器 =====
// 是否内置走服务端过滤（bank + builtin 查询参数）；题目类型为动态值，
// 由当前 tab 数据派生选项、客户端筛选。
const categoryFilter = ref('')
const builtinFilter = ref<'' | 'yes' | 'no'>('')

const categoryOptions = computed(() => [...new Set(items.value.map((it) => it.category))].sort())

// 分类过滤后按 category 分组（保持后端顺序）。
const groups = computed(() => {
  const byCat = new Map<string, TrainingItem[]>()
  for (const it of items.value) {
    if (categoryFilter.value && it.category !== categoryFilter.value) continue
    const key = it.category || 'uncategorized'
    if (!byCat.has(key)) byCat.set(key, [])
    byCat.get(key)!.push(it)
  }
  return [...byCat.entries()].map(([category, list]) => ({ category, items: list }))
})

const solvedCount = computed(() => items.value.filter((it) => it.solved).length)
const solvedPercent = computed(() => (total.value > 0 ? Math.round((solvedCount.value / total.value) * 100) : 0))

const base = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

async function load() {
  loading.value = true
  categoryFilter.value = '' // 切 tab/来源后分类集合变化，重置已失效的选择
  try {
    const filter: { bank: ChallengeBank; builtin?: boolean } = { bank: activeBank.value }
    if (builtinFilter.value === 'yes') filter.builtin = true
    if (builtinFilter.value === 'no') filter.builtin = false
    const resp = await trainingApi.list(1, 100, filter)
    items.value = resp.items
    total.value = resp.total
    // 超过 100 条时分页拉余下页。
    if (resp.total > resp.items.length) await loadRemaining(filter, resp.items.length)
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function loadRemaining(filter: { bank: ChallengeBank; builtin?: boolean }, loaded: number) {
  let offset = loaded
  while (offset < total.value) {
    const page = Math.floor(offset / 100) + 1
    const resp = await trainingApi.list(page, 100, filter)
    items.value.push(...resp.items)
    offset += resp.items.length
    if (resp.items.length === 0) break
  }
}

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

watch([activeBank, builtinFilter], load)
watch(() => route.query.bank, value => {
  if (banks.includes(value as ChallengeBank)) activeBank.value = value as ChallengeBank
})
onMounted(load)
</script>

<template>
  <header class="head">
    <h2 class="page-title">题<span class="accent">库</span></h2>
    <div v-if="total" class="solve-progress">
      <span class="label">{{ solvedCount }}/{{ total }}</span>
      <UiProgress :percentage="solvedPercent" :stroke-width="8" class="bar" />
      <span class="label">{{ solvedPercent }}%</span>
    </div>
  </header>

  <UiTabs v-model="activeBank" class="bank-tabs">
    <UiTabPane v-for="b in banks" :key="b" :name="b" :label="BANK_LABELS[b]" />
  </UiTabs>

  <!-- 过滤条在结果为空时也要保留，否则选了过滤条件后无法切回「来源：全部」。 -->
  <div v-if="!loading" class="filters">
    <UiSelect v-model="categoryFilter" class="filter" placeholder="题目类型：全部">
      <UiOption label="题目类型：全部" value="" />
      <UiOption v-for="c in categoryOptions" :key="c" :label="c" :value="c" />
    </UiSelect>
    <UiSelect v-model="builtinFilter" class="filter">
      <UiOption label="来源：全部" value="" />
      <UiOption label="来源：内置题集" value="yes" />
      <UiOption label="来源：管理员上传" value="no" />
    </UiSelect>
  </div>

  <UiSkeleton v-if="loading" :rows="6" animated />
  <UiEmpty
    v-else-if="groups.length === 0"
    :description="emptyHint[activeBank]"
  />
  <template v-else>
    <section v-for="g in groups" :key="g.category" class="group">
      <header class="cat-head">
        <h3 class="cat">{{ g.category }}</h3>
        <span class="cat-count">{{ g.items.length }} 题</span>
      </header>
      <div class="card-grid">
        <router-link
          v-for="it in g.items"
          :key="it.id"
          class="row chal-link"
          :to="`${base}/training/${it.slug}`"
        >
          <div class="chal-card tall-card" :class="{ solved: it.solved }">
            <div class="chal-title">
              <span class="title-text">{{ it.title }}</span>
              <span v-if="it.solved" class="solved-check" title="已通关">✓</span>
            </div>
            <div class="card-meta">
              <UiTag :type="difficultyType(it.difficulty)" size="small" effect="plain">
                {{ it.difficulty }}
              </UiTag>
              <span class="score">{{ it.score }} 分</span>
              <span class="spacer" />
              <UiTag v-if="it.solved" type="success" size="small">已通关</UiTag>
              <UiTag v-else-if="it.has_env" type="primary" size="small" effect="plain">容器环境</UiTag>
              <UiTag v-if="it.is_builtin" size="small" effect="plain">内置</UiTag>
            </div>
          </div>
        </router-link>
      </div>
    </section>
  </template>
</template>

<style scoped>
.head { display: flex; align-items: center; gap: 1.5rem; flex-wrap: wrap; margin-bottom: 0.75rem; }
.accent { color: var(--accent-foreground); }
.solve-progress { flex: 1; min-width: 220px; max-width: 380px; }
.solve-progress .bar { flex: 1; }
.solve-progress .bar :deep(.ui-progress-label) { display: none; }
.bank-tabs { margin-bottom: 0.25rem; }
.filters { display: flex; gap: 0.75rem; margin-bottom: 1.25rem; }
.filter { width: 200px; }
.group { margin-bottom: 2.25rem; }
.cat-head {
  display: flex; align-items: baseline; gap: 0.7rem;
  margin-bottom: 0.9rem;
  border-bottom: 1px solid var(--border);
  padding-bottom: 0.45rem;
}
.cat {
  font-size: 1rem; color: var(--accent-foreground); margin: 0;
  font-family: Consolas, monospace;
}
.cat::before { content: '# '; opacity: 0.7; }
.cat-count { color: var(--muted-foreground); font-size: 0.8rem; font-family: Consolas, monospace; }
.chal-link { text-decoration: none; color: inherit; }
.chal-card {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 0.95rem 1.05rem 0.85rem;
}
.chal-card.solved { border-color: rgba(255, 128, 64, 0.35); }
.chal-title { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.65rem; }
.title-text {
  color: var(--foreground); font-size: 0.98rem; font-weight: 600;
  line-height: 1.45;
}
.solved-check {
  color: var(--accent-foreground); font-weight: 700;
  border: 1px solid var(--border);
  background: var(--accent);
  border-radius: 50%; width: 20px; height: 20px;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 0.78rem; flex: none;
}
.chal-card .spacer { flex: 1; }
.score { color: var(--muted-foreground); font-size: 0.85rem; font-family: Consolas, monospace; }
</style>
