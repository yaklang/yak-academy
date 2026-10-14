<script setup lang="ts">
// 学习路径：有序 Room 引用视图（teaching-integration：不内联内容、不引用 Challenge）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { pathApi, type LearningPath } from '../api'

const loading = ref(true)
const paths = ref<LearningPath[]>([])
const selected = ref<LearningPath | null>(null)

async function load() {
  loading.value = true
  try {
    paths.value = await pathApi.list()
    selected.value = paths.value[0] ?? null
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function openPath(id: number) {
  try {
    selected.value = await pathApi.get(id)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

function doneCount(p: LearningPath) {
  return p.items.filter((i) => i.completed).length
}

onMounted(load)
</script>

<template>
  <h2>学习路径</h2>
  <p class="lead">路径是教程 Room 的有序视图；完成态来自事件派生的 Progress。</p>

  <el-skeleton v-if="loading" :rows="4" animated />
  <el-empty v-else-if="!paths.length" description="暂无学习路径，请管理员创建" />

  <div v-else class="layout">
    <ul class="path-list">
      <li
        v-for="p in paths"
        :key="p.id"
        :class="{ active: selected?.id === p.id }"
        @click="openPath(p.id)"
      >
        <span class="title">{{ p.title }}</span>
        <span class="meta">{{ doneCount(p) }}/{{ p.items.length }}</span>
      </li>
    </ul>

    <div v-if="selected" class="detail">
      <h3>{{ selected.title }}</h3>
      <ol class="items">
        <li v-for="it in selected.items" :key="`${it.order}-${it.room_slug}`">
          <el-tag :type="it.completed ? 'success' : 'info'" size="small">
            {{ it.completed ? '已完成' : '未完成' }}
          </el-tag>
          <span class="order">#{{ it.order }}</span>
          <!-- topic_slug 为响应富化字段，缺失（Room 已下架等）时降级为纯文本 -->
          <router-link
            v-if="it.topic_slug"
            class="room-link"
            :to="`/topics/${it.topic_slug}/rooms/${it.room_slug}`"
          >{{ it.title || it.room_slug }}</router-link>
          <code v-else>{{ it.room_slug }}</code>
        </li>
      </ol>
    </div>
  </div>
</template>

<style scoped>
.lead { color: var(--el-text-color-secondary); margin-top: 0; }
.layout { display: grid; grid-template-columns: 240px 1fr; gap: 1.5rem; align-items: start; }
.path-list { list-style: none; margin: 0; padding: 0; border-right: 1px solid var(--el-border-color); }
.path-list li {
  display: flex; justify-content: space-between; gap: 0.5rem;
  padding: 0.7rem 0.8rem; cursor: pointer; border-radius: 4px;
}
.path-list li:hover, .path-list li.active { background: var(--el-fill-color-light); }
.title { font-weight: 600; }
.meta { color: var(--el-text-color-secondary); font-size: 0.85rem; }
.items { padding-left: 1.2rem; }
.items li { display: flex; align-items: center; gap: 0.6rem; margin: 0.6rem 0; }
.order { color: var(--el-text-color-secondary); font-size: 0.85rem; }
.room-link { color: var(--el-color-primary); text-decoration: none; }
.room-link:hover { text-decoration: underline; }
@media (max-width: 720px) {
  .layout { grid-template-columns: 1fr; }
  .path-list { border-right: none; border-bottom: 1px solid var(--el-border-color); padding-bottom: 0.5rem; }
}
</style>
