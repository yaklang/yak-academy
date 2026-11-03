<script setup lang="ts">
// 知识浮层：点击正文的 #knowledge:<id> 链接后，在鼠标附近弹出的轻量卡片。
// 交互对齐 wiki 预览：不压暗页面、不阻断滚动，点外部 / Esc 关闭。
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ExternalLink, ShieldCheck, Wrench, X } from '@lucide/vue'
import { findKnowledge, type KnowledgeKind } from '@/lib/knowledge'

interface Anchor {
  x: number
  y: number
  linkLeft: number
  linkRight: number
}

type Placement = 'right' | 'left' | 'bottom' | 'top'

const props = defineProps<{ open: boolean; id: string; anchor: Anchor }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const VIEWPORT_MARGIN = 16
const ANCHOR_GAP = 14
const ARROW_INSET = 18

const entry = computed(() => findKnowledge(props.id))
const kindLabel = computed(() => (entry.value?.kind === 'yakit' ? 'Yakit 知识' : '网络安全'))
const kindIcon = (kind: KnowledgeKind) => (kind === 'yakit' ? Wrench : ShieldCheck)

const popoverRef = ref<HTMLElement | null>(null)
const coords = ref({ left: 0, top: 0 })
const placement = ref<Placement>('right')
const arrowOffset = ref(24)
const ready = ref(false)
let lastActiveElement: HTMLElement | null = null

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max)

async function positionPopover() {
  await nextTick()
  const el = popoverRef.value
  if (!props.open || !el) return

  const rect = el.getBoundingClientRect()
  const width = rect.width
  const height = rect.height
  const vw = window.innerWidth
  const vh = window.innerHeight
  const maxLeft = Math.max(VIEWPORT_MARGIN, vw - width - VIEWPORT_MARGIN)
  const maxTop = Math.max(VIEWPORT_MARGIN, vh - height - VIEWPORT_MARGIN)

  // 优先放在鼠标（链接）右侧；右侧不够时翻到左侧；两侧都不够（窄屏）再改为上下布局。
  const linkLeft = Math.min(props.anchor.linkLeft, props.anchor.x)
  const linkRight = Math.max(props.anchor.linkRight, props.anchor.x)
  const linkCenterX = (linkLeft + linkRight) / 2
  let left = linkRight + ANCHOR_GAP
  let top = props.anchor.y - 16
  let side: Placement = 'right'

  if (left + width + VIEWPORT_MARGIN > vw) {
    if (linkLeft - ANCHOR_GAP - width - VIEWPORT_MARGIN >= 0) {
      side = 'left'
      left = linkLeft - ANCHOR_GAP - width
    } else {
      side = 'bottom'
      left = linkCenterX - width / 2
      top = props.anchor.y + ANCHOR_GAP
      if (top + height + VIEWPORT_MARGIN > vh) {
        side = 'top'
        top = props.anchor.y - ANCHOR_GAP - height
      }
    }
  }

  left = clamp(left, VIEWPORT_MARGIN, maxLeft)
  top = clamp(top, VIEWPORT_MARGIN, maxTop)

  placement.value = side
  coords.value = { left, top }
  arrowOffset.value = side === 'right' || side === 'left'
    ? clamp(props.anchor.y - top, ARROW_INSET, Math.max(ARROW_INSET, height - ARROW_INSET))
    : clamp(linkCenterX - left, ARROW_INSET, Math.max(ARROW_INSET, width - ARROW_INSET))
  ready.value = true
}

function close(restoreFocus = false) {
  if (!props.open) return
  emit('update:open', false)
  if (restoreFocus) {
    void nextTick(() => {
      if (lastActiveElement?.isConnected) lastActiveElement.focus({ preventScroll: true })
    })
  }
}

function onPointerDown(event: PointerEvent) {
  const target = event.target
  if (target instanceof Node && popoverRef.value?.contains(target)) return
  close(false)
}

function onKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    close(true)
  }
}

function onResize() {
  if (props.open) void positionPopover()
}

function onScroll(event: Event) {
  if (event.target instanceof Node && popoverRef.value?.contains(event.target)) return
  close(false)
}

function activate() {
  lastActiveElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  ready.value = false
  document.addEventListener('pointerdown', onPointerDown, true)
  document.addEventListener('keydown', onKeyDown)
  window.addEventListener('resize', onResize)
  window.addEventListener('scroll', onScroll, true)
  void positionPopover().then(() => {
    if (props.open) popoverRef.value?.focus({ preventScroll: true })
  })
}

function deactivate() {
  document.removeEventListener('pointerdown', onPointerDown, true)
  document.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('resize', onResize)
  window.removeEventListener('scroll', onScroll, true)
  ready.value = false
}

watch(
  () => props.open,
  (open) => {
    if (open) activate()
    else deactivate()
  },
)

watch(
  () => [
    props.id,
    props.anchor.x,
    props.anchor.y,
    props.anchor.linkLeft,
    props.anchor.linkRight,
  ],
  () => {
    if (props.open) {
      ready.value = false
      void positionPopover()
    }
  },
)

onBeforeUnmount(deactivate)
</script>

