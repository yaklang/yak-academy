<script setup lang="ts">
// 教程板块首页（学生端为仪表盘式）：欢迎 hero（ReactBits AuroraBlur 风格极光
// 背景 + 学习进度）→ 学习路线专题网格 → 训练入口速达卡。
// 管理端共用本视图（/admin/tutorials）：只渲染专题列表，不加 hero，
// 内容管理入口在管理端外壳的导航里。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowRight } from '@lucide/vue'
import { topicApi, progressApi, BANK_LABELS, type Topic, type ChallengeBank } from '../api'
import { ApiError } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { Button } from '@/components/ui/button'
import AuroraBlur from '@/components/AuroraBlur.vue'

const route = useRoute()
// 普通端 /topics/:slug；管理端 /admin/tutorials/:slug。
const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))
const isAdmin = computed(() => route.path.startsWith('/admin'))
const auth = useAuthStore()

const topics = ref<Topic[]>([])
const loading = ref(true)
const empty = ref(false)
const loadError = ref('')
const totalRooms = computed(() => topics.value.reduce((sum, t) => sum + t.room_count, 0))

// hero 只在学生端出现；进度拉取失败时整行隐藏，不影响布局。
const progress = ref<{ xp: number; completed_rooms: number; in_progress_rooms: number } | null>(null)

// 训练入口速达卡：深链到题库对应 tab，标签与题库页共用同一映射避免漂移。
const quickBanks: Array<{ bank: ChallengeBank; desc: string }> = [
  { bank: 'tutorial', desc: '教程配套练习，读完 Room 即刻上手' },
  { bank: 'ctf', desc: '独立夺旗挑战，按类型与难度筛选' },
  { bank: 'acm', desc: '算法题目训练，在线判题即时反馈' },
]

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

onMounted(async () => {
  load()
  if (!isAdmin.value && auth.token) {
    try {
      progress.value = await progressApi.mine()
    } catch {
      // 进度拉取失败不阻断首页
    }
  }
})
</script>

<template>
  <!-- 学生端欢迎 hero：参考 CloudCTF 仪表盘的引导式首页，管理端不渲染 -->
  <section v-if="!isAdmin" class="learn-hero">
    <AuroraBlur />
    <div class="hero-copy">
      <p class="hero-eyebrow">YAK ACADEMY · 学习路线</p>
      <h1 class="hero-title">欢迎回来<span v-if="auth.user?.username">，{{ auth.user.username }}</span></h1>
      <p class="hero-sub">按专题循序渐进：每个专题由多篇教程 Room 组成，读完即可上手配套题目。</p>
      <div class="hero-actions">
        <a class="hero-cta primary" href="#topics">进入学习路线 <ArrowRight :size="15" aria-hidden="true" /></a>
        <router-link class="hero-cta" to="/training">浏览题库</router-link>
      </div>
      <ul v-if="progress" class="hero-stats">
        <li><b class="mono">{{ progress.xp }}</b><span>XP</span></li>
        <li><b class="mono">{{ progress.completed_rooms }}</b><span>已完成 Room</span></li>
        <li v-if="progress.in_progress_rooms"><b class="mono">{{ progress.in_progress_rooms }}</b><span>进行中</span></li>
      </ul>
    </div>
    <div class="hero-orbit" aria-hidden="true">
      <span class="ring r1"></span>
      <span class="ring r2"></span>
      <span class="ring r3"></span>
      <span class="core mono">YAK</span>
    </div>
  </section>

  <header id="topics" class="page-head section-head">
    <div>
      <h2 class="page-title">学习<span class="accent">路线</span></h2>
      <p class="page-sub">
        {{ isAdmin ? '管理端视图：专题与 Room 结构同学生端，内容管理在顶部导航。' : '挑一个专题，从第一篇 Room 开始。' }}
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

  <!-- 训练入口：把题库三个 tab 提升为首页速达卡（学生端） -->
  <section v-if="!isAdmin && !loading && !loadError" class="quick-section" aria-label="训练入口">
    <header class="quick-head">
      <h2 class="quick-title">训练入口</h2>
      <router-link to="/training" class="quick-more">查看全部题库 <ArrowRight :size="14" aria-hidden="true" /></router-link>
    </header>
    <div class="quick-grid">
      <router-link v-for="q in quickBanks" :key="q.bank" :to="`/training?bank=${q.bank}`" class="quick-card">
        <span class="quick-kind mono">{{ String(quickBanks.indexOf(q) + 1).padStart(2, '0') }}</span>
        <h3>{{ BANK_LABELS[q.bank] }}</h3>
        <p>{{ q.desc }}</p>
        <span class="go">进入题库 <ArrowRight :size="14" aria-hidden="true" /></span>
      </router-link>
    </div>
  </section>
