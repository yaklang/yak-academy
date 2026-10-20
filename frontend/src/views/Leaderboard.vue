<script setup lang="ts">
// 排行榜页（事件派生数据视图）：前三名领奖台 + 名次表。
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { progressApi, type LeaderboardEntry } from '../api'

const entries = ref<LeaderboardEntry[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)

// 当前页前三名进领奖台（排行榜按分值降序，首页即全场前三）。
const podium = computed(() =>
  page.value === 1
    ? entries.value.slice(0, 3).map((e, i) => ({ ...e, place: i + 1 }))
    : [],
)
const rest = computed(() => entries.value)

async function load() {
  loading.value = true
  try {
    const resp = await progressApi.leaderboard(page.value)
    entries.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <h2 class="page-title">排行<span class="accent">榜</span></h2>
    <p class="page-sub">按总分排序，事件溯源实时派生。</p>
  </header>

  <!-- 领奖台：2-1-3 经典高度（第 1 名居中最高） -->
  <div v-if="podium.length" class="podium" aria-hidden="true">
    <template v-for="p in podium" :key="p.rank">
      <div v-if="p.place === 2" class="pod d2">
        <span class="medal">🥈</span>
        <span class="p-name">{{ p.username }}</span>
        <span class="p-score mono">{{ p.score }}</span>
      </div>
      <div v-if="p.place === 1" class="pod d1">
        <span class="medal">🥇</span>
        <span class="p-name">{{ p.username }}</span>
        <span class="p-score mono">{{ p.score }}</span>
      </div>
      <div v-if="p.place === 3" class="pod d3">
        <span class="medal">🥉</span>
        <span class="p-name">{{ p.username }}</span>
        <span class="p-score mono">{{ p.score }}</span>
      </div>
    </template>
  </div>

  <el-table :data="rest" v-loading="loading">
    <el-table-column prop="rank" label="排名" width="80">
      <template #default="{ row }">
        <span class="mono" :class="{ 'top-rank': row.rank <= 3 }">{{ row.rank }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="username" label="用户名">
      <template #default="{ row }">
        <span :class="{ 'top-name': row.rank <= 3 }">{{ row.username }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="score" label="分数" width="120">
      <template #default="{ row }">
        <span class="mono score-cell">{{ row.score }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="solved_count" label="解题数" width="100">
      <template #default="{ row }">
        <span class="mono">{{ row.solved_count }}</span>
      </template>
    </el-table-column>
  </el-table>
  <el-pagination
    v-if="total > 20"
    layout="prev, pager, next"
    :total="total"
    :page-size="20"
    v-model:current-page="page"
    @current-change="load"
  />
</template>

<style scoped>
.page-head { margin-bottom: 1.5rem; }
.accent { color: var(--el-color-primary); }
.podium {
  display: flex; align-items: flex-end; justify-content: center;
  gap: 0.9rem; margin-bottom: 1.75rem;
}
.pod {
  display: flex; flex-direction: column; align-items: center; gap: 0.3rem;
  background: var(--el-bg-color-overlay);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 1rem 1.4rem 0.9rem;
  min-width: 150px;
}
.pod.d1 {
  border-color: rgba(0, 230, 118, 0.45);
  background: linear-gradient(180deg, rgba(0, 230, 118, 0.09), var(--el-bg-color-overlay) 55%);
  padding: 1.5rem 1.8rem 1.1rem;
}
.pod.d2 { padding-bottom: 0.7rem; }
.pod.d3 { padding-bottom: 0.55rem; }
.medal { font-size: 1.5rem; }
.pod.d1 .medal { font-size: 1.9rem; }
.p-name { font-weight: 700; color: var(--el-text-color-primary); }
.p-score { color: var(--el-color-primary); font-weight: 700; }
.mono { font-family: Consolas, monospace; }
.top-rank { color: var(--el-color-primary); font-weight: 700; }
.top-name { font-weight: 700; color: var(--el-text-color-primary); }
.score-cell { color: var(--el-text-color-primary); }
/* 窄屏：领奖台三列收缩（去 min-width、缩内边距），长用户名省略号截断 */
@media (max-width: 640px) {
  .podium { gap: 0.5rem; }
  .pod { min-width: 0; flex: 1; max-width: 150px; padding: 0.8rem 0.5rem 0.7rem; }
  .pod.d1 { padding: 1rem 0.6rem 0.8rem; }
  .medal { font-size: 1.2rem; }
  .pod.d1 .medal { font-size: 1.5rem; }
  .p-name { font-size: 0.85rem; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}
</style>