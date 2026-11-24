<script setup lang="ts">
// 学院首页：静态品牌视觉 + 可解释的能力说明。不使用矩阵雨、解码文字、
// 点击火花等装饰动画；所有信息都对应真实功能，不展示假数据。
import { computed } from 'vue'
import { ArrowDown, ArrowRight, ArrowUpRight, BookOpen, Braces, Check, Flag, ScanLine } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import PublicShell from '@/components/layout/PublicShell.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const enter = computed(() => (auth.token ? '/learn' : '/login'))

const steps = [
  { title: '理解原理', text: '沿专题教程梳理知识，先知道为什么。' },
  { title: '动手验证', text: '在独立实验环境中把思路变成操作。' },
  { title: '完成挑战', text: '提交答案、记录进度，在竞赛中检验能力。' },
]

const methods = [
  { title: '先理解，再动手。', icon: BookOpen, text: '沿专题教程梳理知识，用文字与视频读懂问题背后的原理。每一步，都知道为什么。' },
  { title: '在真实环境中验证。', icon: ScanLine, text: '启动独立靶机，把思路落到实际操作中。从发现问题，到验证和解决问题。' },
  { title: '让每一次练习有积累。', icon: Flag, text: '提交答案、记录进度，在挑战与竞赛中检验所学，逐步建立自己的安全能力。' },
]

const paths = [
  { title: '专题教程', icon: BookOpen, description: '从知识点到完整路线，按自己的节奏持续学习。', to: '/learn' },
  { title: 'CTF 靶场', icon: ScanLine, description: '独立实验环境，把安全知识变成解题能力。', to: '/training?bank=ctf' },
  { title: 'ACM 编程', icon: Braces, description: '编写、提交、验证，在具体问题中打磨编程思路。', to: '/training?bank=acm' },
  { title: '竞赛实战', icon: Flag, description: '参加开放竞赛或邀请赛，迎接一场完整的实战挑战。', to: '/competitions' },
]

const questions = [
  { title: '刚开始学安全，应该从哪里开始？', text: '登录后进入专题教程，选择适合自己的学习路线。先理解知识点，再尝试关联的实验和题目，按自己的节奏逐步深入。' },
  { title: '练习需要自己搭建靶机吗？', text: '对于提供实验环境的题目，可以在题目页面启动独立靶机。具体的访问方式、实验说明和提交要求会在题目中展示。' },
  { title: '如何记录学习进度？', text: '平台会记录教程完成情况与题目提交结果。登录后可以在个人空间查看学习进度、积分和已获得的徽章。' },
]
</script>

