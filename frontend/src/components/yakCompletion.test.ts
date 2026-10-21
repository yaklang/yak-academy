// yak 补全源单测：直接构造 CompletionContext 驱动 yakCompletion，
// 覆盖成员补全（lib. 前缀）、词首补全（关键字/内建/库前缀/片段）、
// 未知前缀不补、数据源真实性（挑战用到的函数必须在列）。
import { describe, expect, it } from 'vitest'
import { EditorState } from '@codemirror/state'
import { CompletionContext } from '@codemirror/autocomplete'
import { yakCompletion } from './yakCompletion'
import { LIB_FUNCS } from './yakVocab'

const optionsAt = (doc: string) => {
  const state = EditorState.create({ doc })
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
