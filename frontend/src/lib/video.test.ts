import { describe, expect, it } from 'vitest'
import { videoKind } from './video'

describe('videoKind', () => {
  it('detects mp4/webm direct links regardless of query, fragment, or case', () => {
    expect(videoKind('https://cdn.example.com/intro.mp4')).toBe('direct')
    expect(videoKind('https://cdn.example.com/intro.webm?token=a')).toBe('direct')
    expect(videoKind('https://cdn.example.com/intro.MP4#t=10')).toBe('direct')
  })

  it('uses iframe for embeddable pages', () => {
    expect(videoKind('https://player.bilibili.com/player.html?bvid=BV1xx')).toBe('embed')
    expect(videoKind('https://www.youtube.com/embed/abc')).toBe('embed')
  })

  it('rejects empty, relative, and non-http(s) URLs', () => {
    expect(videoKind('')).toBe('none')
    expect(videoKind('   ')).toBe('none')
    expect(videoKind('/videos/intro.mp4')).toBe('none')
    expect(videoKind('//cdn.example.com/intro.mp4')).toBe('none')
    expect(videoKind('javascript:alert(1)')).toBe('none')
    expect(videoKind('ftp://cdn.example.com/intro.mp4')).toBe('none')
  })
})
