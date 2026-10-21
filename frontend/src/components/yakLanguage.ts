// yak 语言高亮（CodeMirror 6 StreamLanguage）。
// 词法规则移植自 yakit 前端的 Monarch 定义（app/renderer/src/main/src/utils/
// monacoSpec/yakEditor.ts）——yakit 基于 Monaco，本仓库编辑器为 CodeMirror 6，
// 无现成实现可抄，故按同一套语义手写流式分词器：
//   关键字 / 基础类型（uint…string/omap/any）/ 内建函数（println…）/
//   标准库前缀（codec. str. cli. …，取自 yak 标准库目录）；
//   f"x${expr}" 内插表达式（${} 内按普通代码逐 token 着色）、x"..." 字节串、
//   反引号原串、<<<HEREDOC、// 与 /* */ 注释、文件首行 #! shebang。
// 语法检测即着色（与 yakit 编辑器的 Monarch 高亮同级）：
// 编译级校验由判题引擎负责，前端不内置 yak 编译器。

import { StreamLanguage, LanguageSupport } from '@codemirror/language'
import type { StreamParser, StringStream } from '@codemirror/language'

const KEYWORDS = new Set([
  'break', 'case', 'continue', 'default', 'defer', 'else', 'elif', 'for', 'go',
  'if', 'in', 'range', 'return', 'select', 'switch', 'chan', 'func', 'fn',
  'def', 'class', 'include', 'type', 'try', 'catch', 'finally', 'map',
])
// bool/true/false/nil/undefined/string/var 在 yakit 中按关键字/类型着色，
// 这里归入类型（声明位与字面量位统一粉色，视觉上与控制流蓝色区分）。
const TYPES = new Set([
  'uint', 'uint8', 'uint16', 'uint32', 'uint64', 'int', 'int8', 'int16',
  'int32', 'int64', 'float', 'float32', 'float64', 'double', 'byte', 'string',
  'bool', 'var', 'any', 'omap', 'true', 'false', 'nil', 'undefined',
])
// 全局内建函数（yakit Monarch 的 globals）。
const BUILTINS = new Set([
  'println', 'printf', 'print', 'panic', 'recover', 'wait', 'make', 'len',
  'cap', 'append', 'new', 'delete', 'exit', 'typeof',
])
// 常用 yak 标准库前缀（仅在后随 `.` 时着色，避免误伤同名局部变量）。
const LIBS = new Set([
  'codec', 'str', 'cli', 'poc', 'fuzz', 'http', 'file', 'semgrep', 'so',
  'sleep', 'rand', 'strings', 'time', 'json', 'xlsx', 'csv', 'fp', 'nmap',
  'loader', 'yakit', 'yak', 'ui', 'graph', 'brute', 'sqli', 'space',
  'speaker', 'engine', 'sync', 'ssh', 'rawfuzz',
])

const IDENT_START = /[A-Za-z_]/
const OPERATOR = /[+\-*/%=<>!&|^~?]/
const BRACKETS = new Set(['(', ')', '[', ']', '{', '}'])
const PUNCT = new Set([',', ';', ':', '.'])

type Ctx =
  | { k: 'comment' }
  | { k: 'str'; quote: string }
  | { k: 'raw' }
  | { k: 'xstr' }
  | { k: 'fstr'; exprDepth: number | null }
  | { k: 'heredoc'; id: string; started: boolean }

class YakState {
  ctx: Ctx[] = []
  afterDot = false

  clone(): YakState {
    const s = new YakState()
    s.ctx = this.ctx.map((c) => ({ ...c }) as Ctx)
    s.afterDot = this.afterDot
    return s
  }

