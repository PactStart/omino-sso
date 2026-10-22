package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// AppAuthService 负责应用内的成员、角色、权限与授权（specs/003-app-management-pages）。
// 只经 store 访问数据；api 层不得直接持有 store（宪法原则 III）。
type AppAuthService struct {
	st *store.Store
}

// NewAppAuthService 构造。
func NewAppAuthService(st *store.Store) *AppAuthService {
	return &AppAuthService{st: st}
}

// 业务错误。
var (
	ErrAppValidation              = errors.New("app auth validation failed")
	ErrAppRoleNotFound            = errors.New("app role not found")
	ErrAppPermissionNotFound      = errors.New("app permission not found")
	ErrAppMemberNotFound          = errors.New("app member not found")
	ErrAppDeptNotFound            = errors.New("app dept not found")
	ErrTooManyIDs                 = errors.New("too many ids")
	ErrAppRoleNameExists          = errors.New("app role name exists")
	ErrAppPermissionCodeExists    = errors.New("app permission code exists")
	ErrAppPermissionInvalidParent = errors.New("app permission invalid parent")
	ErrAppResourceStale           = errors.New("app resource stale")
	// ErrAppClientDisabled / ErrAppNotAdmin 只用于「为什么拒绝」的服务端日志，
	// 不映射为不同的 HTTP 响应（门户侧统一 403，避免泄露应用是否存在）。
	ErrAppClientDisabled = errors.New("app client disabled")
	ErrAppNotAdmin       = errors.New("app admin required")
)

// ErrAppPermissionHasChildren 携带子节点数量，便于 handler 生成可读提示。
type ErrAppPermissionHasChildren struct {
	Count int64
}

func (e *ErrAppPermissionHasChildren) Error() string {
	return fmt.Sprintf("app permission has %d children", e.Count)
}

// 上限（见 plan.md Performance Goals）。
const (
	maxMemberBatch       = 500
	maxSelectAllIDs      = 5000
	maxPermissionGrant   = 1000
	maxRoleMemberGrant   = 5000
	maxKeywordLen        = 64
	maxCandidatePageSize = 50
	maxPermissionDepth   = 100
)

var (
	roleNamePattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	permCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
)

// MemberFilter 成员/候选筛选。
type MemberFilter struct {
	Keyword  string
	DeptID   string
	Page     int
	PageSize int
}

// MemberItem 成员视图。字段顺序必须与 store.MemberRow 一致（两者直接转换）。
type MemberItem struct {
	UserID      string
	Username    string
	DisplayName string
	DeptID      string
	DeptName    string
	Status      int8
	JoinedAt    int64
	RoleCount   int64
	IsAdmin     bool
}

// CandidateItem 候选视图。
type CandidateItem struct {
	UserID      string
	Username    string
	DisplayName string
	DeptID      string
	DeptName    string
}

// RoleItem 角色视图。
type RoleItem struct {
	model.ClientRole
	PermissionCount int64
	MemberCount     int64
}

// PermissionItem 权限视图。
type PermissionItem struct {
	model.ClientPermission
	ChildCount int64
	RoleCount  int64
}

// PermissionInput 新建权限入参。
type PermissionInput struct {
	Code        string
	Name        string
	Description *string
	ParentID    *string
	Sort        int
}

// PermissionPatch 修改权限入参（指针=三态，缺失不变）。
type PermissionPatch struct {
	Code        *string
	Name        *string
	Description *string
	ParentID    *string
	SetParent   bool // 为 true 时 ParentID 三态生效（含置为根级）
	Sort        *int
	// UpdatedAt 为读取时的版本（毫秒），非空时做乐观版本校验（FR-034）。
	UpdatedAt *int64
}

// ClientDetail 应用详情 + 成员数。
func (s *AppAuthService) ClientDetail(ctx context.Context, clientID string) (*model.Client, int64, error) {
	c, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrClientNotFound
		}
		return nil, 0, err
	}
	n, err := s.st.CountClientMembers(ctx, clientID)
	if err != nil {
		return nil, 0, err
	}
	return c, n, nil
}

