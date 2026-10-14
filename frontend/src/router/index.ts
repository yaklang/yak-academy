import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

// 信息架构：两个端。
//   普通端 /（教程 / 训练）—— 普通人与管理员均可。
//   管理端 /admin —— 仅管理员（global_role === admin）；普通人无入口且路由守卫拦截。
// 两端复用同一组视图组件（Topics/TopicDetail/RoomDetail/Training）。
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
        // 教程板块
        { path: '', name: 'topics', component: () => import('../views/Topics.vue') },
        { path: 'topics/:topicSlug', name: 'topic-detail', component: () => import('../views/TopicDetail.vue') },
        { path: 'topics/:topicSlug/rooms/:roomSlug', name: 'room', component: () => import('../views/RoomDetail.vue') },
        // 训练板块
        { path: 'training', name: 'training', component: () => import('../views/Training.vue') },
        { path: 'training/:challengeSlug', name: 'challenge-detail', component: () => import('../views/ChallengeDetail.vue') },
        // 学习路径
        { path: 'paths', name: 'paths', component: () => import('../views/LearningPaths.vue') },
        // 徽章（计算型读模型视图）
        { path: 'badges', name: 'badges', component: () => import('../views/Badges.vue') },
        // 次级页面（不进主导航）
        { path: 'leaderboard', name: 'leaderboard', component: () => import('../views/Leaderboard.vue') },
      ],
    },
    {
      path: '/admin',
      component: () => import('../layouts/AdminLayout.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
      children: [
        { path: '', redirect: '/admin/tutorials' },
        // 教程板块（预览视角，复用普通端视图）
        { path: 'tutorials', name: 'admin-tutorials', component: () => import('../views/Topics.vue') },
        { path: 'tutorials/:topicSlug', name: 'admin-topic-detail', component: () => import('../views/TopicDetail.vue') },
        { path: 'tutorials/:topicSlug/rooms/:roomSlug', name: 'admin-room', component: () => import('../views/RoomDetail.vue') },
        // 训练板块（预览视角）
        { path: 'training', name: 'admin-training', component: () => import('../views/Training.vue') },
        { path: 'training/:challengeSlug', name: 'admin-challenge-detail', component: () => import('../views/ChallengeDetail.vue') },
        // 资源管理（教程管理 / 题目管理 / 路径管理 + 容器、用户）
        { path: 'panel', redirect: '/admin/panel/tutorials' },
        { path: 'panel/tutorials', name: 'admin-tutorials-mgmt', component: () => import('../views/admin/ContentPackages.vue') },
        { path: 'panel/challenges', name: 'admin-challenges', component: () => import('../views/admin/Challenges.vue') },
        { path: 'panel/paths', name: 'admin-paths', component: () => import('../views/admin/Paths.vue') },
        { path: 'panel/containers', name: 'admin-containers', component: () => import('../views/admin/Containers.vue') },
        { path: 'panel/users', name: 'admin-users', component: () => import('../views/admin/Users.vue') },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.name === 'login') return true
  if (!auth.token) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  // 登录态下首次导航统一预取用户信息：布局首次绘制即有用户名/角色/管理入口，
  // 避免刷新后头部出现"无管理入口、无用户信息"的空窗；token 失效则回登录页。
  if (!auth.user) {
    try {
      await auth.fetchMe()
    } catch {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }
  if (to.meta.requiresAdmin && auth.user?.global_role !== 'admin') {
    return { name: 'topics' }
  }
  return true
})

export default router