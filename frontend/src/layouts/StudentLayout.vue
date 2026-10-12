<script setup lang="ts">
// 学生端布局：顶部导航（主题/路径/排行榜）。
</script>

<template>
  <el-container class="layout">
    <el-header class="nav">
      <span class="brand">yak-academy</span>
      <nav>
        <router-link to="/">主题</router-link>
        <router-link to="/paths">学习路径</router-link>
        <router-link to="/leaderboard">排行榜</router-link>
      </nav>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; }
.nav { display: flex; align-items: center; gap: 2rem; border-bottom: 1px solid #eee; }
.brand { font-weight: 700; }
nav a { margin-right: 1rem; color: #333; text-decoration: none; }
nav a.router-link-active { color: #409eff; }
</style>