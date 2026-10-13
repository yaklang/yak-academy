<script setup lang="ts">
// 主题详情页：Room 列表（教程正文入口）。普通端与管理端共用。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { topicApi, type TopicDetail } from '../api'

const route = useRoute()
const topic = ref<TopicDetail | null>(null)
const loading = ref(true)
// 普通端 /topics/:slug/rooms/:roomSlug；管理端 /admin/tutorials/...。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))
const topicSlug = computed(() => route.params.topicSlug as string)

onMounted(async () => {
  try {
    topic.value = await topicApi.rooms(topicSlug.value)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <template v-if="topic">
    <div class="head">
      <h2>{{ topic.title }}</h2>
      <p class="summary">{{ topic.summary }}</p>
    </div>
    <el-empty v-if="!loading && topic.rooms.length === 0" description="该主题暂无 Room" />
    <div v-else class="rooms">
      <router-link
        v-for="r in topic.rooms"
        :key="r.slug"
        :to="`${base}/${topicSlug}/rooms/${r.slug}`"
        class="card-link"
      >
        <el-card shadow="hover">
          <div class="room-head">
            <h3>{{ r.title }}</h3>
            <el-tag size="small">{{ r.difficulty }}</el-tag>
          </div>
          <span class="enter">进入学习 →</span>
        </el-card>
      </router-link>
    </div>
  </template>
</template>

<style scoped>
.head { margin-bottom: 1.2rem; }
.summary { color: var(--el-text-color-secondary); margin: 0.3rem 0 0; }
.rooms { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 1rem; }
.card-link { text-decoration: none; color: inherit; }
.room-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; }
.enter { color: var(--el-color-primary); font-size: 0.85rem; }
</style>