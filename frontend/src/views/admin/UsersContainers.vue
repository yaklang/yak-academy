<script setup lang="ts">
// 用户与容器合并页：账号画像与容器运行态同属“人及其运行环境”的管理域，
// 用页面级 tab 收拢，避免管理端顶栏继续膨胀。tab 通过 ?tab=containers
// 深链；旧 /admin/panel/containers 路由重定向到该 tab。
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UsersPanel from './Users.vue'
import ContainersPanel from './Containers.vue'

const route = useRoute()
const router = useRouter()
const routeTab = computed(() => (route.query.tab === 'containers' ? 'containers' : 'users'))
const activeTab = ref<string>(routeTab.value)

watch(routeTab, (value) => {
  if (activeTab.value !== value) activeTab.value = value
})

watch(activeTab, (value) => {
  if (routeTab.value === value) return
  router.replace({ query: { ...route.query, tab: value === 'containers' ? value : undefined } })
})
</script>

<template>
  <header class="page-head">
    <div>
      <h2 class="page-title">用户与<span class="accent">容器</span></h2>
      <p class="page-sub">
        账号、角色与容器运行态集中管理：用户侧支持画像筛选与密码重置，容器侧展示主机性能、活跃实例与生命周期日志。
      </p>
    </div>
  </header>

  <UiTabs v-model="activeTab">
    <UiTabPane name="users" label="用户管理">
      <UsersPanel embedded />
    </UiTabPane>
    <UiTabPane name="containers" label="容器管理">
      <ContainersPanel embedded />
    </UiTabPane>
  </UiTabs>
</template>

<style scoped>
.page-title .accent { color: var(--accent-foreground); }
</style>
