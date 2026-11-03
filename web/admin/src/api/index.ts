import { request, type RequestOptions } from './client'
import type {
  AddMembersResult,
  AppCandidate,
  AppPermission,
  AppRole,
  AuditQuery,
  AuditRow,
  ClientDetail,
  ClientRow,
  CreateClientPayload,
  CreateOrgPayload,
  CreatePermissionPayload,
  CreateRolePayload,
  CreatedClient,
  CreateUserPayload,
  FileConfirmPayload,
  FileQuery,
  FileRow,
  FileUploadPayload,
  MemberAdminResult,
  MemberGrantResult,
  MemberQuery,
  Me,
  OrgDetail,
  OrgNode,
  Paged,
  PermissionExportDocument,
  PermissionGrantResult,
  PermissionImportDocument,
  PermissionImportPreview,
  PermissionImportResult,
  AddOrgGrantsPayload,
  AddOrgGrantsResult,
  MemberPage,
  OrgGrantList,
  RemoveOrgGrantsResult,
  RemoveMembersResult,
  RoleDetail,
  RotatedSecret,
  SendRecordPage,
  SendRecordQuery,
  UpdateClientPayload,
  UpdateOrgPayload,
  UpdatePermissionPayload,
  UpdateRolePayload,
  UpdateUserPayload,
  UploadCredential,
  UserIdSet,
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

export function createOrg(payload: CreateOrgPayload) {
  return request<OrgDetail>({ url: '/admin/orgs', method: 'POST', data: payload })
}

export function updateOrg(id: string, payload: UpdateOrgPayload) {
  return request<OrgDetail>({
    url: `/admin/orgs/${encodeURIComponent(id)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function deleteOrg(id: string) {
  return request<null>({ url: `/admin/orgs/${encodeURIComponent(id)}`, method: 'DELETE' })
}

export interface ClientListQuery {
  keyword?: string
  page?: number
  pageSize?: number
  /** false = 仅看无成员应用（FR-032） */
  hasMembers?: 'true' | 'false'
}

export function listClients(query: ClientListQuery) {
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

/* ---------------- 003 应用子页面 ---------------- */

function clientBase(clientId: string): string {
  return `/admin/clients/${encodeURIComponent(clientId)}`
}

/* 详情 */

export function getClient(clientId: string) {
  return request<ClientDetail>({ url: clientBase(clientId), method: 'GET' })
}

export function updateClient(clientId: string, payload: UpdateClientPayload) {
  return request<ClientDetail>({ url: clientBase(clientId), method: 'PATCH', data: payload })
}

/* 成员 */

export function listAppMembers(clientId: string, query: MemberQuery = {}) {
  return request<MemberPage>({
    url: `${clientBase(clientId)}/members`,
    method: 'GET',
    params: query,
  })
}

/** 当前筛选下的全部成员 ID（跨页全选用，服务端上限 5000） */
export function listAppMemberIds(clientId: string, query: MemberQuery = {}) {
  return request<UserIdSet>({
    url: `${clientBase(clientId)}/members`,
    method: 'GET',
    params: { ...query, all: 'true' },
  })
}

export function listMemberCandidates(clientId: string, query: MemberQuery = {}) {
  return request<Paged<AppCandidate>>({
    url: `${clientBase(clientId)}/members/candidates`,
    method: 'GET',
    params: query,
  })
}

export function listCandidateIds(clientId: string, query: MemberQuery = {}) {
  return request<UserIdSet>({
    url: `${clientBase(clientId)}/members/candidates`,
    method: 'GET',
    params: { ...query, all: 'true' },
  })
}

export function addAppMembers(clientId: string, payload: { userIds: string[] }) {
  return request<AddMembersResult>({
    url: `${clientBase(clientId)}/members`,
    method: 'POST',
    data: payload,
  })
}

/** 按部门整批加入（含子部门），返回 新增/已存在/已停用 三类计数 */
/**
 * 组织授权（010）：把门户可见性授予部门（可含子部门）。
 * 说明：原「按部门加入成员」（一次性快照）入口已由本能力取代（spec FR-027），
 * 后端接口仍保留兼容期，前端不再调用。
 */
export function listOrgGrants(clientId: string) {
  return request<OrgGrantList>({
    url: `${clientBase(clientId)}/org-grants`,
    method: 'GET',
  })
}

export function addOrgGrants(clientId: string, payload: AddOrgGrantsPayload) {
  return request<AddOrgGrantsResult>({
    url: `${clientBase(clientId)}/org-grants`,
    method: 'POST',
    data: payload,
  })
}

export function removeOrgGrants(clientId: string, orgIds: string[]) {
  return request<RemoveOrgGrantsResult>({
    url: `${clientBase(clientId)}/org-grants/remove`,
    method: 'POST',
    data: { orgIds },
  })
}

export function removeAppMembers(clientId: string, userIds: string[]) {
  return request<RemoveMembersResult>({
    url: `${clientBase(clientId)}/members/remove`,
    method: 'POST',
    data: { userIds },
  })
}

/**
 * 设置/取消应用管理员（006）。幂等：提交与当前相同的值仍返回 200。
 * 授予与取消管理员的入口只在 admin 端，门户不提供。
 */
export function setAppMemberAdmin(clientId: string, userId: string, isAdmin: boolean) {
  return request<MemberAdminResult>({
    url: `${clientBase(clientId)}/members/${encodeURIComponent(userId)}/admin`,
    method: 'PUT',
    data: { isAdmin },
  })
}

/* 角色 */

export function listAppRoles(clientId: string) {
  return request<Paged<AppRole>>({ url: `${clientBase(clientId)}/roles`, method: 'GET' })
}

export function createAppRole(clientId: string, payload: CreateRolePayload) {
  return request<AppRole>({ url: `${clientBase(clientId)}/roles`, method: 'POST', data: payload })
}

export function getAppRole(clientId: string, roleId: string) {
  return request<RoleDetail>({
    url: `${clientBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'GET',
  })
}

export function updateAppRole(clientId: string, roleId: string, payload: UpdateRolePayload) {
  return request<RoleDetail>({
    url: `${clientBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function deleteAppRole(clientId: string, roleId: string) {
  return request<null>({
    url: `${clientBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'DELETE',
  })
}

/* 权限 */

export function listAppPermissions(clientId: string) {
  return request<Paged<AppPermission>>({
    url: `${clientBase(clientId)}/permissions`,
    method: 'GET',
  })
}

export function createAppPermission(clientId: string, payload: CreatePermissionPayload) {
  return request<AppPermission>({
    url: `${clientBase(clientId)}/permissions`,
    method: 'POST',
    data: payload,
  })
}

export function updateAppPermission(
  clientId: string,
  permissionId: string,
  payload: UpdatePermissionPayload,
) {
  return request<AppPermission>({
    url: `${clientBase(clientId)}/permissions/${encodeURIComponent(permissionId)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function deleteAppPermission(clientId: string, permissionId: string) {
  return request<null>({
    url: `${clientBase(clientId)}/permissions/${encodeURIComponent(permissionId)}`,
    method: 'DELETE',
  })
}

/* ---------- 011：权限点批量导入 / 导出 ---------- */

/** source 仅用于审计统计（paste|file），服务端把非法值一律按 paste 处理。 */
export type PermissionImportSource = 'paste' | 'file'

export function validatePermissionImport(
  clientId: string,
  doc: PermissionImportDocument,
  source: PermissionImportSource,
) {
  return request<PermissionImportPreview>({
    url: `${clientBase(clientId)}/permissions/import/validate`,
    method: 'POST',
    params: { source },
    data: doc,
  })
}

export function confirmPermissionImport(
  clientId: string,
  doc: PermissionImportDocument,
  source: PermissionImportSource,
) {
  return request<PermissionImportResult>({
    url: `${clientBase(clientId)}/permissions/import`,
    method: 'POST',
    params: { source },
    data: doc,
  })
}

export function exportPermissions(clientId: string) {
  return request<PermissionExportDocument>({
    url: `${clientBase(clientId)}/permissions/export`,
    method: 'GET',
  })
}

/* 授权 */

export function putRolePermissions(
  clientId: string,
  roleId: string,
  payload: { permissionIds: string[]; updatedAt?: number },
) {
  return request<PermissionGrantResult>({
    url: `${clientBase(clientId)}/roles/${encodeURIComponent(roleId)}/permissions`,
    method: 'PUT',
    data: payload,
  })
}

export function putRoleMembers(
  clientId: string,
  roleId: string,
  payload: { userIds: string[]; updatedAt?: number },
) {
  return request<MemberGrantResult>({
    url: `${clientBase(clientId)}/roles/${encodeURIComponent(roleId)}/members`,
    method: 'PUT',
    data: payload,
  })
}

/* ---------------- 004 文件上传 ---------------- */

export function getUploadCredential(payload: { scene: string; format: string; size: number }) {
  return request<UploadCredential>({ url: '/files/upload-credential', method: 'POST', data: payload })
}

/** 通用上传确认；targetId 为空时仅落文件记录（如注册应用前上传 logo） */
export function confirmUpload(payload: FileConfirmPayload) {
  return request<FileRow>({ url: '/files/confirm', method: 'POST', data: payload })
}

export function setClientLogo(clientId: string, payload: FileUploadPayload) {
  return request<FileRow>({
    url: `/admin/clients/${encodeURIComponent(clientId)}/logo`,
    method: 'POST',
    data: payload,
  })
}

export function setUserAvatar(userId: string, payload: FileUploadPayload) {
  return request<FileRow>({
    url: `/admin/users/${encodeURIComponent(userId)}/avatar`,
    method: 'POST',
    data: payload,
  })
}

export function clearClientLogo(clientId: string) {
  return request<null>({
    url: `/admin/clients/${encodeURIComponent(clientId)}/logo`,
    method: 'DELETE',
  })
}

export function clearUserAvatar(userId: string) {
  return request<null>({
    url: `/admin/users/${encodeURIComponent(userId)}/avatar`,
    method: 'DELETE',
  })
}

export function listFiles(query: FileQuery) {
  return request<Paged<FileRow>>({ url: '/admin/files', method: 'GET', params: query })
}

export function updateFileStatus(id: string, status: 'active' | 'disabled' | 'deleted') {
  return request<FileRow>({
    url: `/admin/files/${encodeURIComponent(id)}`,
    method: 'PATCH',
    data: { status },
  })
}

/* ---------------- 005 发送记录 ---------------- */

export function listSendRecords(query: SendRecordQuery) {
  return request<SendRecordPage>({ url: '/admin/send-records', method: 'GET', params: query })
}
