<script setup lang="ts">
import { ArrowDown, ArrowRight, ArrowUpRight, BookOpen, Braces, Check, Flag, Plus, ScanLine, Terminal } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import Brand from '@/components/Brand.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import Reveal from '@/components/Reveal.vue'
import TerminalRain from '@/components/TerminalRain.vue'
import CipherReveal from '@/components/CipherReveal.vue'
import ClickSpark from '@/components/ClickSpark.vue'
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
const auth = useAuthStore()
const enter = computed(() => auth.token ? '/learn' : '/login')

const methods = [
  { title: '先理解，再动手。', icon: BookOpen, text: '沿专题教程梳理知识，用文字与视频读懂问题背后的原理。每一步，都知道为什么。' },
  { title: '在真实环境中验证。', icon: ScanLine, text: '启动独立靶机，把思路落到实际操作中。从发现问题，到验证和解决问题。' },
  { title: '让每一次练习有积累。', icon: Flag, text: '提交答案、记录进度，在挑战与竞赛中检验所学，逐步建立自己的安全能力。' },
]
const paths = [
  { title: '专题教程', english: 'GUIDED LEARNING', icon: BookOpen, description: '从知识点到完整路线，按自己的节奏持续学习。', to: '/learn' },
  { title: 'CTF 靶场', english: 'HANDS-ON LABS', icon: ScanLine, description: '独立实验环境，把安全知识变成解题能力。', to: '/training?bank=ctf' },
  { title: 'ACM 编程', english: 'CODE & SOLVE', icon: Braces, description: '编写、提交、验证，在具体问题中打磨编程思路。', to: '/training?bank=acm' },
  { title: '竞赛实战', english: 'TAKE THE CHALLENGE', icon: Flag, description: '参加开放竞赛或邀请赛，迎接一场完整的实战挑战。', to: '/competitions' },
]
const questions = [
  { title: '刚开始学安全，应该从哪里开始？', text: '登录后进入专题教程，选择适合自己的学习路线。先理解知识点，再尝试关联的实验和题目，按自己的节奏逐步深入。' },
  { title: '练习需要自己搭建靶机吗？', text: '对于提供实验环境的题目，可以在题目页面启动独立靶机。具体的访问方式、实验说明和提交要求会在题目中展示。' },
  { title: '如何记录学习进度？', text: '平台会记录教程完成情况与题目提交结果。登录后可以在个人空间查看学习进度、积分和已获得的徽章。' },
]
const stats = [
  { value: '4', label: '训练方式' },
  { value: '∞', label: '练习次数' },
  { value: '24/7', label: '靶机随启随用' },
  { value: '100%', label: '动手实练' },
]
function scrollTo(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' })
}
</script>

