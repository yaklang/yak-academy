// yak 代码补全（静态表，数据见 yakVocab.ts）。
// yakit 的补全走 yak 语言服务器 IPC（浏览器端无此通道），这里按教学场景做
// 静态补全：关键字 / 类型 / 内建函数 / 标准库前缀（含一句话说明）、
// `lib.` 成员补全（真实导出函数全名）、常用片段（for-range / ACM stdin 模板）。

import { snippet } from '@codemirror/autocomplete'
import type { Completion, CompletionContext, CompletionResult } from '@codemirror/autocomplete'
import {
  KEYWORDS, TYPES, BUILTINS, BUILTIN_DESC, LIB_DESC, FN_DESC, LIB_FUNCS,
} from './yakVocab'

const keywordOptions: Completion[] = KEYWORDS.map((w) => ({
  label: w,
  type: 'keyword',
  detail: '关键字',
  boost: -20,
}))

const typeOptions: Completion[] = TYPES.map((w) => ({
  label: w,
  type: 'type',
  detail: '类型',
}))

const builtinOptions: Completion[] = BUILTINS.map((w) => ({
  label: w,
  type: 'function',
  detail: BUILTIN_DESC[w] ?? '内建函数',
}))

// 库前缀直接补成 `str.`，继续输入即触发成员补全。
const libOptions: Completion[] = Object.keys(LIB_FUNCS).map((lib) => ({
  label: lib,
  apply: `${lib}.`,
  type: 'namespace',
  detail: LIB_DESC[lib] ?? 'yak 标准库',
  boost: 10,
}))

function memberOptions(lib: string): Completion[] {
  return (LIB_FUNCS[lib] ?? []).map((name) => ({
    label: name,
    type: 'method',
    detail: FN_DESC[`${lib}.${name}`] ?? `${lib} 标准库`,
  }))
}

const snippetOptions: Completion[] = [
  {
    label: 'forrange',
    type: 'text',
    detail: '片段：for i := range …',
    apply: snippet('for i := range ${1:list} {\n\t${2}\n}'),
    boost: 5,
  },
  {
    label: 'forrangev',
    type: 'text',
    detail: '片段：for k, v := range …',
    apply: snippet('for k, v := range ${1:list} {\n\t${2}\n}'),
  },
  {
    label: 'func',
    type: 'text',
    detail: '片段：函数定义',
    apply: snippet('func ${1:name}(${2:args}) {\n\t${3}\n}'),
  },
  {
    label: 'stdin',
    type: 'text',
    detail: '片段：ACM 读 stdin 模板',
    apply: snippet(
      'res := io.ReadAll(os.Stdin)\n' +
      'lines := str.ParseStringToLines(string(res[0]))\n' +
      'for i := range lines {\n\ttoks := str.SplitAndTrim(lines[i], " ")\n\t${1}\n}',
    ),
    boost: 20,
  },
]

const ident = /^[A-Za-z_][A-Za-z0-9_]*$/

/** yak 补全源：`lib.` 成员上下文给库函数，其余给关键字/内建/库前缀/片段。 */
export function yakCompletion(context: CompletionContext): CompletionResult | null {
  const line = context.state.doc.lineAt(context.pos)
  const before = line.text.slice(0, context.pos - line.from)

  const member = /([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z0-9_]*)$/.exec(before)
  if (member) {
    const [, lib, typed] = member
    if (!LIB_FUNCS[lib]) return null // 未知前缀（局部变量等）不给成员补全
    return {
      from: context.pos - typed.length,
      options: memberOptions(lib),
      validFor: /^[A-Za-z0-9_]*$/,
    }
  }

  const word = context.matchBefore(/[A-Za-z_][A-Za-z0-9_]*/)
  if (!word || (word.from === word.to && !context.explicit)) return null
  return {
    from: word.from,
    options: [...builtinOptions, ...libOptions, ...keywordOptions, ...typeOptions, ...snippetOptions],
    validFor: ident,
  }
}
