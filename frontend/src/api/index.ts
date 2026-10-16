// 按契约（docs/openapi.yaml）组织的 API 模块。改接口先改契约再改这里（api-contract skill）。
import { http } from './client'

export interface TokenResponse { token: string; expires_at: string }
export interface User { id: number; username: string; global_role: 'student' | 'admin' }

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
  extend_count: number
  created_at: string
}
export interface Paged<T> { total: number; page: number; page_size: number; items: T[] }
export interface RoomSummary { slug: string; title: string; difficulty: 'apprentice' | 'practitioner' | 'expert' }
export interface TopicDetail extends Topic { rooms: RoomSummary[] }
// 题库 tab（bank）：教程题目 = 内置题集专属（仅一键导入写入，上传传 tutorial 一律 422）；
// CTF（上传缺省）/ ACM 为管理员上传题库。
export type ChallengeBank = 'tutorial' | 'ctf' | 'acm'
export const BANK_LABELS: Record<ChallengeBank, string> = {
  tutorial: '教程题目',
  ctf: 'CTF 题目',
  acm: 'ACM 题目',
}

export interface TrainingItem {
  id: number
  slug: string
  title: string
  category: string
  bank: ChallengeBank
  is_builtin: boolean
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  score: number
  solved: boolean
  has_env: boolean
  is_visible?: boolean
  release_at?: string | null
  links?: ChallengeLink[]
  tutorial_slug?: string
  tutorial_title?: string
  tutorial_topic_slug?: string
}
export interface Hint {
  id: number
  title: string
  content: string
  cost: number
  unlocked: boolean
}
export interface HintUnlockResponse extends Hint {
  score: number
}
// 题目外链（随 challenge.json links 上传：配套工具/参考资料）。
export interface ChallengeLink { name: string; url: string }
// 题目附件元数据（学生端凭 JWT 经 downloadAttachment 拉取）。
export interface ChallengeAttachment { filename: string; size: number; created_at: string }
export interface ChallengeDetail extends TrainingItem {
  description_md: string
  hints: Hint[]
  attachments: ChallengeAttachment[]
}
// challenge.json 提示条目（上传格式：仅 title/content/cost；展示用 Hint 含 id/unlocked）。
export interface ChallengeHintInput {
  title: string
  content: string
  cost?: number
}
// 题目定义固定格式（challenge.json，管理员上传：单对象或数组）。
export interface ChallengeJSON {
  slug: string
  title: string
  category: string
  // 题库 tab；缺省 tutorial（教程题目/内部题库）。
  bank?: ChallengeBank
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  description_md?: string
  score: number
  // flag 作用域：shared=题目级共享（默认）；instance=每实例唯一（flag 为含 {token} 的模板，必须配 env）。
  flag_mode?: 'shared' | 'instance'
  challenge: { type: 'static' | 'regex'; flag: string }
  env?: { image: string; port: number }
  hints?: ChallengeHintInput[]
  links?: ChallengeLink[]
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
// 容器生命周期日志条目（append-only，管理端容器日志表格）。
export interface InstanceLog {
  id: number
  instance_id: number
  user_id: number
  username: string
  challenge_id: number
  challenge_title: string
  challenge_slug: string
  action: 'started' | 'stopped'
  source: string
  status: 'creating' | 'running' | 'stopped' | 'destroyed'
  detail: string
  created_at: string
}
export interface LeaderboardEntry { rank: number; username: string; score: number; solved_count: number }
// 徽章（计算型读模型：积分/进度/路径派生数据的实时投影，不落库）。
export interface Badge { code: string; title: string; description: string; earned: boolean }
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

// 用户与角色管理页已下线（两档模型角色为账号固有属性，无独立管理入口），
// 但管理 API 保留：竞赛「导入参赛者」建号、E2E 账号生命周期重置密码仍走这些接口。
export const adminApi = {
  listUsers: (page = 1) => http.get<Paged<User>>(`/admin/users?page=${page}`),
  createUser: (username: string, password: string, globalRole: string) =>
    http.post<User>('/admin/users', { username, password, global_role: globalRole }),
  updateRoles: (id: number, globalRole: string) =>
    http.patch<User>(`/admin/users/${id}/roles`, { global_role: globalRole }),
  resetPassword: (id: number, password: string) =>
    http.post<{ ok: boolean }>(`/admin/users/${id}/reset-password`, { password }),
}

export const authApi = {
  login: (username: string, password: string) =>
    http.post<TokenResponse>('/auth/login', { username, password }),
  // 注册发码：限频时后端不报错，返回 200 + retry_after（秒），前端据此倒计时。
  sendSmsCode: (phone: string) =>
    http.post<{ ok: boolean; retry_after?: number }>('/auth/sms/code', { phone }),
  // 凭验证码建号（角色固定 student），成功直接返回登录 token。
  register: (phone: string, code: string, username: string, password: string) =>
    http.post<TokenResponse>('/auth/register', { phone, code, username, password }),
  me: () => http.get<User>('/me'),
  logout: () => http.post<{ ok: boolean }>('/auth/logout'),
  changePassword: (currentPassword: string, newPassword: string) =>
    http.post<{ ok: boolean }>('/auth/change-password', {
      current_password: currentPassword,
      new_password: newPassword,
    }),
}

// --- 账号信息面板 + 站内邮箱（本人视角） ---
export interface UserProfile {
  id: number
  username: string
  global_role: 'student' | 'admin'
  bio: string
  score: number
  solve_count: number
  avatar: string | null
  created_at: string
}
export interface MailItem {
  id: number
  title: string
  body_md: string
  kind: 'invite' | string
  competition_id: number | null
  invite_code: string | null
  read: boolean
  created_at: string
}
export interface MailPage extends Paged<MailItem> {
  unread: number
}

export const profileApi = {
  profile: () => http.get<UserProfile>('/me/profile'),
  updateBio: (bio: string) => http.patch<UserProfile>('/me/profile', { bio }),
  uploadAvatar: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return http.upload<{ avatar: string | null }>('/me/avatar', form)
  },
  // 头像二进制（blob：头像 URL 需带 JWT，<img> 直连无法带头）。
  avatarBlob: () => http.download('/me/avatar'),
  mails: (page = 1, pageSize = 20) =>
    http.get<MailPage>(`/me/mails?page=${page}&page_size=${pageSize}`),
  unreadCount: () => http.get<{ unread: number }>('/me/mails/unread-count'),
  markRead: (id: number) => http.post<{ read: boolean }>(`/me/mails/${id}/read`),
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
  extend: (challengeSlug: string) => http.post<LabInstance>(`/challenges/${challengeSlug}/instance/extend`),
}

