import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi, type User } from '../api'
import { useCompetitionStore } from './competition'

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

  // 参赛锁定缓存随登录态重置（作废缓存，由路由守卫重查服务端——
  // 重置成 unlock() 会把「未锁定」标记为已检查，锁定者重登后被留在 403 空页）。
  function resetCompetition() {
    useCompetitionStore().reset()
  }

  async function login(username: string, password: string) {
    const resp = await authApi.login(username, password)
    // 作废改密/登出前遗留的 in-flight fetchMe：其 401 兜底会清掉刚存的新 token，
    // 且复用旧 promise 会让新登录的 /me 拉取被吞（改密→重新登录卡在登录页）。
    meInFlight = null
    user.value = null
    resetCompetition()
    setToken(resp.token)
  }

  // 自助注册（手机验证码）：建号成功即视为登录（后端直接签发 token），
  // 与 login 相同的本地态切换（作废 in-flight /me、清用户缓存、重置竞赛锁）。
  async function register(phone: string, code: string, username: string, password: string) {
    const resp = await authApi.register(phone, code, username, password)
    meInFlight = null
    user.value = null
    resetCompetition()
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

  // 登出：吊销当前 token（后端撤销表），无论接口结果如何都清理本地态。
  async function logout() {
    if (token.value) {
      try {
        await authApi.logout()
      } catch {
        // token 已失效/网络失败也照常清理本地登录态
      }
    }
    token.value = null
    user.value = null
    localStorage.removeItem('academy_token')
    resetCompetition()
  }

  return { token, user, login, register, fetchMe, logout, setToken }
})