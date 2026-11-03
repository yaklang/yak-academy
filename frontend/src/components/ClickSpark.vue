<script setup lang="ts">
// Original Vue implementation informed by the Click Spark micro-interaction
// direction in the licensed React Bits Pro library. The paid registry source
// and credentials are not part of this public repository.
import { onMounted, onUnmounted, ref } from 'vue'

// 挂在 App 根部：捕获全局点击，在点击位置炸出几粒强调色火花。
const canvas = ref<HTMLCanvasElement>()
let frame = 0
let width = 0
let height = 0
let ctx: CanvasRenderingContext2D | null = null
let sparkColor = '#7a4526'
let sparks: { x: number; y: number; vx: number; vy: number; life: number }[] = []
let media: MediaQueryList | undefined
let reduced = false

function resize() {
  const element = canvas.value
  if (!element) return
  width = element.width = window.innerWidth
  height = element.height = window.innerHeight
}

function burst(x: number, y: number) {
  if (reduced) return
  sparkColor = getComputedStyle(document.documentElement).getPropertyValue('--accent-foreground').trim() || sparkColor
  for (let i = 0; i < 7; i++) {
    const angle = Math.random() * Math.PI * 2
    const speed = 1.6 + Math.random() * 3.4
    sparks.push({ x, y, vx: Math.cos(angle) * speed, vy: Math.sin(angle) * speed - 1.2, life: 1 })
  }
  if (!frame) frame = requestAnimationFrame(paint)
}

function paint() {
  frame = 0
  if (!ctx) return
  ctx.clearRect(0, 0, width, height)
  sparks = sparks.filter(s => s.life > 0)
  for (const s of sparks) {
    s.x += s.vx
    s.y += s.vy
    s.vy += .12 // 重力
    s.life -= .03
    ctx.fillStyle = sparkColor
    ctx.globalAlpha = Math.max(0, s.life) * .9
    ctx.fillRect(s.x, s.y, 2.2, 2.2)
  }
  ctx.globalAlpha = 1
  if (sparks.length) frame = requestAnimationFrame(paint)
}

function onClick(event: MouseEvent) {
  // 只对可交互元素点击时炸花（按钮/链接），避免页面上所有点击都出粒子。
  const target = event.target as HTMLElement
  if (target.closest('button, a, summary, [data-slot=button]')) burst(event.clientX, event.clientY)
}

function onMotionChange() {
  reduced = media?.matches ?? false
}

onMounted(() => {
  ctx = canvas.value?.getContext('2d') ?? null
  media = matchMedia('(prefers-reduced-motion: reduce)')
  reduced = media.matches
  media.addEventListener('change', onMotionChange)
  resize()
  window.addEventListener('resize', resize)
  window.addEventListener('click', onClick, { passive: true, capture: true })
})

onUnmounted(() => {
  cancelAnimationFrame(frame)
  media?.removeEventListener('change', onMotionChange)
  window.removeEventListener('resize', resize)
  window.removeEventListener('click', onClick, true)
})
</script>

<template>
  <canvas ref="canvas" class="click-spark" aria-hidden="true" />
</template>

<style scoped>
.click-spark { position: fixed; inset: 0; width: 100vw; height: 100dvh; pointer-events: none; z-index: 80; }
</style>