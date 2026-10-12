import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

// 学生端 / 管理端两套布局（layouts/）。
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/Login.vue'),
    },
    {
      path: '/',
      component: () => import('../layouts/StudentLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        { path: '', name: 'topics', component: () => import('../views/Topics.vue') },
        { path: 'topics/:topicSlug/rooms/:roomSlug', name: 'room', component: () => import('../views/RoomDetail.vue') },
        { path: 'paths', name: 'paths', component: () => import('../views/LearningPaths.vue') },
        { path: 'leaderboard', name: 'leaderboard', component: () => import('../views/Leaderboard.vue') },
      ],
    },
    {
      path: '/admin',
      component: () => import('../layouts/AdminLayout.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
      children: [
        { path: 'packages', name: 'admin-packages', component: () => import('../views/admin/ContentPackages.vue') },
        { path: 'users', name: 'admin-users', component: () => import('../views/admin/Users.vue') },
      ],
    },
  ],
})

// 全局守卫：未登录跳 /login（带 redirect）；admin 区域校验角色（rbac 语义前端预检，
// 最终以后端中间件为准——前端只做体验性拦截）。
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.name === 'login') return true
  if (!auth.token) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.meta.requiresAdmin) {
    if (!auth.user) {
      try {
        await auth.fetchMe()
      } catch {
        return { name: 'login', query: { redirect: to.fullPath } }
      }
    }
    if (auth.user?.global_role !== 'admin' && auth.user?.global_role !== 'teacher') {
      return { name: 'topics' }
    }
  }
  return true
})

export default router