<script setup lang="ts">
// 登录页：真实对接 POST /auth/login（bcrypt 校验在后端）。
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { ApiError } from '../api/client'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const loading = ref(false)

// 登录后跳回原目标页（路由守卫 redirect query）
const redirect = computed(() => (route.query.redirect as string) || null)

async function submit() {
  if (!username.value || !password.value) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    await auth.fetchMe()
    router.push(redirect.value ?? '/')
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <el-form @submit.prevent="submit">
      <h2>yak-academy 登录</h2>
      <el-form-item><el-input v-model="username" placeholder="用户名" /></el-form-item>
      <el-form-item><el-input v-model="password" type="password" placeholder="密码" show-password /></el-form-item>
      <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">登录</el-button>
    </el-form>
  </div>
</template>

<style scoped>
.login { max-width: 360px; margin: 10vh auto; }
</style>