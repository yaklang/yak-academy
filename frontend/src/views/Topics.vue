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
const isAdmin = computed(() => route.path.startsWith('/admin'))

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

// 卡片头部的几何纹样背景按 slug 稳定取一个变体，纯装饰。
const patternIdx = (slug: string) => [...slug].reduce((a, c) => a + c.charCodeAt(0), 0) % 3
</script>

<template>
  <header class="page-head">
    <h2 class="page-title">学习<span class="accent">路线</span></h2>
    <p class="page-sub">
      {{ isAdmin ? '管理端视图：专题与 Room 结构同学生端，内容管理在控制面板。' : '按专题循序渐进：每个专题由多篇教程 Room 组成，读完即可上手配套题目。' }}
    </p>
  </header>

  <el-skeleton v-if="loading" :rows="3" animated />
  <el-empty v-else-if="empty" description="暂无内容：等待管理员上传内容包" />
  <div v-else class="card-grid topics">
    <router-link v-for="t in topics" :key="t.slug" :to="`${base}/${t.slug}`" class="topic-link">
      <div class="topic-card tall-card" :class="`pattern-${patternIdx(t.slug)}`">
        <div class="topic-art" aria-hidden="true" />
        <div class="topic-body">
          <h3>{{ t.title }}</h3>
          <p class="summary">{{ t.summary }}</p>
        </div>
        <div class="card-meta">
          <span class="mono"><span class="accent">#</span> {{ t.room_count }} 个 Room</span>
          <span class="go">进入 →</span>
        </div>
      </div>
    </router-link>
  </div>
</template>

<style scoped>
.page-head { margin-bottom: 1.75rem; }
.topics { margin-top: 0; }
.topic-link { text-decoration: none; color: inherit; }
.topic-card {
  background: var(--el-bg-color-overlay);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  overflow: hidden;
}
.topic-card:hover { border-color: var(--el-color-primary-light-5); }
.topic-art {
  height: 84px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  opacity: 0.9;
}
/* 三个稳定变体：绿系格点 / 斜纹 / 网格，纯 CSS 装饰 */
.pattern-0 .topic-art {
  background:
    radial-gradient(rgba(0, 230, 118, 0.16) 1.5px, transparent 1.5px),
    linear-gradient(135deg, rgba(0, 230, 118, 0.08), transparent 60%);
  background-size: 14px 14px, 100% 100%;
}
.pattern-1 .topic-art {
  background:
    repeating-linear-gradient(45deg, rgba(0, 230, 118, 0.07) 0 10px, transparent 10px 20px),
    linear-gradient(160deg, rgba(0, 230, 118, 0.1), transparent 65%);
}
.pattern-2 .topic-art {
  background:
    linear-gradient(rgba(0, 230, 118, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(0, 230, 118, 0.05) 1px, transparent 1px),
    linear-gradient(200deg, rgba(0, 230, 118, 0.09), transparent 60%);
  background-size: 18px 18px, 18px 18px, 100% 100%;
}
.topic-body { padding: 1rem 1.1rem 0.2rem; }
.topic-body h3 { margin: 0 0 0.45rem; font-size: 1.02rem; color: var(--el-text-color-primary); }
.summary {
  margin: 0; font-size: 0.87rem; line-height: 1.6;
  color: var(--el-text-color-secondary);
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.card-meta { padding: 0.8rem 1.1rem 1rem; }
.accent { color: var(--el-color-primary); }
.go { margin-left: auto; color: var(--el-color-primary); font-size: 0.85rem; opacity: 0; transition: opacity 0.15s ease; }
.topic-card:hover .go { opacity: 1; }
</style>