// 按契约（docs/openapi.yaml）组织的 API 模块。改接口先改契约再改这里（api-contract skill）。
import { http } from './client'

export interface TokenResponse { token: string; expires_at: string }
export interface User { id: number; username: string; global_role: 'student' | 'teacher' | 'admin' }
export interface Topic { slug: string; title: string; summary: string; room_count: number }
export interface Room {
  slug: string
  topic_slug: string
  title: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  lesson_md?: string
  video_url?: string
  related_challenge_slug?: string | null
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
export interface RoomSummary { slug: string; title: string; difficulty: 'apprentice' | 'practitioner' | 'expert' }
export interface TopicDetail extends Topic { rooms: RoomSummary[] }
export interface TrainingItem {
  id: number
  slug: string
  title: string
  category: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  score: number
  solved: boolean
  has_env: boolean
  tutorial_slug?: string
  tutorial_title?: string
  tutorial_topic_slug?: string
}
export interface Hint { id?: number; title: string; content: string; cost: number }
export interface ChallengeDetail extends TrainingItem {
  description_md: string
  hints: Hint[]
}
// 题目定义固定格式（challenge.json，管理员上传：单对象或数组）。
export interface ChallengeJSON {
  slug: string
  title: string
  category: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  description_md?: string
  score: number
  challenge: { type: 'static' | 'regex'; flag: string }
  env?: { image: string; port: number }
  hints?: Hint[]
  tutorial_slug?: string
}
export interface AdminLabInstance {
  id: number
  user_id: number
  username: string
  challenge_id: number
  challenge_title: string
  challenge_slug: string
  status: 'creating' | 'running' | 'stopped' | 'destroyed'
  endpoint: string
  expire_at: string
  created_at: string
}
export interface LeaderboardEntry { rank: number; username: string; score: number; solved_count: number }
export interface ContentPackage {
  id: number
  name: string
  latest_version: string
  status: 'draft' | 'published' | 'archived'
  error?: string
}
export interface ContentVersion {
  id: number
  version: string
  status: 'draft' | 'published' | 'archived'
  checksum: string
  created_at: string
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
  rooms: (topicSlug: string) => http.get<TopicDetail>(`/topics/${topicSlug}`),
  room: (topicSlug: string, roomSlug: string) =>
    http.get<Room>(`/topics/${topicSlug}/rooms/${roomSlug}`),
}

export const submissionApi = {
  submit: (challengeSlug: string, answer: string) =>
    http.post<Submission>('/submissions', { challenge_slug: challengeSlug, answer }),
  get: (id: number) => http.get<Submission>(`/submissions/${id}`),
}

// 题目容器环境（题目独立资源，slug 键）。
export const instanceApi = {
  start: (challengeSlug: string) => http.post<LabInstance>(`/challenges/${challengeSlug}/instance`),
  get: (challengeSlug: string) => http.get<LabInstance | null>(`/challenges/${challengeSlug}/instance`),
  destroy: (challengeSlug: string) => http.del<{ deleted: boolean }>(`/challenges/${challengeSlug}/instance`),
}

export const trainingApi = {
  list: (page = 1, pageSize = 20) => http.get<Paged<TrainingItem>>(`/challenges?page=${page}&page_size=${pageSize}`),
  detail: (challengeSlug: string) => http.get<ChallengeDetail>(`/challenges/${challengeSlug}`),
}

export const adminInstanceApi = {
  list: (page = 1) => http.get<Paged<AdminLabInstance>>(`/admin/lab-instances?page=${page}`),
  destroy: (id: number) => http.del<{ deleted: boolean }>(`/admin/lab-instances/${id}`),
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
  versions: (id: number) => http.get<ContentVersion[]>(`/content/packages/${id}/versions`),
  archive: (id: number) => http.post<ContentPackage>(`/content/packages/${id}/archive`),
  rollback: (id: number, versionId: number) =>
    http.post<ContentPackage>(`/content/packages/${id}/rollback`, { version_id: versionId }),
}

// 题目管理（challenge.json 上传 / 表格 / 删除）。
export const adminChallengeApi = {
  upload: (challenges: ChallengeJSON[]) =>
    http.post<{ imported: number; slugs: string[] }>('/admin/challenges', challenges),
  list: (page = 1) => http.get<Paged<TrainingItem>>(`/admin/challenges?page=${page}`),
  remove: (slug: string) => http.del<{ deleted: boolean }>(`/admin/challenges/${slug}`),
}