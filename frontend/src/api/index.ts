// 按契约（docs/openapi.yaml）组织的 API 模块。改接口先改契约再改这里（api-contract skill）。
import { http } from './client'

export interface TokenResponse { token: string; expires_at: string }
// account_code：学号式账号编码（YA{YY}-{NNNNN}，建号时按年分配；旧库迁移行可能为空串）。
// last_login_at：最近登录（从未登录为 null；管理端不活跃判定 = 最近登录或注册超 6 个月）。
export interface User {
  id: number
  username: string
  global_role: 'student' | 'admin'
  account_code: string
  phone: string
  bio: string
  last_login_at: string | null
  created_at: string
}

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
// verdict：ACM 编程题判定结果（flag 判定题为 null，学生端按 status 展示）。
export type SubmissionVerdict = 'ac' | 'wa' | 'tle' | 're' | 'internal_error'
export const verdictLabels: Record<SubmissionVerdict, string> = {
  ac: '通过（AC）',
  wa: '答案错误（WA）',
  tle: '超出时限（TLE）',
  re: '运行错误（RE）',
  internal_error: '判定服务暂不可用，请稍后重试',
}
// 逐组判定明细（ACM 部分得分制）：判定对全部数据组逐组运行，每组独立结论。
export interface SubmissionGroupResult {
  name: string
  sample: boolean
  passed: boolean
  verdict?: 'wa' | 'tle' | 're'
  score: number // 组配置分值（权重；0 = 未配置）
}
export interface Submission {
  id: number
  status: 'pending' | 'correct' | 'incorrect'
  score: number
  verdict?: SubmissionVerdict | null
  groups?: SubmissionGroupResult[] | null
}
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
export type ChallengeDifficulty = 'apprentice' | 'practitioner' | 'expert'
export const BANK_LABELS: Record<ChallengeBank, string> = {
  tutorial: '教程题目',
  ctf: 'CTF 题目',
  acm: 'ACM 题目',
}
export const DIFFICULTY_LABELS: Record<ChallengeDifficulty, string> = {
  apprentice: '入门',
  practitioner: '进阶',
  expert: '专家',
}

