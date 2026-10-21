import { request, type RequestOptions } from './client'
import type {
  AppItem,
  BindEmailResult,
  BindPhoneResult,
  ChangePasswordPayload,
  FileRow,
  LoginPayload,
  LoginResult,
  Me,
  MfaBindStart,
  MfaBoundResult,
  MfaVerifyPayload,
  MfaVerifyResult,
  ResetPasswordPayload,
  SendCodePayload,
  SendCodeResult,
  SessionItem,
  UploadCredential,
  VerifyChannel,
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

/* ---------------- 005 多通道验证与账号安全 ---------------- */

export function sendCode(payload: SendCodePayload) {
  return request<SendCodeResult>({ url: '/auth/code', method: 'POST', data: payload })
}

export function resetPassword(payload: ResetPasswordPayload) {
  return request<Record<string, never>>({ url: '/auth/password/reset', method: 'POST', data: payload })
}

export function bindPhone(payload: { phone: string; code: string }) {
  return request<BindPhoneResult>({ url: '/me/phone', method: 'POST', data: payload })
}

export function bindEmail(payload: { email: string; code: string }) {
  return request<BindEmailResult>({ url: '/me/email', method: 'POST', data: payload })
}

export function resetPasswordWithCode(payload: { channel: VerifyChannel; code: string; newPassword: string }) {
  return request<Record<string, never>>({ url: '/me/password/reset', method: 'POST', data: payload })
}

export function startMfaBind() {
  return request<MfaBindStart>({ url: '/me/mfa/bind', method: 'POST' })
}

export function confirmMfaBind(payload: { mfaSetupTicket: string; code: string }) {
  return request<MfaBoundResult>({ url: '/me/mfa/bind/confirm', method: 'POST', data: payload })
}

export function unbindMfa(payload: { channel: VerifyChannel; code: string }) {
  return request<MfaBoundResult>({ url: '/me/mfa/unbind', method: 'POST', data: payload })
}

/* ---------------- 004 文件上传 ---------------- */

export function getUploadCredential(payload: { scene: string; format: string; size: number }) {
  return request<UploadCredential>({ url: '/files/upload-credential', method: 'POST', data: payload })
}

export function updateMyAvatar(payload: { key: string; format: string; size: number }) {
  return request<FileRow>({ url: '/me/avatar', method: 'POST', data: payload })
}

export function clearMyAvatar() {
  return request<null>({ url: '/me/avatar', method: 'DELETE' })
}
