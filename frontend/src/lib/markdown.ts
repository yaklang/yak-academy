// Markdown rendering shared by lessons and challenge descriptions.
// Content is authored independently of the page shell, so its headings are
// demoted one level: the page owns the only h1 (PageHeader / competition title)
// and every markdown section becomes a proper h2/h3 subsection.
import { marked } from 'marked'
import DOMPurify from 'dompurify'

export function demoteHeadings(root: ParentNode): void {
  // Documents authored from h2 already respect the page h1; only a document
  // that starts at h1 is shifted as a whole so its internal hierarchy holds.
  const first = root.querySelector('h1, h2, h3, h4, h5, h6')
  if (!first || first.tagName !== 'H1') return
  root.querySelectorAll('h1, h2, h3, h4, h5').forEach((node) => {
    const next = document.createElement(`h${Number(node.tagName.slice(1)) + 1}`)
    for (const attr of Array.from(node.attributes)) next.setAttribute(attr.name, attr.value)
    next.innerHTML = node.innerHTML
    node.replaceWith(next)
  })
}

export function renderMarkdownHtml(raw: string | null | undefined): string {
  const root = document.createElement('div')
  root.innerHTML = DOMPurify.sanitize(marked.parse(raw ?? '') as string)
  demoteHeadings(root)
  return root.innerHTML
}