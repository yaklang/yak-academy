import { marked } from 'marked'
import DOMPurify from 'dompurify'

export interface LessonTocItem {
  id: string
  text: string
  level: 2 | 3
}

export interface LessonRender {
  html: string
  toc: LessonTocItem[]
}

// 教程 Markdown 渲染：在 sanitize 之后做结构与交互增强（TOC、代码复制、
// 知识链接、表格滚动容器）。HTML 已由 DOMPurify 清洗，这里只改安全属性。
export function buildLesson(rawMarkdown: string, title?: string): LessonRender {
  const sanitized = DOMPurify.sanitize(marked.parse(rawMarkdown ?? '') as string)
  const root = document.createElement('div')
  root.innerHTML = sanitized

  // 正文常带与页面标题重复的一级标题，隐藏掉避免首屏重复。
  const first = root.firstElementChild
  if (first?.tagName === 'H1' && title?.trim() && first.textContent?.trim() === title.trim()) {
    first.remove()
  }

  const toc: LessonTocItem[] = []
  root.querySelectorAll('h2, h3').forEach((node, index) => {
    const el = node as HTMLElement
    if (el.closest('details')) return // 折叠区内的标题不进入目录，避免跳转到不可见位置
    const text = el.textContent?.trim() ?? ''
    if (!text) return
    const id = `lesson-section-${index + 1}`
    el.id = id
    const level: 2 | 3 = el.tagName === 'H2' ? 2 : 3
    toc.push({ id, text, level })
  })

  root.querySelectorAll('table').forEach((table) => {
    const wrap = document.createElement('div')
    wrap.className = 'table-scroll'
    table.replaceWith(wrap)
    wrap.appendChild(table)
  })

  root.querySelectorAll('pre').forEach((pre) => {
    const code = pre.querySelector('code')
    const lang = code?.className.match(/language-([\w-]+)/)?.[1] ?? 'text'

    const shell = document.createElement('div')
    shell.className = 'code-shell'

    const head = document.createElement('div')
    head.className = 'code-head'

    const label = document.createElement('span')
    label.className = 'code-lang'
    label.textContent = lang

    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'code-copy'
    button.dataset.copy = ''
    button.setAttribute('aria-label', '复制代码')
    button.textContent = '复制'

    head.append(label, button)
    pre.replaceWith(shell)
    shell.append(head, pre)
  })

  root.querySelectorAll('a').forEach((a) => {
    const href = a.getAttribute('href') ?? ''
    if (href.startsWith('#knowledge:')) {
      const id = href.slice('#knowledge:'.length)
      if (!id) return
      a.classList.add('knowledge-link')
      a.dataset.knowledge = id
      a.setAttribute('aria-haspopup', 'dialog')
      a.setAttribute('title', '点击查看知识说明')
      const mark = document.createElement('span')
      mark.className = 'knowledge-mark'
      mark.setAttribute('aria-hidden', 'true')
      mark.textContent = '?'
      a.appendChild(mark)
      return
    }
    if (/^https?:\/\//i.test(href)) {
      a.setAttribute('target', '_blank')
      a.setAttribute('rel', 'noopener noreferrer')
      a.classList.add('external-link')
    }
  })

  root.querySelectorAll('img').forEach((img) => {
    img.setAttribute('loading', 'lazy')
    img.setAttribute('decoding', 'async')
  })

  return { html: root.innerHTML, toc }
}
