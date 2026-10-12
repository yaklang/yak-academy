// 统一错误信封（api-contract skill：{"error":{"code","message"}}）
export interface ApiErrorEnvelope {
  error: { code: string; message: string }
}

export class ApiError extends Error {
  code: string
  status: number
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

const BASE = '/api/v1'

// request 是前端唯一允许的 HTTP 出口（project-conventions skill：
// 组件内禁止直接 fetch/axios，必须经 src/api/）。
async function request<T>(method: string, path: string, body?: unknown, isForm = false): Promise<T> {
  const headers: Record<string, string> = {}
  const token = localStorage.getItem('academy_token')
  if (token) headers['Authorization'] = `Bearer ${token}`
  let payload: BodyInit | undefined
  if (body instanceof FormData) {
    payload = body
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  void isForm
  const resp = await fetch(`${BASE}${path}`, { method, headers, body: payload })
  if (resp.status === 204) return undefined as T
  const text = await resp.text()
  const data = text ? JSON.parse(text) : null
  if (!resp.ok) {
    const env = data as ApiErrorEnvelope | null
    throw new ApiError(resp.status, env?.error?.code ?? 'UNKNOWN', env?.error?.message ?? resp.statusText)
  }
  return data as T
}

export const http = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
  upload: <T>(path: string, form: FormData) => request<T>('POST', path, form),
}