// 题目管理列表查询参数（GET /admin/challenges）：全部可选，零值 = 不过滤。
export interface AdminChallengeQuery {
  page?: number
  q?: string
  bank?: ChallengeBank
  category?: string
  difficulty?: ChallengeDifficulty
  builtin?: boolean
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
// ACM 编程题判定配置（学生端视图：不含测试数据内容；flag 判定题为 null/缺省）。
export interface JudgeConfig {
  engine: string
  time_limit_s: number
  memory_limit_mb: number
  testdata_count: number
  // yak 库白名单（题目配置；null = 未配置，允许全部非黑名单库）。
  // 服务端已扣除全局黑名单；编辑器补全过滤与题面库标签以此为准。
  allowed_libs?: string[] | null
  // 平台全局黑名单（exec/file 等高危库）——补全排除与题面提示用。
  blocked_libs?: string[]
  // 判定引擎是否已装配（ACADEMY_YAK_BIN 配置或管理端下载）。false 时
  // 前端禁用提交/自测入口并提示，代替点击后才收到 internal_error/422。
  engine_available?: boolean
}
// ACM 样例数据组（sample=true；隐藏组不下发）。
export interface JudgeSample { name: string; input: string; output: string }
export interface JudgeSamplesResponse { time_limit_s: number; samples: JudgeSample[] }
// ACM 自测运行（动态执行框）：POST 创建 202 {id} → GET 轮询至 status=done。
// 运营态与判题隔离：不产生提交/事件/积分；sample 模式服务端按样例期望输出
// 比对（matches，token 级空白不敏感），自定义 stdin 模式 matches 恒 null；
// stdout 超过 64KiB 截断（truncated=true）；票据 10 分钟后过期 404。
export interface CodeRunView {
  id: string
  status: 'pending' | 'running' | 'done'
  mode: 'sample' | 'custom'
  sample?: string
  outcome?: 'ok' | 'tle' | 're'
  stdout?: string
  stderr?: string
  truncated: boolean
  time_ms: number
  matches: boolean | null
}
export interface ChallengeDetail extends TrainingItem {
  description_md: string
  judge?: JudgeConfig | null
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
// judge 段存在 = ACM 编程题（与 challenge 段互斥；judge 段的 testdata 内容
// 仅包 zip 上传时由 testdata/ 文件装配，直传 JSON 须内联 input/output）。
export interface JudgeJSON {
  engine: 'yak'
  time_limit_s: number
  memory_limit_mb?: number
  // yak 库白名单（防高危库执行；空/缺省 = 允许全部非黑名单库；服务端校验库名）。
  allowed_libs?: string[]
  testdata: { name: string; input?: string; output?: string; sample?: boolean; score?: number }[]
}
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
  judge?: JudgeJSON | null
  env?: { image: string; port: number }
  hints?: ChallengeHintInput[]
  links?: ChallengeLink[]
  tutorial_slug?: string
}
// 管理端单题完整定义（GET /admin/challenges/{slug}，编辑回填用）：
// 与上传 JSON 同构，另含完整 flag 值列表（题目包 zip 可多 flag，表单只回填首个）。
// ACM 编程题：challenge 段为空对象、judge 段回填（testdata 仅 name/sample 引用）。
export interface ChallengeFullJSON extends ChallengeJSON {
  bank: ChallengeBank
  flag_mode: 'shared' | 'instance'
  flags: string[]
  challenge: { type: 'static' | 'regex'; flag: string }
  judge?: JudgeJSON | null
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
// 删除内容包的结果（教程内容连带移除，事件历史按 append-only 保留）。
export interface PackageDeleteResult { deleted: boolean; topics: number; rooms: number }

// 管理端用户列表条目：用户行 + 画像标记（active = 最近登录或注册在 6 个月内；
// solved_any = 存在判定通过的提交）。过滤条件与标记同口径。
export interface AdminUserItem extends User {
  active: boolean
  solved_any: boolean
}

// 用户列表过滤（GET /admin/users）：全部可选，零值 = 不过滤。
export interface AdminUserQuery {
  page?: number
  q?: string
  role?: 'student' | 'admin'
  active?: boolean
  no_solves?: boolean
}

// 用户管理页（表格 + 过滤/搜索；建号入口在邀请赛导入/一键生成与自助注册）。
export const adminApi = {
  listUsers: (query: AdminUserQuery = {}) => {
    const params = new URLSearchParams()
    if (query.page) params.set('page', String(query.page))
    if (query.q) params.set('q', query.q)
    if (query.role) params.set('role', query.role)
    if (query.active !== undefined) params.set('active', String(query.active))
    if (query.no_solves !== undefined) params.set('no_solves', String(query.no_solves))
    const qs = params.toString()
    return http.get<Paged<AdminUserItem>>(`/admin/users${qs ? `?${qs}` : ''}`)
  },
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
  // code 仅在服务端开启回显模式（ACADEMY_SMS_SHOW_CODE=1，内网/演示部署）时返回。
  sendSmsCode: (phone: string) =>
    http.post<{ ok: boolean; retry_after?: number; code?: string }>('/auth/sms/code', { phone }),
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
  // ACM 编程题样例数据（flag 判定题 404，调用方按题有无 judge 段自行决定是否拉取）。
  judgeSamples: (challengeSlug: string) =>
    http.get<JudgeSamplesResponse>(`/challenges/${challengeSlug}/judge/samples`),
  // 自测运行（sample 组名 / 自定义 stdin 二选一）。创建返回 202 {id}；轮询见 judgeRunGet。
  judgeRun: (challengeSlug: string, body: { source: string; sample?: string; stdin?: string }) =>
    http.post<{ id: string }>(`/challenges/${challengeSlug}/judge/runs`, body),
  judgeRunGet: (challengeSlug: string, id: string) =>
    http.get<CodeRunView>(`/challenges/${challengeSlug}/judge/runs/${id}`),
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

// 备份归档（ops 硬化）：SQLite 快照 + 数据目录 tar.gz，受保留份数约束。
export interface BackupInfo {
  name: string
  size_bytes: number
  created_at: string
  database: string
  store_kinds: string
}

export const adminBackupApi = {
  list: () => http.get<{ items: BackupInfo[] }>('/admin/backups'),
  create: () => http.post<BackupInfo>('/admin/backups'),
  download: (name: string) => http.download(`/admin/backups/${encodeURIComponent(name)}`),
}

// 管理操作审计（ops 硬化）：append-only 审计行，与 Event 学生行为流分工。
export interface AdminAuditLog {
  id: number
  admin_id: number
  admin_name: string
  action: string
  target_type: string
  target_id: string
  detail: Record<string, unknown> | null
  ip: string
  created_at: string
}

export const auditApi = {
  list: (params: { page?: number; page_size?: number; action?: string; admin_id?: number } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set('page', String(params.page))
    if (params.page_size) q.set('page_size', String(params.page_size))
    if (params.action) q.set('action', params.action)
    if (params.admin_id) q.set('admin_id', String(params.admin_id))
    const qs = q.toString()
    return http.get<Paged<AdminAuditLog>>(`/admin/audit-logs${qs ? `?${qs}` : ''}`)
  },
}

// 主机/容器性能快照（ops 硬化）：docker info/stats + Linux /proc + 数据卷 statfs；
// 非 Linux 开发态宿主占用与数据卷用量为 null。
export interface HostUsage {
  cpu_percent: number | null
  mem_total_bytes: number
  mem_used_bytes: number | null
}

export interface VolumeUsage {
  path: string
  total_bytes: number
  used_bytes: number
  avail_bytes: number
}

export interface ContainerResource {
  id: string
  name: string
  cpu_percent: number
  mem_used_bytes: number
  mem_limit_bytes: number
  mem_percent: number
  net_io_bytes: [number | null, number | null]
  block_io_bytes: [number | null, number | null]
  pids: number
}

export interface PerformanceReport {
  ncpu: number
  mem_total_bytes: number
  containers_total: number
  containers_running: number
  containers_paused: number
  containers_stopped: number
  images: number
  host: HostUsage | null
  data_volume: VolumeUsage | null
  containers: ContainerResource[]
}

export const performanceApi = {
  get: () => http.get<PerformanceReport>('/admin/performance'),
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
  remove: (id: number) => http.del<PackageDeleteResult>(`/content/packages/${id}`),
}

// 题目管理（challenge.json / 题目包 zip 上传 / 表格 / 附件 / 删除）。
export interface BuiltinImportResult {
  version: string
  created: number
  updated: number
  removed: number
  topics: number
  rooms: number
  slugs: string[]
  image_built: boolean
}

// vulinbox 基础镜像状态：driver=noop 时环境接口 501、不可构建。
export interface BuiltinImageStatus {
  driver: 'docker' | 'noop'
  image_tag: string
  image_present: boolean
  can_build: boolean
}

// yak 判题引擎 OSS 下载任务进度：total=-1 未知大小（进度条退化为不定态）；
// 任务态仅存服务进程内存，服务重启回 idle。
export interface YakDownloadProgress {
  state: 'idle' | 'running' | 'done' | 'failed'
  received: number
  total: number
  error?: string
}

// yak 判题引擎装配态（题目管理 ACM tab 状态卡）：configured=false 时
// ACM 题提交判 internal_error、自测运行 422。
export interface YakEngineStatus {
  configured: boolean
  path: string
  version: string
  download: YakDownloadProgress
}

// 内置训练集教程目录（教程管理页「内置训练集」区块）：帐面清单来自 embed
// 资产，imported 按内容表探测——内置教程只读，更新走一键导入。
export interface BuiltinTutorialRoom {
  slug: string
  title: string
  difficulty: 'apprentice' | 'practitioner' | 'expert'
  imported: boolean
}

export interface BuiltinTutorialTopic {
  slug: string
  title: string
  rooms: BuiltinTutorialRoom[]
}

export interface BuiltinTutorialCatalog {
  version: string
  topics: BuiltinTutorialTopic[]
  imported_topics: number
  imported_rooms: number
}

export const adminChallengeApi = {
  upload: (challenges: ChallengeJSON[]) =>
    http.post<{ imported: number; slugs: string[] }>('/admin/challenges', challenges),
  uploadPackage: (file: File) => {
    const form = new FormData()
    form.append('package', file)
    return http.upload<{ imported: number; slugs: string[] }>('/admin/challenges/package', form)
  },
  // 一键导入内置训练集（教程 + 题目，一教程对一题）。幂等：重复调用 = 更新。
  importBuiltin: () =>
    http.post<BuiltinImportResult>('/admin/challenges/builtin'),
  // vulinbox 基础镜像：状态查询 / 仅构建（docker build 内从官方 OSS 下载二进制，约 1-3 分钟）。
  builtinImageStatus: () =>
    http.get<BuiltinImageStatus>('/admin/challenges/builtin/image'),
  buildBuiltinImage: () =>
    http.post<BuiltinImageStatus>('/admin/challenges/builtin/image'),
  // yak 判题引擎：状态查询 / 触发 OSS 异步下载（约 220MB，202 返回后台任务，
  // 进度轮询状态接口；下载完成即时装配，无需重启）。
  yakEngineStatus: () =>
    http.get<YakEngineStatus>('/admin/challenges/engine/yak'),
  yakEngineDownload: () =>
    http.post<YakEngineStatus>('/admin/challenges/engine/yak/download'),
  // 内置训练集教程目录与导入状态（教程管理页「内置训练集」区块）。
  builtinTutorialCatalog: () =>
    http.get<BuiltinTutorialCatalog>('/admin/challenges/builtin/tutorials'),
  list: (query: AdminChallengeQuery = {}) => {
    const params = new URLSearchParams()
    if (query.page) params.set('page', String(query.page))
    if (query.q) params.set('q', query.q)
    if (query.bank) params.set('bank', query.bank)
    if (query.category) params.set('category', query.category)
    if (query.difficulty) params.set('difficulty', query.difficulty)
    if (query.builtin !== undefined) params.set('builtin', String(query.builtin))
    const qs = params.toString()
    return http.get<Paged<TrainingItem>>(`/admin/challenges${qs ? `?${qs}` : ''}`)
  },
  // 单题完整定义（编辑回填；含 flag 值，仅 admin）。
  getJSON: (slug: string) => http.get<ChallengeFullJSON>(`/admin/challenges/${slug}`),
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
// 赛制：public = 公开赛（竞赛广场可见，自助报名，已报名者免邀请码入场）；
// invitational = 邀请赛（定向导入/生成参赛者，凭邀请码入场）。创建后不可更改。
export type CompetitionKind = 'public' | 'invitational'
export const KIND_LABELS: Record<CompetitionKind, string> = {
  public: '公开赛',
  invitational: '邀请赛',
}
export interface Competition {
  id: number
  name: string
  description: string
  start_time: string
  end_time: string
  status: CompetitionStatus
  kind: CompetitionKind
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
// 批量导入请求（仅邀请赛）：队伍 + 个人参赛者。条目按「账号编码或用户名」
// 引用既有账号，不自动建号——缺失条目在响应 missing 回报（补号走一键生成）。
export interface ParticipantImportRequest {
  teams?: { name: string; members: string[] }[]
  individuals?: string[]
}
export interface InvitedParticipant {
  username: string
  account_code: string
  team_name: string | null
  invite_code: string
  mail_delivered: boolean
}
export interface ParticipantImportResult {
  participants: InvitedParticipant[]
  // 未匹配到账号的条目原文（账号编码与用户名均未命中），排序返回。
  missing: string[]
}
// 一键生成参赛账号（仅邀请赛）：用户名 = 前缀-零填充序号，可配置自动组队
// 与初始密码（默认 123456）。initial_password 仅新建账号返回，供导出分发；
// 复用既有同名账号为 null（沿用原密码）。
export interface GenerateUsersRequest {
  prefix: string
  count: number
  team_size?: number
  password?: string
}
export interface GeneratedAccount {
  username: string
  account_code: string
  initial_password: string | null
  created: boolean
}
export interface GenerateUsersResult {
  accounts: GeneratedAccount[]
  teams: string[]
  participants: InvitedParticipant[]
}
// 竞赛广场条目（仅公开赛；含当前用户报名状态与报名人数）。
export interface PublicCompetitionItem {
  id: number
  name: string
  description: string
  start_time: string
  end_time: string
  status: CompetitionStatus
  registered: boolean
  participants: number
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
  create: (body: { name: string; description?: string; start_time: string; end_time: string; kind: CompetitionKind }) =>
    http.post<Competition>('/admin/competitions', body),
  detail: (id: number) => http.get<CompetitionDetail>(`/admin/competitions/${id}`),
  update: (id: number, body: { name?: string; description?: string; start_time?: string; end_time?: string }) =>
    http.patch<CompetitionDetail>(`/admin/competitions/${id}`, body),
  // 手动结束竞赛（仅 running；end_time 置为当前时刻。结束后起止时间可重新配置再开赛）。
  end: (id: number) => http.post<CompetitionDetail>(`/admin/competitions/${id}/end`),
  // 删除竞赛（running 422 拒绝——先手动结束）；级联清理参赛者/题目绑定/提交/WP（含 zip 落盘）/容器。
  delete: (id: number) => http.del<{ ok: boolean }>(`/admin/competitions/${id}`),
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
  // 一键生成参赛账号（仅邀请赛）：生成 + 自动组队 + 导入参赛者一步完成。
  generateUsers: (id: number, body: GenerateUsersRequest) =>
    http.post<GenerateUsersResult>(`/admin/competitions/${id}/generate-users`, body),
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
  // 竞赛广场：分页列出公开赛（含当前用户报名状态）；邀请赛不出现在广场。
  plaza: (page = 1, pageSize = 20) =>
    http.get<Paged<PublicCompetitionItem>>(`/competitions?page=${page}&page_size=${pageSize}`),
  // 公开赛自助报名（幂等；个人参赛，不投递邀请邮件——报名即入场资格）。
  register: (id: number) => http.post<PublicCompetitionItem>(`/competitions/${id}/register`),
  myEntry: () => http.get<MyCompetitionEntry>('/competitions/me'),
  // 入场：邀请码入场（两档通用）；公开赛已报名者可省略邀请码。
  enter: (id: number, inviteCode?: string) =>
    http.post<MyCompetitionEntry>(`/competitions/${id}/enter`, inviteCode ? { invite_code: inviteCode } : {}),
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