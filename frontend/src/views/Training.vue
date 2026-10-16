<script setup lang="ts">
// 题库（Burp all-labs 风格）：页顶按题库 tab 切换（教程题目/CTF 题目/ACM 题目），
// tab 内过滤器支持按题目类型（category，客户端）与是否内置（服务端）筛选，
// 列表按 category 分组仅展示标题/难度/分值/容器/通关状态；点题目进详情页
// （启动容器、提交 flag 都在详情完成，列表保持纯浏览）。一次拉全量（page_size=100）。
// 普通端与管理端共用。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { BANK_LABELS, trainingApi, type ChallengeBank, type TrainingItem } from '../api'

const route = useRoute()
const banks: ChallengeBank[] = ['tutorial', 'ctf', 'acm']
const activeBank = ref<ChallengeBank>('tutorial')
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
    ElMessage.error((e as Error).message)
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
onMounted(load)
</script>

<template>
  <div class="head">
    <h2>题库</h2>
    <span v-if="total" class="stat">{{ solvedCount }}/{{ total }} 已通关</span>
  </div>

  <el-tabs v-model="activeBank" class="bank-tabs">
    <el-tab-pane v-for="b in banks" :key="b" :name="b" :label="BANK_LABELS[b]" />
  </el-tabs>

  <!-- 过滤条在结果为空时也要保留，否则选了过滤条件后无法切回「来源：全部」。 -->
  <div v-if="!loading" class="filters">
    <el-select v-model="categoryFilter" class="filter" placeholder="题目类型：全部">
      <el-option label="题目类型：全部" value="" />
      <el-option v-for="c in categoryOptions" :key="c" :label="c" :value="c" />
    </el-select>
    <el-select v-model="builtinFilter" class="filter">
      <el-option label="来源：全部" value="" />
      <el-option label="来源：内置题集" value="yes" />
      <el-option label="来源：管理员上传" value="no" />
    </el-select>
  </div>

  <el-skeleton v-if="loading" :rows="6" animated />
  <el-empty
    v-else-if="groups.length === 0"
    :description="`暂无${BANK_LABELS[activeBank]}：等待管理员上传`"
  />
  <template v-else>
    <section v-for="g in groups" :key="g.category" class="group">
      <h3 class="cat">{{ g.category }}</h3>
      <div v-for="it in g.items" :key="it.id" class="row">
        <router-link class="title" :to="`${base}/training/${it.slug}`">
          {{ it.title }}
          <el-tag v-if="it.solved" type="success" size="small" class="inline-tag">已通关</el-tag>
          <el-tag v-else-if="it.has_env" type="primary" size="small" class="inline-tag">容器环境</el-tag>
          <el-tag v-if="it.is_builtin" size="small" class="inline-tag">内置</el-tag>
        </router-link>
        <span class="right">
          <el-tag :type="difficultyType(it.difficulty)" size="small">{{ it.difficulty }}</el-tag>
          <span class="score">{{ it.score }} 分</span>
        </span>
      </div>
    </section>
  </template>
</template>

<style scoped>
.head { display: flex; align-items: baseline; gap: 1rem; }
.stat { color: var(--el-text-color-secondary); font-size: 0.9rem; font-family: Consolas, monospace; }
.bank-tabs { margin-bottom: 0.25rem; }
.filters { display: flex; gap: 0.75rem; margin-bottom: 1.25rem; }
.filter { width: 200px; }
.group { margin-bottom: 2rem; max-width: 860px; }
.cat {
  font-size: 0.95rem; color: var(--el-color-primary);
  border-bottom: 1px solid var(--el-border-color);
  padding-bottom: 0.4rem; margin-bottom: 0.3rem;
  font-family: Consolas, monospace; text-transform: lowercase;
}
.row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0.5rem 0.25rem; border-bottom: 1px solid var(--el-border-color-extra-light);
}
.row:hover { background: var(--el-fill-color-light); }
.title { color: var(--el-text-color-primary); text-decoration: none; font-size: 0.95rem; }
.title:hover { color: var(--el-color-primary); }
.inline-tag { margin-left: 0.5rem; }
.right { display: flex; align-items: center; gap: 0.7rem; }
.score { color: var(--el-text-color-secondary); font-size: 0.85rem; font-family: Consolas, monospace; }
</style>
