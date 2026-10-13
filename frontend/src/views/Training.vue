<script setup lang="ts">
// 训练场（Burp all-labs 风格）：按 category 分组的题目列表，仅展示
// 标题 / 难度 / 分值 / 容器 / 通关状态；点题目进详情页（启动容器、提交 flag
// 都在详情完成，列表保持纯浏览）。一次拉全量（page_size=100）客户端分组。
// 普通端与管理端共用。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { trainingApi, type TrainingItem } from '../api'

const route = useRoute()
const groups = ref<{ category: string; items: TrainingItem[] }[]>([])
const total = ref(0)
const loading = ref(true)
const solvedCount = ref(0)

const base = computed(() => (route.path.startsWith('/admin') ? '/admin' : ''))

async function load() {
  loading.value = true
  try {
    const resp = await trainingApi.list(1, 100)
    total.value = resp.total
    solvedCount.value = resp.items.filter((it) => it.solved).length
    // 客户端按 category 分组（保持后端顺序）。
    const byCat = new Map<string, TrainingItem[]>()
    for (const it of resp.items) {
      const key = it.category || 'uncategorized'
      if (!byCat.has(key)) byCat.set(key, [])
      byCat.get(key)!.push(it)
    }
    groups.value = [...byCat.entries()].map(([category, items]) => ({ category, items }))
    // 超过 100 条时分页拉余下页。
    if (resp.total > resp.items.length) await loadRemaining(resp.items.length)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function loadRemaining(loaded: number) {
  let offset = loaded
  while (offset < total.value) {
    const page = Math.floor(offset / 100) + 1
    const resp = await trainingApi.list(page, 100)
    for (const it of resp.items) {
      const key = it.category || 'uncategorized'
      const g = groups.value.find((x) => x.category === key)
      if (g) g.items.push(it)
      else groups.value.push({ category: key, items: [it] })
      if (it.solved) solvedCount.value++
    }
    offset += resp.items.length
    if (resp.items.length === 0) break
  }
}

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

onMounted(load)
</script>

<template>
  <div class="head">
    <h2>训练</h2>
    <span v-if="total" class="stat">{{ solvedCount }}/{{ total }} 已通关</span>
  </div>
  <el-skeleton v-if="loading" :rows="6" animated />
  <el-empty v-else-if="groups.length === 0" description="暂无题目：等待管理员上传" />
  <template v-else>
    <section v-for="g in groups" :key="g.category" class="group">
      <h3 class="cat">{{ g.category }}</h3>
      <div v-for="it in g.items" :key="it.id" class="row">
        <router-link class="title" :to="`${base}/training/${it.slug}`">
          {{ it.title }}
          <el-tag v-if="it.solved" type="success" size="small" class="inline-tag">已通关</el-tag>
          <el-tag v-else-if="it.has_env" type="primary" size="small" class="inline-tag">容器环境</el-tag>
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