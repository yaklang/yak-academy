// yak 编辑器暗色主题 + 语法着色表。
// 取色参考 yakit kurior 暗色编辑器（theme.css / monacoSpec/theme.ts），
// 底色按「代码字色全白」的需求压深（yakit 底 #37393c → #212428），
// 个别 yakit 色（如字符串绿 #639300）在深底上对比度不足，保持色相提亮。
// 着色 token：关键字蓝 / 类型粉 / 标准库前缀青 / 数值青绿 / 字符串绿 /
// 操作符紫 / 注释灰斜体——与 yakit 暗色编辑器观感一致。

import { EditorView } from '@codemirror/view'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

const yakChrome = EditorView.theme(
  {
    '&': {
      color: '#ffffff',
      backgroundColor: '#212428',
      fontSize: '13px',
    },
    '.cm-scroller': {
      fontFamily:
        "'JetBrains Mono', 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace",
      lineHeight: '1.65',
    },
    '.cm-content': {
      caretColor: '#7ab7ff',
      padding: '10px 0',
    },
    '.cm-cursor, .cm-dropCursor': {
      borderLeftColor: '#7ab7ff',
      borderLeftWidth: '2px',
    },
    '&.cm-focused': { outline: 'none' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, & ::selection': {
      backgroundColor: '#794622',
    },
    '.cm-gutters': {
      backgroundColor: '#1b1e22',
      color: '#5c6470',
      border: 'none',
      borderRight: '1px solid #2c313a',
    },
    '.cm-lineNumbers .cm-gutterElement': {
      padding: '0 10px 0 14px',
      minWidth: '36px',
    },
    '.cm-activeLineGutter': {
      backgroundColor: '#1b1e22',
      color: '#f0a45d',
    },
    '.cm-activeLine': { backgroundColor: '#272b31' },
    '.cm-matchingBracket': {
      backgroundColor: '#3a3f47',
      outline: '1px solid #6a7280',
      color: 'inherit !important',
    },
    '.cm-nonmatchingBracket': { outline: '1px solid #e06c75' },
    '.cm-placeholder': {
      color: '#5c6470',
      fontStyle: 'italic',
    },
    '.cm-tooltip': {
      backgroundColor: '#2b3038',
      border: '1px solid #3c4149',
      color: '#c8d0dd',
    },
    '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': {
      backgroundColor: '#3d6db5',
      color: '#ffffff',
    },
    '.cm-scroller::-webkit-scrollbar': { width: '10px', height: '10px' },
    '.cm-scroller::-webkit-scrollbar-thumb': {
      backgroundColor: '#3c4149',
      borderRadius: '5px',
    },
    '.cm-scroller::-webkit-scrollbar-track': { backgroundColor: 'transparent' },
    '.cm-foldGutter span': { color: '#868d9a' },
    '.cm-foldGutter span:hover': { color: '#f0a45d' },
  },
  { dark: true },
)

/** yak 语义配色表（暗底对比度调校），单测直接引用。 */
export const yakHighlight = HighlightStyle.define([
  { tag: t.keyword, color: '#63a5ff', fontWeight: '500' },
  { tag: t.typeName, color: '#e277e4' },
  { tag: t.namespace, color: '#52c8d8' },
  { tag: t.function(t.variableName), color: '#e0873c' },
  { tag: t.number, color: '#52c8d8' },
  { tag: t.float, color: '#71d3a3' },
  { tag: t.string, color: '#98c379' },
  { tag: t.special(t.string), color: '#9dd6c8' },
  { tag: t.escape, color: '#d19a66' },
  { tag: t.operator, color: '#bb73ea' },
  { tag: t.bracket, color: '#c8d0dd' },
  { tag: t.punctuation, color: '#9aa3b2' },
  { tag: t.meta, color: '#9aa3b2', fontStyle: 'italic' },
  { tag: t.comment, color: '#868d9a', fontStyle: 'italic' },
])

/** yak 编辑器主题：暗色底 + 白色代码字 + yakit 语义配色。 */
export const yakEditorTheme = [yakChrome, syntaxHighlighting(yakHighlight)]