<template>
  <PublicShell>
    <template #nav>
      <a href="#approach">学习方法</a>
      <a href="#paths">训练方式</a>
      <a href="#questions">常见问题</a>
    </template>
    <template #actions>
      <Button as-child variant="outline">
        <RouterLink :to="enter">
          {{ auth.token ? '进入学院' : '登录学院' }}
          <ArrowUpRight />
        </RouterLink>
      </Button>
    </template>

    <section class="landing-hero" aria-labelledby="landing-title">
      <div class="landing-hero__copy">
        <p class="landing-hero__badge">YAK ACADEMY · 安全实训学院</p>
        <h1 id="landing-title" class="landing-hero__title">
          从理解原理，<br>
          <span>到完成实战。</span>
        </h1>
        <p class="landing-hero__description">
          让安全学习真正发生。连接教程、靶场与竞赛，在每一次动手中建立解决问题的能力。
        </p>
        <div class="landing-hero__ctas">
          <Button size="lg" as-child>
            <RouterLink :to="enter">
              开始学习
              <ArrowRight />
            </RouterLink>
          </Button>
          <Button size="lg" variant="outline" as-child>
            <a href="#approach">
              了解学院
              <ArrowDown />
            </a>
          </Button>
        </div>
        <ul class="landing-hero__facts">
          <li><Check aria-hidden="true" />文字与视频教程</li>
          <li><Check aria-hidden="true" />独立实验环境</li>
          <li><Check aria-hidden="true" />学习进度记录</li>
        </ul>
      </div>

      <aside class="landing-hero__panel" aria-label="学习路径">
        <p class="landing-hero__panel-title">一条完整的学习路径</p>
        <ol class="landing-steps">
          <li v-for="(step, index) in steps" :key="step.title" class="landing-step">
            <span class="landing-step__index">0{{ index + 1 }}</span>
            <div>
              <strong class="landing-step__title">{{ step.title }}</strong>
              <p class="landing-step__text">{{ step.text }}</p>
            </div>
          </li>
        </ol>
      </aside>
    </section>

    <section id="approach" class="landing-section" aria-labelledby="approach-title">
      <header class="landing-section__head">
        <h2 id="approach-title" class="landing-section__title">知识，应该经得起实践。</h2>
        <p class="landing-section__note">把学习、验证和复盘连在一起，形成属于自己的能力。</p>
      </header>
      <div class="landing-methods">
        <article v-for="method in methods" :key="method.title" class="landing-method">
          <component :is="method.icon" class="landing-method__icon" aria-hidden="true" />
          <h3 class="landing-method__title">{{ method.title }}</h3>
          <p class="landing-method__text">{{ method.text }}</p>
        </article>
      </div>
    </section>

    <section id="paths" class="landing-section" aria-labelledby="paths-title">
      <header class="landing-section__head">
        <h2 id="paths-title" class="landing-section__title">从这里，找到你的下一步。</h2>
        <p class="landing-section__note">循序学习，或直接迎接挑战。选择适合你的训练方式。</p>
      </header>
      <div class="landing-paths">
        <RouterLink
          v-for="path in paths"
          :key="path.title"
          :to="auth.token ? path.to : { path: '/login', query: { redirect: path.to } }"
          class="landing-path"
        >
          <span class="landing-path__icon">
            <component :is="path.icon" aria-hidden="true" />
          </span>
          <span class="landing-path__body">
            <span class="landing-path__title">{{ path.title }}</span>
            <span class="landing-path__description">{{ path.description }}</span>
          </span>
          <ArrowUpRight class="landing-path__arrow" aria-hidden="true" />
        </RouterLink>
      </div>
    </section>

    <section id="questions" class="landing-section landing-faq" aria-labelledby="questions-title">
      <header class="landing-faq__head">
        <h2 id="questions-title" class="landing-section__title">开始之前，<br>你可能想了解。</h2>
      </header>
      <div class="landing-faq__list">
        <details v-for="question in questions" :key="question.title" class="landing-faq__item">
          <summary class="landing-faq__summary">
            {{ question.title }}
            <span class="landing-faq__mark" aria-hidden="true">+</span>
          </summary>
          <p class="landing-faq__text">{{ question.text }}</p>
        </details>
      </div>
    </section>

    <section class="landing-closing">
      <div>
        <h2 class="landing-closing__title">下一次突破，从动手开始。</h2>
        <p class="landing-closing__text">带着好奇心来，把解决问题的能力带走。</p>
      </div>
      <Button size="lg" as-child>
        <RouterLink :to="enter">
          进入学院
          <ArrowRight />
        </RouterLink>
      </Button>
    </section>
  </PublicShell>
</template>

<style scoped>
.landing-hero {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  align-items: center;
  gap: var(--space-12);
  padding-block: var(--space-16) var(--space-12);
}

.landing-hero__badge {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.landing-hero__title {
  margin-top: var(--space-4);
  color: var(--text-primary);
  font-size: var(--text-display);
  font-weight: 600;
  line-height: 1.2;
  letter-spacing: -0.01em;
  text-wrap: balance;
}

.landing-hero__title span {
  color: var(--action-primary);
}

.landing-hero__description {
  max-width: 52ch;
  margin-top: var(--space-5);
  color: var(--text-secondary);
  font-size: var(--text-section);
  line-height: var(--leading-relaxed);
}

.landing-hero__ctas {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-8);
}

.landing-hero__facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-5);
  margin-top: var(--space-8);
  padding: 0;
  list-style: none;
  color: var(--text-tertiary);
  font-size: var(--text-meta);
}

