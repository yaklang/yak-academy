// auth store 测试：fetchMe 并发去重（路由守卫 + 布局 + 登录页共享一次 /me）、
// user 已填充时短路、失败清 token 登出。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const meMock = vi.fn()

vi.mock('../api', () => ({
  authApi: { me: (...a: unknown[]) => meMock(...a) },
}))

import { useAuthStore } from './auth'
import type { User } from '../api'

const fakeUser: User = {
  id: 1, username: 'admin', global_role: 'admin',
  account_code: 'YA26-00001', phone: '', bio: '', last_login_at: null,
  created_at: '2026-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('auth store fetchMe', () => {
  it('无本地 token 时登录态为空', () => {
    const auth = useAuthStore()
    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
  })

  it('fetchMe 填充用户信息', async () => {
    localStorage.setItem('academy_token', 't')
    meMock.mockResolvedValue(fakeUser)
    const auth = useAuthStore()
    await auth.fetchMe()
    expect(auth.user).toEqual(fakeUser)
    expect(meMock).toHaveBeenCalledTimes(1)
  })

  it('并发调用共享同一次 /me 请求（守卫与布局同时触发只发一次）', async () => {
    localStorage.setItem('academy_token', 't')
    let resolveMe!: (u: User) => void
    meMock.mockImplementation(() => new Promise<User>((r) => (resolveMe = r)))
    const auth = useAuthStore()
    const p1 = auth.fetchMe()
    const p2 = auth.fetchMe()
    const p3 = auth.fetchMe()
    resolveMe(fakeUser)
    await Promise.all([p1, p2, p3])
    expect(meMock).toHaveBeenCalledTimes(1)
    expect(auth.user).toEqual(fakeUser)
  })

  it('user 已填充时再次调用直接短路，不重发请求', async () => {
    localStorage.setItem('academy_token', 't')
    meMock.mockResolvedValue(fakeUser)
    const auth = useAuthStore()
    await auth.fetchMe()
    await auth.fetchMe()
    expect(meMock).toHaveBeenCalledTimes(1)
  })

  it('请求失败：清空 token 登出并抛错', async () => {
    localStorage.setItem('academy_token', 't')
    meMock.mockRejectedValue(new Error('401'))
    const auth = useAuthStore()
    await expect(auth.fetchMe()).rejects.toThrow('登录已失效')
    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
    expect(localStorage.getItem('academy_token')).toBeNull()
  })

  it('logout 清空登录态与本地存储', async () => {
    localStorage.setItem('academy_token', 't')
    meMock.mockResolvedValue(fakeUser)
    const auth = useAuthStore()
    await auth.fetchMe()
    auth.logout()
    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
    expect(localStorage.getItem('academy_token')).toBeNull()
  })
})