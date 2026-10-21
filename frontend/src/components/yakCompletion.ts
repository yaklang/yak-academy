// yak 代码补全（静态表，数据见 yakVocab.ts）。
// yakit 的补全走 yak 语言服务器 IPC（浏览器端无此通道），这里按教学场景做
// 静态补全：关键字 / 类型 / 内建函数 / 标准库前缀（含一句话说明）、
// `lib.` 成员补全（真实导出函数全名）、常用片段（for-range / ACM stdin 模板）。
// 库选项按题目策略过滤（yakLibPolicy facet：白名单 + 平台黑名单，与后端
// 受理前拦截同源——服务端下发 ChallengeDetail.judge.allowed_libs/blocked_libs）。

import { snippet } from '@codemirror/autocomplete'
import type { Completion, CompletionContext, CompletionResult } from '@codemirror/autocomplete'
import { Facet } from '@codemirror/state'
import {
  KEYWORDS, TYPES, BUILTINS, BUILTIN_DESC, LIB_DESC, FN_DESC, LIB_FUNCS,
} from './yakVocab'

/** 题目级库策略：allowed=null 未配置（允许全部非黑名单）；blocked 为平台黑名单。 */
export interface YakLibPolicy {
  allowed: string[] | null
  blocked: string[]
}

export const defaultYakLibPolicy: YakLibPolicy = { allowed: null, blocked: [] }

/** 编辑器扩展：注入当前题目的库策略（每题一个实例，CodeEditor 按 prop 装配）。 */
export const yakLibPolicy = Facet.define<YakLibPolicy, YakLibPolicy>({
  combine: (values) => (values.length ? values[values.length - 1] : defaultYakLibPolicy),
})

/** 库在该策略下是否可用（黑名单恒禁；白名单配置后仅白名单内可用）。 */
function libUsable(lib: string, policy: YakLibPolicy): boolean {
  if (policy.blocked.includes(lib)) return false
  if (policy.allowed === null) return true
  return policy.allowed.includes(lib)
}

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

// 库前缀直接补成 `str.`，继续输入即触发成员补全（按策略过滤）。
function libOptionsFor(policy: YakLibPolicy): Completion[] {
  return Object.keys(LIB_FUNCS)
    .filter((lib) => libUsable(lib, policy))
    .map((lib) => ({
      label: lib,
      apply: `${lib}.`,
      type: 'namespace',
      detail: LIB_DESC[lib] ?? 'yak 标准库',
      boost: 10,
    }))
}

function memberOptions(lib: string): Completion[] {
  return (LIB_FUNCS[lib] ?? []).map((name) => ({
    label: name,
    type: 'method',
    detail: FN_DESC[`${lib}.${name}`] ?? `${lib} 标准库`,
  }))
}

// stdin 模板依赖 io/os/str 三个库，任一被策略禁用则不提供该片段
//（应用后必被服务端 422 拒绝，不如源头不给）。
function snippetOptionsFor(policy: YakLibPolicy): Completion[] {
  const base: Completion[] = [
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
  ]
  if (libUsable('io', policy) && libUsable('os', policy) && libUsable('str', policy)) {
    base.push({
      label: 'stdin',
      type: 'text',
      detail: '片段：ACM 读 stdin 模板',
      apply: snippet(
        'res := io.ReadAll(os.Stdin)\n' +
        'lines := str.ParseStringToLines(string(res[0]))\n' +
        'for i := range lines {\n\ttoks := str.SplitAndTrim(lines[i], " ")\n\t${1}\n}',
      ),
      boost: 20,
    })
  }
  return base
}

const ident = /^[A-Za-z_][A-Za-z0-9_]*$/

/** yak 补全源：`lib.` 成员上下文给库函数，其余给关键字/内建/库前缀/片段。 */
export function yakCompletion(context: CompletionContext): CompletionResult | null {
  const policy = context.state.facet(yakLibPolicy)
  const line = context.state.doc.lineAt(context.pos)
  const before = line.text.slice(0, context.pos - line.from)

  const member = /([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z0-9_]*)$/.exec(before)
  if (member) {
    const [, lib, typed] = member
    if (!LIB_FUNCS[lib]) return null // 未知前缀（局部变量等）不给成员补全
    if (!libUsable(lib, policy)) return null // 策略禁用的库：成员也不给（防绕过）
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
    options: [
      ...builtinOptions,
      ...libOptionsFor(policy),
      ...keywordOptions,
      ...typeOptions,
      ...snippetOptionsFor(policy),
    ],
    validFor: ident,
  }
}
