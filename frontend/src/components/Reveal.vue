<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
withDefaults(defineProps<{ delay?: number }>(), { delay: 0 })
const element = ref<HTMLElement>()
const visible = ref(false)
let observer: IntersectionObserver | undefined
onMounted(() => {
  if (matchMedia('(prefers-reduced-motion: reduce)').matches || !('IntersectionObserver' in window)) {
    visible.value = true
    return
  }
  observer = new IntersectionObserver(([entry]) => {
    if (entry.isIntersecting) { visible.value = true; observer?.disconnect() }
  }, { threshold: 0.08 })
  if (element.value) observer.observe(element.value)
})
onUnmounted(() => observer?.disconnect())
</script>

<template>
  <div ref="element" class="reveal" :class="{ 'is-visible': visible }" :style="{ '--reveal-delay': `${Math.min(delay, 300)}ms` }"><slot /></div>
</template>

<style scoped>
.reveal { opacity: 0; transform: translateY(8px); transition: opacity 480ms ease-out var(--reveal-delay), transform 480ms ease-out var(--reveal-delay); }
.reveal.is-visible { opacity: 1; transform: translateY(0); }
@media (prefers-reduced-motion: reduce) { .reveal { opacity: 1; transform: none; transition: none; } }
</style>
