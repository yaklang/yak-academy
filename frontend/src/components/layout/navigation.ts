import type { Component } from 'vue'
import {
  Award,
  BarChart3,
  BookOpen,
  Boxes,
  ClipboardList,
  Code2,
  DatabaseBackup,
  FileText,
  Mail,
  ScrollText,
  Settings,
  Trophy,
  UserRound,
  Users,
} from '@lucide/vue'

export interface NavItem {
  label: string
  to: string
  icon: Component
  // 额外前缀：/learn 需要覆盖教程详情 /topics/*。
  match?: string[]
  badge?: 'unread'
}

export interface NavSection {
  label: string
  icon: Component
  items: NavItem[]
}

export const studentNav: NavSection[] = [
  {
    label: '学习',
    icon: BookOpen,
    items: [
      { label: '教程', to: '/learn', icon: BookOpen, match: ['/topics'] },
      { label: '题库', to: '/training', icon: Code2 },
      { label: '竞赛', to: '/competitions', icon: Trophy },
      { label: '排行榜', to: '/leaderboard', icon: BarChart3 },
    ],
  },
  {
    label: '我的',
    icon: Award,
    items: [
      { label: '徽章', to: '/badges', icon: Award },
      { label: '邮箱', to: '/mailbox', icon: Mail, badge: 'unread' },
      { label: '账号信息', to: '/profile', icon: UserRound },
    ],
  },
]

export const adminNav: NavSection[] = [
  {
    label: '学习预览',
    icon: BookOpen,
    items: [
      { label: '教程', to: '/admin/tutorials', icon: BookOpen },
      { label: '题库', to: '/admin/training', icon: Code2 },
    ],
  },
  {
    label: '内容管理',
    icon: FileText,
    items: [
      { label: '教程管理', to: '/admin/panel/tutorials', icon: FileText },
      { label: '题目管理', to: '/admin/panel/challenges', icon: ClipboardList },
    ],
  },
  {
    label: '用户与竞赛',
    icon: Users,
    items: [
      { label: '用户管理', to: '/admin/panel/users', icon: Users },
      { label: '竞赛管理', to: '/admin/panel/competitions', icon: Trophy },
    ],
  },
  {
    label: '系统管理',
    icon: Boxes,
    items: [
      { label: '容器管理', to: '/admin/panel/containers', icon: Boxes },
      { label: '备份管理', to: '/admin/panel/backups', icon: DatabaseBackup },
      { label: '操作审计', to: '/admin/panel/audit-logs', icon: ScrollText },
      { label: '系统设置', to: '/admin/panel/settings', icon: Settings },
    ],
  },
]

export function isNavItemActive(item: NavItem, path: string): boolean {
  if (path === item.to) return true
  if (path.startsWith(`${item.to}/`)) return true
  return (item.match ?? []).some((prefix) => path === prefix || path.startsWith(`${prefix}/`))
}

export function activeSection(nav: NavSection[], path: string): NavSection | undefined {
  return nav.find((section) => section.items.some((item) => isNavItemActive(item, path)))
}
