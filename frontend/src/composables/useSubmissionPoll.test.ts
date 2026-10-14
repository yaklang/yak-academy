// useSubmissionPoll 测试：结算模型的判定轮询分支——首次判对显示加分、
// 重复判对（score=0）不显示 +0 分、错误答案报错、轮询中防重复提交。
import { describe, expect, it, vi, beforeEach } from 'vitest'

// vi.mock 工厂被提升到文件顶部，外部变量不可见；spy 用 vi.hoisted 声明
// 才能在工厂内引用。用例通过导出的 spy 对象断言调用。
const elMessageSpy = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }))
const apiSpy = vi.hoisted(() => ({ submit: vi.fn(), get: vi.fn() }))

vi.mock('element-plus', () => ({
  ElMessage: elMessageSpy,
}))

vi.mock('../api', () => ({
  submissionApi: apiSpy,
}))

import { useSubmissionPoll } from './useSubmissionPoll'
import type { Submission } from '../api'

// 2s 轮询间隔：用假定时器推进，避免真实等待。
vi.useFakeTimers()

async function submitAndJudge(slug: string, answer: string, judged: Submission) {
  apiSpy.submit.mockResolvedValue({ id: 7, status: 'pending', score: 0 })
  apiSpy.get.mockResolvedValue(judged)
  const p = useSubmissionPoll()
  const done = p.submit(slug, answer)
  await vi.advanceTimersByTimeAsync(2100) // 首轮轮询后拿到判定结果
  await done
  return p
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useSubmissionPoll', () => {
  it('首次判对：成功文案含加分并回调 onCorrect', async () => {
    const onCorrect = vi.fn()
    const p = useSubmissionPoll(onCorrect)
    apiSpy.submit.mockResolvedValue({ id: 7, status: 'pending', score: 0 })
    apiSpy.get.mockResolvedValue({ id: 7, status: 'correct', score: 100 })
    const done = p.submit('xss-lab', 'flag{a}')
    await vi.advanceTimersByTimeAsync(2100)
    await done
    expect(elMessageSpy.success).toHaveBeenCalledWith('回答正确！+100 分')
    expect(onCorrect).toHaveBeenCalledWith(expect.objectContaining({ status: 'correct', score: 100 }))
    expect(p.polling.value).toBe(false)
    expect(p.result.value).toMatchObject({ status: 'correct', score: 100 })
  })

  it('重复判对（score=0）：不显示 +0 分，提示已结算', async () => {
    const p = await submitAndJudge('sqli-lab', 'flag{a}', { id: 7, status: 'correct', score: 0 })
    expect(elMessageSpy.success).toHaveBeenCalledWith('回答正确（本题已结算过，不重复计分）')
    expect(p.result.value).toMatchObject({ status: 'correct', score: 0 })
  })

  it('判错：错误文案，不回调 onCorrect', async () => {
    const onCorrect = vi.fn()
    const p = useSubmissionPoll(onCorrect)
    apiSpy.submit.mockResolvedValue({ id: 8, status: 'pending', score: 0 })
    apiSpy.get.mockResolvedValue({ id: 8, status: 'incorrect', score: 0 })
    const done = p.submit('sqli-lab', 'wrong')
    await vi.advanceTimersByTimeAsync(2100)
    await done
    expect(elMessageSpy.error).toHaveBeenCalledWith('回答错误，再试试')
    expect(onCorrect).not.toHaveBeenCalled()
    expect(p.result.value).toMatchObject({ status: 'incorrect', score: 0 })
  })

  it('空答案直接忽略，不发起提交', async () => {
    const p = useSubmissionPoll()
    await p.submit('xss-lab', '')
    expect(apiSpy.submit).not.toHaveBeenCalled()
  })

  it('提交请求失败：错误提示且 submitting 复位', async () => {
    apiSpy.submit.mockRejectedValue(new Error('boom'))
    const p = useSubmissionPoll()
    await p.submit('xss-lab', 'flag{a}')
    expect(elMessageSpy.error).toHaveBeenCalledWith('提交失败：boom')
    expect(p.submitting.value).toBe(false)
    expect(p.polling.value).toBe(false)
  })
})