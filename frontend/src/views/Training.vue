<script setup lang="ts">
// 训练场：跨主题的全量题目列表。容器化题目可内联启动/销毁环境（Docker 驱动部署题目），
// 并支持内联 flag 提交与判定轮询；点题目名可跳转 Room 详情查看配套教程（教题一体）。
// 普通端与管理端共用。
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  trainingApi, instanceApi, submissionApi,
  type TrainingItem, type LabInstance,
} from '../api'

const route = useRoute()
const items = ref<TrainingItem[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(true)
// 每题的内联状态：容器实例 / 环境操作 loading / flag 输入 / 提交轮询状态。
const instances = reactive<Record<number, LabInstance | null>>({})
const envLoading = reactive<Record<number, boolean>>({})
const answers = reactive<Record<number, string>>({})
const results = reactive<Record<number, { status: string; score: number } | null>>({})

const base = computed(() => (route.path.startsWith('/admin') ? '/admin/tutorials' : '/topics'))

async function load() {
  loading.value = true
  try {
    const resp = await trainingApi.list(page.value)
    items.value = resp.items
    total.value = resp.total
    // has_env 的题目拉一次本人实例状态（无实例静默）。
    for (const it of resp.items) {
      if (it.has_env) loadInstance(it)
    }
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function loadInstance(it: TrainingItem) {
  try {
    instances[it.id] = await instanceApi.get(it.room_slug)
  } catch {
    instances[it.id] = null // 无实例（含 501 驱动未配置）静默处理
  }
}

async function startEnv(it: TrainingItem) {
  envLoading[it.id] = true
  try {
    instances[it.id] = await instanceApi.start(it.room_slug)
  } catch (e) {
    ElMessage.error(`环境启动失败：${(e as Error).message}`)
  } finally {
    envLoading[it.id] = false
  }
}

async function destroyEnv(it: TrainingItem) {
  envLoading[it.id] = true
  try {
    await instanceApi.destroy(it.room_slug)
    instances[it.id] = null
  } catch (e) {
    ElMessage.error(`环境销毁失败：${(e as Error).message}`)
  } finally {
    envLoading[it.id] = false
  }
}

async function submit(it: TrainingItem) {
  const answer = answers[it.id]
  if (!answer) return
  try {
    const sub = await submissionApi.submit(it.room_slug, answer)
    // 轮询判定：2s 间隔，上限 30 次（异步判题，event-sourcing-rules skill）。
    for (let i = 0; i < 30; i++) {
      await new Promise((r) => setTimeout(r, 2000))
      const cur = await submissionApi.get(sub.id)
      if (cur.status !== 'pending') {
        results[it.id] = { status: cur.status, score: cur.score }
        if (cur.status === 'correct') {
          ElMessage.success(`${it.title} 回答正确！+${cur.score} 分`)
          it.solved = true
        } else {
          ElMessage.error(`${it.title} 回答错误，再试试`)
        }
        return
      }
    }
    ElMessage.warning('判定超时，请稍后刷新查看')
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}

function difficultyType(d: TrainingItem['difficulty']) {
  return d === 'apprentice' ? 'success' : d === 'practitioner' ? 'warning' : 'danger'
}

onMounted(load)
</script>

<template>
  <h2>训练</h2>
  <el-skeleton v-if="loading" :rows="4" animated />
  <el-empty v-else-if="items.length === 0" description="暂无题目：等待管理员上传内容包" />
  <template v-else>
    <div v-for="it in items" :key="it.id" class="chal-card">
      <el-card>
        <div class="head">
          <router-link class="title" :to="`${base}/${it.topic_slug}/rooms/${it.room_slug}`">
            {{ it.title }}
          </router-link>
          <div class="tags">
            <el-tag :type="difficultyType(it.difficulty)" size="small">{{ it.difficulty }}</el-tag>
            <el-tag v-if="it.has_env" type="primary" size="small">容器环境</el-tag>
            <el-tag v-if="it.solved" type="success" size="small">已通关</el-tag>
          </div>
        </div>
        <p class="meta">所属 Room：{{ it.room_title }} · 分值 {{ it.score }}</p>

        <!-- 容器化题目：内联环境生命周期（开启容器后题目部署在容器中） -->
        <div v-if="it.has_env" class="env-row">
          <template v-if="instances[it.id] && instances[it.id]!.status !== 'destroyed' && instances[it.id]!.status !== 'stopped'">
            <el-tag :type="instances[it.id]!.status === 'running' ? 'success' : 'warning'" size="small">
              环境{{ instances[it.id]!.status === 'running' ? '运行中' : '启动中' }}
            </el-tag>
            <code v-if="instances[it.id]!.endpoint" class="endpoint">{{ instances[it.id]!.endpoint }}</code>
            <span v-if="instances[it.id]!.expire_at" class="expire">
              到期 {{ new Date(instances[it.id]!.expire_at).toLocaleTimeString() }}
            </span>
            <el-button size="small" :loading="envLoading[it.id]" @click="destroyEnv(it)">销毁环境</el-button>
          </template>
          <el-button v-else size="small" type="primary" :loading="envLoading[it.id]" @click="startEnv(it)">
            开启容器
          </el-button>
        </div>

        <!-- 内联 flag 提交 -->
        <div class="submit-row">
          <el-input
            v-model="answers[it.id]"
            placeholder="输入 flag"
            :disabled="it.solved"
            @keyup.enter="submit(it)"
          />
          <el-button type="primary" :disabled="it.solved || !answers[it.id]" @click="submit(it)">提交</el-button>
        </div>
        <el-alert
          v-if="results[it.id]"
          :title="results[it.id]!.status === 'correct' ? `判定通过，得分 ${results[it.id]!.score}` : '判定不通过'"
          :type="results[it.id]!.status === 'correct' ? 'success' : 'error'"
          :closable="false"
          class="result"
        />
      </el-card>
    </div>
    <el-pagination
      v-if="total > 20"
      layout="prev, pager, next"
      :total="total"
      :page-size="20"
      v-model:current-page="page"
      @current-change="load"
      class="pager"
    />
  </template>
</template>

<style scoped>
.chal-card { margin-bottom: 1rem; max-width: 720px; }
.head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; }
.title { font-size: 1.05rem; font-weight: 600; color: var(--el-text-color-primary); text-decoration: none; }
.title:hover { color: var(--el-color-primary); }
.tags { display: flex; gap: 0.4rem; }
.meta { color: var(--el-text-color-secondary); font-size: 0.88rem; margin: 0.4rem 0 0.8rem; }
.env-row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.8rem; }
.endpoint { background: var(--el-fill-color-light); padding: 0.1rem 0.4rem; border-radius: 3px; font-family: Consolas, monospace; color: var(--el-color-primary); }
.expire { color: var(--el-text-color-secondary); font-size: 0.8rem; }
.submit-row { display: flex; gap: 0.5rem; }
.result { margin-top: 0.8rem; }
.pager { margin-top: 1rem; }
</style>