// ListMembers 分页查成员；pageSize 上限由 handler 保证 ≤200。
func (s *AppAuthService) ListMembers(ctx context.Context, clientID string, f MemberFilter) ([]MemberItem, int64, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, 0, err
	}
	if len(f.Keyword) > maxKeywordLen {
		return nil, 0, ErrAppValidation
	}
	rows, total, err := s.st.ListClientMembers(ctx, clientID, f.Keyword, f.DeptID, f.Page, f.PageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]MemberItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, MemberItem(r))
	}
	return out, total, nil
}

// AllMemberIDs 跨页全选返回成员 ID，上限 5000。
func (s *AppAuthService) AllMemberIDs(ctx context.Context, clientID string, f MemberFilter) ([]string, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	ids, err := s.st.ListAllMemberIDs(ctx, clientID, f.Keyword, f.DeptID, maxSelectAllIDs)
	if err != nil {
		if errors.Is(err, store.ErrTooManyRows) {
			return nil, ErrTooManyIDs
		}
		return nil, err
	}
	return ids, nil
}

// ListCandidates 候选用户（pageSize ≤50）。
func (s *AppAuthService) ListCandidates(ctx context.Context, clientID string, f MemberFilter) ([]CandidateItem, int64, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, 0, err
	}
	if len(f.Keyword) > maxKeywordLen {
		return nil, 0, ErrAppValidation
	}
	if f.PageSize > maxCandidatePageSize {
		f.PageSize = maxCandidatePageSize
	}
	rows, total, err := s.st.ListCandidates(ctx, clientID, f.Keyword, f.DeptID, f.Page, f.PageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]CandidateItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, CandidateItem(r))
	}
	return out, total, nil
}

// AllCandidateIDs 候选的跨页全选。
func (s *AppAuthService) AllCandidateIDs(ctx context.Context, clientID string, f MemberFilter) ([]string, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	ids, err := s.st.ListAllCandidateIDs(ctx, clientID, f.Keyword, f.DeptID, maxSelectAllIDs)
	if err != nil {
		if errors.Is(err, store.ErrTooManyRows) {
			return nil, ErrTooManyIDs
		}
		return nil, err
	}
	return ids, nil
}

// AddMembers 批量添加成员（幂等）。
func (s *AppAuthService) AddMembers(ctx context.Context, clientID string, userIDs []string, operatorID string) (added, skipped int, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return 0, 0, err
	}
	dedup := dedupStrings(userIDs)
	if len(dedup) == 0 || len(dedup) > maxMemberBatch {
		return 0, 0, ErrAppValidation
	}
	added, err = s.st.AddMembers(ctx, clientID, dedup, operatorID, nowMillis())
	if err != nil {
		return 0, 0, err
	}
	return added, len(dedup) - added, nil
}

// AddMembersByDept 按部门（含子部门）整批加入。
func (s *AppAuthService) AddMembersByDept(ctx context.Context, clientID, deptID string, includeSub bool) (added, existing, disabled int, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return 0, 0, 0, err
	}
	if _, err := s.st.GetOrganization(ctx, deptID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, 0, ErrAppDeptNotFound
		}
		return 0, 0, 0, err
	}
	return s.st.AddMembersByDept(ctx, clientID, deptID, includeSub, nowMillis())
}

// RemoveMembers 批量移除成员（同事务解除角色绑定）。
func (s *AppAuthService) RemoveMembers(ctx context.Context, clientID string, userIDs []string) (removed, bindings, notMember int, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return 0, 0, 0, err
	}
	dedup := dedupStrings(userIDs)
	if len(dedup) == 0 || len(dedup) > maxMemberBatch {
		return 0, 0, 0, ErrAppValidation
	}
	return s.st.RemoveMembers(ctx, clientID, dedup)
}

// ListRoles 角色列表。
func (s *AppAuthService) ListRoles(ctx context.Context, clientID string) ([]RoleItem, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	rows, err := s.st.ListClientRoles(ctx, clientID)
	if err != nil {
		return nil, err
	}
	out := make([]RoleItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, RoleItem(r))
	}
	return out, nil
}