  /** 普通（非字符串/注释态）token：f-string 内插表达式复用同一入口。 */
  normal(stream: StringStream) {
    // 成员访问链：上一 token 若是 `.`，本 token 按「后随成员」着色。
    const wasAfterDot = this.afterDot
    this.afterDot = false
    if (stream.eatSpace()) return null
    const ch = stream.peek()!

    if (stream.match('//')) {
      stream.skipToEnd()
      return 'comment'
    }
    if (stream.match('/*')) {
      this.ctx.push({ k: 'comment' })
      return 'comment'
    }
    if (stream.match('<<<')) {
      const m = stream.match(/^([A-Za-z_][A-Za-z0-9_]*)/) as RegExpMatchArray | null
      if (m) {
        this.ctx.push({ k: 'heredoc', id: m[1], started: false })
        return 'meta'
      }
      return 'operator'
    }
    if (ch === '"' || ch === "'") {
      stream.next()
      this.ctx.push({ k: 'str', quote: ch })
      return 'string'
    }
    if (ch === '`') {
      stream.next()
      this.ctx.push({ k: 'raw' })
      return 'string'
    }
    if (stream.match(/^f"/)) {
      this.ctx.push({ k: 'fstr', exprDepth: null })
      return 'string'
    }
    if (stream.match(/^x"/)) {
      this.ctx.push({ k: 'xstr' })
      return 'string.special'
    }

    if (IDENT_START.test(ch)) {
      const m = stream.match(/^[A-Za-z_][A-Za-z0-9_]*/) as RegExpMatchArray | null
      const word = m?.[0] ?? ''
      const isCall = stream.peek() === '('
      if (KEYWORDS.has(word)) return 'keyword'
      if (TYPES.has(word)) return 'typeName'
      if (BUILTINS.has(word)) return 'variableName.function'
      if (wasAfterDot) {
        // 成员访问：仅函数调用形态单独着色，其余回落默认白。
        return isCall ? 'variableName.function' : null
      }
      if (stream.peek() === '.' && LIBS.has(word)) return 'namespace'
      if (isCall) return 'variableName.function'
      return null
    }

    if (/[0-9]/.test(ch)) {
      if (stream.match(/^0[xX][0-9a-fA-F]+/)) return 'number'
      const m = stream.match(/^(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?/) as RegExpMatchArray | null
      if (m && (m[1].includes('.') || m[3])) return 'float'
      return 'number'
    }

    if (ch === '.') {
      if (/[0-9]/.test(stream.string[stream.pos + 1] ?? '')) {
        const m = stream.match(/^\.\d+([eE][+-]?\d+)?/)
        if (m) return 'float'
      }
      stream.next()
      this.afterDot = true
      return 'punctuation'
    }
    if (BRACKETS.has(ch)) {
      stream.next()
      return 'bracket'
    }
    if (PUNCT.has(ch)) {
      stream.next()
      if (ch === ':') return 'operator' // := 与 : 通道/映射标记统一操作符紫
      return 'punctuation'
    }
    if (OPERATOR.test(ch)) {
      stream.eatWhile(OPERATOR)
      return 'operator'
    }
    if (ch === '#' && stream.pos === 0 && stream.string[1] === '!') {
      stream.skipToEnd()
      return 'meta'
    }
    stream.next()
    return null
  }
}

const yakStream: StreamParser<YakState> = {
  name: 'yak',
  startState: () => new YakState(),
  copyState: (s) => s.clone(),
  token: (stream, state) => {
    const ctx = state.ctx[state.ctx.length - 1]

    if (!ctx) {
      // 文件首行 #!：normal 内按位置判定。
      return state.normal(stream)
    }

    switch (ctx.k) {
      case 'comment': {
        if (stream.match('*/')) state.ctx.pop()
        else stream.skipToEnd()
        return 'comment'
      }
      case 'str': {
        if (stream.eol()) {
          state.ctx.pop() // 未闭合恢复：行尾回普通态
          return null
        }
        if (stream.match(/^\\./)) return 'escape'
        if (stream.peek() === ctx.quote) {
          stream.next()
          state.ctx.pop()
          return 'string'
        }
        stream.eatWhile((c) => c !== '\\' && c !== ctx.quote)
        return 'string'
      }
      case 'raw': {
        if (stream.peek() === '`') {
          stream.next()
          state.ctx.pop()
        } else {
          stream.next()
        }
        return 'string'
      }
      case 'xstr': {
        if (stream.eol()) {
          state.ctx.pop()
          return null
        }
        if (stream.peek() === '"') {
          stream.next()
          state.ctx.pop()
          return 'string.special'
        }
        stream.eatWhile((c) => c !== '"')
        return 'string.special'
      }
      case 'fstr': {
        if (ctx.exprDepth !== null) {
          // ${expr} 内部：按普通代码着色，花括号计数找闭合。
          if (stream.eatSpace()) return null
          if (stream.peek() === '{') {
            ctx.exprDepth++
            stream.next()
            return 'bracket'
          }
          if (stream.peek() === '}') {
            stream.next()
            if (ctx.exprDepth === 0) {
              ctx.exprDepth = null
              return 'operator'
            }
            ctx.exprDepth--
            return 'bracket'
          }
          return state.normal(stream)
        }
        if (stream.eol()) {
          state.ctx.pop()
          return null
        }
        if (stream.match('${')) {
          ctx.exprDepth = 0
          return 'operator'
        }
        if (stream.match(/^\\./)) return 'escape'
        if (stream.peek() === '"') {
          stream.next()
          state.ctx.pop()
          return 'string'
        }
        stream.eatWhile((c) => c !== '\\' && c !== '"' && c !== '$')
        return 'string'
      }
      case 'heredoc': {
        if (!ctx.started) {
          stream.skipToEnd()
          ctx.started = true
          return 'string'
        }
        if (stream.sol() && stream.string.trim() === ctx.id) {
          stream.skipToEnd()
          state.ctx.pop()
          return 'meta'
        }
        stream.skipToEnd()
        return 'string'
      }
    }
    return null
  },
  languageData: {
    commentTokens: { line: '//', block: { open: '/*', close: '*/' } },
    closeBrackets: { brackets: ['(', '[', '{', "'", '"', '`'] },
  },
}

/** yak 语言扩展：高亮 + 注释切换（Ctrl+/）+ 括号补全。 */
export function yak(): LanguageSupport {
  return new LanguageSupport(StreamLanguage.define(yakStream))
}
