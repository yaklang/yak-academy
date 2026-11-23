<script setup lang="ts">
// 管理端竞赛详情分发：按 format 渲染 CTF 详情（现有）/ AWD 控制台 / 考试管理。
// 入口元数据只用于格式判定，赛制页面各自加载自己的管理数据。
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { adminCompetitionApi, type CompetitionDetail } from '../../api'
import CompetitionDetailView from '../admin/CompetitionDetail.vue'
import AwdAdmin from './awd/AwdAdmin.vue'
import ExamAdmin from './exam/ExamAdmin.vue'

const route = useRoute()
const compId = Number(route.params.id)

const detail = ref<CompetitionDetail | null>(null)
const loading = ref(true)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    detail.value = await adminCompetitionApi.detail(compId)
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '竞赛详情加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="admin-entry-loading" role="status" aria-label="正在加载竞赛详情">
    <UiSkeleton :rows="6" />
  </div>
  <UiLoadError v-else-if="loadError" :message="loadError" @retry="load" />
  <AwdAdmin v-else-if="detail?.format === 'awd'" :competition-id="compId" :detail="detail" />
  <ExamAdmin v-else-if="detail?.format === 'exam'" :competition-id="compId" :detail="detail" />
  <CompetitionDetailView v-else />
</template>

<style scoped>
.admin-entry-loading { display: grid; gap: var(--space-4); }
</style>
