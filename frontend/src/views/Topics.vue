<script setup lang="ts">
// 主题列表页：真实加载 GET /topics。
import { onMounted, ref } from 'vue'
import { topicApi, type Topic } from '../api'
import { ApiError } from '../api/client'

const topics = ref<Topic[]>([])
const loading = ref(true)
const empty = ref(false)

onMounted(async () => {
  try {
    const page = await topicApi.list()
    topics.value = page.items
    empty.value = page.items.length === 0
  } catch (e) {
    if (e instanceof ApiError && e.status === 501) {
      empty.value = true
    }
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <h2>安全主题</h2>
  <el-skeleton v-if="loading" :rows="3" animated />
  <el-empty v-else-if="empty" description="暂无内容：等待管理员上传内容包" />
  <div v-else class="topics">
    <el-card v-for="t in topics" :key="t.slug" shadow="hover">
      <h3>{{ t.title }}</h3>
      <p>{{ t.summary }}</p>
      <span class="count">{{ t.room_count }} 个 Room</span>
    </el-card>
  </div>
</template>

<style scoped>
.topics { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 1rem; }
.count { color: #999; font-size: 0.85rem; }
</style>