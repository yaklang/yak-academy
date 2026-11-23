<script setup lang="ts">
// 公开证书验证页：凭验证码读取证书快照（无需登录），支持浏览器打印成 PDF。
// 只展示证书本身信息，不返回账号/用户 ID 等内部标识。
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { certificateApi, type CertificateVerificationView } from '../api'
import { ApiError } from '../api/client'
import { formatDateTime } from '../lib/format'

const route = useRoute()
const code = String(route.params.code ?? '')
const cert = ref<CertificateVerificationView | null>(null)
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    cert.value = await certificateApi.verify(code)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      error.value = '未找到该证书：请核对验证码是否完整，或联系赛事组委会。'
    } else {
      error.value = e instanceof Error ? e.message : '证书验证失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}

function printCertificate() {
  window.print()
}

onMounted(load)
</script>

<template>
  <div class="cert-page">
    <header class="cert-topbar">
      <span class="cert-brand">YAK ACADEMY · 证书验证</span>
      <router-link to="/" class="cert-home">返回首页</router-link>
    </header>

    <main class="cert-body">
      <UiSkeleton v-if="loading" :rows="6" class="cert-skeleton" />
      <UiLoadError v-else-if="error" title="证书验证失败" :message="error" @retry="load" />
      <article v-else-if="cert" class="cert-paper">
        <div class="cert-seal" aria-hidden="true">
          <svg width="34" height="38" viewBox="0 0 27 30" fill="none">
            <path
              d="M3 4L13.5 12.5L24 4M13.5 12.5V27M3 12L9 17M24 12L18 17"
              stroke="currentColor"
              stroke-width="2.5"
              stroke-linecap="square"
            />
          </svg>
        </div>
        <p class="cert-eyebrow">CERTIFICATE OF ACHIEVEMENT</p>
        <h1 class="cert-heading">{{ cert.recipient_name }}</h1>
        <p class="cert-lead">
          在 <b>{{ cert.competition_name }}</b> 中荣获
          <b>{{ cert.title }}</b>（{{ cert.level }}）。
        </p>
        <div class="cert-meta">
          <div>
            <span>证书级别</span>
            <b>{{ cert.level }}</b>
          </div>
          <div>
            <span>竞赛名次</span>
            <b>{{ cert.rank }}</b>
          </div>
          <div>
            <span>竞赛得分</span>
            <b>{{ cert.score }}</b>
          </div>
          <div>
            <span>签发时间</span>
            <b>{{ cert.issued_at ? formatDateTime(cert.issued_at) : '—' }}</b>
          </div>
        </div>
        <div class="cert-footer">
          <div class="cert-code">
            <span>验证码</span>
            <code>{{ cert.code }}</code>
          </div>
          <UiTag type="success" size="small">已验证 · 证书有效</UiTag>
        </div>
        <p class="cert-notice">
          本证书由 YAK Academy 赛事系统签发，验证信息以本页快照为准。
          可访问 <code>/certificates/{{ cert.code }}</code> 复核。
        </p>
      </article>

      <div v-if="cert" class="cert-actions">
        <UiButton type="primary" @click="printCertificate">打印 / 保存 PDF</UiButton>
        <span class="cert-actions-hint">使用浏览器打印功能可保存为 PDF 或直接打印。</span>
      </div>
    </main>
  </div>
</template>

<style scoped>
.cert-page {
  min-height: 100vh;
  background:
    radial-gradient(circle at 15% 10%, color-mix(in srgb, var(--primary) 8%, transparent), transparent 45%),
    var(--background);
  color: var(--foreground);
}
.cert-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  width: min(100%, 960px);
  margin-inline: auto;
  padding: var(--space-5);
}
.cert-brand {
  font-size: var(--text-xs);
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--muted-foreground);
}
.cert-home {
  font-size: var(--text-sm);
  color: var(--accent-foreground);
  text-decoration: underline;
  text-underline-offset: 3px;
}
.cert-body {
  display: grid;
  gap: var(--space-5);
  width: min(100%, 960px);
  margin-inline: auto;
  padding: 0 var(--space-5) var(--space-10);
}
.cert-skeleton { min-height: 420px; }
.cert-paper {
  position: relative;
  padding: clamp(2rem, 5vw, 4rem);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--card);
  box-shadow: 0 18px 60px color-mix(in srgb, var(--foreground) 8%, transparent);
  text-align: center;
}
.cert-paper::before {
  content: '';
  position: absolute;
  inset: 12px;
  border: 1px solid color-mix(in srgb, var(--primary) 28%, var(--border));
  border-radius: calc(var(--radius-lg) - 6px);
  pointer-events: none;
}
.cert-seal {
  display: grid;
  place-items: center;
  width: 64px;
  height: 64px;
  margin: 0 auto var(--space-4);
  border: 1px solid var(--border);
  border-radius: 50%;
  color: var(--primary);
  background: var(--muted);
}
.cert-eyebrow {
  margin: 0 0 var(--space-3);
  font-size: var(--text-xs);
  letter-spacing: 0.22em;
  color: var(--muted-foreground);
}
.cert-heading {
  margin: 0 0 var(--space-3);
  font-size: clamp(2rem, 5vw, 3.25rem);
  font-weight: 700;
  line-height: 1.15;
}
.cert-lead {
  max-width: 34em;
  margin: 0 auto var(--space-6);
  font-size: var(--text-base);
  line-height: 1.8;
  color: var(--muted-foreground);
}
.cert-lead b { color: var(--foreground); font-weight: 600; }
.cert-meta {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-6);
}
.cert-meta > div {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--muted);
}
.cert-meta span { font-size: var(--text-xs); color: var(--muted-foreground); }
.cert-meta b { font-size: var(--text-base); font-weight: 600; font-variant-numeric: tabular-nums; }
.cert-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}
.cert-code { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xs); color: var(--muted-foreground); }
.cert-code code { font-size: var(--text-sm); color: var(--foreground); letter-spacing: 0.08em; }
.cert-notice {
  margin: var(--space-4) 0 0;
  font-size: var(--text-xs);
  line-height: 1.7;
  color: var(--muted-foreground);
}
.cert-notice code { font-family: var(--font-mono); }
.cert-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); }
.cert-actions-hint { font-size: var(--text-xs); color: var(--muted-foreground); }
@media (max-width: 720px) {
  .cert-meta { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media print {
  .cert-topbar,
  .cert-actions { display: none !important; }
  .cert-page { background: #fff; }
  .cert-body { width: 100%; padding: 0; }
  .cert-paper {
    border: 2px solid #111;
    box-shadow: none;
    border-radius: 0;
    break-inside: avoid;
  }
}
</style>