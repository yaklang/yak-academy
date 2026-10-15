// 统一错误信封（api-contract skill：{"error":{"code","message"}}）
export interface ApiErrorEnvelope {
  error: { code: string; message: string }
}

export class ApiError extends Error {
  code: string
  status: number
  // 错误信封外的附加数据（如 409 单容器冲突的 current：当前活跃实例）。
  data: Record<string, unknown> | null
  constructor(status: number, code: string, message: string, data: Record<string, unknown> | null = null) {
    super(message)
    this.status = status
    this.code = code
    this.data = data
  }
}

const BASE = '/api/v1'

// request 是前端唯一允许的 HTTP 出口（project-conventions skill：
// 组件内禁止直接 fetch/axios，必须经 src/api/）。
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
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
  const resp = await fetch(`${BASE}${path}`, { method, headers, body: payload })
  if (resp.status === 204) return undefined as T
  const text = await resp.text()
  const data = text ? JSON.parse(text) : null
  if (!resp.ok) {
    const env = data as ApiErrorEnvelope | null
    // 信封外字段（current 等）原样透传，供调用方按 code 读取。
    const extra: Record<string, unknown> = {}
    if (data && typeof data === 'object' && !Array.isArray(data)) {
      for (const [k, v] of Object.entries(data as Record<string, unknown>)) {
        if (k !== 'error') extra[k] = v
      }
    }
    throw new ApiError(resp.status, env?.error?.code ?? 'UNKNOWN', env?.error?.message ?? resp.statusText,
      Object.keys(extra).length ? extra : null)
  }
  return data as T
}

// downloadBlob 带 JWT 拉取二进制附件（附件 URL 不公开，鉴权走 Authorization header；
// 浏览器直接 <a href> 无法带头，故 fetch → blob → 本地保存）。
async function downloadBlob(path: string) {
  const headers: Record<string, string> = {}
  const token = localStorage.getItem('academy_token')
  if (token) headers['Authorization'] = `Bearer ${token}`
  const resp = await fetch(`${BASE}${path}`, { headers })
  if (!resp.ok) {
    let code = 'UNKNOWN'
    let message = resp.statusText
    try {
      const env = (await resp.json()) as ApiErrorEnvelope
      code = env?.error?.code ?? code
      message = env?.error?.message ?? message
    } catch { /* 二进制端点错误响应仍为 JSON 信封，解析失败保留状态码语义 */ }
    throw new ApiError(resp.status, code, message)
  }
  return resp.blob()
}

export const http = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  // body 可选：契约中个别 DELETE 携带请求体（如竞赛题目解绑的 slugs）。
  del: <T>(path: string, body?: unknown) => request<T>('DELETE', path, body),
  upload: <T>(path: string, form: FormData) => request<T>('POST', path, form),
  download: (path: string) => downloadBlob(path),
}