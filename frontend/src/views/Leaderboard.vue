<script setup lang="ts">
// 排行榜页（事件派生数据视图）。
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { progressApi, type LeaderboardEntry } from '../api'

const entries = ref<LeaderboardEntry[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const resp = await progressApi.leaderboard(page.value)
    entries.value = resp.items
    total.value = resp.total
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <h2>排行榜</h2>
  <el-table :data="entries" v-loading="loading">
    <el-table-column prop="rank" label="排名" width="80" />
    <el-table-column prop="username" label="用户名" />
    <el-table-column prop="score" label="分数" width="120" />
    <el-table-column prop="solved_count" label="解题数" width="100" />
  </el-table>
  <el-pagination
    v-if="total > 20"
    layout="prev, pager, next"
    :total="total"
    :page-size="20"
    v-model:current-page="page"
    @current-change="load"
  />
</template>