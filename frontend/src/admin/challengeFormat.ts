// challenge.json 前端校验与样例工具。
// 规则镜像后端 ContentService.validateChallengeJSON（content.go）：
// 前端只做快速反馈，后端校验仍是最终裁决（api-contract skill）。
import type { ChallengeHintInput, ChallengeJSON } from '../api'

const slugPattern = /^[a-z0-9-]+$/
const difficulties = ['apprentice', 'practitioner', 'expert'] as const

// 校验单个题目定义；返回错误消息数组（空 = 通过）。
export function validateChallenge(chal: ChallengeJSON): string[] {
  const errs: string[] = []
  const where = (field: string, msg: string) => errs.push(`${field}：${msg}`)

  if (!chal.slug) where('slug', '必填')
  else if (!slugPattern.test(chal.slug)) where('slug', '只允许小写字母/数字/连字符 [a-z0-9-]+')

  if (!chal.title) where('title', '必填')
  if (!chal.category) where('category', '必填')
  else if (!slugPattern.test(chal.category)) where('category', '格式同 slug，[a-z0-9-]+')

  if (!chal.difficulty || !(difficulties as readonly string[]).includes(chal.difficulty))
    where('difficulty', '必须为 apprentice / practitioner / expert')

  if (chal.score === undefined || chal.score === null) where('score', '必填')
  else if (chal.score < 0) where('score', '必须为非负整数')

  if (!chal.challenge?.type || !['static', 'regex'].includes(chal.challenge.type))
    where('challenge.type', '必须为 static 或 regex')
  if (!chal.challenge?.flag) where('challenge.flag', '必填')
  else if (chal.challenge.type === 'regex') {
    try {
      new RegExp(chal.challenge.flag)
    } catch (e) {
      where('challenge.flag', `正则无法编译：${(e as Error).message}`)
    }
  }

  const mode = chal.flag_mode ?? 'shared'
  if (!['shared', 'instance'].includes(mode)) {
    where('flag_mode', '必须为 shared 或 instance')
  } else if (mode === 'instance') {
    if (!chal.env?.image || !chal.env?.port) where('env', 'flag_mode=instance 必须声明容器 env（image + port）')
    if (!chal.challenge?.flag?.includes('{token}'))
      where('challenge.flag', 'instance 模式必须为含 {token} 的模板，如 flag{lab-{token}}')
  }

  if (chal.env && (chal.env.image || chal.env.port)) {
    if (!chal.env.image) where('env.image', 'env 段出现时必填')
    if (!chal.env.port || chal.env.port < 1 || chal.env.port > 65535)
      where('env.port', '必须为 1-65535')
  }

  const hints: ChallengeHintInput[] = chal.hints ?? []
  hints.forEach((h, i) => {
    if (!h.title) where(`hints[${i}].title`, '必填')
    if (!h.content) where(`hints[${i}].content`, '必填')
    if (h.cost !== undefined && (h.cost < 0 || !Number.isInteger(h.cost)))
      where(`hints[${i}].cost`, '必须为非负整数')
  })
  return errs
}

// 解析 challenge.json 文本（单对象或数组）；返回规范化的数组与错误。
export function parseChallengeJSON(text: string): { list: ChallengeJSON[]; error?: string } {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (e) {
    return { list: [], error: `不是合法的 JSON：${(e as Error).message}` }
  }
  const list = Array.isArray(parsed) ? parsed : [parsed]
  if (list.length === 0) return { list: [], error: '题目数组为空' }
  for (const item of list) {
    if (typeof item !== 'object' || item === null || Array.isArray(item)) {
      return { list: [], error: '内容必须是题目对象或题目对象数组' }
    }
  }
  return { list: list as ChallengeJSON[] }
}

// 内置样例（examples/challenges/sqli-login-bypass 的镜像，含检测点自动判定说明）。
// 仅用于管理页「下载样例」参考，不进数据库（content-package-spec skill：禁止内置内容）。
export const SAMPLE_CHALLENGE_JSON: ChallengeJSON = {
  slug: 'sqli-login-bypass',
  title: 'SQL 注入登录绕过',
  category: 'sql-injection',
  difficulty: 'apprentice',
  description_md:
    '# 背景\n\n某站点的登录接口直接把用户名拼进 SQL 语句，没有做任何过滤。\n\n# 任务目标\n\n启动容器后访问题目 endpoint（`/login`），用 SQL 注入绕过登录校验。当后端确认「admin 身份登录成功」，题目自动判定通关——无需手动提交 flag。',
  score: 100,
  flag_mode: 'shared',
  challenge: { type: 'static', flag: 'flag{answer}' },
  env: { image: 'vulnapp:1.0', port: 80 },
  hints: [{ title: '提示一', content: '看 UNION', cost: 10 }],
  tutorial_slug: 'sqli-basics',
}

// 触发浏览器下载样例文件（不落仓库产物）。
export function downloadSampleChallenge() {
  const blob = new Blob([JSON.stringify(SAMPLE_CHALLENGE_JSON, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'challenge.json'
  a.click()
  URL.revokeObjectURL(url)
}