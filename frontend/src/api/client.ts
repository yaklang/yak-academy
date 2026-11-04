// 统一错误信封（api-contract skill：{"error":{"code","message"}}）
export interface ApiErrorEnvelope {
  error: { code: string; message: string }
}

export class ApiError extends Error {
  code: string
  status: number
  // 错误信封外的附加数据（如 409 单容器冲突的 current：当前活跃实例）。
  data: Record<string, unknown> | null
  constructor(status: number, code: string, message: string, data: Record<string, unknown> | null = null, cause?: unknown) {
    super(message, { cause })
    this.code = code
    this.status = status
    this.data = data
  }
}

const BASE = '/api/v1'

// 非 JSON 响应（旧后端未知路由的 text/plain 404、反向代理错误页等）统一转成
// 中文可读错误：直接透出 JSON.parse 的 SyntaxError 既不可读，也丢掉了 HTTP
// 状态这一最有用的排查线索。
function nonJSONError(resp: Response, text: string): ApiError {
  const snippet = text.trim().slice(0, 80)
  if (resp.status === 404) {
    return new ApiError(resp.status, 'NOT_FOUND',
      '接口不存在（HTTP 404）：当前后端版本可能低于前端，请更新并重启后端后重试')
  }
  if (resp.status >= 500) {
    return new ApiError(resp.status, 'INTERNAL',
      `服务器返回了无法解析的响应（HTTP ${resp.status}），请稍后重试；若持续出现请联系管理员`)
  }
  return new ApiError(resp.status, 'UNKNOWN',
    `服务器响应格式异常（HTTP ${resp.status}）${snippet ? `：${snippet}` : ''}`)
}

// 缺错误信封时的中文兜底（后端错误响应始终带信封；仅反代/网关异常会走到这里）。
function statusFallback(resp: Response): string {
  if (resp.status === 401) return '登录已过期，请重新登录'
  if (resp.status === 403) return '没有权限执行此操作'
  return `请求失败（HTTP ${resp.status}）`
}

// 网络层失败（后端未启动、连接被重置、DNS 失败）同样不能透出
// TypeError: Failed to fetch —— 对学员没有可操作信息。
function networkError(cause: unknown): ApiError {
  return new ApiError(0, 'NETWORK',
    '无法连接服务器：请确认后端已启动或网络可用，然后重试', null, cause)
}

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
  let resp: Response
  try {
    resp = await fetch(`${BASE}${path}`, { method, headers, body: payload })
  } catch (e) {
    throw networkError(e)
  }
  if (resp.status === 204) return undefined as T
  const text = await resp.text()
  // 仅按 JSON 解析：text/plain 或 HTML 错误页不能假设可解析。
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      throw nonJSONError(resp, text)
    }
  }
  if (!resp.ok) {
    const env = data as ApiErrorEnvelope | null
    // 信封外字段（current 等）原样透传，供调用方按 code 读取。
    const extra: Record<string, unknown> = {}
    if (data && typeof data === 'object' && !Array.isArray(data)) {
      for (const [k, v] of Object.entries(data as Record<string, unknown>)) {
        if (k !== 'error') extra[k] = v
      }
    }
    throw new ApiError(resp.status, env?.error?.code ?? 'UNKNOWN',
      env?.error?.message || statusFallback(resp),
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
  let resp: Response
  try {
    resp = await fetch(`${BASE}${path}`, { headers })
  } catch (e) {
    throw networkError(e)
  }
  if (!resp.ok) {
    // 二进制端点的错误响应仍应为 JSON 信封；若是反代 HTML/纯文本，
    // 复用可读中文兜底，避免把 statusText 英文丢给用户。
    const text = await resp.text()
    let env: ApiErrorEnvelope | null = null
    if (text) {
      try {
        env = JSON.parse(text) as ApiErrorEnvelope
      } catch {
        throw nonJSONError(resp, text)
      }
    }
    throw new ApiError(resp.status, env?.error?.code ?? 'UNKNOWN',
      env?.error?.message || statusFallback(resp))
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