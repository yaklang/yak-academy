// challenge.json 前端校验与样例工具。
// 规则镜像后端 ContentService.validateChallengeJSON（content.go）：
// 前端只做快速反馈，后端校验仍是最终裁决（api-contract skill）。
import type { ChallengeHintInput, ChallengeJSON, ChallengeLink } from '../api'

const slugPattern = /^[a-z0-9-]+$/
const difficulties = ['apprentice', 'practitioner', 'expert'] as const
// 教程题目 tab 专属内置题集：上传只接受 ctf/acm（tutorial 由内置导入注入）。
const banks = ['ctf', 'acm'] as const

// 校验单个题目定义；返回错误消息数组（空 = 通过）。
export function validateChallenge(chal: ChallengeJSON): string[] {
  const errs: string[] = []
  const where = (field: string, msg: string) => errs.push(`${field}：${msg}`)

  if (!chal.slug) where('slug', '必填')
  else if (!slugPattern.test(chal.slug)) where('slug', '只允许小写字母/数字/连字符 [a-z0-9-]+')

  if (!chal.title) where('title', '必填')
  if (!chal.category) where('category', '必填')
  else if (!slugPattern.test(chal.category)) where('category', '格式同 slug，[a-z0-9-]+')

  if (chal.bank && !(banks as readonly string[]).includes(chal.bank))
    where('bank', '上传仅可选 ctf / acm（tutorial 为内置题集专属，缺省 ctf）')

  if (!chal.difficulty || !(difficulties as readonly string[]).includes(chal.difficulty))
    where('difficulty', '必须为 apprentice / practitioner / expert')

  if (chal.score === undefined || chal.score === null) where('score', '必填')
  else if (chal.score < 0) where('score', '必须为非负整数')

  // 判定定义二选一：judge 段（ACM 编程题）或 challenge 段（flag 判定题）。
  if (chal.judge) {
    if (chal.challenge?.type || chal.challenge?.flag)
      where('judge', '与 challenge 段互斥：ACM 编程题不通过 flag 判定')
    if (chal.bank && chal.bank !== 'acm')
      where('bank', 'ACM 编程题（judge 段）必须归 acm 题库')
    if (chal.bank === 'ctf')
      where('bank', 'ACM 编程题（judge 段）必须归 acm 题库')
    if (chal.judge.engine !== 'yak') where('judge.engine', '必须为 yak')
    if (!chal.judge.time_limit_s || chal.judge.time_limit_s < 1 || chal.judge.time_limit_s > 60)
      where('judge.time_limit_s', '必须为 1-60 秒')
    const mem = chal.judge.memory_limit_mb ?? 128
    if (mem < 16 || mem > 512) where('judge.memory_limit_mb', '必须为 16-512 MB')
    const names = new Set<string>()
    chal.judge.testdata?.forEach((t, i) => {
      if (!t.name) where(`judge.testdata[${i}].name`, '必填')
      else if (!/^[a-z0-9][a-z0-9-]*$/.test(t.name))
        where(`judge.testdata[${i}].name`, '只允许小写字母/数字/连字符')
      else if (names.has(t.name)) where(`judge.testdata[${i}].name`, '组名重复')
      if (t.score !== undefined && t.score !== null && t.score < 0)
        where(`judge.testdata[${i}].score`, '必须为非负整数（0 = 未配置，全有全无）')
      names.add(t.name)
    })
  } else {
    if (chal.bank === 'acm')
      where('judge', 'ACM 题库的题目必须为编程题：challenge.yml/challenge.json 带 judge 段（对齐 ICPC 源码判定模型）')
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
  }

  const mode = chal.flag_mode ?? 'shared'
  if (!['shared', 'instance'].includes(mode)) {
    where('flag_mode', '必须为 shared 或 instance')
  } else if (mode === 'instance') {
    if (!chal.env?.image || !chal.env?.port) where('env', 'flag_mode=instance 必须声明容器 env（image + port）')
    if (!chal.challenge?.flag?.includes('{token}'))
      where('challenge.flag', 'instance 模式必须为含 {token} 的模板，如 flag{lab-{token}}')
  }
  if (chal.judge && mode === 'instance')
    where('flag_mode', 'ACM 编程题不支持 instance 作用域（无 flag/容器实例）')

  if (chal.env && (chal.env.image || chal.env.port)) {
    if (!chal.env.image) where('env.image', 'env 段出现时必填')
    if (!chal.env.port || chal.env.port < 1 || chal.env.port > 65535)
      where('env.port', '必须为 1-65535')
  }
  if (chal.judge && chal.env?.image)
    where('env', 'ACM 编程题不支持容器环境（判题在 worker 内完成）')

  const hints: ChallengeHintInput[] = chal.hints ?? []
  hints.forEach((h, i) => {
    if (!h.title) where(`hints[${i}].title`, '必填')
    if (!h.content) where(`hints[${i}].content`, '必填')
    if (h.cost !== undefined && (h.cost < 0 || !Number.isInteger(h.cost)))
      where(`hints[${i}].cost`, '必须为非负整数')
  })

  const links: ChallengeLink[] = chal.links ?? []
  links.forEach((l, i) => {
    if (!l.name) where(`links[${i}].name`, '必填')
    if (!/^https?:\/\//.test(l.url)) where(`links[${i}].url`, '必须为 http(s) 地址')
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
  bank: 'ctf',
  difficulty: 'apprentice',
  description_md:
    '# 背景\n\n某站点的登录接口直接把用户名拼进 SQL 语句，没有做任何过滤。\n\n# 任务目标\n\n启动容器后访问题目 endpoint（`/login`），用 SQL 注入绕过登录校验。当后端确认「admin 身份登录成功」，题目自动判定通关——无需手动提交 flag。',
  score: 100,
  flag_mode: 'shared',
  challenge: { type: 'static', flag: 'flag{answer}' },
  env: { image: 'vulnapp:1.0', port: 80 },
  hints: [{ title: '提示一', content: '看 UNION', cost: 10 }],
  links: [{ name: 'SQL 注入速查表', url: 'https://portswigger.net/web-security/sql-injection/cheat-sheet' }],
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