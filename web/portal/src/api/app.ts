/**
 * 门户应用管理接口（006-app-admin-role）。
 *
 * 全部走 `/api/v1/me/apps/:clientId/*`，由服务端 middleware.AppAdmin 按
 * 「该应用的应用管理员」判定；平台 admin 角色不自动放行（spec A2）。
 * 拒绝一律 403 FORBIDDEN，本层不做二次权限判断。
 *
 * 与 admin 端 `api/index.ts` 的对应函数保持同构（同一后端 handler），
 * 仅 baseURL 与组织树来源不同（门户走只读的 /me/orgs/tree）。
 */
import { request } from './client'
import type {
  AddMembersResult,
  AppCandidate,
  AppDetail,
  AppManageContext,
  AppMember,
  AppPermission,
  AppRole,
  CreatePermissionPayload,
  CreateRolePayload,
  FileRow,
  MemberGrantResult,
  MemberQuery,
  OrgNode,
  Paged,
  PermissionGrantResult,
  RemoveMembersResult,
  RoleDetail,
  UpdateAppBasicInfoPayload,
  UpdatePermissionPayload,
  UpdateRolePayload,
  UserIdSet,
} from './types'

function appBase(clientId: string): string {
  return `/me/apps/${encodeURIComponent(clientId)}`
}

/** 管理页摘要：应用名称 / 图标 / 描述 / 状态 / 成员数 */
export function getManageContext(clientId: string) {
  return request<AppManageContext>({ url: appBase(clientId), method: 'GET' })
}

/** 应用详情：门户只读展示 + 四个可编辑字段的回填来源 */
export function getAppDetail(clientId: string) {
  return request<AppDetail>({ url: `${appBase(clientId)}/detail`, method: 'GET' })
}

/** 更新应用基础信息（仅名称 / 描述 / Logo / 回调地址，其余字段服务端忽略） */
export function updateAppBasicInfo(clientId: string, payload: UpdateAppBasicInfoPayload) {
  return request<AppDetail>({ url: appBase(clientId), method: 'PATCH', data: payload })
}

/** 绑定已直传的 Logo（scene=app_logo） */
export function setAppLogo(clientId: string, payload: { key: string; format: string; size: number }) {
  return request<FileRow>({ url: `${appBase(clientId)}/logo`, method: 'POST', data: payload })
}

/** 清除应用 Logo（幂等） */
export function clearAppLogo(clientId: string) {
  return request<null>({ url: `${appBase(clientId)}/logo`, method: 'DELETE' })
}

/** 只读组织树（部门筛选与「按部门加入」） */
export function listOrgTree() {
  return request<OrgNode[]>({ url: '/me/orgs/tree', method: 'GET' })
}

/* ---------------- 成员 ---------------- */

export function listAppMembers(clientId: string, query: MemberQuery = {}) {
  return request<Paged<AppMember>>({
    url: `${appBase(clientId)}/members`,
    method: 'GET',
    params: query,
  })
}

/** 当前筛选下的全部成员 ID（跨页全选用，服务端上限 5000） */
export function listAppMemberIds(clientId: string, query: MemberQuery = {}) {
  return request<UserIdSet>({
    url: `${appBase(clientId)}/members`,
    method: 'GET',
    params: { ...query, all: 'true' },
  })
}

export function listMemberCandidates(clientId: string, query: MemberQuery = {}) {
  return request<Paged<AppCandidate>>({
    url: `${appBase(clientId)}/members/candidates`,
    method: 'GET',
    params: query,
  })
}

export function listCandidateIds(clientId: string, query: MemberQuery = {}) {
  return request<UserIdSet>({
    url: `${appBase(clientId)}/members/candidates`,
    method: 'GET',
    params: { ...query, all: 'true' },
  })
}

export function addAppMembers(clientId: string, payload: { userIds: string[] }) {
  return request<AddMembersResult>({
    url: `${appBase(clientId)}/members`,
    method: 'POST',
    data: payload,
  })
}

/** 按部门整批加入（含子部门），返回 新增/已存在/已停用 三类计数 */
export function addAppMembersByDept(
  clientId: string,
  payload: { deptId: string; includeSubDepts: boolean },
) {
  return request<AddMembersResult>({
    url: `${appBase(clientId)}/members`,
    method: 'POST',
    data: payload,
  })
}

export function removeAppMembers(clientId: string, userIds: string[]) {
  return request<RemoveMembersResult>({
    url: `${appBase(clientId)}/members/remove`,
    method: 'POST',
    data: { userIds },
  })
}

/* ---------------- 角色 ---------------- */

export function listAppRoles(clientId: string) {
  return request<Paged<AppRole>>({ url: `${appBase(clientId)}/roles`, method: 'GET' })
}

export function createAppRole(clientId: string, payload: CreateRolePayload) {
  return request<AppRole>({ url: `${appBase(clientId)}/roles`, method: 'POST', data: payload })
}

export function getAppRole(clientId: string, roleId: string) {
  return request<RoleDetail>({
    url: `${appBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'GET',
  })
}

export function updateAppRole(clientId: string, roleId: string, payload: UpdateRolePayload) {
  return request<RoleDetail>({
    url: `${appBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function deleteAppRole(clientId: string, roleId: string) {
  return request<null>({
    url: `${appBase(clientId)}/roles/${encodeURIComponent(roleId)}`,
    method: 'DELETE',
  })
}

/* ---------------- 权限 ---------------- */

export function listAppPermissions(clientId: string) {
  return request<Paged<AppPermission>>({
    url: `${appBase(clientId)}/permissions`,
    method: 'GET',
  })
}

export function createAppPermission(clientId: string, payload: CreatePermissionPayload) {
  return request<AppPermission>({
    url: `${appBase(clientId)}/permissions`,
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
    url: `${appBase(clientId)}/permissions/${encodeURIComponent(permissionId)}`,
    method: 'PATCH',
    data: payload,
  })
}

export function deleteAppPermission(clientId: string, permissionId: string) {
  return request<null>({
    url: `${appBase(clientId)}/permissions/${encodeURIComponent(permissionId)}`,
    method: 'DELETE',
  })
}

/* ---------------- 授权 ---------------- */

export function putRolePermissions(
  clientId: string,
  roleId: string,
  payload: { permissionIds: string[]; updatedAt?: number },
) {
  return request<PermissionGrantResult>({
    url: `${appBase(clientId)}/roles/${encodeURIComponent(roleId)}/permissions`,
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
    url: `${appBase(clientId)}/roles/${encodeURIComponent(roleId)}/members`,
    method: 'PUT',
    data: payload,
  })
}
