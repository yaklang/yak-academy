// challengeFormat 校验规则锁定测试：前端规则镜像后端 validateChallengeJSON，
// 两端漂移（难度枚举、instance 约束、env 成对等）会被这里捕获。
import { describe, expect, it } from 'vitest'
import {
  parseChallengeJSON,
  SAMPLE_BASIC_QUESTIONS_JSON,
  SAMPLE_CHALLENGE_JSON,
  validateChallenge,
} from './challengeFormat'
import type { ChallengeJSON, QuestionJSON } from '../api'

// 以样例为基准做局部覆盖，避免每个用例重复全量字段。
function base(overrides: Partial<ChallengeJSON> = {}): ChallengeJSON {
  return { ...SAMPLE_CHALLENGE_JSON, ...overrides }
}

// 基础题样例（单选）作为基准。
function basic(overrides: Partial<ChallengeJSON> = {}, question: Partial<QuestionJSON> = {}): ChallengeJSON {
  const sample = SAMPLE_BASIC_QUESTIONS_JSON[0]
  return { ...sample, ...overrides, question: { ...sample.question!, ...question } }
}

describe('validateChallenge', () => {
  it('内置样例通过校验', () => {
    expect(validateChallenge(SAMPLE_CHALLENGE_JSON)).toEqual([])
  })

  it('slug/category 必须匹配 [a-z0-9-]+', () => {
    expect(validateChallenge(base({ slug: 'Xss_Lab' }))[0]).toContain('slug')
    expect(validateChallenge(base({ category: 'Web安全' }))[0]).toContain('category')
  })

  it('difficulty 只允许三级枚举', () => {
    // 'beginner' 是故意的非法值，绕过类型层才能测运行时校验。
    const bad = 'beginner' as unknown as ChallengeJSON['difficulty']
    expect(validateChallenge(base({ difficulty: bad }))[0]).toContain('difficulty')
    expect(validateChallenge(base({ difficulty: 'expert' }))).toEqual([])
  })

  it('bank 上传仅可选 ctf/acm/basic，tutorial 为内置题集专属', () => {
    // 'web' / 'tutorial' 是故意的非法值，绕过类型层测运行时校验（镜像后端枚举）。
    const bad = 'web' as unknown as ChallengeJSON['bank']
    const tutorial = 'tutorial' as unknown as ChallengeJSON['bank']
    expect(validateChallenge(base({ bank: bad })).join('\n')).toContain('bank')
    expect(validateChallenge(base({ bank: tutorial })).join('\n')).toContain('bank')
    expect(validateChallenge(base({ bank: 'ctf' }))).toEqual([])
    expect(validateChallenge(base({ bank: undefined }))).toEqual([])
  })

  it('ACM 题库：必须为编程题（judge 段），flag 判定题归 ctf', () => {
    // bank=acm + challenge 段（flag 判定）→ 报错：ACM 不通过 flag 验证。
    const errs = validateChallenge(base({ bank: 'acm' }))
    expect(errs.join('\n')).toContain('judge')
  })

  it('judge 段规则：互斥/bank 归置/引擎/时限/组名', () => {
    const judge = {
      engine: 'yak' as const,
      time_limit_s: 5,
      testdata: [{ name: '01', sample: true }],
    }
    const acmCode = base({ bank: 'acm', challenge: undefined, env: undefined, judge })
    expect(validateChallenge(acmCode)).toEqual([])
    // 与 challenge 段互斥。
    expect(validateChallenge(base({ judge, challenge: { type: 'static', flag: 'f' } })).join('\n')).toContain('互斥')
    // ctf 题带 judge 段 → 归置错误。
    expect(validateChallenge(base({ bank: 'ctf', challenge: undefined, judge })).join('\n')).toContain('acm')
    // 引擎枚举与时限范围。
    const badEngine = { ...judge, engine: 'python' as unknown as 'yak' }
    expect(validateChallenge(base({ challenge: undefined, judge: badEngine })).join('\n')).toContain('engine')
    const badLimit = { ...judge, time_limit_s: 0 }
    expect(validateChallenge(base({ challenge: undefined, judge: badLimit })).join('\n')).toContain('time_limit_s')
    // 组名格式与重复。
    const badName = { ...judge, testdata: [{ name: '1A', sample: true }] }
    expect(validateChallenge(base({ challenge: undefined, judge: badName })).join('\n')).toContain('name')
    // judge 题不能带容器环境。
    expect(validateChallenge(base({ challenge: undefined, judge, env: { image: 'app:1', port: 80 } })).join('\n')).toContain('容器环境')
  })

  it('score 必须非负', () => {
    expect(validateChallenge(base({ score: -1 }))[0]).toContain('score')
  })

  it('regex 类型的 flag 必须可编译', () => {
    const errs = validateChallenge(
      base({ challenge: { type: 'regex', flag: 'flag{([unclosed' } }),
    )
    expect(errs.join('\n')).toContain('正则无法编译')
  })

  it('challenge.type 只允许 static/regex', () => {
    // 同上：'dynamic' 是故意的非法值。
    const badType = 'dynamic' as unknown as NonNullable<ChallengeJSON['challenge']>['type']
    expect(validateChallenge(base({ challenge: { type: badType, flag: 'x' } })).join('\n')).toContain('type')
  })

  it('flag_mode=instance 必须有 env 且 flag 含 {token}', () => {
    const errs = validateChallenge(
      base({ flag_mode: 'instance', challenge: { type: 'static', flag: 'flag{no-token}' } }),
    )
    expect(errs.join('\n')).toContain('instance')
    expect(errs.join('\n')).toContain('{token}')

    const ok = validateChallenge(
      base({ flag_mode: 'instance', challenge: { type: 'static', flag: 'flag{lab-{token}}' } }),
    )
    expect(ok).toEqual([])
  })

  it('env 段出现时 image 与 port 成对且 port 合法', () => {
    expect(validateChallenge(base({ env: { image: 'app:1', port: 0 } })).join('\n')).toContain('env.port')
    expect(validateChallenge(base({ env: { image: 'app:1', port: 65536 } })).join('\n')).toContain('env.port')
  })

  it('hints 校验：标题/正文必填、cost 非负整数', () => {
    const errs = validateChallenge(base({ hints: [{ title: '', content: 'x', cost: -1 }] }))
    expect(errs.join('\n')).toContain('hints[0].title')
    expect(errs.join('\n')).toContain('hints[0].cost')
  })

  it('基础题四种题型样例全部通过校验', () => {
    for (const sample of SAMPLE_BASIC_QUESTIONS_JSON) {
      expect(validateChallenge(sample), sample.slug).toEqual([])
    }
  })

  it('判定定义三选一互斥：question 不能与 challenge/judge 同时出现', () => {
    expect(validateChallenge(basic({ challenge: { type: 'static', flag: 'f' } })).join('\n')).toContain('互斥')
    const judge = { engine: 'yak' as const, time_limit_s: 5, testdata: [{ name: '01' }] }
    expect(validateChallenge(basic({ judge })).join('\n')).toContain('互斥')
  })

  it('基础题必须 bank=basic 且不允许容器/instance', () => {
    expect(validateChallenge(basic({ bank: 'ctf' })).join('\n')).toContain('basic')
    expect(validateChallenge(basic({ env: { image: 'app:1', port: 80 } })).join('\n')).toContain('容器环境')
    expect(validateChallenge(basic({ flag_mode: 'instance' })).join('\n')).toContain('flag_mode')
    expect(validateChallenge(base({ bank: 'basic' })).join('\n')).toContain('question')
  })

  it('基础题题面描述必填', () => {
    expect(validateChallenge(basic({ description_md: '   ' })).join('\n')).toContain('description_md')
  })

  it('选项规则：数量/key 格式/唯一/文本/答案命中', () => {
    expect(validateChallenge(basic({}, { options: [{ key: 'A', text: 'a' }] })).join('\n')).toContain('2-26')
    expect(validateChallenge(basic({}, { options: [{ key: 'a', text: 'a' }, { key: 'B', text: 'b' }] })).join('\n')).toContain('key')
    expect(validateChallenge(basic({}, { options: [{ key: 'A', text: 'a' }, { key: 'A', text: 'b' }] })).join('\n')).toContain('重复')
    expect(validateChallenge(basic({}, { options: [{ key: 'A', text: '' }, { key: 'B', text: 'b' }] })).join('\n')).toContain('text')
    expect(validateChallenge(basic({}, { answer: 'Z' })).join('\n')).toContain('不在选项中')
  })

  it('多选答案：非空数组、元素命中、无重复', () => {
    expect(validateChallenge(basic({}, { type: 'multiple', answer: [] })).join('\n')).toContain('非空')
    expect(validateChallenge(basic({}, { type: 'multiple', answer: ['A', 'Z'] })).join('\n')).toContain('不是已声明')
    expect(validateChallenge(basic({}, { type: 'multiple', answer: ['A', 'A'] })).join('\n')).toContain('重复')
    expect(validateChallenge(basic({}, { type: 'multiple', answer: ['A', 'C'] }))).toEqual([])
  })

  it('判断题：不允许 options，答案必须为布尔值', () => {
    const tf = SAMPLE_BASIC_QUESTIONS_JSON[2]
    expect(validateChallenge({ ...tf, question: { ...tf.question!, options: [{ key: 'A', text: 'a' }] } }).join('\n')).toContain('不允许选项')
    expect(validateChallenge({ ...tf, question: { ...tf.question!, answer: 'true' } }).join('\n')).toContain('true 或 false')
  })

  it('填空题：不允许 options，答案非空，正则必须可编译', () => {
    const fill = SAMPLE_BASIC_QUESTIONS_JSON[3]
    expect(validateChallenge({ ...fill, question: { ...fill.question!, options: [{ key: 'A', text: 'a' }] } }).join('\n')).toContain('不允许选项')
    expect(validateChallenge({ ...fill, question: { ...fill.question!, answer: ['  '] } }).join('\n')).toContain('不能为空')
    expect(
      validateChallenge({
        ...fill,
        question: { ...fill.question!, answer: ['[unclosed'], match: { regex: true } },
      }).join('\n'),
    ).toContain('正则无法编译')
  })
})

describe('parseChallengeJSON', () => {
  it('单对象解析为长度 1 的数组', () => {
    const { list, error } = parseChallengeJSON(JSON.stringify(SAMPLE_CHALLENGE_JSON))
    expect(error).toBeUndefined()
    expect(list).toHaveLength(1)
  })

  it('对象数组原样保留长度', () => {
    const { list } = parseChallengeJSON(JSON.stringify([SAMPLE_CHALLENGE_JSON, base({ slug: 'another-one' })]))
    expect(list).toHaveLength(2)
  })

  it('非法 JSON / 空数组 / 非对象元素报错', () => {
    expect(parseChallengeJSON('not json').error).toContain('JSON')
    expect(parseChallengeJSON('[]').error).toContain('空')
    expect(parseChallengeJSON('[42]').error).toContain('对象')
  })
})
