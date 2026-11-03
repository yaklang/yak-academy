// 站内时间统一按中文 24 小时制展示，避免不同浏览器 locale 下出现
// “11/3/2026, 6:54:10 PM” 或月/日顺序漂移。
const dateTimeFormat = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

const timeFormat = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

function parse(value: string | null | undefined) {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatDateTime(value: string | null | undefined): string {
  const date = parse(value)
  return date ? dateTimeFormat.format(date) : '-'
}

export function formatTime(value: string | null | undefined): string {
  const date = parse(value)
  return date ? timeFormat.format(date) : '-'
}
