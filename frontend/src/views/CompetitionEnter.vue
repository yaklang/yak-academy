<script setup lang="ts">
// 竞赛入场页（满屏独立布局，不在 StudentLayout 内）。
// 邀请赛参赛者从站内邀请邮件跳转至此（带 competition_id + invite_code 预填），
// 显式输入邀请码入场；公开赛已报名者可留空邀请码直接入场
// （报名入口在竞赛广场 /competitions）。
// 邀请赛入场后平台其余入口锁定直到比赛结束；公开赛入场后可自由进出。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { notify } from '@/lib/notifications'
import { competitionApi, type Competition } from '../api'
import { useCompetitionStore } from '../stores/competition'

const route = useRoute()
const router = useRouter()
const store = useCompetitionStore()

const compId = Number(route.params.id)
const code = ref((route.query.code as string) ?? '')
const entering = ref(false)
const comp = ref<Competition | null>(null)
const loadFailed = ref('')

const title = computed(() => comp.value?.name ?? `竞赛 #${compId}`)

async function load() {
  try {
    const entry = await competitionApi.myEntry()
    // 已入场且仍是该竞赛 → 直接进首页（邮件链接重复点击/公开赛重进）。
    if (entry.entered && entry.competition && entry.competition.id === compId) {
      await store.refresh()
      router.replace(`/competitions/${compId}`)
      return
    }
    if (entry.competition) comp.value = entry.competition
  } catch {
    // 未入场时无竞赛详情可用（view 接口要求已入场），展示通用入场框。
  }
}

async function enter() {
  // 邀请码可留空：公开赛已报名者免码入场（未报名/邀请赛无码由后端报错提示）。
  entering.value = true
  try {
    const entry = await competitionApi.enter(compId, code.value.trim() || undefined)
    // 入场成功以 entered 判定：公开赛不锁定（locked=false），仍是成功入场。
    if (!entry.entered || !entry.competition) {
      loadFailed.value = '入场失败：竞赛可能已结束'
      return
    }
    // 锁定态以服务端为准（邀请赛锁定、公开赛/管理员不锁）。
    await store.refresh()
    notify.success('入场成功，欢迎参赛！')
    router.replace(`/competitions/${entry.competition.id}`)
  } catch (e) {
    notify.error((e as Error).message)
  } finally {
    entering.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="enter-page">
    <main class="enter-shell">
      <header class="enter-head">
        <p class="enter-eyebrow">YAK ACADEMY · COMPETITION</p>
        <h1 class="enter-title">{{ title }}</h1>
        <p class="enter-sub">输入邀请码进入赛场；公开赛已报名者可直接进入。</p>
      </header>

      <UiCard shadow="never" class="enter-card">
        <UiForm label-position="top" @submit.prevent="enter">
          <UiFormItem label="邀请码">
            <UiInput v-model="code" placeholder="粘贴邀请邮件中的邀请码" size="large" @keyup.enter="enter" />
            <p class="field-hint">公开赛已报名者可留空邀请码直接进入。</p>
          </UiFormItem>
          <UiButton type="primary" size="large" :loading="entering" class="enter-btn" @click="enter">
            进入竞赛
          </UiButton>
        </UiForm>

        <UiAlert
          type="warning"
          :closable="false"
          title="入场须知"
          description="邀请赛入场后将被锁定在竞赛页面，平台其他板块不可访问，直到比赛结束；公开赛入场后可自由进出，随时返回平台。"
          class="lock-tip"
        />
        <UiAlert v-if="loadFailed" type="error" :closable="false" :title="loadFailed" class="enter-alert" />
        <p class="enter-help">
          还没有邀请邮件？邀请赛参赛者请到
          <router-link to="/mailbox">站内邮箱</router-link>
          查收竞赛邀请；公开赛可先到
          <router-link to="/competitions">竞赛广场</router-link>
          报名。
        </p>
      </UiCard>
    </main>
  </div>
</template>

<style scoped>
.enter-page {
  display: flex;
  min-height: 100vh;
  min-height: 100svh;
  align-items: center;
  justify-content: center;
  padding: var(--space-8) var(--space-4);
  background: var(--background);
}
.enter-shell {
  display: grid;
  gap: var(--space-6);
  width: min(100%, 480px);
  min-width: 0;
}
.enter-head { display: grid; gap: var(--space-2); }
.enter-eyebrow {
  margin: 0;
  color: var(--accent-foreground);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 0.14em;
}
.enter-title {
  margin: 0;
  color: var(--foreground);
  font-size: var(--text-2xl);
  font-weight: 650;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  overflow-wrap: anywhere;
  text-wrap: balance;
}
.enter-sub {
  margin: 0;
  color: var(--muted-foreground);
  font-size: var(--text-sm);
  line-height: var(--leading-relaxed);
}
.enter-card { width: 100%; padding: 0; }
.field-hint {
  margin: var(--space-2) 0 0;
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-normal);
}
.enter-btn { width: 100%; margin-top: var(--space-1); }
.lock-tip { margin-top: var(--space-5); }
.enter-alert { margin-top: var(--space-3); }
.enter-help {
  margin: var(--space-5) 0 0;
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  color: var(--muted-foreground);
  font-size: var(--text-xs);
  line-height: var(--leading-relaxed);
}
.enter-help a { color: var(--accent-foreground); text-decoration: underline; text-underline-offset: 3px; }
@media (max-width: 560px) {
  .enter-page { padding: var(--space-6) var(--space-4); align-items: flex-start; }
  .enter-shell { gap: var(--space-5); }
}
</style>