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
  /**
   * auto（默认）：跟随主题——深色用发光橙雨，浅色用深橙油墨雨。
   * 注意不要用 Boolean prop 表达三态：未传入的 Boolean prop 会被 Vue
   * 转成 false，从而永远走不到“跟随主题”分支。
   */
  mode?: 'auto' | 'ink' | 'glow'
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
  mode: 'auto',
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
  // 浅色是油墨（ink），不是霓虹：头更深、尾更淡，靠对比度而不是发光。
  // 深橙 #c2410c 对白底约 5.2:1；深色用低饱和铜色，避免荧光橙。
  const ink = props.mode === 'ink' || (props.mode === 'auto' && resolved.value !== 'dark')
  if (ink) {
    return { ink, head: props.head ?? 'rgba(194,65,12,.8)', trail: props.trailColor ?? 'rgba(194,65,12,.22)' }
  }
  return { ink, head: props.head ?? 'rgba(214,164,126,.96)', trail: props.trailColor ?? 'rgba(209,154,114,.42)' }
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
  const { ink, head, trail } = palette()
  // 拖尾用 destination-out 擦除旧像素的透明度，不铺任何底色：
  // canvas 永远透明，页面背景（浅色卡片）直接透出来。经典矩阵雨的
  // 整屏 wash 会把画布累积成一块不透明色板，浅色下就是「黑底」。
  if (reduced) {
    ctx.clearRect(0, 0, width, height)
  } else {
    ctx.save()
    ctx.globalCompositeOperation = 'destination-out'
    const base = .18 - .13 * props.trailLength
    const erase = 1 - Math.pow(1 - base, Math.min(3, dt * 60))
    ctx.fillStyle = `rgba(0,0,0,${erase})`
    ctx.fillRect(0, 0, width, height)
    ctx.restore()
  }
  ctx.font = `500 ${Math.round(cell * .92)}px ui-monospace, 'SF Mono', Consolas, Menlo, monospace`
  ctx.textAlign = 'center'
  const rows = Math.ceil(height / cell)
  const slow = props.slowStrength
  // 浅色雨量约减半、速度更慢、尾迹更短：白底会放大每个字，宁疏勿密。
  const active = ink ? props.density * .55 : props.density
  const pace = ink ? .62 : 1
  const span = Math.min(rows, ink ? 12 : 18)
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
    col.head += col.speed * cell * rate * dt * 9 * pace
    if (col.head - rows * cell > height) {
      col.head = -cell * (1 + Math.random() * 2)
      col.speed = (Math.max(.35, .6 + Math.random() * .9)) * props.speed
      col.glyphs = randomGlyphs(rows + 3)
      // 密度：按比例让部分列重生后休眠片刻，保持画面疏密有致。
      // density 是"活跃比例"，所以休眠概率 = 1 - density，休眠时长随密度反比缩短。
      col.wait = Math.random() > active ? Math.random() * 2.4 * (1 - active) : 0
      continue
    }
    const headRow = Math.floor(col.head / cell)
    for (let s = 0; s < span; s++) {
      const row = headRow - s
      if (row < 0) break
      const y = row * cell + cell * .5
      const fade = s === 0 ? 1 : Math.pow(1 - s / span, 1.6)
      if (s === 0) {
        // 亮头：在深底上给足不透明度；浅色下由墨色本身承担对比度。
        ctx.fillStyle = head
        ctx.globalAlpha = .96
        ctx.fillText(col.glyphs[row % col.glyphs.length], x, y)
      } else {
        ctx.fillStyle = trail
        ctx.globalAlpha = fade * (ink ? .6 : .85)
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
  // 先停掉正在跑的帧循环再手动重绘，避免 refresh 叠加出第二条循环。
  cancelAnimationFrame(frame)
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

watch(resolved, refresh)
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