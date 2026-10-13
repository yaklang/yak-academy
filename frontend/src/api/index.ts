// 按契约（docs/openapi.yaml）组织的 API 模块。改接口先改契约再改这里（api-contract skill）。
import { http } from './client'

export interface TokenResponse { token: string; expires_at: string }
export interface User { id: number; username: string; global_role: 'student' | 'teacher' | 'admin' }
export interface Topic { slug: string; title: string; summary: string; room_count: number }
export interface Lab { challenge_id: number; title: string; score: number; solved: boolean; has_env: boolean }
export interface Room {
  slug: string
  topic_slug: string
  title: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  lesson_md?: string
  lab: Lab | null
}
export interface Submission { id: number; status: 'pending' | 'correct' | 'incorrect'; score: number }
export interface LabInstance {
  id: number
  status: 'creating' | 'running' | 'stopped' | 'destroyed'
  endpoint: string
  expire_at: string
  created_at: string
}
export interface Paged<T> { total: number; page: number; page_size: number; items: T[] }
export interface LeaderboardEntry { rank: number; username: string; score: number; solved_count: number }
export interface ContentPackage {
  id: number
  name: string
  latest_version: string
  status: 'draft' | 'published' | 'archived'
  error?: string
}

export interface CourseRole { topic_slug: string; role: string }

export const adminApi = {
  listUsers: (page = 1) => http.get<Paged<User>>(`/admin/users?page=${page}`),
  createUser: (username: string, password: string, globalRole: string) =>
    http.post<User>('/admin/users', { username, password, global_role: globalRole }),
  updateRoles: (id: number, globalRole: string, courseRoles: CourseRole[]) =>
    http.patch<User>(`/admin/users/${id}/roles`, { global_role: globalRole, course_roles: courseRoles }),
  deleteCourseRole: (id: number, topicSlug: string) =>
    http.del<{ deleted: boolean }>(`/admin/users/${id}/course-roles/${topicSlug}`),
}

export const authApi = {
  login: (username: string, password: string) =>
    http.post<TokenResponse>('/auth/login', { username, password }),
  me: () => http.get<User>('/me'),
}

export const topicApi = {
  list: (page = 1) => http.get<Paged<Topic>>(`/topics?page=${page}`),
  rooms: (topicSlug: string) => http.get<Topic>(`/topics/${topicSlug}`),
  room: (topicSlug: string, roomSlug: string) =>
    http.get<Room>(`/topics/${topicSlug}/rooms/${roomSlug}`),
}

export const submissionApi = {
  submit: (roomSlug: string, answer: string) => http.post<Submission>('/submissions', { room_slug: roomSlug, answer }),
  get: (id: number) => http.get<Submission>(`/submissions/${id}`),
}

export const instanceApi = {
  start: (roomSlug: string) => http.post<LabInstance>(`/rooms/${roomSlug}/lab/instance`),
  get: (roomSlug: string) => http.get<LabInstance | null>(`/rooms/${roomSlug}/lab/instance`),
  destroy: (roomSlug: string) => http.del<{ deleted: boolean }>(`/rooms/${roomSlug}/lab/instance`),
}

export const progressApi = {
  mine: () => http.get<{ xp: number; completed_rooms: number; in_progress_rooms: number }>('/progress'),
  leaderboard: (page = 1) => http.get<Paged<LeaderboardEntry>>(`/leaderboard?page=${page}`),
}

export const contentApi = {
  list: (page = 1) => http.get<Paged<ContentPackage>>(`/content/packages?page=${page}`),
  upload: (file: File) => {
    const form = new FormData()
    form.append('package', file)
    return http.upload<ContentPackage>('/content/packages', form)
  },
  get: (id: number) => http.get<ContentPackage>(`/content/packages/${id}`),
  publish: (id: number) => http.post<ContentPackage>(`/content/packages/${id}/publish`),
}