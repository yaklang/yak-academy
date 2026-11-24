// 统一的中文状态映射：页面与 StatusTag 共用一份，避免同一枚举在不同页面
// 出现不同译名（例如 running 有时写“进行中”有时写“运行中”）。
import { DIFFICULTY_LABELS } from '@/api'

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'primary'

export interface StatusMeta {
  label: string
  tone: Tone
}

export type StatusKind =
  | 'competition'
  | 'awd'
  | 'attack'
  | 'exam'
  | 'package'
  | 'container'
  | 'submission'
  | 'difficulty'
  | 'role'
  | 'user'
  | 'mail'

const fallback = (value?: string | null): StatusMeta => ({
  label: value ? String(value) : '未知',
  tone: 'neutral',
})

const MAPS: Record<Exclude<StatusKind, 'difficulty' | 'role' | 'user'>, Record<string, StatusMeta>> = {
  competition: {
    scheduled: { label: '未开始', tone: 'info' },
    running: { label: '进行中', tone: 'success' },
    ended: { label: '已结束', tone: 'neutral' },
  },
  awd: {
    pending: { label: '待开始', tone: 'info' },
    running: { label: '进行中', tone: 'success' },
    paused: { label: '已暂停', tone: 'warning' },
    ended: { label: '已结束', tone: 'neutral' },
  },
  attack: {
    pending: { label: '待审核', tone: 'info' },
    accepted: { label: '已通过', tone: 'success' },
    rejected: { label: '已驳回', tone: 'danger' },
  },
  exam: {
    not_started: { label: '未开始', tone: 'info' },
    in_progress: { label: '答题中', tone: 'warning' },
    submitted: { label: '已交卷', tone: 'success' },
    expired: { label: '已过期', tone: 'neutral' },
  },
  package: {
    draft: { label: '草稿', tone: 'neutral' },
    published: { label: '已发布', tone: 'success' },
    archived: { label: '已归档', tone: 'warning' },
  },
  container: {
    creating: { label: '创建中', tone: 'info' },
    running: { label: '运行中', tone: 'success' },
    stopped: { label: '已停止', tone: 'neutral' },
    destroyed: { label: '已销毁', tone: 'neutral' },
  },
  submission: {
    pending: { label: '判题中', tone: 'info' },
    correct: { label: '通过', tone: 'success' },
    incorrect: { label: '未通过', tone: 'danger' },
  },
  mail: {
    unread: { label: '未读', tone: 'warning' },
    read: { label: '已读', tone: 'neutral' },
  },
}

const DIFFICULTY_TONE: Record<string, Tone> = {
  apprentice: 'success',
  practitioner: 'warning',
  expert: 'danger',
}

const ROLE_LABELS: Record<string, string> = {
  student: '学员',
  admin: '管理员',
}

const USER_STATUS: Record<string, StatusMeta> = {
  active: { label: '正常', tone: 'success' },
  inactive: { label: '不活跃', tone: 'warning' },
  disabled: { label: '已停用', tone: 'danger' },
  locked: { label: '已锁定', tone: 'danger' },
}

export function statusMeta(kind: StatusKind, value?: string | null): StatusMeta {
  if (kind === 'difficulty') {
    if (!value) return fallback(value)
    return { label: DIFFICULTY_LABELS[value as keyof typeof DIFFICULTY_LABELS] ?? value, tone: DIFFICULTY_TONE[value] ?? 'neutral' }
  }
  if (kind === 'role') {
    return value ? { label: ROLE_LABELS[value] ?? value, tone: value === 'admin' ? 'primary' : 'neutral' } : fallback(value)
  }
  if (kind === 'user') {
    return value ? USER_STATUS[value] ?? fallback(value) : fallback(value)
  }
  return value ? MAPS[kind][value] ?? fallback(value) : fallback(value)
}

export function statusLabel(kind: StatusKind, value?: string | null): string {
  return statusMeta(kind, value).label
}