<template>
  <Teleport to="body">
    <section
      v-if="open"
      ref="popoverRef"
      class="knowledge-popover"
      :class="{ 'is-ready': ready }"
      :data-placement="placement"
      :style="{
        left: `${coords.left}px`,
        top: `${coords.top}px`,
        '--arrow-offset': `${arrowOffset}px`,
      }"
      role="dialog"
      :aria-label="entry?.title ?? '知识链接'"
      tabindex="-1"
    >
      <span class="knowledge-arrow" aria-hidden="true" />

      <div class="knowledge-card">
        <header class="knowledge-head">
          <h3 class="knowledge-title">{{ entry?.title ?? '知识链接' }}</h3>
          <button class="knowledge-close" type="button" aria-label="关闭知识说明" @click="close(true)">
            <X :size="15" aria-hidden="true" />
          </button>
        </header>

        <template v-if="entry">
          <div class="knowledge-meta">
            <span class="knowledge-kind" :data-kind="entry.kind">
              <component :is="kindIcon(entry.kind)" :size="13" aria-hidden="true" />
              {{ kindLabel }}
            </span>
            <span class="knowledge-source">来源：{{ entry.source.label }}</span>
          </div>
          <p class="knowledge-summary">{{ entry.summary }}</p>
          <ul class="knowledge-points">
            <li v-for="point in entry.points" :key="point">{{ point }}</li>
          </ul>
          <a
            class="knowledge-origin"
            :href="entry.source.url"
            target="_blank"
            rel="noopener noreferrer"
            @click="close(false)"
          >
            查看官网原文
            <ExternalLink :size="14" aria-hidden="true" />
          </a>
        </template>

        <div v-else class="knowledge-missing">
          <p>该知识条目暂未收录。</p>
          <a href="https://www.yaklang.com/products/intro/" target="_blank" rel="noopener noreferrer">
            前往 Yakit 官网手册
          </a>
        </div>
      </div>
    </section>
  </Teleport>
</template>

<style scoped>
.knowledge-popover {
  position: fixed;
  z-index: 80;
  width: min(380px, calc(100vw - 32px));
  visibility: hidden;
}
.knowledge-popover.is-ready {
  visibility: visible;
  animation: knowledge-popover-in 160ms ease-out;
}
.knowledge-popover:focus { outline: none; }
.knowledge-popover:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }

.knowledge-card {
  display: grid;
  gap: var(--space-3);
  max-height: calc(100vh - 32px);
  padding: var(--space-4);
  overflow: auto;
  overscroll-behavior: contain;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--popover);
  color: var(--popover-foreground);
  box-shadow: var(--shadow-popover);
}

.knowledge-arrow {
  position: absolute;
  width: 10px;
  height: 10px;
  background: var(--popover);
  pointer-events: none;
}
.knowledge-popover[data-placement='right'] .knowledge-arrow {
  left: -6px;
  top: calc(var(--arrow-offset) - 5px);
  border-left: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  transform: rotate(45deg);
}
.knowledge-popover[data-placement='left'] .knowledge-arrow {
  right: -6px;
  top: calc(var(--arrow-offset) - 5px);
  border-top: 1px solid var(--border);
  border-right: 1px solid var(--border);
  transform: rotate(45deg);
}
.knowledge-popover[data-placement='bottom'] .knowledge-arrow {
  top: -6px;
  left: calc(var(--arrow-offset) - 5px);
  border-top: 1px solid var(--border);
  border-left: 1px solid var(--border);
  transform: rotate(45deg);
}
.knowledge-popover[data-placement='top'] .knowledge-arrow {
  bottom: -6px;
  left: calc(var(--arrow-offset) - 5px);
  border-right: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  transform: rotate(45deg);
}

.knowledge-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
}
.knowledge-title {
  margin: 0;
  font-size: var(--text-md);
  font-weight: 650;
  line-height: var(--leading-normal);
  letter-spacing: 0;
}
.knowledge-close {
  display: inline-grid;
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  padding: 0;
  place-items: center;
  border: 0;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--muted-foreground);
}
.knowledge-close:hover { background: var(--muted); color: var(--foreground); }

.knowledge-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  flex-wrap: wrap;
}
.knowledge-kind {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
}
.knowledge-kind[data-kind='yakit'] { color: var(--foreground); }
.knowledge-source {
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-normal);
  overflow-wrap: anywhere;
}
.knowledge-summary {
  margin: 0;
  font-size: var(--text-base);
  line-height: var(--leading-relaxed);
}
.knowledge-points {
  display: grid;
  gap: var(--space-1);
  margin: 0;
  padding-left: var(--space-5);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.knowledge-points li::marker { color: var(--accent-foreground); }
.knowledge-origin {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  justify-self: start;
  color: var(--accent-foreground);
  font-size: var(--text-sm);
  font-weight: 500;
}
.knowledge-origin:hover { text-decoration: underline; text-underline-offset: 3px; }
.knowledge-missing {
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.knowledge-missing p { margin: 0 0 var(--space-2); }
.knowledge-missing a {
  color: var(--accent-foreground);
  text-decoration: underline;
  text-underline-offset: 3px;
}

@keyframes knowledge-popover-in {
  from { opacity: 0; transform: translateY(2px) scale(0.99); }
  to { opacity: 1; transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .knowledge-popover.is-ready { animation: none; }
}
@media (max-width: 640px) {
  .knowledge-card { gap: var(--space-2); padding: var(--space-3); }
  .knowledge-close { width: 44px; height: 44px; }
}
</style>
