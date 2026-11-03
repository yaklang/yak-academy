// 教程视频地址分类：Room.video_url 由 lesson.md front matter 提供，
// 后端已限制为 http/https 绝对地址。直链按 pathname 后缀判定
// （查询串与 fragment 不影响），其余可嵌入页面走 iframe。
export type VideoKind = 'direct' | 'embed' | 'none'

const DIRECT_EXTENSIONS = ['.mp4', '.webm']

export function videoKind(raw: string | null | undefined): VideoKind {
  const src = raw?.trim() ?? ''
  if (!src) return 'none'

  let url: URL
  try {
    url = new URL(src)
  } catch {
    return 'none'
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return 'none'

  const path = url.pathname.toLowerCase()
  return DIRECT_EXTENSIONS.some((ext) => path.endsWith(ext)) ? 'direct' : 'embed'
}
