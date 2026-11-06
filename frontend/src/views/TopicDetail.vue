<script setup lang="ts">
// 主题详情页：上方学习路线（横向节点导航），下方具体专题（纵向树形节点）。
// 普通端与管理端共用；题目是独立资源，Room 详情页再给配套题目入口。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowRight } from '@lucide/vue'
import { DIFFICULTY_LABELS, topicApi, type TopicDetail } from '../api'

const route = useRoute()
const topic = ref<TopicDetail | null>(null)
const loading = ref(true)
const loadError = ref('')

// 普通端 /topics/:slug/rooms/:slug；管理端 /admin/tutorials/...。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))
const topicSlug = computed(() => route.params.topicSlug as string)

const roomCount = computed(() => topic.value?.rooms.length ?? 0)
const difficultyRange = computed(() => {
  const rooms = topic.value?.rooms ?? []
  if (rooms.length === 0) return ''
  const levels = ['apprentice', 'practitioner', 'expert'] as const
  const present = levels.filter((level) => rooms.some((r) => r.difficulty === level))
  if (present.length === 1) return DIFFICULTY_LABELS[present[0]]
  return `${DIFFICULTY_LABELS[present[0]]} → ${DIFFICULTY_LABELS[present[present.length - 1]]}`
})

const pad = (n: number) => String(n).padStart(2, '0')