// GetRole 角色详情（含两类绑定 ID）。
func (s *AppAuthService) GetRole(ctx context.Context, clientID, roleID string) (*RoleItem, []string, []string, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, nil, nil, err
	}
	r, err := s.st.GetClientRole(ctx, clientID, roleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrAppRoleNotFound
		}
		return nil, nil, nil, err
	}
	permIDs, memberIDs, err := s.st.RoleBindings(ctx, roleID)
	if err != nil {
		return nil, nil, nil, err
	}
	return &RoleItem{ClientRole: *r}, permIDs, memberIDs, nil
}

// CreateRole 新建角色；名称冲突返回 ErrAppRoleNameExists。
func (s *AppAuthService) CreateRole(ctx context.Context, clientID, name string, description *string) (*model.ClientRole, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if !roleNamePattern.MatchString(name) {
		return nil, ErrAppValidation
	}
	if description != nil && len(*description) > 100 {
		return nil, ErrAppValidation
	}
	now := nowMillis()
	r := &model.ClientRole{
		ID:          id.New(),
		ClientID:    clientID,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.st.CreateClientRole(ctx, r); err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			return nil, ErrAppRoleNameExists
		}
		return nil, err
	}
	return r, nil
}

// UpdateRole 改名/改描述；版本不符返回 ErrAppResourceStale。
func (s *AppAuthService) UpdateRole(ctx context.Context, clientID, roleID string, name, description *string, expected *int64) (*model.ClientRole, error) {
	if name != nil {
		v := strings.TrimSpace(*name)
		if !roleNamePattern.MatchString(v) {
			return nil, ErrAppValidation
		}
		name = &v
	}
	if description != nil && len(*description) > 100 {
		return nil, ErrAppValidation
	}
	if err := s.st.UpdateClientRole(ctx, clientID, roleID, name, description, expected, nowMillis()); err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, ErrAppRoleNotFound
		case errors.Is(err, store.ErrDuplicateKey):
			return nil, ErrAppRoleNameExists
		case errors.Is(err, store.ErrStaleResource):
			return nil, ErrAppResourceStale
		}
		return nil, err
	}
	return s.st.GetClientRole(ctx, clientID, roleID)
}

// DeleteRole 删除角色，返回被解除的绑定快照（供审计）。
func (s *AppAuthService) DeleteRole(ctx context.Context, clientID, roleID string) (*model.ClientRole, []string, []string, error) {
	role, err := s.st.GetClientRole(ctx, clientID, roleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrAppRoleNotFound
		}
		return nil, nil, nil, err
	}
	permIDs, memberIDs, err := s.st.DeleteClientRole(ctx, clientID, roleID)
	if err != nil {
		return nil, nil, nil, err
	}
	return role, permIDs, memberIDs, nil
}

// ListPermissions 权限扁平列表。
func (s *AppAuthService) ListPermissions(ctx context.Context, clientID string) ([]PermissionItem, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	rows, err := s.st.ListClientPermissions(ctx, clientID)
	if err != nil {
		return nil, err
	}
	out := make([]PermissionItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, PermissionItem(r))
	}
	return out, nil
}

// CreatePermission 新建权限（父级须属于同一应用）。
func (s *AppAuthService) CreatePermission(ctx context.Context, clientID string, in PermissionInput) (*model.ClientPermission, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	in.Code = strings.TrimSpace(in.Code)
	if !permCodePattern.MatchString(in.Code) {
		return nil, ErrAppValidation
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		return nil, ErrAppValidation
	}
	if in.Description != nil && len(*in.Description) > 255 {
		return nil, ErrAppValidation
	}
	if in.ParentID != nil {
		if err := s.ensureParent(ctx, clientID, *in.ParentID, ""); err != nil {
			return nil, err
		}
	}
	now := nowMillis()
	p := &model.ClientPermission{
		ID:          id.New(),
		ClientID:    clientID,
		ParentID:    in.ParentID,
		Code:        in.Code,
		Name:        in.Name,
		Description: in.Description,
		Sort:        in.Sort,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.st.CreateClientPermission(ctx, p); err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			return nil, ErrAppPermissionCodeExists
		}
		return nil, err
	}
	return p, nil
}

