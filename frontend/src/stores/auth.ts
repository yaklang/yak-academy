import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi, type User } from '../api'

// 登录态：token + 当前用户。登录后拉取 /me 获取真实角色。
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('academy_token'))
  const user = ref<User | null>(null)

  function setToken(t: string) {
    token.value = t
    localStorage.setItem('academy_token', t)
  }

  async function login(username: string, password: string) {
    const resp = await authApi.login(username, password)
    setToken(resp.token)
  }

  // 拉取当前用户（登录后/页面刷新时调用；失败则清除本地 token）
  async function fetchMe() {
    try {
      user.value = await authApi.me()
    } catch {
      logout()
      throw new Error('登录已失效')
    }
  }

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('academy_token')
  }

  return { token, user, login, fetchMe, logout, setToken }
})