// yak 语法配色保留语义区分，编辑器底色、选区和提示框跟随学院明暗主题。

import { EditorView } from '@codemirror/view'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

const yakChrome = EditorView.theme(
  {
    '&': {
      color: 'var(--foreground)',
      backgroundColor: 'var(--background)',
      fontSize: '13px',
    },
    '.cm-scroller': {
      fontFamily:
        "'JetBrains Mono', 'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace",
      lineHeight: '1.65',
    },
    '.cm-content': {
      caretColor: 'var(--accent-foreground)',
      padding: '10px 0',
    },
    '.cm-cursor, .cm-dropCursor': {
      borderLeftColor: 'var(--accent-foreground)',
      borderLeftWidth: '2px',
    },
    '&.cm-focused': { outline: 'none' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, & ::selection': {
      backgroundColor: 'var(--accent)',
    },
    '.cm-gutters': {
      backgroundColor: 'var(--muted)',
      color: 'var(--muted-foreground)',
      border: 'none',
      borderRight: '1px solid var(--border)',
    },
    '.cm-lineNumbers .cm-gutterElement': {
      padding: '0 10px 0 14px',
      minWidth: '36px',
    },
    '.cm-activeLineGutter': {
      backgroundColor: 'var(--muted)',
      color: 'var(--accent-foreground)',
    },
    '.cm-activeLine': { backgroundColor: 'var(--muted)' },
    '.cm-matchingBracket': {
      backgroundColor: 'var(--muted)',
      outline: '1px solid var(--ring)',
      color: 'inherit !important',
    },
    '.cm-nonmatchingBracket': { outline: '1px solid var(--destructive)' },
    '.cm-placeholder': {
      color: 'var(--muted-foreground)',
      fontStyle: 'italic',
    },
    '.cm-tooltip': {
      backgroundColor: 'var(--popover)',
      border: '1px solid var(--border)',
      color: 'var(--code-bracket)',
    },
    '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': {
      backgroundColor: 'var(--accent)',
      color: 'var(--foreground)',
    },
    '.cm-scroller::-webkit-scrollbar': { width: '10px', height: '10px' },
    '.cm-scroller::-webkit-scrollbar-thumb': {
      backgroundColor: 'var(--border)',
      borderRadius: '5px',
    },
    '.cm-scroller::-webkit-scrollbar-track': { backgroundColor: 'transparent' },
    '.cm-foldGutter span': { color: 'var(--muted-foreground)' },
    '.cm-foldGutter span:hover': { color: 'var(--accent-foreground)' },
  },
  { dark: false },
)

/** yak 语义配色表：由明暗主题的 CSS tokens 提供可读颜色。 */
export const yakHighlight = HighlightStyle.define([
  { tag: t.keyword, color: 'var(--code-keyword)', fontWeight: '500' },
  { tag: t.typeName, color: 'var(--code-type)' },
  { tag: t.namespace, color: 'var(--code-namespace)' },
  { tag: t.function(t.variableName), color: 'var(--code-function)' },
  { tag: t.number, color: 'var(--code-namespace)' },
  { tag: t.float, color: 'var(--code-number)' },
  { tag: t.string, color: 'var(--code-string)' },
  { tag: t.special(t.string), color: 'var(--code-string)' },
  { tag: t.escape, color: 'var(--code-function)' },
  { tag: t.operator, color: 'var(--code-type)' },
  { tag: t.bracket, color: 'var(--code-bracket)' },
  { tag: t.punctuation, color: 'var(--muted-foreground)' },
  { tag: t.meta, color: 'var(--muted-foreground)', fontStyle: 'italic' },
  { tag: t.comment, color: 'var(--muted-foreground)', fontStyle: 'italic' },
])

/** yak 编辑器主题：与学院页面共享明暗模式。 */
export const yakEditorTheme = [yakChrome, syntaxHighlighting(yakHighlight)]