// UpdatePermission 修改权限；移动父级时做成环校验。
func (s *AppAuthService) UpdatePermission(ctx context.Context, clientID, permID string, patch PermissionPatch) (*model.ClientPermission, error) {
	if patch.Code != nil {
		v := strings.TrimSpace(*patch.Code)
		if !permCodePattern.MatchString(v) {
			return nil, ErrAppValidation
		}
		patch.Code = &v
	}
	if patch.Name != nil {
		v := strings.TrimSpace(*patch.Name)
		if v == "" || len(v) > 64 {
			return nil, ErrAppValidation
		}
		patch.Name = &v
	}
	if patch.Description != nil && len(*patch.Description) > 255 {
		return nil, ErrAppValidation
	}
	if patch.SetParent {
		if patch.ParentID != nil {
			if err := s.ensureParent(ctx, clientID, *patch.ParentID, permID); err != nil {
				return nil, err
			}
		}
	}
	updates := map[string]any{}
	if patch.Code != nil {
		updates["code"] = *patch.Code
	}
	if patch.Name != nil {
		updates["name"] = *patch.Name
	}
	if patch.Description != nil {
		updates["description"] = *patch.Description
	}
	if patch.Sort != nil {
		updates["sort"] = *patch.Sort
	}
	if patch.SetParent {
		if patch.ParentID == nil {
			updates["parent_id"] = nil
		} else {
			updates["parent_id"] = *patch.ParentID
		}
	}
	if err := s.st.UpdateClientPermission(ctx, clientID, permID, updates, patch.UpdatedAt, nowMillis()); err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, ErrAppPermissionNotFound
		case errors.Is(err, store.ErrDuplicateKey):
			return nil, ErrAppPermissionCodeExists
		case errors.Is(err, store.ErrStaleResource):
			return nil, ErrAppResourceStale
		}
		return nil, err
	}
	return s.st.GetClientPermission(ctx, clientID, permID)
}

// DeletePermission 删除权限；有子节点返回 ErrAppPermissionHasChildren。
func (s *AppAuthService) DeletePermission(ctx context.Context, clientID, permID string) (*model.ClientPermission, []string, error) {
	p, err := s.st.GetClientPermission(ctx, clientID, permID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAppPermissionNotFound
		}
		return nil, nil, err
	}
	n, err := s.st.CountPermissionChildren(ctx, permID)
	if err != nil {
		return nil, nil, err
	}
	if n > 0 {
		return nil, nil, &ErrAppPermissionHasChildren{Count: n}
	}
	roleIDs, err := s.st.DeleteClientPermission(ctx, clientID, permID)
	if err != nil {
		return nil, nil, err
	}
	return p, roleIDs, nil
}

// PutRolePermissions 覆盖式设置角色权限；版本不符返回 ErrAppResourceStale。
func (s *AppAuthService) PutRolePermissions(ctx context.Context, clientID, roleID string, ids []string, expected *int64) (final []string, added, removed int, err error) {
	dedup := dedupStrings(ids)
	if len(dedup) > maxPermissionGrant {
		return nil, 0, 0, ErrTooManyIDs
	}
	if len(dedup) > 0 {
		n, err := s.st.CountPermissionsInClient(ctx, clientID, dedup)
		if err != nil {
			return nil, 0, 0, err
		}
		if int(n) != len(dedup) {
			return nil, 0, 0, ErrAppValidation
		}
	}
	now := nowMillis()
	err = s.st.Transaction(ctx, func(ctx context.Context) error {
		if e := s.st.BumpRoleVersion(ctx, clientID, roleID, expected, now); e != nil {
			return e
		}
		var e error
		added, removed, e = s.st.ReplaceRolePermissions(ctx, clientID, roleID, dedup, now)
		return e
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, 0, 0, ErrAppRoleNotFound
		case errors.Is(err, store.ErrStaleResource):
			return nil, 0, 0, ErrAppResourceStale
		}
		return nil, 0, 0, err
	}
	return dedup, added, removed, nil
}

