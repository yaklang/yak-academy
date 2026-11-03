<script setup lang="ts">
// Original Vue/Canvas rendering informed by the Luminous Filaments and Horizon
// Bloom visual directions in the licensed React Bits Pro library. The paid
// registry source and credentials are not part of this public repository.
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useTheme } from '@/lib/theme'

const props = withDefaults(defineProps<{ variant?: 'filaments' | 'horizon' }>(), { variant: 'filaments' })
const canvas = ref<HTMLCanvasElement>()
const { resolved } = useTheme()
let observer: ResizeObserver | undefined
let visibility: IntersectionObserver | undefined
let media: MediaQueryList | undefined
let frame = 0
let width = 0
let height = 0
let visible = true
let lastFrame = 0
let elapsed = 0
let disposed = false

function paint(time: number) {
  const element = canvas.value
  const ctx = element?.getContext('2d', { alpha: true })
  if (!ctx || !width || !height) return
  const dark = resolved.value === 'dark'
  ctx.clearRect(0, 0, width, height)
  const centerX = width * (props.variant === 'horizon' ? .48 : .66)
  const centerY = height * .69
  const halo = ctx.createRadialGradient(centerX, centerY, 0, centerX, centerY, width * .46)
  halo.addColorStop(0, dark ? 'rgba(209,154,114,.15)' : 'rgba(111,69,43,.08)')
  halo.addColorStop(.5, dark ? 'rgba(209,154,114,.05)' : 'rgba(111,69,43,.03)')
  halo.addColorStop(1, 'rgba(111,69,43,0)')
  ctx.fillStyle = halo
  ctx.fillRect(0, 0, width, height)

  if (props.variant === 'filaments') {
    const count = width < 600 ? 42 : 84
    for (let i = 0; i < count; i++) {
      const phase = i / (count - 1)
      const fan = (phase - .5) * 2
      ctx.beginPath()
      for (let step = 0; step <= 56; step++) {
        const t = step / 56
        const sway = Math.sin(t * 4 + time * .22 + phase * 2) * .065 * t
        const x = width * (.66 + fan * .065 + fan * .47 * t ** 1.7 + sway)
        const y = height * (-.13 + t * 1.45)
        if (!step) ctx.moveTo(x, y); else ctx.lineTo(x, y)
      }
      const luminance = .35 + .35 * Math.sin(time * .3 + phase * Math.PI * 3) ** 2
      const stroke = ctx.createLinearGradient(0, 0, width * .5, height)
      stroke.addColorStop(0, dark ? `rgba(242,218,192,${luminance * .9})` : `rgba(140,100,70,${luminance * .75})`)
      stroke.addColorStop(.35, dark ? `rgba(209,154,114,${.55})` : `rgba(111,69,43,${.34})`)
      stroke.addColorStop(1, 'rgba(111,69,43,0)')
      ctx.strokeStyle = stroke
      ctx.lineWidth = i % 7 === 0 ? 1.5 : .65
      ctx.shadowColor = dark ? '#d19a72' : '#6f452b'
      ctx.shadowBlur = dark && i % 4 === 0 ? 10 : 0
      ctx.stroke()
    }
  } else {
    const crest = height * .72
    for (let i = 22; i >= 0; i--) {
      ctx.beginPath()
      for (let step = 0; step <= 90; step++) {
        const x = width * step / 90
        const y = crest + ((x - width * .5) / width) ** 2 * height * .72 - i * .6
        if (!step) ctx.moveTo(x, y); else ctx.lineTo(x, y)
      }
      ctx.strokeStyle = dark
        ? `rgba(${196 + i * 2},${146 + i * 2},${110 + i * 2},${i === 0 ? .75 : .015 + .02 * (1 - i / 22)})`
        : `rgba(${111 + i},${69 + i},${43 + i},${i === 0 ? .72 : .012 + .018 * (1 - i / 22)})`
      ctx.lineWidth = i === 0 ? 1.3 : 3
      ctx.shadowColor = dark ? '#d19a72' : '#6f452b'
      ctx.shadowBlur = i === 0 ? 14 : 0
      ctx.stroke()
    }
    const sunX = width * (.54 + Math.sin(time * .12) * .035)
    const sunY = crest + ((sunX - width * .5) / width) ** 2 * height * .72
    const sun = ctx.createRadialGradient(sunX, sunY, 0, sunX, sunY, height * .24)
    sun.addColorStop(0, dark ? 'rgba(244,224,202,.8)' : 'rgba(255,250,243,.9)')
    sun.addColorStop(.05, dark ? 'rgba(214,164,126,.45)' : 'rgba(190,145,108,.42)')
    sun.addColorStop(.3, dark ? 'rgba(209,154,114,.1)' : 'rgba(111,69,43,.08)')
    sun.addColorStop(1, 'rgba(111,69,43,0)')
    ctx.fillStyle = sun
    ctx.fillRect(0, 0, width, height)
  }
  ctx.shadowBlur = 0
  for (let i = 0; i < 42; i++) {
    const x = ((i * 157.73) % 991) / 991 * width
    const y = ((i * 71.91) % 601) / 601 * height * .72
    ctx.fillStyle = `rgba(${dark ? '214,164,126' : '111,69,43'},${.08 + .18 * (Math.sin(time * .24 + i) * .5 + .5)})`
    ctx.fillRect(x, y, i % 5 === 0 ? 2 : 1, i % 5 === 0 ? 2 : 1)
  }
}
function loop(now: number) {
  frame = 0
  if (disposed || !visible || document.hidden || media?.matches) return
  const interval = width < 600 ? 50 : 33
  if (now - lastFrame >= interval) {
    elapsed += Math.min(now - lastFrame, 70) / 1000
    lastFrame = now
    paint(elapsed)
  }
  frame = requestAnimationFrame(loop)
}
function resume() {
  cancelAnimationFrame(frame)
  frame = 0
  paint(elapsed)
  if (!disposed && visible && !document.hidden && !media?.matches) {
    lastFrame = performance.now()
    frame = requestAnimationFrame(loop)
  }
}
onMounted(() => {
  media = matchMedia('(prefers-reduced-motion: reduce)')
  media.addEventListener('change', resume)
  document.addEventListener('visibilitychange', resume)
  observer = new ResizeObserver(([entry]) => {
    width = entry.contentRect.width
    height = entry.contentRect.height
    if (!canvas.value) return
    const dpr = Math.min(devicePixelRatio || 1, width < 600 ? 1 : 1.5)
    canvas.value.width = Math.round(width * dpr)
    canvas.value.height = Math.round(height * dpr)
    canvas.value.getContext('2d')?.setTransform(dpr, 0, 0, dpr, 0, 0)
    resume()
  })
  if (canvas.value) observer.observe(canvas.value)
  visibility = new IntersectionObserver(([entry]) => { visible = entry.isIntersecting; resume() })
  if (canvas.value) visibility.observe(canvas.value)
})
watch(resolved, resume)
onUnmounted(() => {
  disposed = true
  cancelAnimationFrame(frame)
  observer?.disconnect()
  visibility?.disconnect()
  media?.removeEventListener('change', resume)
  document.removeEventListener('visibilitychange', resume)
})
</script>

<template><div class="academy-background" :data-variant="variant" aria-hidden="true"><canvas ref="canvas" /></div></template>

<style scoped>
.academy-background { position: absolute; inset: 0; overflow: hidden; pointer-events: none; z-index: -1; background: radial-gradient(ellipse at 65% 60%, rgb(111 69 43 / .06), transparent 65%); }
canvas { width: 100%; height: 100%; display: block; }
</style>
