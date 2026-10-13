<script setup lang="ts">
// 教程板块首页：主题列表，卡片可点击进入主题详情（Room 列表）。
// 在普通端与管理端共用（管理端路由前缀不同，链接按当前路由推导）。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { topicApi, type Topic } from '../api'
import { ApiError } from '../api/client'

const route = useRoute()
// 普通端 /topics/:slug；管理端 /admin/tutorials/:slug。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))

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
  <h2>教程</h2>
  <el-skeleton v-if="loading" :rows="3" animated />
  <el-empty v-else-if="empty" description="暂无内容：等待管理员上传内容包" />
  <div v-else class="topics">
    <router-link v-for="t in topics" :key="t.slug" :to="`${base}/${t.slug}`" class="card-link">
      <el-card shadow="hover">
        <h3>{{ t.title }}</h3>
        <p>{{ t.summary }}</p>
        <span class="count">{{ t.room_count }} 个 Room</span>
      </el-card>
    </router-link>
  </div>
</template>

<style scoped>
.topics { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 1rem; }
.card-link { text-decoration: none; color: inherit; }
.count { color: var(--el-text-color-secondary); font-size: 0.85rem; }
</style>