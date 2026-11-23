<script setup lang="ts">
// 竞赛入口分发（满屏独立布局）：先取格式无关的入口元数据，再按 format
// 渲染 CTF / AWD / Exam 参赛视图。AWD 与 CTF 在页面层完全独立，互不引用。
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { competitionApi, type CompetitionEntryView, type CompetitionProfileInput, type CompetitionProfileView } from '../../api'
import { ApiError } from '../../api/client'
import { notify } from '@/lib/notifications'
import CompetitionHome from '../CompetitionHome.vue'
import AwdParticipant from './awd/AwdParticipant.vue'
import ExamParticipant from './exam/ExamParticipant.vue'

const route = useRoute()
const compId = Number(route.params.id)

const entry = ref<CompetitionEntryView | null>(null)
const loading = ref(true)
const loadError = ref('')
const profile = ref<CompetitionProfileView | null>(null)
const showProfile = ref(false)
const savingProfile = ref(false)
const profileForm = ref<CompetitionProfileInput>({
  real_name: '',
  school: '',
  student_no: '',
  email: '',
  phone: '',
})
const canEditProfile = computed(
  () => profile.value !== null && entry.value?.competition.status !== 'ended',
)
const profileButtonLabel = computed(() => (profile.value?.has_profile ? '参赛信息' : '补填参赛信息'))

function syncProfileForm() {
  profileForm.value = {
    real_name: profile.value?.real_name ?? '',
    school: profile.value?.school ?? '',
    student_no: profile.value?.student_no ?? '',
    email: profile.value?.email ?? '',
    phone: profile.value?.phone ?? '',
  }
}

function openProfile() {
  syncProfileForm()
  showProfile.value = true
}

async function saveProfile() {
  if (!profileForm.value.real_name.trim()) {
    notify.warning('请填写姓名')
    return
  }
  savingProfile.value = true
  try {
    profile.value = await competitionApi.updateProfile(compId, {
      ...profileForm.value,
      real_name: profileForm.value.real_name.trim(),
    })
    syncProfileForm()
    showProfile.value = false
    notify.success('参赛信息已保存')
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) notify.error((e as Error).message)
  } finally {
    savingProfile.value = false
  }
}

async function loadProfile() {
  try {
    profile.value = await competitionApi.profile(compId)
    syncProfileForm()
  } catch (e) {
    // 非参赛者（404）不展示补填入口；其余错误不阻塞竞赛入口。
    if (!(e instanceof ApiError)) return
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    entry.value = await competitionApi.entry(compId)
    await loadProfile()
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
  <ExamParticipant v-else-if="entry?.competition.format === 'exam'" :competition-id="compId" />
  <CompetitionHome v-else-if="entry?.competition.format === 'ctf'" />
  <div v-else class="entry-unsupported">
    <UiAlert
      type="warning"
      :closable="false"
      title="该竞赛形式尚未接入参赛端"
      description="当前可进入 CTF、AWD 与在线考试竞赛。"
    />
    <router-link class="entry-link" to="/competitions">返回竞赛广场</router-link>
  </div>

  <UiButton v-if="canEditProfile" class="entry-profile-fab" size="small" @click="openProfile">
    {{ profileButtonLabel }}
  </UiButton>
  <UiDialog v-model="showProfile" title="参赛信息" width="560px">
    <p class="entry-profile-hint">
      实名信息仅用于本场赛务（签到、证书、晋级），不会修改平台账号资料。
    </p>
    <UiForm label-width="90px">
      <UiFormItem label="姓名" required>
        <UiInput v-model="profileForm.real_name" maxlength="64" placeholder="与证件一致" />
      </UiFormItem>
      <UiFormItem label="学校 / 单位">
        <UiInput v-model="profileForm.school" maxlength="128" />
      </UiFormItem>
      <UiFormItem label="学号">
        <UiInput v-model="profileForm.student_no" maxlength="64" />
      </UiFormItem>
      <UiFormItem label="邮箱">
        <UiInput v-model="profileForm.email" maxlength="128" placeholder="用于证书通知（可选）" />
      </UiFormItem>
      <UiFormItem label="手机">
        <UiInput v-model="profileForm.phone" maxlength="32" placeholder="用于赛务联系（可选）" />
      </UiFormItem>
    </UiForm>
    <template #footer>
      <UiButton @click="showProfile = false">取消</UiButton>
      <UiButton type="primary" :loading="savingProfile" :disabled="savingProfile" @click="saveProfile">
        保存
      </UiButton>
    </template>
  </UiDialog>
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
.entry-profile-fab { position: fixed; right: var(--space-5); bottom: var(--space-5); z-index: 60; }
.entry-profile-hint { margin: 0 0 var(--space-4); color: var(--muted-foreground); font-size: var(--text-xs); line-height: 1.6; }
</style>
