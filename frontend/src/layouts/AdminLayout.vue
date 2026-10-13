<script setup lang="ts">
// 管理端布局：教程 | 训练 | 管理面板 三组导航（管理面板含题目/教程上传、容器管理、用户与角色）。
// 底部提供"返回普通端"入口。教程/训练与管理端预览共用普通端视图。
import { useAuthStore } from '../stores/auth'
import { useRouter } from 'vue-router'

const auth = useAuthStore()
const router = useRouter()

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <el-container class="layout">
    <el-aside width="220px" class="side">
      <div class="brand">yak-academy 管理端</div>
      <nav>
        <router-link to="/admin/tutorials">教程</router-link>
        <router-link to="/admin/training">训练</router-link>
        <div class="group-title">管理面板</div>
        <router-link to="/admin/panel/packages" class="sub">教程 / 题目上传</router-link>
        <router-link to="/admin/panel/containers" class="sub">容器管理</router-link>
        <router-link to="/admin/panel/users" class="sub">用户与角色</router-link>
      </nav>
      <div class="side-footer">
        <router-link to="/" class="back">← 返回普通端</router-link>
        <el-button v-if="auth.token" link size="small" @click="logout">登出</el-button>
      </div>
    </el-aside>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; }
.side { display: flex; flex-direction: column; border-right: 1px solid #eee; }
.brand { font-weight: 700; padding: 1rem; }
nav { display: flex; flex-direction: column; padding: 0 1rem; gap: 0.5rem; flex: 1; }
nav a { color: #333; text-decoration: none; }
nav a.router-link-active { color: #409eff; }
nav .sub { padding-left: 1rem; font-size: 0.92rem; color: #555; }
.group-title { margin-top: 0.5rem; font-size: 0.8rem; color: #999; }
.side-footer { display: flex; align-items: center; justify-content: space-between; padding: 1rem; }
.back { color: #e6a23c; text-decoration: none; font-weight: 600; font-size: 0.9rem; }
</style>