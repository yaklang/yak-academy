<script setup lang="ts">
// Original Vue/CSS implementation informed by the Aurora Blur visual
// direction in the licensed React Bits Pro library (soft drifting glow
// layers behind hero content). The paid registry source and credentials
// are not part of this public repository.
//
// 纯 CSS 实现：三层大尺寸径向渐变光斑 + 模糊，缓慢漂移；颜色取设计令牌，
// 明暗主题自动切换；prefers-reduced-motion 时静止。宿主需 position:relative。
</script>

<template>
  <div class="aurora-blur" aria-hidden="true">
    <span class="aurora-blob a1"></span>
    <span class="aurora-blob a2"></span>
    <span class="aurora-blob a3"></span>
    <span class="aurora-scrim"></span>
  </div>
</template>

<style scoped>
.aurora-blur {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
}
.aurora-blob {
  position: absolute;
  width: 58%;
  max-width: 560px;
  aspect-ratio: 1;
  border-radius: 50%;
  filter: blur(56px);
  will-change: transform;
}
.a1 {
  top: -38%;
  left: -10%;
  background: radial-gradient(circle, color-mix(in srgb, var(--primary) 40%, transparent), transparent 68%);
  animation: aurora-drift-a 26s ease-in-out infinite alternate;
}
.a2 {
  right: -14%;
  bottom: -52%;
  background: radial-gradient(circle, color-mix(in srgb, var(--accent) 55%, transparent), transparent 68%);
  animation: aurora-drift-b 34s ease-in-out infinite alternate;
}
.a3 {
  top: -12%;
  right: 16%;
  width: 38%;
  background: radial-gradient(circle, color-mix(in srgb, var(--primary) 22%, transparent), transparent 68%);
  animation: aurora-drift-c 44s ease-in-out infinite alternate;
}
/* 文案侧渐隐纱，保证前景文本对比度不依赖光斑位置 */
.aurora-scrim {
  position: absolute;
  inset: 0;
  background: linear-gradient(100deg, var(--card) 8%, transparent 62%);
}
@keyframes aurora-drift-a {
  from { transform: translate3d(0, 0, 0) scale(1); }
  to { transform: translate3d(12%, 8%, 0) scale(1.18); }
}
@keyframes aurora-drift-b {
  from { transform: translate3d(0, 0, 0) scale(1.1); }
  to { transform: translate3d(-10%, -7%, 0) scale(0.94); }
}
@keyframes aurora-drift-c {
  from { transform: translate3d(0, 0, 0); }
  to { transform: translate3d(-14%, 10%, 0); }
}
@media (prefers-reduced-motion: reduce) {
  .aurora-blob { animation: none; }
}
</style>