.landing-hero__facts li {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.landing-hero__facts :deep(svg) {
  width: 14px;
  height: 14px;
  color: var(--status-success);
}

.landing-hero__panel {
  padding: var(--space-6);
  border: 1px solid var(--border);
  border-radius: var(--radius-overlay);
  background: var(--bg-surface);
}

.landing-hero__panel-title {
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.landing-steps {
  display: grid;
  gap: var(--space-4);
  margin: var(--space-5) 0 0;
  padding: 0;
  list-style: none;
}

.landing-step {
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: var(--space-3);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}

.landing-step:first-child {
  padding-top: 0;
  border-top: 0;
}

.landing-step__index {
  color: var(--action-primary);
  font-size: var(--text-meta);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.landing-step__title {
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 600;
}

.landing-step__text {
  margin-top: var(--space-1);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-normal);
}

.landing-section {
  padding-block: var(--space-12);
  border-top: 1px solid var(--border);
}

.landing-section__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-8);
  flex-wrap: wrap;
  margin-bottom: var(--space-8);
}

.landing-section__title {
  color: var(--text-primary);
  font-size: var(--text-metric);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  text-wrap: balance;
}

.landing-section__note {
  max-width: 40ch;
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.landing-methods {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-4);
}

.landing-method {
  padding: var(--space-5);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.landing-method__icon {
  width: 22px;
  height: 22px;
  color: var(--action-primary);
}

.landing-method__title {
  margin-top: var(--space-6);
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.landing-method__text {
  margin-top: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.landing-paths {
  display: grid;
  border-top: 1px solid var(--border);
}

.landing-path {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr) 20px;
  align-items: center;
  gap: var(--space-4);
  padding-block: var(--space-5);
  border-bottom: 1px solid var(--border);
  transition: background-color 120ms ease;
}

.landing-path:hover {
  background: var(--bg-hover);
}

.landing-path__icon {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: var(--radius-control);
  background: var(--action-subtle);
  color: var(--action-subtle-fg);
}

.landing-path__icon :deep(svg) {
  width: 20px;
  height: 20px;
}

.landing-path__body {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.landing-path__title {
  color: var(--text-primary);
  font-size: var(--text-section);
  font-weight: 600;
}

.landing-path__description {
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

.landing-path__arrow {
  width: 18px;
  height: 18px;
  color: var(--text-tertiary);
}

.landing-faq {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
  gap: var(--space-12);
}

.landing-faq__list {
  border-top: 1px solid var(--border);
}

.landing-faq__item {
  border-bottom: 1px solid var(--border);
}

.landing-faq__summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding-block: var(--space-4);
  color: var(--text-primary);
  font-size: var(--text-body);
  font-weight: 500;
  list-style: none;
}

.landing-faq__summary::-webkit-details-marker {
  display: none;
}

.landing-faq__mark {
  color: var(--text-tertiary);
  font-size: var(--text-section);
  transition: transform 160ms ease;
}

.landing-faq__item[open] .landing-faq__mark {
  transform: rotate(45deg);
}

.landing-faq__text {
  padding: 0 0 var(--space-4);
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.landing-closing {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-8);
  flex-wrap: wrap;
  margin-top: var(--space-8);
  padding: var(--space-8);
  border: 1px solid var(--border);
  border-radius: var(--radius-overlay);
  background: var(--bg-surface);
}

.landing-closing__title {
  color: var(--text-primary);
  font-size: var(--text-metric);
  font-weight: 600;
  letter-spacing: 0;
  text-wrap: balance;
}

.landing-closing__text {
  margin-top: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--text-caption);
}

@media (max-width: 1023px) {
  .landing-hero {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-8);
    padding-block: var(--space-10);
  }

  .landing-hero__panel {
    max-width: 520px;
  }

  .landing-methods {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .landing-faq {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-6);
  }
}

@media (max-width: 767px) {
  .landing-hero__title {
    font-size: var(--text-display);
  }

  .landing-hero__description {
    font-size: var(--text-body);
  }

  .landing-section {
    padding-block: var(--space-8);
  }

  .landing-methods {
    grid-template-columns: minmax(0, 1fr);
  }

  .landing-closing {
    padding: var(--space-5);
  }

  .landing-path {
    grid-template-columns: 36px minmax(0, 1fr);
  }

  .landing-path__arrow {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .landing-path,
  .landing-faq__mark {
    transition: none;
  }
}
</style>
