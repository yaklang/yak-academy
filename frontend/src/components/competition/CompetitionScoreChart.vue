<script setup lang="ts">
// 竞赛累计得分曲线：按通关时间绘制阶梯线（得分只增不减），
// 起点固定在开赛时刻，便于把"何时开始拿分"放进赛程上下文。
// Chart.js 仅绘制 canvas；数值在 KPI 与题目列表里都有文字版本。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  CategoryScale,
  Chart,
  Filler,
  LineController,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
  type ChartConfiguration,
} from 'chart.js'
import type { CompetitionScorePoint } from '@/api'
import { useTheme } from '@/lib/theme'

Chart.register(LineController, CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip)

const props = defineProps<{
  points: CompetitionScorePoint[]
  startAt: string
  titles: Record<number, string>
}>()

const canvas = ref<HTMLCanvasElement>()
const { resolved } = useTheme()
let chart: Chart | undefined
const reducedMotion = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

const rows = computed<CompetitionScorePoint[]>(() => {
  const start: CompetitionScorePoint = { at: props.startAt, challenge_id: 0, delta: 0, score: 0 }
  return [start, ...props.points]
})

const total = computed(() => props.points.at(-1)?.score ?? 0)
const summary = computed(() =>
  props.points.length === 0
    ? '得分曲线：暂无通关记录'
    : `得分曲线：累计 ${total.value} 分，共 ${props.points.length} 次通关`,
)

function token(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function axisTime(at: string) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return at
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function buildChart() {
  const el = canvas.value
  if (!el || rows.value.length <= 1) return undefined
  const primary = token('--primary') || '#7a5236'
  const accent = token('--accent-foreground') || '#6f452b'
  const border = token('--border') || '#e6ddd2'
  const muted = token('--muted-foreground') || '#6b6158'
  const surface = token('--panel') || '#f4eadf'
  const gridLine = `${muted}33`

  const config: ChartConfiguration<'line'> = {
    type: 'line',
    data: {
      labels: rows.value.map((r) => axisTime(r.at)),
      datasets: [{
        label: '累计得分',
        data: rows.value.map((r) => r.score),
        borderColor: primary,
        backgroundColor: `${primary}1f`,
        fill: true,
        stepped: 'after',
        borderWidth: 2,
        pointRadius: (ctx) => (ctx.dataIndex === 0 || ctx.dataIndex === rows.value.length - 1 ? 3 : 2),
        pointHoverRadius: 4,
        pointBackgroundColor: accent,
        pointBorderColor: surface,
        pointBorderWidth: 2,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: reducedMotion ? false : { duration: 220 },
      interaction: { mode: 'index', intersect: false },
      layout: { padding: { top: 6, right: 6 } },
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: surface,
          borderColor: border,
          borderWidth: 1,
          titleColor: token('--foreground') || '#241f1a',
          bodyColor: muted,
          padding: 10,
          displayColors: false,
          callbacks: {
            title: (items) => axisTime(rows.value[items[0]?.dataIndex ?? 0]?.at ?? ''),
            label: (ctx) => {
              const row = rows.value[ctx.dataIndex]
              if (!row || row.challenge_id === 0) return '比赛开始 · 0 分'
              return `累计 ${row.score} 分（+${row.delta}）`
            },
            afterLabel: (ctx) => {
              const row = rows.value[ctx.dataIndex]
              if (!row || row.challenge_id === 0) return ''
              return props.titles[row.challenge_id] ?? `题目 #${row.challenge_id}`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          border: { display: false },
          ticks: { color: muted, maxTicksLimit: 6, font: { size: 10 } },
        },
        y: {
          beginAtZero: true,
          border: { display: false },
          grid: { color: gridLine },
          ticks: { color: muted, precision: 0, font: { size: 10 } },
        },
      },
    },
  }
  return new Chart(el, config)
}

function render() {
  chart?.destroy()
  chart = buildChart()
}

watch(() => props.points, render, { deep: true, flush: 'post' })
watch(resolved, render, { flush: 'post' })

onMounted(render)
onBeforeUnmount(() => chart?.destroy())
</script>

<template>
  <div v-if="points.length === 0" class="chart-empty">
    还没有通关记录。完成第一道题后，这里会绘制累计得分曲线。
  </div>
  <div v-else class="chart-box">
    <canvas ref="canvas" role="img" :aria-label="summary" />
  </div>
</template>

<style scoped>
.chart-box { height: 238px; min-width: 0; }
.chart-empty {
  height: 238px;
  display: grid;
  place-items: center;
  padding: 0 24px;
  color: var(--muted-foreground);
  font-size: 13px;
  text-align: center;
}
</style>
