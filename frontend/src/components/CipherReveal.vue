<script setup lang="ts">
// Original Vue implementation informed by the Cipher Reveal text animation
// direction in the licensed React Bits Pro library. The paid registry source
// and credentials are not part of this public repository.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  text: string
  /** 每个字符开始解码的延迟基准（ms），0 = 全部同时 */
  duration?: number
  /** 每个字符乱码停留时长（ms） */
  scramble?: number
  /** 滚动速率（字符/秒） */
  rate?: number
  /** 进入视口才播放 */
  trigger?: 'view' | 'mount'
}>(), { duration: 900, scramble: 480, rate: 22, trigger: 'mount' })

const root = ref<HTMLElement>()
const visible = ref(false)
const playing = ref(false)

const UPPER = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const LOWER = 'abcdefghijklmnopqrstuvwxyz'
const DIGITS = '0123456789'
const SYMBOLS = '#$%&*<>=?!'

// 按 grapheme 切字（中文/emoji 安全），保留空白原样。
const segments = computed(() => Array.from(props.text).map(glyph => ({ glyph, blank: /^\s$/.test(glyph) })))

interface Cell {
  el: HTMLElement | null
  glyph: string
  pool: string[]
  start: number
  end: number
  step: number
  shown: string
  face: HTMLElement | null
  mask: HTMLElement | null
}

let cells: Cell[] = []
let raf = 0
let reduced = false
let observer: IntersectionObserver | undefined
let lastGlyphs: string[] = []

function poolFor(glyph: string) {
  if (/^[A-Z]/.test(glyph)) return UPPER.split('')
  if (/[0-9]/.test(glyph)) return DIGITS.split('')
  if (/[\u4e00-\u9fff]/.test(glyph)) return '的一是了我不人在他有这上们来到时大地为子中你说生国年着就那和要她出也得里后自以会家可下而过事去能对多发当'.split('')
  if (/[a-z]/.test(glyph)) return LOWER.split('')
  return SYMBOLS.split('')
}

const rand = (n: number) => {
  const x = Math.sin(n * 127.1 + 311.7) * 43758.5453
  return x - Math.floor(x)
}

function measure() {
  const host = root.value
  if (!host) return
  const nodes = Array.from(host.querySelectorAll<HTMLElement>('[data-cipher]'))
  const now = performance.now()
  cells = nodes.map((el, i) => {
    const glyph = el.dataset.glyph ?? ''
    const old = cells.find(c => c.el === el)
    const face = el.querySelector<HTMLElement>('[data-face]')
    const mask = el.querySelector<HTMLElement>('[data-mask]')
    return {
      el, glyph, face, mask,
      pool: poolFor(glyph),
      // 从左到右错落解码，rank 决定每个字符的入场时刻。
      start: now + (props.duration * i) / Math.max(nodes.length - 1, 1),
      end: now + (props.duration * i) / Math.max(nodes.length - 1, 1) + props.scramble,
      step: -1, shown: '',
      ...(old ? {} : {}),
    }
  })
}

function tick(now: number) {
  raf = 0
  let busy = false
  for (const cell of cells) {
    if (now >= cell.end) {
      // 定格：真字符淡入，乱码层清空。
      if (cell.face && cell.face.style.opacity !== '1') {
        cell.face.style.transition = 'opacity 220ms cubic-bezier(.2,.7,.2,1)'
        cell.face.style.opacity = '1'
        cell.mask!.style.transition = 'opacity 220ms'
        cell.mask!.style.opacity = '0'
      }
      continue
    }
    if (now < cell.start) { busy = true; continue }
    busy = true
    if (cell.mask) {
      const step = Math.floor((now / 1000) * props.rate)
      if (step !== cell.step) {
        cell.step = step
        const glyph = cell.pool[Math.floor(rand(step + cell.pool.length) * cell.pool.length)] ?? cell.glyph
        cell.mask.textContent = glyph
        cell.mask.style.opacity = '.55'
      }
      if (cell.face) cell.face.style.opacity = '0'
    }
  }
  if (busy) raf = requestAnimationFrame(tick)
  else playing.value = false
}

function reveal() {
  if (reduced) {
    for (const cell of cells) { cell.face!.style.opacity = '1'; cell.mask!.style.opacity = '0' }
    return
  }
  measure()
  playing.value = true
  if (!raf) raf = requestAnimationFrame(tick)
}

function onEnter() {
  const host = root.value
  const rect = host?.getBoundingClientRect()
  if (!rect || playing.value) return
  const x = pointer.x - rect.left, y = pointer.y - rect.top
  const reach = 64
  let woke = false
  const now = performance.now()
  for (const cell of cells) {
    const cx = Number(cell.el?.dataset.x ?? 0), cy = Number(cell.el?.dataset.y ?? 0)
    if (Math.hypot(cx - x, cy - y) > reach) continue
    if (now < Number(cell.el?.dataset.cool ?? 0)) continue
    // 悬停扰动：附近字符重新乱码化，随后定格回真字符。
    cell.start = now
    cell.end = now + 260
    cell.el!.dataset.cool = String(now + 600)
    woke = true
  }
  if (woke && !raf) { playing.value = true; raf = requestAnimationFrame(tick) }
}

const pointer = { x: 0, y: 0 }
function onMove(event: PointerEvent) {
  pointer.x = event.clientX; pointer.y = event.clientY
  onEnter()
}

function settleAll() {
  for (const cell of cells) { if (cell.face) cell.face.style.opacity = '1'; if (cell.mask) cell.mask.style.opacity = '0' }
}

onMounted(() => {
  reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
  measure()
  // 初始全部隐藏真字符（乱码模式起点）
  if (!reduced) for (const cell of cells) { cell.face!.style.opacity = '0' }
  if (reduced || props.trigger === 'mount') reveal()
  else {
    observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) { reveal(); observer?.disconnect() }
    }, { threshold: .4 })
    observer.observe(root.value!)
  }
  root.value?.addEventListener('pointermove', onMove)
})

watch(() => props.text, () => {
  lastGlyphs = []
  cancelAnimationFrame(raf)
  raf = 0
  measure()
  reveal()
})

onUnmounted(() => {
  cancelAnimationFrame(raf)
  observer?.disconnect()
  root.value?.removeEventListener('pointermove', onMove)
})
</script>

<template>
  <span ref="root" class="cipher" aria-label="text">
    <span v-for="(seg, i) in segments" :key="i" :data-glyph="seg.glyph" data-cipher class="cipher-cell">
      <template v-if="seg.blank">{{ seg.glyph }}</template>
      <template v-else>
        <span data-face class="cipher-face">{{ seg.glyph }}</span>
        <span data-mask class="cipher-mask" aria-hidden="true" />
      </template>
    </span>
  </span>
</template>

<style scoped>
.cipher { white-space: pre-wrap; }
.cipher-cell { position: relative; display: inline-block; }
.cipher-mask { position: absolute; left: 50%; top: 0; transform: translateX(-50%); opacity: 0; pointer-events: none; color: var(--accent-foreground); white-space: pre; }
@media (prefers-reduced-motion: reduce) { .cipher-face { opacity: 1 !important; } }
</style>