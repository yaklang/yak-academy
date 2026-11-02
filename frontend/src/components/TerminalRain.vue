<script setup lang="ts">
// Original Vue/Canvas 2D implementation informed by the Terminal Rain visual
// direction in the licensed React Bits Pro library. The paid registry source
// and credentials are not part of this public repository.
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useTheme } from '@/lib/theme'

const props = withDefaults(defineProps<{
  /** 前景色：雨滴亮头 */
  head?: string
  /** 拖尾色 */
  trailColor?: string
  /** 明文模式：浅色主题下用墨滴（深色字符画在浅底上） */
  ink?: boolean
  /** 字号（px），影响列密度 */
  glyphSize?: number
  /** 0-1，列的活跃比例 */
  density?: number
  /** 速度倍率 */
  speed?: number
  /** 0-1，拖尾淡出快慢（大 = 长尾） */
  trailLength?: number
  /** 鼠标接近时雨滴减速的比例（0 关闭交互） */
  slowStrength?: number
}>(), {
  glyphSize: 18,
  density: .38,
  speed: 1,
  trailLength: .78,
  slowStrength: .75,
})

const root = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
const { resolved } = useTheme()

const GLYPHS = '0123456789ABCDEF$#@&*<>?!%'
let frame = 0
let width = 0
let height = 0
let visible = true
let disposed = false
let columns: { head: number; speed: number; wait: number; glyphs: string[] }[] = []
let cell = 18
let colW = 12
let last = 0
let pointer = { x: -1e4, y: -1e4 }
let resizeObserver: ResizeObserver | undefined
let visibilityObserver: IntersectionObserver | undefined
let media: MediaQueryList | undefined
let reduced = false

function palette() {
  const dark = resolved.value === 'dark'
  if (props.ink === undefined ? !dark : props.ink) {
    return { head: props.head ?? 'rgba(154,72,16,.92)', trail: props.trailColor ?? 'rgba(214,91,22,.34)', wash: 'rgba(250,249,246,' }
  }
  return { head: props.head ?? 'rgba(255,173,117,.96)', trail: props.trailColor ?? 'rgba(255,138,76,.42)', wash: 'rgba(12,12,13,' }
}

// 预生成每列随机字符，绘制时不重新取样（矩阵雨的字符只在重生时换新）。
function randomGlyphs(count: number) {
  return Array.from({ length: count }, () => GLYPHS[Math.floor(Math.random() * GLYPHS.length)])
}

function rebuild() {
  const element = canvas.value
  if (!element) return
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  const rect = (element.parentElement as HTMLElement).getBoundingClientRect()
  width = Math.max(1, Math.round(rect.width * ratio))
  height = Math.max(1, Math.round(rect.height * ratio))
  element.width = width
  element.height = height
  cell = Math.max(12, props.glyphSize * ratio)
  colW = cell * .68
  const count = Math.ceil(width / colW) + 1
  columns = Array.from({ length: count }, () => {
    const speed = (Math.max(.35, .6 + Math.random() * .9)) * props.speed
    return { head: Math.random() * height, speed, wait: 0, glyphs: randomGlyphs(Math.ceil(height / cell) + 3) }
  })
}

