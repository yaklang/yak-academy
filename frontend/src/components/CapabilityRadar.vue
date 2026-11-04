<script setup lang="ts">
// 能力画像雷达图：按题目分类聚合「已通关 / 可见题数」的覆盖率。
// Chart.js 只负责绘制 canvas；数值同时也以文字列表呈现，颜色不是唯一信息载体。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Chart,
  Filler,
  LineElement,
  PointElement,
  RadarController,
  RadialLinearScale,
  Tooltip,
  type ChartConfiguration,
} from 'chart.js'
import { CATEGORY_LABELS, type CapabilityDimension } from '@/api'
import { useTheme } from '@/lib/theme'

Chart.register(RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip)

const props = defineProps<{ dimensions: CapabilityDimension[] }>()

const canvas = ref<HTMLCanvasElement>()
const { resolved } = useTheme()
let chart: Chart | undefined
const reducedMotion = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

const label = (category: string) => CATEGORY_LABELS[category] ?? category

// 覆盖率（0-100）：分母是该分类当前可见题数，避免题量多的分类压平其它轴。
const rows = computed(() =>
  props.dimensions.map((d) => ({
    ...d,
    label: label(d.category),
    percent: d.total > 0 ? Math.round((d.solved / d.total) * 100) : 0,
  })),
)
// 全 0% 时雷达图只剩网格，视觉上等同“空面板”——显式给零进度说明，
// 让“还没开始”与“加载失败”区分开。
const allZero = computed(() => rows.value.length > 0 && rows.value.every((r) => r.solved === 0))

function token(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function buildChart() {
  const el = canvas.value
  if (!el || rows.value.length === 0) return undefined
  const primary = token('--primary') || '#7a5236'
  const accent = token('--accent-foreground') || '#6f452b'
  const border = token('--border') || '#e6ddd2'
  const muted = token('--muted-foreground') || '#6b6158'
  const surface = token('--panel') || '#f4eadf'
  // 网格线比边框略重一档：深色下 --border 贴近面板色，雷达会糊成一片灰。
  const gridLine = `${muted}4d`

  const config: ChartConfiguration<'radar'> = {
    type: 'radar',
    data: {
      labels: rows.value.map((r) => r.label),
      datasets: [{
        label: '覆盖率',
        data: rows.value.map((r) => r.percent),
        borderColor: primary,
        backgroundColor: `${primary}2b`,
        pointBackgroundColor: accent,
        pointBorderColor: surface,
        pointBorderWidth: 2,
        pointRadius: 3,
        pointHoverRadius: 4,
        borderWidth: 2,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: reducedMotion ? false : { duration: 240 },
      layout: { padding: 6 },
      plugins: {
        tooltip: {
          backgroundColor: surface,
          borderColor: border,
          borderWidth: 1,
          titleColor: token('--foreground') || '#241f1a',
          bodyColor: muted,
          padding: 10,
          displayColors: false,
          callbacks: {
            label: (ctx) => {
              const row = rows.value[ctx.dataIndex]
              return `${row.solved} / ${row.total} 题 · ${row.percent}%`
            },
          },
        },
      },
      scales: {
        r: {
          min: 0,
          max: 100,
          beginAtZero: true,
          angleLines: { color: gridLine },
          grid: { color: gridLine },
          ticks: {
            stepSize: 50,
            color: muted,
            backdropColor: 'transparent',
            font: { size: 10 },
          },
          pointLabels: {
            color: token('--foreground') || '#241f1a',
            font: { size: 12 },
          },
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

watch(() => props.dimensions, render, { deep: true, flush: 'post' })
watch(resolved, render, { flush: 'post' })

onMounted(render)
onBeforeUnmount(() => chart?.destroy())
</script>

<template>
  <div class="capability-body">
    <UiEmpty
      v-if="rows.length === 0"
      description="暂无可统计的题目：等管理员发布内容后，这里会按分类显示你的掌握情况。"
      class="capability-empty"
    />
    <template v-else>
      <p v-if="allZero" class="zero-note" role="status">
        还没有通关记录。完成任意题目后，这里会按分类点亮对应维度。
      </p>
      <div class="radar-stage" :class="{ 'is-zero': allZero }">
        <canvas
          ref="canvas"
          role="img"
          :aria-label="`能力画像雷达图：${rows.map((r) => `${r.label} ${r.solved}/${r.total} 题`).join('，')}`"
        />
      </div>
      <ul class="legend" aria-label="各分类通关情况">
        <li v-for="row in rows" :key="row.category" class="legend-row">
          <span class="legend-name">{{ row.label }}</span>
          <span class="legend-value mono">{{ row.solved }}<span class="legend-total">/{{ row.total }}</span></span>
          <span class="legend-track" aria-hidden="true">
            <span class="legend-fill" :style="{ width: `${row.percent}%` }" />
          </span>
          <span class="legend-percent mono">{{ row.percent }}%</span>
        </li>
      </ul>
    </template>
  </div>
</template>

<style scoped>
.capability-body { display: grid; gap: var(--space-5); }
.capability-empty { min-height: 220px; }
.zero-note {
  margin: 0;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--primary) 8%, transparent);
  color: var(--foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-normal);
}
.radar-stage { position: relative; height: 260px; min-width: 0; }
.radar-stage.is-zero canvas { opacity: .72; }
.legend { display: grid; gap: var(--space-2); margin: 0; padding: 0; list-style: none; }
.legend-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(56px, 96px) 42px;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-sm);
  color: var(--foreground);
}
.legend-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.legend-value { color: var(--accent-foreground); font-size: var(--text-sm); }
.legend-total { color: var(--muted-foreground); }
.legend-track { display: block; height: 6px; border-radius: 999px; background: color-mix(in srgb, var(--muted-foreground) 24%, transparent); overflow: hidden; }
.legend-fill { display: block; height: 100%; border-radius: inherit; background: var(--primary); }
.legend-percent { color: var(--muted-foreground); font-size: var(--text-xs); text-align: right; }
@media (max-width: 560px) {
  .legend-row { grid-template-columns: minmax(0, 1fr) auto 42px; }
  .legend-track { display: none; }
  .radar-stage { height: 220px; }
}
</style>
