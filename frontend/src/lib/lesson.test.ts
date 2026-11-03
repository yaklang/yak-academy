import { describe, expect, it } from 'vitest'
import { buildLesson } from './lesson'

describe('buildLesson', () => {
  it('drops a duplicate first H1 and builds a TOC from h2/h3', () => {
    const { html, toc } = buildLesson(
      '# 教程标题\n\n## 原理背景\n\n正文\n\n### 子步骤\n\n更多正文',
      '教程标题',
    )
    expect(html).not.toContain('<h1>教程标题</h1>')
    expect(toc.map((item) => [item.text, item.level])).toEqual([
      ['原理背景', 2],
      ['子步骤', 3],
    ])
    expect(html).toContain('id="lesson-section-1"')
    expect(html).toContain('id="lesson-section-2"')
  })

  it('wraps code blocks with a language label and copy button', () => {
    const { html } = buildLesson('```sql\nselect 1\n```')
    expect(html).toContain('class="code-shell"')
    expect(html).toContain('class="code-lang">sql<')
    expect(html).toContain('data-copy')
    expect(html).toContain('复制代码')
  })

  it('marks knowledge links and hardens external links', () => {
    const { html } = buildLesson(
      '[SQL 注入](#knowledge:sql-injection)\n\n[官网](https://www.yaklang.com/)',
    )
    expect(html).toContain('data-knowledge="sql-injection"')
    expect(html).toContain('class="knowledge-link"')
    expect(html).toContain('aria-haspopup="dialog"')
    expect(html).toContain('target="_blank"')
    expect(html).toContain('rel="noopener noreferrer"')
  })

  it('keeps headings inside details out of the TOC', () => {
    const { toc } = buildLesson('## 可见标题\n\n<details><summary>展开</summary>\n\n### 折叠标题\n\n</details>')
    expect(toc.map((item) => item.text)).toEqual(['可见标题'])
  })
})
