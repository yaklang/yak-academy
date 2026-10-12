<script setup lang="ts">
// 管理端布局：内容包管理 / 用户管理。
</script>

<template>
  <el-container class="layout">
    <el-aside width="200px">
      <div class="brand">yak-academy 管理端</div>
      <nav>
        <router-link to="/admin/packages">内容包</router-link>
        <router-link to="/admin/users">用户与角色</router-link>
      </nav>
    </el-aside>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<style scoped>
.layout { min-height: 100vh; }
.brand { font-weight: 700; padding: 1rem; }
nav { display: flex; flex-direction: column; padding: 0 1rem; gap: 0.5rem; }
nav a { color: #333; text-decoration: none; }
nav a.router-link-active { color: #409eff; }
</style>