<script setup lang="ts">
// 普通端布局：导航只有 教程 | 训练；头部右侧 XP、排行榜与用户区。
// 仅管理员显示「进入管理端」；普通人完全看不到管理入口。
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { authApi, progressApi } from '../api'

const auth = useAuthStore()
const router = useRouter()
const displayName = computed(() => auth.user?.username ?? '')
const role = computed(() => auth.user?.global_role ?? '')
const roleLabel = computed(() => (role.value === 'admin' ? '管理员' : '普通人'))
const canAdmin = computed(() => role.value === 'admin')
const xp = ref<number | null>(null)
const completedRooms = ref<number | null>(null)
const inProgressRooms = ref<number | null>(null)
const pwdVisible = ref(false)
const pwdForm = reactive({ current: '', next: '', confirm: '' })
const pwdLoading = ref(false)

onMounted(async () => {
  // 用户身份由路由守卫统一预取（刷新/直进 URL 不再出现头部空窗），这里只拉进度。
  if (auth.token) {
    try {
      const p = await progressApi.mine()
      xp.value = p.xp
      completedRooms.value = p.completed_rooms
      inProgressRooms.value = p.in_progress_rooms
    } catch {
      // 进度拉取失败不打断布局
    }
  }
})

function logout() {
  auth.logout()
  router.push('/login')
}

async function submitChangePassword() {
  if (!pwdForm.current || pwdForm.next.length < 6) {
    ElMessage.warning('请填写当前密码，且新密码至少 6 位')
    return
  }
  if (pwdForm.next !== pwdForm.confirm) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  pwdLoading.value = true
  try {
    await authApi.changePassword(pwdForm.current, pwdForm.next)
    pwdVisible.value = false
    ElMessage.success('密码已修改，请重新登录')
    // 改密后全部 token 失效：清本地态回登录页
    await auth.logout()
    router.push('/login')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    pwdLoading.value = false
  }
}
</script>

<template>
  <el-container class="layout">
    <el-header class="nav">
      <span class="brand"><span class="prompt">$</span> yak-academy</span>
      <nav>
        <router-link to="/">教程</router-link>
        <router-link to="/training">训练</router-link>
      </nav>
      <div class="spacer" />
      <span v-if="xp !== null" class="xp">XP: {{ xp }}</span>
      <span v-if="completedRooms !== null" class="stats" title="事件派生的 Progress：已完成 / 进行中的教程 Room">
        已完成 {{ completedRooms }} · 进行中 {{ inProgressRooms }}
      </span>
      <router-link v-if="canAdmin" to="/admin" class="admin-entry">进入管理端</router-link>
      <router-link to="/leaderboard" class="secondary">排行榜</router-link>
      <router-link to="/badges" class="secondary">徽章</router-link>
      <span v-if="displayName" class="user">{{ displayName }}（{{ roleLabel }}）</span>
      <el-button v-if="auth.token" link @click="pwdVisible = true">改密码</el-button>
      <el-button v-if="auth.token" link @click="logout">登出</el-button>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>

  <el-dialog v-model="pwdVisible" title="修改密码" width="420px">
    <el-form label-width="90px">
      <el-form-item label="当前密码">
        <el-input v-model="pwdForm.current" type="password" show-password />
      </el-form-item>
      <el-form-item label="新密码">
        <el-input v-model="pwdForm.next" type="password" show-password placeholder="至少 6 位" />
      </el-form-item>
      <el-form-item label="确认新密码">
        <el-input v-model="pwdForm.confirm" type="password" show-password />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="pwdVisible = false">取消</el-button>
      <el-button type="primary" :loading="pwdLoading" @click="submitChangePassword">确认修改</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.layout { min-height: 100vh; }
.nav {
  display: flex; align-items: center; gap: 2rem;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}
.brand { font-weight: 700; font-family: Consolas, monospace; color: var(--el-text-color-primary); }
.brand .prompt { color: var(--el-color-primary); }
nav { display: flex; gap: 1.5rem; }
nav a {
  color: var(--el-text-color-regular); text-decoration: none;
  padding-bottom: 2px; border-bottom: 2px solid transparent;
}
nav a.router-link-active { color: var(--el-color-primary); border-bottom-color: var(--el-color-primary); }
.spacer { flex: 1; }
.xp { color: var(--el-color-primary); font-weight: 600; font-family: Consolas, monospace; }
.stats { color: var(--el-text-color-secondary); font-size: 0.85rem; }
.admin-entry { color: var(--el-color-warning); text-decoration: none; font-weight: 600; }
.secondary { color: var(--el-text-color-secondary); text-decoration: none; font-size: 0.9rem; }
.user { color: var(--el-text-color-secondary); font-size: 0.9rem; }
</style>