// 基础题作答序列化锁定测试：编码形状必须与后端
// service/objective.go normalizeObjectiveSubmission 一致。
import { describe, expect, it } from 'vitest'
import { emptyQuestionDraft, serializeQuestionAnswer } from './question'

describe('serializeQuestionAnswer', () => {
  it('单选：输出 JSON 字符串，空白视为未作答', () => {
    expect(serializeQuestionAnswer('single', { ...emptyQuestionDraft(), single: 'B' })).toBe('"B"')
    expect(serializeQuestionAnswer('single', { ...emptyQuestionDraft(), single: '  ' })).toBeNull()
  })

  it('多选：去重、排序后输出 JSON 数组', () => {
    expect(
      serializeQuestionAnswer('multiple', { ...emptyQuestionDraft(), multiple: ['C', 'A', 'C'] }),
    ).toBe('["A","C"]')
    expect(serializeQuestionAnswer('multiple', emptyQuestionDraft())).toBeNull()
  })

  it('判断：false 不是空值，null 才是未作答', () => {
    expect(serializeQuestionAnswer('truefalse', { ...emptyQuestionDraft(), truefalse: false })).toBe('false')
    expect(serializeQuestionAnswer('truefalse', { ...emptyQuestionDraft(), truefalse: true })).toBe('true')
    expect(serializeQuestionAnswer('truefalse', emptyQuestionDraft())).toBeNull()
  })

  it('填空：保留原始输入（trim 由题目匹配策略决定），仅空白判空', () => {
    expect(serializeQuestionAnswer('fill', { ...emptyQuestionDraft(), fill: ' Paris ' })).toBe('" Paris "')
    expect(serializeQuestionAnswer('fill', { ...emptyQuestionDraft(), fill: '   ' })).toBeNull()
  })
})
