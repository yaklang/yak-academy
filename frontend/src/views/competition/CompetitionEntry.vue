<script setup lang="ts">
// 竞赛入口分发（满屏独立布局）：先取格式无关的入口元数据，再按 format
// 渲染 CTF / AWD / Exam 参赛视图。AWD 与 CTF 在页面层完全独立，互不引用。
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { competitionApi, type CompetitionEntryView } from '../../api'
import { ApiError } from '../../api/client'
import CompetitionHome from '../CompetitionHome.vue'
import AwdParticipant from './awd/AwdParticipant.vue'

const route = useRoute()
const compId = Number(route.params.id)

const entry = ref<CompetitionEntryView | null>(null)
const loading = ref(true)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    entry.value = await competitionApi.entry(compId)
  } catch (e) {
    // 401 由登录流程接管，避免覆盖跳转。
    if (e instanceof ApiError && e.status === 401) return
    loadError.value = e instanceof Error ? e.message : '竞赛加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="entry-loading" role="status" aria-label="正在加载竞赛">
    <UiSkeleton :rows="6" />
  </div>
  <div v-else-if="loadError" class="entry-error">
    <UiLoadError title="无法加载竞赛" :message="loadError" @retry="load" />
    <router-link class="entry-link" :to="`/competitions/enter/${compId}`">前往竞赛入场页</router-link>
  </div>
  <AwdParticipant v-else-if="entry?.competition.format === 'awd'" :competition-id="compId" />
  <CompetitionHome v-else-if="entry?.competition.format === 'ctf'" />
  <div v-else class="entry-unsupported">
    <UiAlert
      type="warning"
      :closable="false"
      title="该竞赛形式尚未接入参赛端"
      description="在线考试的参赛页面将在后续版本提供；当前可进入 CTF 与 AWD 竞赛。"
    />
    <router-link class="entry-link" to="/competitions">返回竞赛广场</router-link>
  </div>
</template>

<style scoped>
.entry-loading,
.entry-error,
.entry-unsupported {
  display: grid;
  min-height: 100vh;
  align-content: start;
  gap: var(--space-4);
  width: min(100%, 960px);
  margin-inline: auto;
  padding: var(--space-8) var(--space-4);
  background: var(--background);
}
.entry-error,
.entry-unsupported { align-content: center; justify-items: center; }
.entry-loading { align-content: center; }
.entry-link { color: var(--accent-foreground); font-size: var(--text-sm); text-decoration: underline; text-underline-offset: 3px; }
</style>
