import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi, type User } from '../api'

// 登录态：token + 当前用户。登录后拉取 /me 获取真实角色。
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('academy_token'))
  const user = ref<User | null>(null)
  // 进行中的 /me 请求去重：路由守卫与布局可能同时触发，只发一次。
  let meInFlight: Promise<void> | null = null

  function setToken(t: string) {
    token.value = t
    localStorage.setItem('academy_token', t)
  }

  async function login(username: string, password: string) {
    const resp = await authApi.login(username, password)
    setToken(resp.token)
  }

  // 拉取当前用户（登录后/页面刷新时调用；失败则清除本地 token）。
  // 并发调用共享同一次请求；已完成（user 已填充）时直接返回。
  async function fetchMe(): Promise<void> {
    if (user.value) return
    if (meInFlight) return meInFlight
    meInFlight = (async () => {
      try {
        user.value = await authApi.me()
      } catch {
        logout()
        throw new Error('登录已失效')
      } finally {
        meInFlight = null
      }
    })()
    return meInFlight
  }

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('academy_token')
  }

  return { token, user, login, fetchMe, logout, setToken }
})