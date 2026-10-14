// challengeFormat 校验规则锁定测试：前端规则镜像后端 validateChallengeJSON，
// 两端漂移（难度枚举、instance 约束、env 成对等）会被这里捕获。
import { describe, expect, it } from 'vitest'
import { parseChallengeJSON, SAMPLE_CHALLENGE_JSON, validateChallenge } from './challengeFormat'
import type { ChallengeJSON } from '../api'

// 以样例为基准做局部覆盖，避免每个用例重复全量字段。
function base(overrides: Partial<ChallengeJSON> = {}): ChallengeJSON {
  return { ...SAMPLE_CHALLENGE_JSON, ...overrides }
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
    const badType = 'dynamic' as unknown as ChallengeJSON['challenge']['type']
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
