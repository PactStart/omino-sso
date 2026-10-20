import { request, type RequestOptions } from './client'
import type {
  AuditQuery,
  AuditRow,
  ClientRow,
  CreateClientPayload,
  CreatedClient,
  CreateUserPayload,
  Me,
  OrgNode,
  Paged,
  RotatedSecret,
  UpdateUserPayload,
  UserRow,
} from './types'

export function getMe(options: RequestOptions = {}) {
  return request<Me>({ url: '/me', method: 'GET' }, options)
}

export function logout() {
  return request<null>({ url: '/auth/logout', method: 'POST' })
}

export interface UserListQuery {
  keyword?: string
  deptId?: string
  status?: string
  page?: number
  pageSize?: number
}

export function listUsers(query: UserListQuery) {
  return request<Paged<UserRow>>({ url: '/admin/users', method: 'GET', params: query })
}

export function createUser(payload: CreateUserPayload) {
  return request<UserRow>({ url: '/admin/users', method: 'POST', data: payload })
}

export function updateUser(id: string, payload: UpdateUserPayload) {
  return request<UserRow>({
    url: `/admin/users/${encodeURIComponent(id)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function getOrgTree() {
  return request<OrgNode[]>({ url: '/admin/orgs/tree', method: 'GET' })
}

export function listClients(query: { keyword?: string; page?: number; pageSize?: number }) {
  return request<Paged<ClientRow>>({ url: '/admin/clients', method: 'GET', params: query })
}

export function createClient(payload: CreateClientPayload) {
  return request<CreatedClient>({ url: '/admin/clients', method: 'POST', data: payload })
}

export function rotateClientSecret(clientId: string) {
  return request<RotatedSecret>({
    url: `/admin/clients/${encodeURIComponent(clientId)}/secret`,
    method: 'POST',
    data: { gracePeriodSeconds: 0 },
  })
}

export function listAuditLogs(query: AuditQuery) {
  return request<Paged<AuditRow>>({ url: '/admin/audit-logs', method: 'GET', params: query })
}
