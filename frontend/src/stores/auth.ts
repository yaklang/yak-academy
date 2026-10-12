import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi, type User } from '../api'

// 登录态：token + 当前用户。第一版骨架仅本地持有；Phase2 接真实登录。
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

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('academy_token')
  }

  return { token, user, login, logout, setToken }
})