export const trainingApi = {
  // bank/builtin 为题库页 tab 与「是否内置」过滤器（缺省 = 全部）。
  list: (
    page = 1,
    pageSize = 20,
    filter?: { bank?: ChallengeBank; builtin?: boolean },
  ) => {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (filter?.bank) params.set('bank', filter.bank)
    if (filter?.builtin !== undefined) params.set('builtin', String(filter.builtin))
    return http.get<Paged<TrainingItem>>(`/challenges?${params}`)
  },
  detail: (challengeSlug: string) => http.get<ChallengeDetail>(`/challenges/${challengeSlug}`),
  unlockHint: (challengeSlug: string, hintId: number) =>
    http.post<HintUnlockResponse>(`/challenges/${challengeSlug}/hints/${hintId}/unlock`),
}


export const adminInstanceApi = {
  list: (page = 1) => http.get<Paged<AdminLabInstance>>(`/admin/lab-instances?page=${page}`),
  active: () => http.get<{ items: AdminLabInstance[] }>('/admin/lab-instances/active'),
  logs: (page = 1, pageSize = 20) =>
    http.get<Paged<InstanceLog>>(`/admin/lab-instances/logs?page=${page}&page_size=${pageSize}`),
  destroy: (id: number) => http.del<{ deleted: boolean }>(`/admin/lab-instances/${id}`),
}

export const progressApi = {
  mine: () => http.get<{ xp: number; completed_rooms: number; in_progress_rooms: number }>('/progress'),
  leaderboard: (page = 1) => http.get<Paged<LeaderboardEntry>>(`/leaderboard?page=${page}`),
}

