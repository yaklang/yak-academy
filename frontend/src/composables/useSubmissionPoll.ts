// 提交判定轮询：2s 间隔，上限 30 次（异步判题，event-sourcing-rules skill）。
// ChallengeDetail 页共用；判定中禁用重复提交。
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { submissionApi, type Submission } from '../api'

export function useSubmissionPoll(onCorrect?: (sub: Submission) => void) {
  const submitting = ref(false)
  const polling = ref(false)
  const result = ref<{ status: string; score: number } | null>(null)

  // 轮询判定结果直到出分或超时。
  async function poll(id: number) {
    polling.value = true
    for (let i = 0; i < 30; i++) {
      await new Promise((r) => setTimeout(r, 2000))
      try {
        const sub = await submissionApi.get(id)
        if (sub.status !== 'pending') {
          result.value = sub
          if (sub.status === 'correct') {
            ElMessage.success(`回答正确！+${sub.score} 分`)
            onCorrect?.(sub)
          } else {
            ElMessage.error('回答错误，再试试')
          }
          polling.value = false
          return
        }
      } catch (e) {
        polling.value = false
        ElMessage.error((e as Error).message)
        return
      }
    }
    polling.value = false
    ElMessage.warning('判定超时，请稍后刷新查看')
  }

  async function submit(challengeSlug: string, answer: string) {
    if (!answer || submitting.value || polling.value) return
    submitting.value = true
    result.value = null
    try {
      const sub = await submissionApi.submit(challengeSlug, answer)
      submitting.value = false
      await poll(sub.id)
    } catch (e) {
      submitting.value = false
      ElMessage.error(`提交失败：${(e as Error).message}`)
    }
  }

  return { submitting, polling, result, submit }
}