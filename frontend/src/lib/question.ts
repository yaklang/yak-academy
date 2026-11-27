// 基础题作答序列化：前端按题型收集草稿，提交时编码为后端受理的答案 JSON
// （single: "A" / multiple: ["A","C"] / truefalse: true / fill: "答案"）。
// 与后端 service/objective.go 的 normalizeObjectiveSubmission 保持同构。
import type { QuestionType } from '../api'

export interface QuestionDraft {
  single: string
  multiple: string[]
  truefalse: boolean | null
  fill: string
}

export function emptyQuestionDraft(): QuestionDraft {
  return { single: '', multiple: [], truefalse: null, fill: '' }
}

// 空值返回 null（提交按钮禁用）；多选去重并排序，与后端落库形态一致。
export function serializeQuestionAnswer(
  type: QuestionType,
  draft: QuestionDraft,
): string | null {
  switch (type) {
    case 'single': {
      const key = draft.single.trim()
      return key ? JSON.stringify(key) : null
    }
    case 'multiple': {
      const keys = [...new Set(draft.multiple.map((key) => key.trim()).filter(Boolean))].sort()
      return keys.length ? JSON.stringify(keys) : null
    }
    case 'truefalse':
      return draft.truefalse === null ? null : JSON.stringify(draft.truefalse)
    case 'fill':
      // 填空保持原始输入（是否 trim 由题目 match 策略在服务端决定），仅空白判空。
      return draft.fill.trim() ? JSON.stringify(draft.fill) : null
    default:
      return null
  }
}
