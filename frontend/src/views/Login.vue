<script setup lang="ts">
// 登录页（Phase2 接真实登录；第一版占位骨架）。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const username = ref('')
const password = ref('')

async function submit() {
  try {
    await auth.login(username.value, password.value)
    router.push('/')
  } catch {
    // Phase2: 错误提示
  }
}
</script>

<template>
  <div class="login">
    <el-form @submit.prevent="submit">
      <h2>yak-academy 登录</h2>
      <el-form-item><el-input v-model="username" placeholder="用户名" /></el-form-item>
      <el-form-item><el-input v-model="password" type="password" placeholder="密码" /></el-form-item>
      <el-button type="primary" native-type="submit">登录</el-button>
    </el-form>
  </div>
</template>

<style scoped>
.login { max-width: 360px; margin: 10vh auto; }
</style>