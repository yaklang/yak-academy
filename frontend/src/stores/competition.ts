import { defineStore } from 'pinia'
import { ref } from 'vue'
import { competitionApi } from '../api'

// 参赛锁定态（服务端派生：已入场 ∧ 竞赛未结束）。前端缓存仅做 UX 引导，
// 真正的强制隔离在后端中间件（非竞赛接口一律 403）。
// 锁只可能经"入场页显式输入邀请码"产生，入场成功即置位；登录/登出后重查。
export const useCompetitionStore = defineStore('competition', () => {
  const lockedCompetitionId = ref<number | null>(null)
  let checked = false

  async function refresh(): Promise<number | null> {
    try {
      const entry = await competitionApi.myEntry()
      lockedCompetitionId.value = entry.locked && entry.competition ? entry.competition.id : null
    } catch {
      lockedCompetitionId.value = null
    }
    checked = true
    return lockedCompetitionId.value
  }

  // 路由守卫用：进程内只查一次；置位后的解锁（比赛结束）由竞赛首页自查自解。
  async function ensureChecked(): Promise<number | null> {
    return checked ? lockedCompetitionId.value : refresh()
  }

  function lock(id: number) {
    lockedCompetitionId.value = id
    checked = true
  }

  function unlock() {
    lockedCompetitionId.value = null
    checked = true
  }

  return { lockedCompetitionId, refresh, ensureChecked, lock, unlock }
})
