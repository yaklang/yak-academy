import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useCompetitionStore } from '../stores/competition'

// 信息架构：两个端。
//   普通端 /（教程 / 题库，路由段保留 /training）——普通人与管理员均可。
//   管理端 /admin —— 仅管理员（global_role === admin）；普通人无入口且路由守卫拦截。
// 两端复用同一组视图组件（Topics/TopicDetail/RoomDetail/Training）。
// 竞赛参赛流程为独立满屏布局（入场页 + 竞赛首页），对应"入场后锁定在竞赛页面"。
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
        // 题库板块
        { path: 'training', name: 'training', component: () => import('../views/Training.vue') },
        { path: 'training/:challengeSlug', name: 'challenge-detail', component: () => import('../views/ChallengeDetail.vue') },
        // 徽章（计算型读模型视图）
        { path: 'badges', name: 'badges', component: () => import('../views/Badges.vue') },
        // 次级页面（不进主导航）
        { path: 'leaderboard', name: 'leaderboard', component: () => import('../views/Leaderboard.vue') },
        // 账号信息面板 + 站内邮箱（次级页面，入口在头部用户区）
        { path: 'profile', name: 'profile', component: () => import('../views/Profile.vue') },
        { path: 'mailbox', name: 'mailbox', component: () => import('../views/Mailbox.vue') },
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
        // 题库板块（预览视角）
        { path: 'training', name: 'admin-training', component: () => import('../views/Training.vue') },
        { path: 'training/:challengeSlug', name: 'admin-challenge-detail', component: () => import('../views/ChallengeDetail.vue') },
        // 资源管理（教程管理 / 题目管理 + 容器、竞赛）。
        // 用户与角色不再提供管理页（两档模型下角色为账号固有属性；建号入口在竞赛导入参赛者）。
        { path: 'panel', redirect: '/admin/panel/tutorials' },
        { path: 'panel/tutorials', name: 'admin-tutorials-mgmt', component: () => import('../views/admin/ContentPackages.vue') },
        { path: 'panel/challenges', name: 'admin-challenges', component: () => import('../views/admin/Challenges.vue') },
        { path: 'panel/containers', name: 'admin-containers', component: () => import('../views/admin/Containers.vue') },
        { path: 'panel/competitions', name: 'admin-competitions', component: () => import('../views/admin/Competitions.vue') },
        {
          path: 'panel/competitions/:id',
          name: 'admin-competition-detail',
          component: () => import('../views/admin/CompetitionDetail.vue'),
        },
      ],
    },
    // 竞赛参赛流程（独立满屏布局，不进两端导航）。
    // 入场后后端中间件强制隔离（非竞赛接口一律 403），前端守卫只做 UX 引导。
    {
      path: '/competitions/enter/:id',
      name: 'competition-enter',
      component: () => import('../views/CompetitionEnter.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/competitions/:id',
      name: 'competition-home',
      component: () => import('../views/CompetitionHome.vue'),
      meta: { requiresAuth: true },
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
  // 参赛锁定（仅学生；管理员不锁）：已入场且竞赛未结束时，
  // 平台其余路径全部引导回竞赛首页 —— 与后端锁定中间件同语义。
  if (auth.user?.global_role !== 'admin' && !to.path.startsWith('/competitions')) {
    const compStore = useCompetitionStore()
    const lockedId = await compStore.ensureChecked()
    if (lockedId) return { path: `/competitions/${lockedId}` }
  }
  return true
})

export default router