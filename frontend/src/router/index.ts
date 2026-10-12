import { createRouter, createWebHistory } from 'vue-router'

// 学生端 / 管理端两套布局（layouts/）。页面为第一版占位骨架（架构文档 §9）。
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: () => import('../views/Login.vue'),
    },
    {
      path: '/',
      component: () => import('../layouts/StudentLayout.vue'),
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
      children: [
        { path: 'packages', name: 'admin-packages', component: () => import('../views/admin/ContentPackages.vue') },
        { path: 'users', name: 'admin-users', component: () => import('../views/admin/Users.vue') },
      ],
    },
  ],
})

export default router