// 路线轨道需要覆盖全部节点（移动端横向滚动时连接线不能提前截断）。
const routeMinWidth = computed(
  () => `max(100%, calc(${Math.max(roomCount.value, 1)} * (180px + var(--space-4))))`,
)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    topic.value = await topicApi.rooms(topicSlug.value)
  } catch (e) {
    topic.value = null
    loadError.value = e instanceof Error ? e.message : '专题加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="topic-skeleton" role="status" aria-label="正在加载专题">
    <div class="sk-head" aria-hidden="true">
      <i class="sk-line short" />
      <i class="sk-line title" />
      <i class="sk-line medium" />
    </div>
    <div class="sk-route" aria-hidden="true">
      <div v-for="n in 5" :key="n" class="sk-stop">
        <i class="sk-line short" />
        <i class="sk-line" />
        <i class="sk-line short" />
      </div>
    </div>
    <div class="sk-tree" aria-hidden="true">
      <div v-for="n in 4" :key="n" class="sk-room">
        <i class="sk-node" />
        <div class="sk-room-card">
          <i class="sk-line" />
          <i class="sk-line short" />
        </div>
      </div>
    </div>
  </div>
  <UiLoadError v-else-if="loadError" :message="loadError" @retry="load" />
  <template v-else-if="topic">
    <header class="page-head">
      <div>
        <p class="route-eyebrow">LEARNING ROUTE · 学习路线</p>
        <h2 class="page-title">{{ topic.title }}</h2>
        <p v-if="topic.summary" class="page-sub">{{ topic.summary }}</p>
      </div>
      <div class="page-actions route-meta">
        <span><b class="mono">{{ roomCount }}</b> 个 Room</span>
        <span v-if="difficultyRange">{{ difficultyRange }}</span>
      </div>
    </header>

    <UiEmpty v-if="roomCount === 0" description="该主题暂无 Room" />

    <template v-else>
      <section class="route-section" aria-labelledby="route-title">
        <div class="section-head">
          <h3 id="route-title">学习路线</h3>
          <span class="section-hint">按顺序完成 {{ roomCount }} 个学习节点，点击节点跳转到对应专题</span>
        </div>
        <nav class="route-map" aria-label="学习路线节点">
          <ol :style="{ minWidth: routeMinWidth }">
            <li v-for="(r, i) in topic.rooms" :key="r.slug">
              <a :href="`#room-${r.slug}`" class="route-stop">
                <span class="route-index mono">{{ pad(i + 1) }}</span>
                <span class="route-name">{{ r.title }}</span>
                <span class="route-difficulty">{{ DIFFICULTY_LABELS[r.difficulty] }}</span>
              </a>
            </li>
          </ol>
        </nav>
      </section>

      <section class="subjects-section" aria-labelledby="subjects-title">
        <div class="section-head">
          <h3 id="subjects-title">具体专题</h3>
          <span class="section-hint">共 {{ roomCount }} 个学习节点</span>
        </div>
        <div class="room-tree">
          <router-link
            v-for="(r, i) in topic.rooms"
            :id="`room-${r.slug}`"
            :key="r.slug"
            :to="`${base}/${topicSlug}/rooms/${r.slug}`"
            class="tree-item"
          >
            <span class="tree-node mono" aria-hidden="true">{{ pad(i + 1) }}</span>
            <div class="room-card">
              <div class="room-head">
                <h4>{{ r.title }}</h4>
                <UiTag size="small" effect="plain">{{ DIFFICULTY_LABELS[r.difficulty] }}</UiTag>
              </div>
              <span class="enter">进入学习 <ArrowRight :size="14" aria-hidden="true" /></span>
            </div>
          </router-link>
        </div>
      </section>
    </template>
  </template>
</template>

<style scoped>
.route-eyebrow {
  margin: 0 0 var(--space-2);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 0.12em;
}
.route-meta {
  align-items: baseline;
  gap: var(--space-3);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
}
.route-meta .mono { color: var(--foreground); }
.topic-skeleton { max-width: 1040px; }
.sk-head { display: grid; gap: var(--space-3); margin-bottom: var(--space-8); }
.sk-line { display: block; width: 100%; height: 12px; border-radius: 4px; background: var(--muted); }
.sk-line.short { width: 22%; }
.sk-line.medium { width: 58%; }
.sk-line.title { width: 38%; height: 26px; }
.sk-route {
  display: flex;
  gap: var(--space-4);
  padding-top: var(--space-4);
  margin-bottom: var(--space-8);
  border-top: 1px solid var(--border);
  overflow: hidden;
}
.sk-stop { display: grid; flex: 1 0 160px; gap: var(--space-2); }
.sk-tree { display: grid; gap: var(--space-4); }
.sk-room { display: grid; grid-template-columns: var(--space-10) minmax(0, 1fr); align-items: center; gap: var(--space-4); }
.sk-node { display: block; width: var(--space-10); height: var(--space-10); border-radius: 999px; background: var(--muted); }
.sk-room-card {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.sk-room-card .sk-line:first-child { width: 46%; }
.sk-room-card .sk-line:last-child { width: 24%; }
.topic-skeleton .sk-line,
.topic-skeleton .sk-node { animation: topic-skeleton-pulse 1.4s ease-in-out infinite; }
@keyframes topic-skeleton-pulse { 50% { opacity: .45; } }
.section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}
.section-head h3 {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-lg);
  font-weight: 600;
  letter-spacing: 0;
}
.section-hint {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-relaxed);
}
.route-section { margin-bottom: var(--space-8); }
.route-map { overflow-x: auto; }
.route-map ol {
  position: relative;
  display: flex;
  gap: var(--space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}
.route-map ol::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: var(--border);
}
.route-map li { flex: 1 0 180px; min-width: 180px; }
.route-stop {
  position: relative;
  display: block;
  padding-top: var(--space-5);
  border-radius: var(--radius-control);
}
.route-stop::before {
  content: '';
  position: absolute;
  top: calc(var(--space-1) * -1);
  left: 0;
  width: var(--space-2);
  height: var(--space-2);
  border-radius: 999px;
  background: var(--accent-foreground);
}
.route-stop:hover .route-name,
.route-stop:focus-visible .route-name { color: var(--accent-foreground); }
.route-index {
  display: block;
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
}
.route-name {
  display: block;
  margin-top: var(--space-1);
  color: var(--foreground);
  font-size: var(--text-sm);
  font-weight: 600;
  line-height: var(--leading-normal);
}
.route-difficulty {
  display: block;
  margin-top: var(--space-1);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
}
.room-tree {
  position: relative;
  display: grid;
  gap: var(--space-4);
}
.room-tree::before {
  content: '';
  position: absolute;
  top: var(--space-5);
  bottom: var(--space-5);
  left: calc(var(--space-10) / 2);
  width: 1px;
  background: var(--border);
}
.tree-item {
  position: relative;
  display: grid;
  grid-template-columns: var(--space-10) minmax(0, 1fr);
  align-items: center;
  gap: var(--space-4);
  scroll-margin-top: calc(var(--space-16) + var(--space-6));
}
.tree-item:focus-visible { outline: none; }
.tree-item:focus-visible .room-card {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
.tree-node {
  position: relative;
  z-index: 1;
  display: grid;
  width: var(--space-10);
  height: var(--space-10);
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--card);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
}
.tree-node::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 100%;
  width: var(--space-4);
  height: 1px;
  background: var(--border);
}
.room-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--space-4);
  min-height: 72px;
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
  transition: border-color 150ms ease, background-color 150ms ease;
}
.tree-item:hover .room-card { border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); }
.room-head {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: var(--space-3);
  flex-wrap: wrap;
  min-width: 0;
}
.room-head h4 {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-md);
  font-weight: 600;
  line-height: var(--leading-normal);
}
.enter {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  margin-top: 0;
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 500;
  white-space: nowrap;
}

@media (max-width: 720px) {
  .tree-item {
    grid-template-columns: var(--space-8) minmax(0, 1fr);
    gap: var(--space-3);
    scroll-margin-top: calc(var(--space-16) + var(--space-10));
  }
  .room-tree::before { left: calc(var(--space-8) / 2); }
  .tree-node {
    width: var(--space-8);
    height: var(--space-8);
  }
  .tree-node::after { width: var(--space-3); }
  .room-card { padding: var(--space-4); }
}
@media (max-width: 560px) {
  .room-card {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }
  .enter { justify-self: start; }
}
</style>