function paint(time: number) {
  const element = canvas.value
  const ctx = element?.getContext('2d', { alpha: true })
  if (!ctx || !width || !height) return
  const dt = Math.min(.05, Math.max(0, (time - last) / 1000)) || .016
  last = time
  const { head, trail, wash } = palette()
  // 拖尾：每帧叠一层低透明度底色冲刷上一帧（矩阵雨经典手法）。
  // 透明度越低拖尾越长；reduced-motion 时直接整层盖掉（只留静帧）。
  ctx.fillStyle = `${wash}${reduced ? 1 : 0.22 * (1 - props.trailLength * .5)})`
  ctx.fillRect(0, 0, width, height)
  ctx.font = `500 ${Math.round(cell * .92)}px ui-monospace, 'SF Mono', Consolas, Menlo, monospace`
  ctx.textAlign = 'center'
  const rows = Math.ceil(height / cell)
  const slow = props.slowStrength
  for (let c = 0; c < columns.length; c++) {
    const col = columns[c]
    if (col.wait > 0) {
      col.wait -= dt
      continue
    }
    const x = c * colW + colW / 2
    // 鼠标附近的雨滴减速（bullet-time），远离立即恢复。
    let rate = 1
    if (slow > 0) {
      const dx = x - pointer.x
      const dy = Math.min(Math.abs(col.head - pointer.y), cell * 6)
      const reach = cell * 9
      const field = Math.exp(-(dx * dx + dy * dy * .25) / (reach * reach))
      rate = Math.max(1 - slow * field, .06)
    }
    col.head += col.speed * cell * rate * dt * 9
    if (col.head - rows * cell > height) {
      col.head = -cell * (1 + Math.random() * 2)
      col.speed = (Math.max(.35, .6 + Math.random() * .9)) * props.speed
      col.glyphs = randomGlyphs(rows + 3)
      // 密度：按比例让部分列重生后休眠片刻，保持画面疏密有致。
      // density 是"活跃比例"，所以休眠概率 = 1 - density，休眠时长随密度反比缩短。
      col.wait = Math.random() > props.density ? Math.random() * 2.4 * (1 - props.density) : 0
      continue
    }
    const headRow = Math.floor(col.head / cell)
    const span = Math.min(rows, 18)
    for (let s = 0; s < span; s++) {
      const row = headRow - s
      if (row < 0) break
      const y = row * cell + cell * .5
      const fade = s === 0 ? 1 : Math.pow(1 - s / span, 1.6)
      if (s === 0) {
        // 亮头：双层描一次辉光，让雨滴头部在深底上真正"亮"起来。
        ctx.fillStyle = head
        ctx.globalAlpha = .96
        ctx.fillText(col.glyphs[row % col.glyphs.length], x, y)
      } else {
        ctx.fillStyle = trail
        ctx.globalAlpha = fade * .85
        ctx.fillText(col.glyphs[row % col.glyphs.length], x, y)
      }
    }
  }
  ctx.globalAlpha = 1
  if (reduced) return // 降级：只画一帧静态雨，不再继续动画
  if (visible && !document.hidden && !disposed) frame = requestAnimationFrame(paint)
}

function onPointerMove(event: PointerEvent) {
  const rect = canvas.value?.getBoundingClientRect()
  if (!rect) return
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  pointer = { x: (event.clientX - rect.left) * ratio, y: (event.clientY - rect.top) * ratio }
}

function refresh() {
  reduced = media?.matches ?? false
  const element = canvas.value
  const ctx = element?.getContext('2d')
  if (ctx && width && height) {
    // 主题切换时全量清底重画，避免两种色系叠影。
    ctx.clearRect(0, 0, width, height)
    paint(performance.now())
  }
}

onMounted(() => {
  reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
  rebuild()
  paint(performance.now())
  resizeObserver = new ResizeObserver(() => { rebuild(); if (reduced) paint(last) })
  resizeObserver.observe(root.value!)
  visibilityObserver = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting
    if (visible) { last = performance.now(); if (!disposed && !reduced) frame = requestAnimationFrame(paint) }
  })
  visibilityObserver.observe(root.value!)
  media = matchMedia('(prefers-reduced-motion: reduce)')
  media.addEventListener('change', refresh)
  root.value?.parentElement?.addEventListener('pointermove', onPointerMove)
})

watch(() => resolved, refresh)
watch(() => [props.glyphSize, props.density, props.speed, props.trailLength], rebuild)

onUnmounted(() => {
  disposed = true
  cancelAnimationFrame(frame)
  resizeObserver?.disconnect()
  visibilityObserver?.disconnect()
  media?.removeEventListener('change', refresh)
  root.value?.parentElement?.removeEventListener('pointermove', onPointerMove)
})
</script>

<template>
  <div ref="root" class="terminal-rain" aria-hidden="true">
    <canvas ref="canvas" />
  </div>
</template>

<style scoped>
.terminal-rain { position: absolute; inset: 0; overflow: clip; pointer-events: none; }
.terminal-rain canvas { position: absolute; inset: 0; width: 100%; height: 100%; display: block; }
</style>