</template>

<style scoped>
/* ===== 欢迎 hero ===== */
.learn-hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr);
  align-items: center;
  gap: var(--space-6);
  overflow: hidden;
  margin-bottom: var(--space-10);
  padding: var(--space-10) var(--space-8);
  border: 1px solid var(--border);
  border-radius: var(--radius-panel);
  background: var(--panel);
}
.hero-copy { position: relative; min-width: 0; }
.hero-eyebrow {
  margin: 0 0 var(--space-3);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 0.14em;
}
.hero-title {
  margin: 0 0 var(--space-3);
  color: var(--foreground);
  font-size: var(--text-3xl);
  font-weight: 650;
  line-height: var(--leading-tight);
  /* 中文标题字距为 0（frontend-style skill：仅大号拉丁标题允许负字距） */
  letter-spacing: 0;
  overflow-wrap: anywhere;
  text-wrap: balance;
}
.hero-sub {
  max-width: 560px;
  margin: 0 0 var(--space-6);
  color: var(--muted-foreground);
  font-size: var(--text-md);
  line-height: var(--leading-relaxed);
  text-wrap: pretty;
}
.hero-actions { display: flex; flex-wrap: wrap; gap: var(--space-3); }
.hero-cta {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-height: 40px;
  padding: 0 var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  color: var(--foreground);
  font-size: var(--text-base);
  font-weight: 500;
  text-decoration: none;
  transition: border-color 150ms ease, background-color 150ms ease;
}
.hero-cta:hover { border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); }
.hero-cta.primary {
  border-color: transparent;
  background: var(--primary);
  color: var(--primary-foreground);
}
.hero-cta.primary:hover { background: color-mix(in srgb, var(--primary) 88%, black); }
.hero-cta:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.hero-stats {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-6);
  margin: var(--space-6) 0 0;
  padding: 0;
  list-style: none;
}
.hero-stats li { display: grid; gap: 2px; }
.hero-stats b {
  color: var(--foreground);
  font-size: var(--text-xl);
  font-weight: 650;
  line-height: var(--leading-tight);
}
.hero-stats span { color: var(--muted-foreground); font-size: var(--text-xs); }

/* 右侧轨道徽记：同心环 + YAK 核心，慢速旋转的装饰 */
.hero-orbit {
  position: relative;
  justify-self: center;
  width: min(220px, 100%);
  aspect-ratio: 1;
}
.hero-orbit .ring {
  position: absolute;
  border: 1px solid color-mix(in srgb, var(--primary) 30%, var(--border));
  border-radius: 50%;
}
.hero-orbit .r1 { inset: 0; border-style: dashed; animation: orbit-spin 60s linear infinite; }
.hero-orbit .r2 { inset: 14%; border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); }
.hero-orbit .r3 { inset: 30%; border-style: dashed; animation: orbit-spin 40s linear infinite reverse; }
.hero-orbit .core {
  position: absolute;
  inset: 38%;
  display: grid;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--primary) 40%, var(--border));
  border-radius: 50%;
  background: color-mix(in srgb, var(--primary) 10%, var(--card));
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 700;
  letter-spacing: 0.12em;
}
@keyframes orbit-spin { to { transform: rotate(360deg); } }

.section-head { scroll-margin-top: 96px; }
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
  background: var(--panel);
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
  font-size: var(--text-base);
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

/* ===== 训练入口 ===== */
.quick-section { margin-top: var(--space-10); }
.quick-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
}
.quick-title {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-xl);
  font-weight: 600;
  letter-spacing: 0;
}
.quick-more {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  text-decoration: none;
}
.quick-more:hover { color: var(--accent-foreground); }
.quick-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-5);
}
.quick-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--card);
  color: inherit;
  text-decoration: none;
  transition: border-color 150ms ease;
}
.quick-card:hover { border-color: color-mix(in srgb, var(--primary) 45%, var(--border)); }
.quick-card:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.quick-kind {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
}
.quick-card h3 {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-md);
  font-weight: 600;
}
.quick-card p {
  margin: 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.quick-card .go { margin-top: var(--space-3); font-size: var(--text-xs); }

@media (max-width: 900px) {
  .learn-hero { grid-template-columns: minmax(0, 1fr); padding: var(--space-8) var(--space-6); }
  .hero-orbit { display: none; }
}
@media (max-width: 720px) {
  .topic-stats { width: 100%; }
  .quick-grid { grid-template-columns: minmax(0, 1fr); }
  .hero-title { font-size: var(--text-2xl); }
}
@media (prefers-reduced-motion: reduce) {
  .hero-orbit .ring { animation: none; }
}
</style>
