// yak 补全源单测：直接构造 CompletionContext 驱动 yakCompletion，
// 覆盖成员补全（lib. 前缀）、词首补全（关键字/内建/库前缀/片段）、
// 未知前缀不补、数据源真实性（挑战用到的函数必须在列）；
// 库策略（yakLibPolicy facet：白名单 + 平台黑名单）下的补全过滤。
import { describe, expect, it } from 'vitest'
import { EditorState } from '@codemirror/state'
import { CompletionContext } from '@codemirror/autocomplete'
import { yakCompletion, yakLibPolicy, defaultYakLibPolicy, type YakLibPolicy } from './yakCompletion'
import { LIB_FUNCS } from './yakVocab'

const optionsAt = (doc: string) => {
  const state = EditorState.create({ doc })
  const ctx = new CompletionContext(state, doc.length, false)
  const result = yakCompletion(ctx)
  return result ? result.options.map((o) => o.label) : null
}

// 按指定库策略构造编辑器状态（模拟 CodeEditor 的 policyComp 注入）。
const optionsAtWith = (doc: string, policy: YakLibPolicy) => {
  const state = EditorState.create({ doc, extensions: [yakLibPolicy.of(policy)] })
  const ctx = new CompletionContext(state, doc.length, false)
  const result = yakCompletion(ctx)
  return result ? result.options.map((o) => o.label) : null
}

describe('yakCompletion 补全', () => {
  it('成员补全：codec. 给出该库真实导出函数', () => {
    const labels = optionsAt('codec.')
    expect(labels).not.toBeNull()
    expect(labels).toContain('EncodeToHex')
    expect(labels).toContain('Md5')
    // from 对齐到点后（替换半输入的成员名）
    const state = EditorState.create({ doc: 'codec.Encode' })
    const ctx = new CompletionContext(state, 'codec.Encode'.length, false)
    expect(yakCompletion(ctx)?.from).toBe('codec.'.length)
  })

  it('成员补全：str. 覆盖挑战常用函数', () => {
    const labels = optionsAt('str.')
    for (const fn of ['SplitAndTrim', 'ParseStringToLines', 'Join', 'Contains']) {
      expect(labels).toContain(fn)
    }
  })

  it('未知前缀（局部变量）不给成员补全', () => {
    expect(optionsAt('localVar.')).toBeNull()
  })

  it('词首补全：内建 / 库前缀 / 关键字 / 片段齐备', () => {
    const labels = optionsAt('pri')
    expect(labels).toContain('println')
    expect(labels).toContain('printf')
    const any = optionsAt('st')!
    expect(any).toContain('str')
    const kw = optionsAt('fo')!
    expect(kw).toContain('for')
    const snip = optionsAt('std')!
    expect(snip).toContain('stdin')
  })

  it('词首补全：库前缀带中文说明，成员带说明或回退', () => {
    const state = EditorState.create({ doc: 'co' })
    const ctx = new CompletionContext(state, 2, false)
    const opt = yakCompletion(ctx)!.options.find((o) => o.label === 'codec')!
    expect(opt.detail).toContain('编码解码')
    const mstate = EditorState.create({ doc: 'codec.Md5' })
    const mctx = new CompletionContext(mstate, 'codec.Md5'.length, false)
    const mopt = yakCompletion(mctx)!.options.find((o) => o.label === 'Md5')!
    expect(mopt.detail).toContain('MD5')
  })

  it('词汇表真实性：ACM 读 stdin 链路的函数/对象在列', () => {
    for (const fn of ['ReadAll', 'ReadFile']) expect(LIB_FUNCS.io).toContain(fn)
    for (const v of ['Stdin', 'Stdout', 'Getenv']) expect(LIB_FUNCS.os).toContain(v)
    // rand/strings 不是 yak 标准库（yaklang Import 表无此项），不得混入
    expect(Object.keys(LIB_FUNCS)).not.toContain('rand')
    expect(Object.keys(LIB_FUNCS)).not.toContain('strings')
  })
})

describe('yakCompletion 库策略过滤', () => {
  it('默认策略（未配置白名单）：全部库可选', () => {
    expect(defaultYakLibPolicy.allowed).toBeNull()
    const labels = optionsAt('co')
    expect(labels).toContain('codec')
  })

  it('白名单：库前缀仅白名单内可选', () => {
    const labels = optionsAtWith('c', { allowed: ['str', 'io', 'os'], blocked: [] })!
    expect(labels).toContain('str')
    expect(labels).not.toContain('codec')
    expect(labels).not.toContain('fuzz')
  })

  it('白名单：白名单外库的成员补全直接拒绝', () => {
    expect(optionsAtWith('codec.', { allowed: ['str', 'io', 'os'], blocked: [] })).toBeNull()
    // 白名单内库不受影响
    expect(optionsAtWith('str.', { allowed: ['str', 'io', 'os'], blocked: [] })).toContain('Join')
  })

  it('黑名单：黑名单库不进词首补全、成员补全拒绝', () => {
    // exec/file 不在词表（本就未收录），用词表内的库验证黑名单机制
    const labels = optionsAtWith('c', { allowed: null, blocked: ['codec'] })!
    expect(labels).not.toContain('codec')
    expect(optionsAtWith('codec.', { allowed: null, blocked: ['codec'] })).toBeNull()
  })

  it('黑名单优先于白名单：白名单配了黑名单库也拦', () => {
    const policy: YakLibPolicy = { allowed: ['str', 'codec'], blocked: ['codec'] }
    const labels = optionsAtWith('c', policy)!
    expect(labels).toContain('str')
    expect(labels).not.toContain('codec')
    expect(optionsAtWith('codec.', policy)).toBeNull()
  })

  it('stdin 片段依赖 io/os/str：任一禁用则不提供', () => {
    const full = optionsAtWith('std', { allowed: ['str', 'io', 'os'], blocked: [] })!
    expect(full).toContain('stdin')
    const noStr = optionsAtWith('std', { allowed: ['io', 'os', 'codec'], blocked: [] })!
    expect(noStr).not.toContain('stdin')
    // for-range / func 片段不依赖库，恒可用
    expect(noStr).toContain('forrange')
    expect(noStr).toContain('func')
  })
})
