<script setup lang="ts">
// 竞赛入场页（满屏独立布局，不在应用侧栏内）。
// 邀请赛参赛者从站内邀请邮件跳转至此（带 competition_id + invite_code 预填），
// 显式输入邀请码入场；公开赛已报名者可留空邀请码直接入场。
// 邀请赛入场后平台其余入口锁定直到比赛结束；公开赛入场后可自由进出。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LoaderCircle } from '@lucide/vue'
import Brand from '@/components/Brand.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import FormField from '@/components/layout/FormField.vue'
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
  loadFailed.value = ''
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
    loadFailed.value = (e as Error).message
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
        <Brand to="/" />
        <p class="enter-badge">竞赛入场</p>
        <h1 class="enter-title">{{ title }}</h1>
        <p class="enter-sub">输入邀请码进入赛场；公开赛已报名者可直接进入。</p>
      </header>

      <section class="enter-card" aria-label="入场信息">
        <form class="enter-form" novalidate @submit.prevent="enter">
          <FormField
            label="邀请码"
            html-for="invite-code"
            hint="公开赛已报名者可留空邀请码直接进入。"
          >
            <template #default="{ id, describedby }">
              <Input
                :id="id"
                v-model="code"
                placeholder="粘贴邀请邮件中的邀请码"
                :aria-describedby="describedby"
                @keyup.enter="enter"
              />
            </template>
          </FormField>
          <Button type="submit" class="enter-btn" :disabled="entering" :aria-busy="entering">
            <LoaderCircle v-if="entering" class="spin" aria-hidden="true" />
            进入竞赛
          </Button>
        </form>

        <div class="enter-notice" role="status">
          <strong>入场须知</strong>
          <p>邀请赛入场后将被锁定在竞赛页面，平台其他板块不可访问，直到比赛结束；公开赛入场后可自由进出，随时返回平台。</p>
        </div>

        <p v-if="loadFailed" class="enter-error" role="alert">
          {{ loadFailed }}
        </p>

        <p class="enter-help">
          还没有邀请邮件？邀请赛参赛者请到
          <RouterLink to="/mailbox">站内邮箱</RouterLink>
          查收竞赛邀请；公开赛可先到
          <RouterLink to="/competitions">竞赛广场</RouterLink>
          报名。
        </p>
      </section>
    </main>
  </div>
</template>

<style scoped>
.enter-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100dvh;
  padding: var(--space-8) var(--space-4);
  background: var(--bg-canvas);
}

.enter-shell {
  display: grid;
  gap: var(--space-6);
  width: min(100%, 480px);
  min-width: 0;
}

.enter-head {
  display: grid;
  gap: var(--space-2);
}

.enter-badge {
  margin-top: var(--space-4);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  font-weight: 500;
  letter-spacing: 0.06em;
}

.enter-title {
  color: var(--text-primary);
  font-size: var(--text-page);
  font-weight: 600;
  line-height: var(--leading-tight);
  letter-spacing: 0;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.enter-sub {
  color: var(--text-secondary);
  font-size: var(--text-caption);
  line-height: var(--leading-relaxed);
}

.enter-card {
  display: grid;
  gap: var(--space-5);
  padding: var(--space-6);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  background: var(--bg-surface);
}

.enter-form {
  display: grid;
  gap: var(--space-4);
}

.enter-btn {
  width: 100%;
  min-height: 44px;
  justify-content: center;
  gap: var(--space-2);
}

.enter-notice {
  display: grid;
  gap: var(--space-1);
  padding: var(--space-3);
  border-radius: var(--radius-control);
  background: var(--bg-subtle);
  color: var(--text-secondary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.enter-notice strong {
  color: var(--text-primary);
  font-size: var(--text-caption);
  font-weight: 600;
}

.enter-error {
  padding: var(--space-3);
  border: 1px solid color-mix(in srgb, var(--status-danger) 32%, transparent);
  border-radius: var(--radius-control);
  background: color-mix(in srgb, var(--status-danger) 8%, var(--bg-surface));
  color: var(--status-danger);
  font-size: var(--text-caption);
  line-height: var(--leading-normal);
}

.enter-help {
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
  color: var(--text-tertiary);
  font-size: var(--text-meta);
  line-height: var(--leading-relaxed);
}

.enter-help a {
  color: var(--action-subtle-fg);
  text-decoration: underline;
  text-underline-offset: 3px;
}

@media (max-width: 560px) {
  .enter-page {
    align-items: flex-start;
    padding: var(--space-6) var(--space-4);
  }

  .enter-shell {
    gap: var(--space-4);
  }

  .enter-card {
    padding: var(--space-5);
  }
}
</style>
