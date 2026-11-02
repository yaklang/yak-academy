<script setup lang="ts">
// 教程板块首页：专题列表，卡片可点击进入专题详情（Room 列表）。
// 在普通端与管理端共用（管理端路由前缀不同，链接按当前路由推导）。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowRight } from '@lucide/vue'
import { topicApi, type Topic } from '../api'
import { ApiError } from '../api/client'
import { Button } from '@/components/ui/button'

const route = useRoute()
// 普通端 /topics/:slug；管理端 /admin/tutorials/:slug。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))
const isAdmin = computed(() => route.path.startsWith('/admin'))

const topics = ref<Topic[]>([])
const loading = ref(true)
const empty = ref(false)
const loadError = ref('')
const totalRooms = computed(() => topics.value.reduce((sum, t) => sum + t.room_count, 0))

async function load() {
  loading.value = true
  loadError.value = ''
  empty.value = false
  try {
    const page = await topicApi.list()
    topics.value = page.items
    empty.value = page.items.length === 0
  } catch (e) {
    // 501 = 教程功能未装配，按空态处理；其他错误必须让用户看见并可重试，
    // 不能伪装成“暂无内容”。
    if (e instanceof ApiError && e.status === 501) {
      empty.value = true
    } else {
      loadError.value = e instanceof Error ? e.message : '专题加载失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">学习<span class="accent">路线</span></h2>
      <p class="page-sub">
        {{ isAdmin ? '管理端视图：专题与 Room 结构同学生端，内容管理在控制面板。' : '按专题循序渐进：每个专题由多篇教程 Room 组成，读完即可上手配套题目。' }}
      </p>
    </div>
    <div v-if="!loading && topics.length" class="topic-stats" aria-label="专题统计">
      <span><b>{{ topics.length }}</b> 个专题</span>
      <span><b>{{ totalRooms }}</b> 个 Room</span>
    </div>
  </header>

  <UiSkeleton v-if="loading" :rows="3" animated />
  <div v-else-if="loadError" class="load-error" role="alert">
    <p>{{ loadError }}</p>
    <Button variant="outline" size="sm" @click="load">重试</Button>
  </div>
  <UiEmpty v-else-if="empty" description="暂无内容：等待管理员上传内容包" />
  <div v-else class="card-grid topics">
    <router-link v-for="(t, i) in topics" :key="t.slug" :to="`${base}/${t.slug}`" class="topic-link">
      <!-- 卡片容器必须是普通 div：article 会成为独立地标，链接无法从
           后代文本计算可访问名，e2e 与读屏都拿不到「Web 安全」这类链接名。 -->
      <div class="topic-card tall-card">
        <div class="topic-top">
          <span class="topic-index mono">{{ String(i + 1).padStart(2, '0') }}</span>
          <span class="topic-kind">专题</span>
        </div>
        <h3>{{ t.title }}</h3>
        <p v-if="t.summary" class="summary">{{ t.summary }}</p>
        <div class="card-meta">
          <span class="mono"><span class="accent">#</span> {{ t.room_count }} 个 Room</span>
          <span class="go">进入专题 <ArrowRight :size="14" aria-hidden="true" /></span>
        </div>
      </div>
    </router-link>
  </div>
</template>

<style scoped>
.topics { margin-top: 0; }
.topic-stats {
  display: flex;
  align-items: baseline;
  gap: var(--space-5);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
}
.topic-stats b {
  margin-right: var(--space-1);
  color: var(--foreground);
  font-size: var(--text-md);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.topic-link { display: block; min-width: 0; text-decoration: none; color: inherit; }
.topic-card {
  padding: var(--space-5);
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
}
.topic-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.topic-index {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
}
.topic-kind {
  padding: 2px 8px;
  border: 1px solid color-mix(in srgb, var(--primary) 22%, var(--border));
  border-radius: 999px;
  background: var(--accent);
  color: var(--accent-foreground);
  font-size: 11px;
  line-height: 1.5;
}
.topic-card h3 {
  margin: 0 0 var(--space-3);
  color: var(--foreground);
  font-size: var(--text-lg);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
}
.summary {
  display: -webkit-box;
  margin: 0;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.card-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-top: auto;
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
}
.accent { color: var(--accent-foreground); }
.go {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--accent-foreground);
  font-weight: 500;
  white-space: nowrap;
}
.load-error {
  display: grid;
  justify-items: center;
  gap: var(--space-3);
  padding: var(--space-12) var(--space-6);
  border: 1px dashed var(--border);
  border-radius: var(--radius-card);
  color: var(--muted-foreground);
  text-align: center;
}
.load-error p { margin: 0; font-size: var(--text-sm); }

@media (max-width: 720px) {
  .topic-stats { width: 100%; }
}
</style>