// PutRoleMembers 覆盖式设置角色成员；缺失的成员自动补入应用。
func (s *AppAuthService) PutRoleMembers(ctx context.Context, clientID, roleID string, userIDs []string, expected *int64) (final []string, added, removed, autoJoined int, err error) {
	dedup := dedupStrings(userIDs)
	if len(dedup) > maxRoleMemberGrant {
		return nil, 0, 0, 0, ErrTooManyIDs
	}
	now := nowMillis()
	err = s.st.Transaction(ctx, func(ctx context.Context) error {
		if e := s.st.BumpRoleVersion(ctx, clientID, roleID, expected, now); e != nil {
			return e
		}
		var e error
		added, removed, autoJoined, e = s.st.ReplaceRoleMembers(ctx, clientID, roleID, dedup, now)
		return e
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, 0, 0, 0, ErrAppRoleNotFound
		case errors.Is(err, store.ErrStaleResource):
			return nil, 0, 0, 0, ErrAppResourceStale
		}
		return nil, 0, 0, 0, err
	}
	return dedup, added, removed, autoJoined, nil
}

// AuthorizeAppAdmin 判定用户能否以「应用管理员」身份管理该应用（006）。
// 返回的哨兵只用于服务端日志：门户侧一律映射为 403，不区分失败原因（FR-010）。
func (s *AppAuthService) AuthorizeAppAdmin(ctx context.Context, clientID, userID string) error {
	if clientID == "" || userID == "" {
		return ErrAppMemberNotFound
	}
	c, m, err := s.st.GetClientMembership(ctx, clientID, userID)
	if err != nil {
		return err
	}
	switch {
	case c == nil:
		return ErrClientNotFound
	case c.Status != 1:
		return ErrAppClientDisabled
	case m == nil:
		return ErrAppMemberNotFound
	case !m.IsAdmin:
		return ErrAppNotAdmin
	}
	return nil
}

// SetMemberAdmin 设置/取消某成员的应用管理员标记（仅由 admin 端调用，FR-007）。
// 幂等：值未变化时返回 changed=false 且不写库；返回变更前的值供调用方写审计。
func (s *AppAuthService) SetMemberAdmin(ctx context.Context, clientID, userID string, isAdmin bool) (before bool, changed bool, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return false, false, err
	}
	_, m, err := s.st.GetClientMembership(ctx, clientID, userID)
	if err != nil {
		return false, false, err
	}
	if m == nil {
		return false, false, ErrAppMemberNotFound
	}
	if m.IsAdmin == isAdmin {
		return m.IsAdmin, false, nil
	}
	if _, err := s.st.SetMemberAdmin(ctx, clientID, userID, isAdmin); err != nil {
		return m.IsAdmin, false, err
	}
	return m.IsAdmin, true, nil
}

// Snapshot 返回用户在该应用内的角色名与权限编码（空集返回空切片，不返回 nil）。
func (s *AppAuthService) Snapshot(ctx context.Context, clientID, userID string) ([]string, []string, error) {
	return s.st.SnapshotAppContext(ctx, clientID, userID)
}

func (s *AppAuthService) ensureClient(ctx context.Context, clientID string) error {
	if _, err := s.st.GetClient(ctx, clientID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrClientNotFound
		}
		return err
	}
	return nil
}

// ensureParent 校验父级权限属于同一应用、不是自身，且不形成环。
func (s *AppAuthService) ensureParent(ctx context.Context, clientID, parentID, selfID string) error {
	cur := parentID
	for depth := 0; depth < maxPermissionDepth; depth++ {
		if cur == "" {
			return nil
		}
		if selfID != "" && cur == selfID {
			return ErrAppPermissionInvalidParent
		}
		p, err := s.st.GetClientPermission(ctx, clientID, cur)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAppPermissionInvalidParent
			}
			return err
		}
		if p.ParentID == nil {
			return nil
		}
		cur = *p.ParentID
	}
	return ErrAppPermissionInvalidParent
}

func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
