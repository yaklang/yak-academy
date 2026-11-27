// challenge.json 前端校验与样例工具。
// 规则镜像后端 ContentService.validateChallengeJSON（content.go）：
// 前端只做快速反馈，后端校验仍是最终裁决（api-contract skill）。
import type { ChallengeHintInput, ChallengeJSON, ChallengeLink, QuestionJSON, QuestionOption } from '../api'

const slugPattern = /^[a-z0-9-]+$/
const difficulties = ['apprentice', 'practitioner', 'expert'] as const
// 教程题目 tab 专属内置题集：上传只接受 ctf/acm/basic（tutorial 由内置导入注入）。
const banks = ['ctf', 'acm', 'basic'] as const
const questionTypes = ['single', 'multiple', 'truefalse', 'fill'] as const
const optionKeyPattern = /^[A-Z0-9]{1,8}$/

// 基础题 question 段校验（镜像后端 service/objective.go 的 normalizeQuestionDefinition）。
function validateQuestion(
  chal: ChallengeJSON,
  where: (field: string, msg: string) => void,
) {
  const q = chal.question as QuestionJSON
  if (!q) {
    where('question', '基础题必须带 question 段')
    return
  }
  if (!(questionTypes as readonly string[]).includes(q.type)) {
    where('question.type', '必须为 single / multiple / truefalse / fill')
    return
  }
  if (!chal.description_md?.trim()) where('description_md', '基础题必须填写题面描述')
  if (q.explanation && new TextEncoder().encode(q.explanation).length > 16 * 1024)
    where('question.explanation', '不能超过 16KiB')

  if (q.type === 'single' || q.type === 'multiple') {
    const options: QuestionOption[] = Array.isArray(q.options) ? q.options : []
    if (options.length < 2 || options.length > 26)
      where('question.options', '单选/多选需要 2-26 个选项')
    const keys = new Set<string>()
    options.forEach((opt, i) => {
      const key = (opt.key ?? '').trim()
      const text = (opt.text ?? '').trim()
      if (!optionKeyPattern.test(key))
        where(`question.options[${i}].key`, '只允许大写字母/数字，1-8 位（如 A、B、A1）')
      else if (keys.has(key)) where(`question.options[${i}].key`, `选项 key 重复：${key}`)
      if (!text) where(`question.options[${i}].text`, '必填')
      else if (text.length > 500) where(`question.options[${i}].text`, '不能超过 500 字')
      if (key) keys.add(key)
    })
    if (q.type === 'single') {
      if (typeof q.answer !== 'string' || !q.answer.trim())
        where('question.answer', '单选答案必须是选项 key 字符串')
      else if (!keys.has(q.answer.trim()))
        where('question.answer', `答案 ${q.answer.trim()} 不在选项中`)
    } else {
      if (!Array.isArray(q.answer) || q.answer.length === 0) {
        where('question.answer', '多选答案必须是非空 key 数组')
      } else {
        const answered = new Set<string>()
        q.answer.forEach((raw, i) => {
          const key = typeof raw === 'string' ? raw.trim() : ''
          if (!key || !keys.has(key)) where(`question.answer[${i}]`, '不是已声明的选项 key')
          else if (answered.has(key)) where(`question.answer[${i}]`, `答案 key 重复：${key}`)
          if (key) answered.add(key)
        })
      }
    }
    if (q.match) where('question.match', '仅填空题可配置匹配策略')
    return
  }

  if ((q.options?.length ?? 0) > 0) where('question.options', '判断题/填空题不允许选项')
  if (q.match && q.type === 'truefalse') where('question.match', '仅填空题可配置匹配策略')

  if (q.type === 'truefalse') {
    if (typeof q.answer !== 'boolean') where('question.answer', '判断题答案必须是 true 或 false')
    return
  }

  // fill：字符串或字符串数组；空白项与全空数组非法（重复项由后端静默去重）。
  const answers = Array.isArray(q.answer)
    ? q.answer
    : typeof q.answer === 'string'
      ? [q.answer]
      : []
  if (answers.length === 0) {
    where('question.answer', '填空答案必须是字符串或非空字符串数组')
    return
  }
  answers.forEach((raw, i) => {
    if (typeof raw !== 'string' || !raw.trim()) {
      where(`question.answer[${i}]`, '答案不能为空')
      return
    }
    if (q.match?.regex) {
      try {
        new RegExp(raw.trim(), q.match.case_sensitive ? '' : 'i')
      } catch (e) {
        where(`question.answer[${i}]`, `正则无法编译：${(e as Error).message}`)
      }
    }
  })
}

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
    where('bank', '上传仅可选 ctf / acm / basic（tutorial 为内置题集专属，缺省 ctf）')

  if (!chal.difficulty || !(difficulties as readonly string[]).includes(chal.difficulty))
    where('difficulty', '必须为 apprentice / practitioner / expert')

  if (chal.score === undefined || chal.score === null) where('score', '必填')
  else if (chal.score < 0) where('score', '必须为非负整数')

  // 判定定义三选一：challenge（flag 题）/ judge（ACM 编程题）/ question（基础题）。
  const hasFlag = Boolean(chal.challenge?.type || chal.challenge?.flag)
  const hasJudge = Boolean(chal.judge)
  const hasQuestion = Boolean(chal.question)
  const defs = [hasFlag, hasJudge, hasQuestion].filter(Boolean).length
  if (defs > 1)
    where('judge / challenge / question', '判定定义三选一互斥：flag 题、ACM 编程题、基础题只能选一种')

  if (hasQuestion) {
    validateQuestion(chal, where)
    if (chal.bank && chal.bank !== 'basic') where('bank', '基础题（question 段）必须归 basic 题库')
    if (chal.flag_mode && chal.flag_mode !== 'shared') where('flag_mode', '基础题不支持 flag_mode=instance')
    if (chal.env?.image || chal.env?.port) where('env', '基础题不支持容器环境（env）')
  } else if (hasJudge) {
    if (chal.bank && chal.bank !== 'acm')
      where('bank', 'ACM 编程题（judge 段）必须归 acm 题库')
    if (chal.judge!.engine !== 'yak') where('judge.engine', '必须为 yak')
    if (!chal.judge!.time_limit_s || chal.judge!.time_limit_s < 1 || chal.judge!.time_limit_s > 60)
      where('judge.time_limit_s', '必须为 1-60 秒')
    const mem = chal.judge!.memory_limit_mb ?? 128
    if (mem < 16 || mem > 512) where('judge.memory_limit_mb', '必须为 16-512 MB')
    const names = new Set<string>()
    chal.judge!.testdata?.forEach((t, i) => {
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
    if (chal.bank === 'basic')
      where('question', 'basic 题库的题目必须带 question 段（基础题）')
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
    if (!hasQuestion) {
      if (!chal.env?.image || !chal.env?.port) where('env', 'flag_mode=instance 必须声明容器 env（image + port）')
      if (!chal.challenge?.flag?.includes('{token}'))
        where('challenge.flag', 'instance 模式必须为含 {token} 的模板，如 flag{lab-{token}}')
    }
  }
  if (hasJudge && mode === 'instance')
    where('flag_mode', 'ACM 编程题不支持 instance 作用域（无 flag/容器实例）')

  if (chal.env && (chal.env.image || chal.env.port)) {
    if (!chal.env.image) where('env.image', 'env 段出现时必填')
    if (!chal.env.port || chal.env.port < 1 || chal.env.port > 65535)
      where('env.port', '必须为 1-65535')
  }
  if (hasJudge && chal.env?.image)
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

// 基础题样例（四种题型各一题；仅「下载样例」参考，不进数据库）。
export const SAMPLE_BASIC_QUESTIONS_JSON: ChallengeJSON[] = [
  {
    slug: 'basic-sample-single',
    title: 'HTTP 401 状态码的含义',
    category: 'web-basics',
    bank: 'basic',
    difficulty: 'apprentice',
    description_md: '客户端访问受保护资源但没有提供有效身份凭证时，服务器通常返回哪个状态码？',
    score: 10,
    question: {
      type: 'single',
      options: [
        { key: 'A', text: '200 OK' },
        { key: 'B', text: '401 Unauthorized' },
        { key: 'C', text: '403 Forbidden' },
        { key: 'D', text: '500 Internal Server Error' },
      ],
      answer: 'B',
      explanation: '401 表示请求缺少有效认证信息；403 表示身份已确认但无权访问该资源。',
    },
  },
  {
    slug: 'basic-sample-multiple',
    title: '哪些 HTTP 方法是安全方法',
    category: 'web-basics',
    bank: 'basic',
    difficulty: 'apprentice',
    description_md: '下列哪些 HTTP 方法属于「安全方法」（不改变服务器状态）？',
    score: 20,
    question: {
      type: 'multiple',
      options: [
        { key: 'A', text: 'GET' },
        { key: 'B', text: 'HEAD' },
        { key: 'C', text: 'POST' },
        { key: 'D', text: 'DELETE' },
      ],
      answer: ['A', 'B'],
      explanation: 'GET 与 HEAD 是只读的安全方法；POST 与 DELETE 会改变服务器状态。',
    },
  },
  {
    slug: 'basic-sample-truefalse',
    title: 'HTTPS 是否加密请求正文',
    category: 'web-basics',
    bank: 'basic',
    difficulty: 'apprentice',
    description_md: '判断：HTTPS 会对 HTTP 请求与响应的正文进行加密传输。',
    score: 10,
    question: {
      type: 'truefalse',
      answer: true,
      explanation: 'TLS 在传输层加密整个 HTTP 报文，但不改变应用层语义。',
    },
  },
  {
    slug: 'basic-sample-fill',
    title: 'SQL 注入：恒真条件',
    category: 'sql-injection',
    bank: 'basic',
    difficulty: 'apprentice',
    description_md: '请填写一个能让字符串拼接的登录 SQL 恒为真的经典注入片段。',
    score: 30,
    question: {
      type: 'fill',
      answer: ["' OR 1=1 --", "' or 1=1 --"],
      match: { case_sensitive: false, trim: true, regex: false },
      explanation: '闭合单引号后追加恒真条件 OR 1=1，再用 -- 注释掉后续密码校验。',
    },
  },
]

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

// 触发浏览器下载基础题样例（四题型数组）。
export function downloadSampleBasicQuestions() {
  const blob = new Blob([JSON.stringify(SAMPLE_BASIC_QUESTIONS_JSON, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'basic-questions.json'
  a.click()
  URL.revokeObjectURL(url)
}