export const badgeApi = {
  list: () => http.get<Badge[]>('/badges'),
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

// 题目管理（challenge.json / 题目包 zip 上传 / 表格 / 附件 / 删除）。
export interface BuiltinImportResult {
  version: string
  created: number
  updated: number
  slugs: string[]
  image_built: boolean
}

export const adminChallengeApi = {
  upload: (challenges: ChallengeJSON[]) =>
    http.post<{ imported: number; slugs: string[] }>('/admin/challenges', challenges),
  uploadPackage: (file: File) => {
    const form = new FormData()
    form.append('package', file)
    return http.upload<{ imported: number; slugs: string[] }>('/admin/challenges/package', form)
  },
  // 一键导入内置训练题集（vulinbox）。幂等：重复调用 = 更新已导入题目。
  importBuiltin: () =>
    http.post<BuiltinImportResult>('/admin/challenges/builtin'),
  list: (page = 1) => http.get<Paged<TrainingItem>>(`/admin/challenges?page=${page}`),
  remove: (slug: string) => http.del<{ deleted: boolean }>(`/admin/challenges/${slug}`),
  listAttachments: (slug: string) =>
    http.get<ChallengeAttachment[]>(`/admin/challenges/${slug}/attachments`),
  uploadAttachment: (slug: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return http.upload<ChallengeAttachment>(`/admin/challenges/${slug}/attachments`, form)
  },
  deleteAttachment: (slug: string, filename: string) =>
    http.del<{ deleted: boolean }>(`/admin/challenges/${slug}/attachments/${filename}`),
  // 可见性/定时发布。releaseAt 三态：undefined 不改；null 清空；RFC3339 设置。
  updateVisibility: (
    slug: string,
    body: { is_visible?: boolean; release_at?: string | null },
  ) => http.patch<TrainingItem>(`/admin/challenges/${slug}/visibility`, body),
}

// 学生端附件下载（blob 保存）。
export function downloadAttachment(slug: string, filename: string) {
  return http.download(`/challenges/${slug}/attachments/${encodeURIComponent(filename)}`)
}

// --- 竞赛域（管理端 + 参赛者） ---

export type CompetitionStatus = 'scheduled' | 'running' | 'ended'
export interface Competition {
  id: number
  name: string
  description: string
  start_time: string
  end_time: string
  status: CompetitionStatus
  created_at: string
}
export interface CompetitionDetail extends Competition {
  stats: {
    teams: number
    participants: number
    entered: number
    challenges: number
    instances: number
    warnings: number
    writeups: number
  }
}
// 参赛者视角题目条目（含描述与通关状态）。
export interface CompetitionChallengeItem {
  id: number
  slug: string
  title: string
  category: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  score: number
  has_env: boolean
  description_md: string
  solved: boolean
}
export interface CompetitionParticipantItem {
  id: number
  user_id: number
  username: string
  team_id: number | null
  team_name: string | null
  invite_code: string
  entered: boolean
  entered_at: string | null
  last_ip: string
  created_at: string
}
export interface CheatWarningItem {
  id: number
  user_id: number
  username: string
  team_id: number | null
  team_name: string | null
  kind: 'cross_team_flag' | 'ip_anomaly'
  reason: string
  created_at: string
}
export interface WriteupItem {
  id: number
  challenge_id: number
  challenge_title: string
  user_id: number
  username: string
  team_name: string | null
  submitted_at: string
  content: string
  zip_name: string
}
export interface MyWriteupItem {
  challenge_id: number
  content_md: string
  zip_name: string
  updated_at: string
  updated_by: string
}
// 榜单条目（team/individual 两模式复用：individual 时 members 只有一个用户名）。
export interface TeamLeaderboardEntry {
  rank: number
  team_id: number
  team_name: string | null
  score: number
  solved_count: number
  members: string[]
  last_solve_at: string | null
}
export interface LeaderboardResp {
  mode: 'team' | 'individual'
  items: TeamLeaderboardEntry[]
}
// 批量导入请求：队伍（成员按用户名，缺失自动建号）+ 个人参赛者。
export interface ParticipantImportRequest {
  teams?: { name: string; members: string[] }[]
  individuals?: string[]
}
export interface ParticipantImportResult {
  created_accounts: { username: string; initial_password: string }[]
  participants: { username: string; team_name: string | null; invite_code: string; mail_delivered: boolean }[]
}
export interface MyCompetitionEntry {
  competition: Competition | null
  locked: boolean
  entered: boolean
  team_name: string | null
}
export interface CompetitionHomeView {
  competition: Competition
  team_name: string | null
  teammates: string[]
  challenges: CompetitionChallengeItem[]
  my_submissions: { id: number; status: 'pending' | 'correct' | 'incorrect'; score: number; created_at: string }[]
  writeup_submitted: boolean
}

// 竞赛管理（管理端）。
export const adminCompetitionApi = {
  list: (page = 1, pageSize = 20) =>
    http.get<Paged<Competition>>(`/admin/competitions?page=${page}&page_size=${pageSize}`),
  create: (body: { name: string; description?: string; start_time: string; end_time: string }) =>
    http.post<Competition>('/admin/competitions', body),
  detail: (id: number) => http.get<CompetitionDetail>(`/admin/competitions/${id}`),
  update: (id: number, body: { name?: string; description?: string; start_time?: string; end_time?: string }) =>
    http.patch<CompetitionDetail>(`/admin/competitions/${id}`, body),
  challenges: (id: number) =>
    http.get<{ items: TrainingItem[] }>(`/admin/competitions/${id}/challenges`),
  // 双模式绑定：{"slugs":[...]} 绑既有题目；数组=challenge.json 批量导入（单题失败整体拒绝）。
  bindChallenges: (id: number, body: { slugs: string[] } | ChallengeJSON[]) =>
    http.post<{ added: number; items: TrainingItem[] }>(`/admin/competitions/${id}/challenges`, body),
  unbindChallenges: (id: number, slugs: string[]) =>
    http.del<{ removed: number }>(`/admin/competitions/${id}/challenges`, { slugs }),
  participants: (id: number, page = 1, pageSize = 20) =>
    http.get<Paged<CompetitionParticipantItem> & { teams: TeamLeaderboardEntry[] }>(
      `/admin/competitions/${id}/participants?page=${page}&page_size=${pageSize}`),
  importParticipants: (id: number, body: ParticipantImportRequest) =>
    http.post<ParticipantImportResult>(`/admin/competitions/${id}/participants`, body),
  instances: (id: number, page = 1, pageSize = 20) =>
    http.get<Paged<AdminLabInstance>>(`/admin/competitions/${id}/instances?page=${page}&page_size=${pageSize}`),
  warnings: (id: number) =>
    http.get<{ items: CheatWarningItem[] }>(`/admin/competitions/${id}/warnings`),
  writeups: (id: number) =>
    http.get<{ items: WriteupItem[] }>(`/admin/competitions/${id}/writeups`),
  writeupZip: (id: number, wid: number) =>
    http.download(`/admin/competitions/${id}/writeups/${wid}/zip`),
  leaderboard: (id: number, mode: 'team' | 'individual' = 'team') =>
    http.get<LeaderboardResp>(`/admin/competitions/${id}/leaderboard?mode=${mode}`),
}

// 竞赛参赛（学生端）。
export const competitionApi = {
  myEntry: () => http.get<MyCompetitionEntry>('/competitions/me'),
  enter: (id: number, inviteCode: string) =>
    http.post<MyCompetitionEntry>(`/competitions/${id}/enter`, { invite_code: inviteCode }),
  view: (id: number) => http.get<CompetitionHomeView>(`/competitions/${id}/view`),
  startInstance: (id: number, slug: string) =>
    http.post<LabInstance>(`/competitions/${id}/challenges/${slug}/instance`),
  submitFlag: (id: number, slug: string, answer: string) =>
    http.post<Submission>(`/competitions/${id}/submissions`, { challenge_slug: slug, answer }),
  leaderboard: (id: number, mode: 'team' | 'individual' = 'team') =>
    http.get<LeaderboardResp>(`/competitions/${id}/leaderboard?mode=${mode}`),
  myWriteups: (id: number) =>
    http.get<{ items: MyWriteupItem[] }>(`/competitions/${id}/writeups`),
  saveWriteup: (id: number, challengeId: number, contentMd: string) =>
    http.put<{ ok: boolean }>(`/competitions/${id}/writeups/${challengeId}`, { content_md: contentMd }),
  uploadWriteupZip: (id: number, challengeId: number, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return http.upload<{ ok: boolean; zip_name: string }>(`/competitions/${id}/writeups/${challengeId}/zip`, form)
  },
}