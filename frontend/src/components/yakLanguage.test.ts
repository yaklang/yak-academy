// yak 语法高亮管道单测：
// 1) 分词层——同一 EditorState 装配 yak 语言，断言语法树节点名（流式解析器
//    以点分 tag 路径为节点名，如 'namespace'、'variableName.function'）；
// 2) 着色层——highlightingFor 断言高亮表覆盖各 tag（词法移植自 yakit Monarch）。
import { describe, expect, it } from 'vitest'
import { EditorState } from '@codemirror/state'
import { syntaxHighlighting, syntaxTree, ensureSyntaxTree, highlightingFor } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'
import { yak } from './yakLanguage'
import { yakHighlight } from './yakTheme'

const stateFor = (code: string) => EditorState.create({ doc: code, extensions: [yak()] })

/** 文档解析出的语法树节点名集合（等待完整解析，避免懒解析截断）。 */
const nodeNames = (code: string): Set<string> => {
  const s = stateFor(code)
  const set = new Set<string>()
  const tree = ensureSyntaxTree(s, s.doc.length, 1e6) ?? syntaxTree(s)
  tree.iterate({ enter: (n) => { set.add(n.name) } })
  return set
}

describe('yakLanguage 分词', () => {
  it('关键字 / 类型 / 内建函数 / 标准库前缀 / 调用 / 数值 / 操作符', () => {
    const n = nodeNames('for i in range(3) { println(a) }\nvar b = codec.EncodeToHex("x")')
    expect(n.has('keyword')).toBe(true) // for / in
    expect(n.has('typeName')).toBe(true) // var
    expect(n.has('namespace')).toBe(true) // codec.
    expect(n.has('variableName.function')).toBe(true) // println / EncodeToHex
    expect(n.has('number')).toBe(true) // 3
    expect(n.has('operator')).toBe(true) // =
    expect(n.has('bracket')).toBe(true) // { }
    expect(n.has('string')).toBe(true) // "x"
  })

  it('注释：行注释与块注释', () => {
    expect(nodeNames('// 行注释\n/* 块\n注释 */ a := 1').has('comment')).toBe(true)
  })

  it('f-string：字符串体与 ${} 内插表达式逐 token 着色', () => {
    const n = nodeNames('println(f"n=${i + 1}")')
    expect(n.has('string')).toBe(true)
    expect(n.has('number')).toBe(true) // 内插里的 1
    expect(n.has('operator')).toBe(true) // ${ } 与 +
  })

  it('x-string 与反引号原串', () => {
    const n = nodeNames('a := x"616263"\nb := `raw\nstring`')
    expect(n.has('string.special')).toBe(true) // x"..."
    expect(n.has('string')).toBe(true) // `...`
  })

  it('heredoc：定界符与正文', () => {
    const n = nodeNames('h := <<<EOF\nline1\nEOF\ndone := 1')
    expect(n.has('meta')).toBe(true) // <<<EOF 与闭合 EOF
    expect(n.has('string')).toBe(true) // 正文行
    expect(n.has('number')).toBe(true) // heredoc 后恢复正常
  })

  it('成员访问：库前缀仅在后随 `.` 时着色，局部同名变量不着色', () => {
    expect(nodeNames('codec.Md5("x")').has('namespace')).toBe(true)
    expect(nodeNames('codec = 1').has('namespace')).toBe(false)
  })

  it('shebang 首行按 meta 处理', () => {
    expect(nodeNames('#!/usr/bin/env yak\na := 1').has('meta')).toBe(true)
  })
})

describe('yakHighlight 着色表', () => {
  it('分词器产出的各 tag 均有着色条目', () => {
    const s = EditorState.create({ doc: '', extensions: [syntaxHighlighting(yakHighlight)] })
    for (const tag of [
      t.keyword, t.typeName, t.namespace, t.function(t.variableName), t.number,
      t.float, t.string, t.special(t.string), t.escape, t.operator, t.bracket,
      t.punctuation, t.meta, t.comment,
    ]) {
      expect(highlightingFor(s, [tag])).toBeTruthy()
    }
  })
})