<template>
  <div class="public-container">
    <header class="public-header">
      <Brand />
      <nav class="public-nav" aria-label="学院介绍"><a href="#approach" @click.prevent="scrollTo('approach')">学习方法</a><a href="#paths" @click.prevent="scrollTo('paths')">训练方式</a><a href="#questions" @click.prevent="scrollTo('questions')">常见问题</a></nav>
      <div class="public-actions"><ThemeToggle /><Button as-child variant="outline"><RouterLink :to="enter">{{ auth.token ? '进入学院' : '登录学院' }}<ArrowUpRight class="size-4" /></RouterLink></Button></div>
    </header>
    <main>
      <section class="public-hero hero-terminal" aria-labelledby="landing-title">
        <TerminalRain class="hero-rain" />
        <Reveal class="hero-copy"><p class="eyebrow"><Terminal :size="14" />YAK 安全实训学院</p><h1 id="landing-title" class="hero-title"><CipherReveal text="从理解原理，" /><br><em><CipherReveal text="到完成实战。" :duration="700" /></em></h1><p class="hero-description">让安全学习，真正发生。<br>连接教程、靶场与竞赛，在每一次动手中建立解决问题的能力。</p><div class="hero-ctas"><Button as-child><RouterLink :to="enter">开始学习<ArrowRight class="size-4" /></RouterLink></Button><Button variant="outline" @click="scrollTo('approach')">了解学院<ArrowDown class="size-4" /></Button></div><div class="hero-footnote"><span><Check :size="14" />文字与视频</span><span><Check :size="14" />独立实验环境</span><span><Check :size="14" />学习进度记录</span></div></Reveal>
        <Reveal class="hero-figure" :delay="100"><div class="hero-signal"><span class="signal-orbit orbit-one" /><span class="signal-orbit orbit-two" /><span class="signal-emblem">Y<span>↗</span></span><div class="signal-label"><span class="signal-light" />YOUR NEXT LEVEL STARTS HERE</div></div><div class="hero-capabilities"><span><BookOpen :size="16" />理解原理</span><span><ScanLine :size="16" />动手验证</span><span><Flag :size="16" />完成挑战</span></div></Reveal>
      </section>
      <section class="hero-stats" aria-label="平台能力">
        <div v-for="stat in stats" :key="stat.label" class="stat-item"><strong>{{ stat.value }}</strong><span>{{ stat.label }}</span></div>
      </section>
      <section id="approach" class="public-section" aria-labelledby="approach-title"><Reveal><div class="section-bar"><div><p class="section-index">01 / THE APPROACH</p><h2 id="approach-title" class="section-title">知识，应该经得起实践。</h2></div><p class="section-note">把学习、验证和复盘连在一起，<br>形成属于自己的能力。</p></div></Reveal><div class="method-grid"><Reveal v-for="(method, index) in methods" :key="method.title" :delay="index * 70"><article class="method-item"><div class="method-heading"><span class="method-number">0{{ index + 1 }}</span><component :is="method.icon" :size="22" :stroke-width="1.5" /></div><h3>{{ method.title }}</h3><p>{{ method.text }}</p></article></Reveal></div></section>
      <section id="paths" class="public-section" aria-labelledby="paths-title"><Reveal><div class="section-bar"><div><p class="section-index">02 / FIND YOUR PATH</p><h2 id="paths-title" class="section-title">从这里，找到你的下一步。</h2></div><p class="section-note">循序学习，或直接迎接挑战。<br>选择适合你的训练方式。</p></div></Reveal><RouterLink v-for="path in paths" :key="path.title" :to="auth.token ? path.to : { path: '/login', query: { redirect: path.to } }" class="path-row"><span class="path-icon"><component :is="path.icon" :size="21" :stroke-width="1.5" /></span><h3 class="path-title">{{ path.title }}<small>{{ path.english }}</small></h3><p class="path-description">{{ path.description }}</p><ArrowUpRight :size="21" :stroke-width="1.5" /></RouterLink></section>
      <section id="questions" class="public-section faq-layout" aria-labelledby="questions-title"><div><p class="section-index">03 / A FEW ANSWERS</p><h2 id="questions-title" class="section-title">开始之前，<br>你可能想了解。</h2></div><div><details v-for="question in questions" :key="question.title" class="faq-item"><summary>{{ question.title }}<Plus :size="18" /></summary><p>{{ question.text }}</p></details></div></section>
      <section class="closing"><div><h2>下一次突破，从动手开始。</h2><p>带着好奇心来，把解决问题的能力带走。</p></div><Button as-child><RouterLink :to="enter">进入学院<ArrowRight class="size-4" /></RouterLink></Button></section>
    </main>
    <footer class="public-footer"><Brand /><span>学以致用，知行合一。</span><div class="footer-links"><a href="https://github.com/yaklang" target="_blank" rel="noopener noreferrer">GitHub ↗</a><span>© {{ new Date().getFullYear() }} Yak Academy</span></div></footer>
    <!-- 点击火花仅在公开页生效（fixed 画布随组件挂载） -->
    <ClickSpark />
  </div>
</template>

<style scoped>
/* Terminal-rain hero：雨幕铺满整个 hero 卡片，文字层压在上方。 */
.hero-terminal { position: relative; }
.hero-rain { position: absolute; inset: 0; z-index: 0; border-radius: inherit; mask-image: linear-gradient(100deg, transparent 8%, #000 45%); }
.hero-terminal .hero-copy, .hero-terminal .hero-figure { position: relative; z-index: 1; }
/* 解码标题与 em 强调共用同一字号体系（继承 .hero-title）。 */
.hero-title .cipher-mask { color: var(--accent-foreground); opacity: .55; }
/* 矩阵雨上叠加轻遮罩压噪，让文字保持对比度（太厚会吃掉雨幕）。 */
.hero-terminal::after { content: ''; position: absolute; inset: 0; z-index: 1; pointer-events: none; background: color-mix(in srgb, var(--background) 18%, transparent); border-radius: inherit; }
.hero-terminal .hero-copy, .hero-terminal .hero-figure { z-index: 2; }
/* 数据条：hero 与方法区之间的能力统计。 */
.hero-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 20px; margin-block: 20px; }
.stat-item { display: grid; gap: 6px; padding: 24px; border: 1px solid var(--border); border-radius: 16px; background: var(--panel); }
.stat-item strong { font-size: 32px; font-weight: 600; letter-spacing: -.04em; font-variant-numeric: tabular-nums; color: var(--accent-foreground); }
.stat-item span { font-size: 12px; color: var(--muted-foreground); }
@media (max-width: 767px) {
  .hero-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); margin-block: 12px; }
  .stat-item { padding: 18px; }
  .stat-item strong { font-size: 26px; }
  .hero-rain { mask-image: linear-gradient(180deg, transparent 4%, #000 40%); }
}
</style>