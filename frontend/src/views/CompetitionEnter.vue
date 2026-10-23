<script setup lang="ts">
// 竞赛入场页（满屏独立布局，不在 StudentLayout 内）。
// 邀请赛参赛者从站内邀请邮件跳转至此（带 competition_id + invite_code 预填），
// 显式输入邀请码入场；公开赛已报名者可留空邀请码直接入场
// （报名入口在竞赛广场 /competitions）。
// 入场后平台其余入口锁定，直到比赛结束。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
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
    // 已入场且仍是该竞赛 → 直接进首页（邮件链接重复点击）。
    if (entry.locked && entry.competition && entry.competition.id === compId) {
      store.lock(compId)
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
    if (!entry.locked || !entry.competition) {
      loadFailed.value = '入场失败：竞赛可能已结束'
      return
    }
    store.lock(entry.competition.id)
    ElMessage.success('入场成功，欢迎参赛！')
    router.replace(`/competitions/${entry.competition.id}`)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    entering.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="enter-page">
    <el-card shadow="never" class="enter-card">
      <h2>{{ title }}</h2>
      <p class="dim">
        凭邀请码入场（邀请码在站内邮箱的竞赛邀请邮件中）；
        公开赛已报名者可留空邀请码直接进入。
      </p>
      <el-form label-position="top" @submit.prevent="enter">
        <el-form-item label="邀请码（公开赛已报名可留空）">
          <el-input v-model="code" placeholder="邀请码" size="large" @keyup.enter="enter" />
        </el-form-item>
        <el-button type="primary" size="large" :loading="entering" class="enter-btn" @click="enter">
          进入竞赛
        </el-button>
      </el-form>
      <el-alert
        type="warning"
        :closable="false"
        title="入场后将被锁定在竞赛页面，平台其他板块不可访问，直到比赛结束。"
        class="lock-tip"
      />
      <p v-if="loadFailed" class="dim">{{ loadFailed }}</p>
      <p class="dim">
        还没有邀请邮件？邀请赛参赛者请到
        <router-link to="/mailbox">站内邮箱</router-link>
        查收竞赛邀请；公开赛可先到
        <router-link to="/competitions">竞赛广场</router-link>
        报名。
      </p>
    </el-card>
  </div>
</template>

<style scoped>
.enter-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-lighter);
}
.enter-card { width: 440px; padding: 0.5rem; }
.enter-btn { width: 100%; margin-top: 0.4rem; }
.lock-tip { margin-top: 1rem; }
.dim { color: var(--el-text-color-secondary); font-size: 0.85rem; }
</style>