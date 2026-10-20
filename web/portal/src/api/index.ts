import { request, type RequestOptions } from './client'
import type {
  AppItem,
  ChangePasswordPayload,
  LoginPayload,
  LoginResult,
  Me,
  MfaVerifyPayload,
  MfaVerifyResult,
  SessionItem,
} from './types'

export function login(payload: LoginPayload) {
  return request<LoginResult>({ url: '/auth/login', method: 'POST', data: payload })
}

export function verifyMfa(payload: MfaVerifyPayload) {
  return request<MfaVerifyResult>({ url: '/auth/mfa/verify', method: 'POST', data: payload })
}

export function logout() {
  return request<null>({ url: '/auth/logout', method: 'POST' })
}

export function getMe(options: RequestOptions = {}) {
  return request<Me>({ url: '/me', method: 'GET' }, options)
}

export function getMyApps() {
  return request<AppItem[]>({ url: '/me/apps', method: 'GET' })
}

export function getMySessions() {
  return request<SessionItem[]>({ url: '/me/sessions', method: 'GET' })
}

export function revokeSession(sid: string) {
  return request<null>({ url: `/me/sessions/${encodeURIComponent(sid)}`, method: 'DELETE' })
}

export function changePassword(payload: ChangePasswordPayload) {
  return request<null>({ url: '/me/password', method: 'POST', data: payload })
}
