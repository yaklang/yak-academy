<script setup lang="ts">
// 主题详情页：Room 列表（教程正文入口）。普通端与管理端共用。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { topicApi, type TopicDetail } from '../api'

const route = useRoute()
const topic = ref<TopicDetail | null>(null)
const loading = ref(true)
// 普通端 /topics/:slug/rooms/:slug；管理端 /admin/tutorials/...。
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
    <header class="head">
      <h2 class="page-title">{{ topic.title }}</h2>
      <p class="summary">{{ topic.summary }}</p>
    </header>
    <el-empty v-if="!loading && topic.rooms.length === 0" description="该主题暂无 Room" />
    <div v-else class="rooms">
      <router-link
        v-for="(r, i) in topic.rooms"
        :key="r.slug"
        :to="`${base}/${topicSlug}/rooms/${r.slug}`"
        class="room-link"
      >
        <div class="room-card tall-card">
          <span class="index">{{ String(i + 1).padStart(2, '0') }}</span>
          <div class="room-head">
            <h3>{{ r.title }}</h3>
            <el-tag size="small" effect="plain">{{ r.difficulty }}</el-tag>
          </div>
          <span class="enter">进入学习 →</span>
        </div>
      </router-link>
    </div>
  </template>
</template>

<style scoped>
.head { margin-bottom: 1.5rem; }
.summary { color: var(--el-text-color-secondary); margin: 0.3rem 0 0; }
.rooms { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 1rem; }
.room-link { text-decoration: none; color: inherit; }
.room-card {
  background: var(--el-bg-color-overlay);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 1rem 1.1rem 0.9rem;
  position: relative;
}
.index {
  position: absolute; top: 0.7rem; right: 0.9rem;
  font-family: Consolas, monospace; font-size: 1.4rem; font-weight: 700;
  color: var(--el-color-primary-light-7);
}
.room-head { display: flex; align-items: center; gap: 0.6rem; padding-right: 2.6rem; }
.room-head h3 { margin: 0; font-size: 0.98rem; }
.enter { margin-top: auto; color: var(--el-color-primary); font-size: 0.85rem; opacity: 0.9; }
</style>