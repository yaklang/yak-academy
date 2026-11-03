<script setup lang="ts">
// Room 视频区：mp4/webm 直链用原生 video，其余可嵌入页面用 iframe。
// 视频地址来自管理员内容包，后端已做 http(s) 白名单；这里仍按分类渲染，
// 旧数据中的非法地址只提示、不生成可点击链接。
import { computed, ref, watch } from 'vue'
import { ExternalLink, Play, TriangleAlert } from '@lucide/vue'
import { videoKind } from '@/lib/video'

const props = defineProps<{ src: string }>()

const kind = computed(() => videoKind(props.src))
const failed = ref(false)

watch(
  () => props.src,
  () => {
    failed.value = false
  },
)
</script>

<template>
  <section v-if="kind === 'none'" class="video-card invalid" role="alert">
    <div class="video-invalid">
      <TriangleAlert :size="18" aria-hidden="true" />
      <div>
        <strong>视频地址不可用</strong>
        <p>当前视频地址不是可播放的 http(s) 链接，请联系管理员更新教程包。</p>
      </div>
    </div>
  </section>

  <section v-else class="video-card" aria-label="视频教程">
    <header class="video-head">
      <span class="video-label">
        <Play :size="14" aria-hidden="true" />
        视频教程
      </span>
      <a class="video-open" :href="src" target="_blank" rel="noopener noreferrer">
        在新窗口打开
        <ExternalLink :size="14" aria-hidden="true" />
      </a>
    </header>

    <div class="video-stage">
      <video
        v-if="kind === 'direct' && !failed"
        :src="src"
        controls
        playsinline
        preload="metadata"
        @error="failed = true"
      />
      <iframe
        v-else-if="kind === 'embed'"
        :src="src"
        title="视频教程"
        allowfullscreen
        sandbox="allow-scripts allow-same-origin allow-presentation allow-popups allow-popups-to-escape-sandbox"
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
        referrerpolicy="strict-origin-when-cross-origin"
      />
      <div v-else class="video-fallback" role="alert">
        <p>视频加载失败，可能是地址失效或站点不允许嵌入。</p>
        <a :href="src" target="_blank" rel="noopener noreferrer">在新窗口打开视频</a>
      </div>
    </div>
  </section>
</template>

<style scoped>
.video-card {
  max-width: min(
    var(--lesson-reading-width, 780px),
    calc(100% - var(--lesson-toc-width, 220px) - var(--lesson-gutter, var(--space-10)))
  );
  margin-bottom: var(--space-8);
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--panel);
}
.video-card.invalid { padding: var(--space-4); }
.video-invalid {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  color: var(--warning);
}
.video-invalid strong { display: block; color: var(--foreground); font-size: var(--text-sm); }
.video-invalid p { margin: var(--space-1) 0 0; color: var(--muted-foreground); font-size: var(--text-sm); line-height: var(--leading-relaxed); }
.video-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}
.video-label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--foreground);
  font-size: var(--text-sm);
  font-weight: 600;
}
.video-label svg { color: var(--accent-foreground); }
.video-open {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-height: var(--space-10);
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  border-radius: var(--radius-control);
}
.video-open:hover { text-decoration: underline; text-underline-offset: 3px; }
.video-stage {
  aspect-ratio: 16 / 9;
  background: var(--muted);
}
.video-stage video,
.video-stage iframe {
  display: block;
  width: 100%;
  height: 100%;
  border: 0;
}
.video-stage video { object-fit: contain; }
.video-fallback {
  display: flex;
  height: 100%;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  padding: var(--space-6);
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
  text-align: center;
}
.video-fallback a { color: var(--accent-foreground); font-weight: 500; }
.video-fallback a:hover { text-decoration: underline; text-underline-offset: 3px; }

@media (max-width: 1024px) {
  .video-card { max-width: 100%; }
}
@media (max-width: 720px) {
  .video-head {
    flex-direction: column;
    align-items: flex-start;
  }
  .video-open { min-height: var(--space-12); }
}
</style>
