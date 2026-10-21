import axios, { type AxiosRequestConfig } from 'axios'
import { API_BASE_URL } from '@/config'

/** 统一响应包（AGENTS.md §5） */
export interface ApiEnvelope<T> {
  code: string
  message: string
  data: T
  traceId: string
}

export class ApiError extends Error {
  readonly code: string
  readonly httpStatus: number
  readonly traceId: string

  constructor(code: string, message: string, httpStatus: number, traceId = '') {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.httpStatus = httpStatus
    this.traceId = traceId
  }
}

/** 会话失效（401 UNAUTHENTICATED）由全局监听统一跳登录 */
export const AUTH_EXPIRED_EVENT = 'portal:auth-expired'
/** 403 FORBIDDEN 统一跳 /403 */
export const FORBIDDEN_EVENT = 'portal:forbidden'

/** 前端自定义文案，其余错误直接使用后端 message（已是中文） */
const CUSTOM_MESSAGE: Record<string, string> = {
  RATE_LIMITED: '操作过于频繁，请稍后再试',
  INTERNAL_ERROR: '服务异常，请稍后重试',
  DEPENDENCY_UNAVAILABLE: '服务暂不可用，请稍后重试',
  NETWORK_ERROR: '网络异常，请重试',
  NETWORK_TIMEOUT: '请求超时，请重试',
}

const http = axios.create({
  // 由 VITE_API_BASE_URL 决定（同源默认 /api/v1；跨域部署可指向独立 API 域名）
  baseURL: API_BASE_URL,
  withCredentials: true,
  timeout: 15000,
  // 业务语义由 code 判断，HTTP 状态码只用于辅助
  validateStatus: () => true,
})

export interface RequestOptions {
  /** silent=true 时不触发全局 401/403 跳转（用于会话探测） */
  silent?: boolean
}

function normalizeError(e: unknown): ApiError {
  const err = e as { code?: string; message?: string }
  if (err?.code === 'ECONNABORTED') return new ApiError('NETWORK_TIMEOUT', CUSTOM_MESSAGE.NETWORK_TIMEOUT!, 0)
  return new ApiError('NETWORK_ERROR', CUSTOM_MESSAGE.NETWORK_ERROR!, 0)
}

export async function request<T>(config: AxiosRequestConfig, options: RequestOptions = {}): Promise<T> {
  let res
  try {
    res = await http.request<ApiEnvelope<T>>(config)
  } catch (e) {
    throw normalizeError(e)
  }

  const body = res.data
  if (res.status >= 200 && res.status < 300 && body?.code === 'OK') {
    return body.data
  }

  const error = new ApiError(
    body?.code || 'INTERNAL_ERROR',
    body?.message || `请求失败（HTTP ${res.status}）`,
    res.status,
    body?.traceId || '',
  )

  if (!options.silent) {
    if (error.code === 'UNAUTHENTICATED') {
      window.dispatchEvent(new CustomEvent(AUTH_EXPIRED_EVENT))
    } else if (error.code === 'FORBIDDEN') {
      window.dispatchEvent(new CustomEvent(FORBIDDEN_EVENT))
    } else if (error.code === 'INTERNAL_ERROR') {
      // 便于按 traceId 查后端日志
      console.error('[api] 服务异常 traceId=', error.traceId)
    }
  }

  throw error
}

/** 面向用户的错误文案 */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    return CUSTOM_MESSAGE[e.code] ?? e.message ?? '操作失败，请重试'
  }
  return '操作失败，请重试'
}

export function errorCode(e: unknown): string {
  return e instanceof ApiError ? e.code : 'UNKNOWN'
}
