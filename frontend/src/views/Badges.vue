<script setup lang="ts">
// 徽章页：目录全集 + 当前用户获得状态（计算型读模型，进度/积分/路径的投影）。
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { badgeApi, type Badge } from '../api'

const badges = ref<Badge[]>([])
const loading = ref(false)

const earnedCount = computed(() => badges.value.filter((b) => b.earned).length)

async function load() {
  loading.value = true
  try {
    badges.value = await badgeApi.list()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <h2 class="page-title">徽<span class="accent">章</span></h2>
  <p class="hint">已获得 {{ earnedCount }} / {{ badges.length }} 枚 —— 完成题目、学习教程与积分里程碑自动解锁。</p>
  <el-skeleton v-if="loading" :rows="4" animated />
  <div v-else class="grid">
    <div v-for="b in badges" :key="b.code" class="badge" :class="{ earned: b.earned }">
      <span class="medal">{{ b.earned ? '🏅' : '🔒' }}</span>
      <div class="meta">
        <span class="title">{{ b.title }}</span>
        <span class="desc">{{ b.description }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hint { color: var(--el-text-color-secondary); font-size: 0.9rem; margin-top: 0; }
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 0.8rem;
}
.badge {
  display: flex;
  align-items: center;
  gap: 0.8rem;
  padding: 0.9rem 1rem;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  opacity: 0.55;
}
.badge.earned {
  opacity: 1;
  border-color: var(--el-color-success-light-5);
  background: var(--el-color-success-light-9);
}
.medal { font-size: 1.6rem; }
.meta { display: flex; flex-direction: column; gap: 0.2rem; }
.title { font-weight: 600; }
.desc { font-size: 0.85rem; color: var(--el-text-color-secondary); }
</style>
