package store

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/omino/omino-sso/internal/store/model"
)

// 应用内成员 / 角色 / 权限 / 授权 与 access token 上下文映射的读写（003）。
// 约束见 specs/003-app-management-pages/data-model.md 与 AGENTS.md §6.9。

var (
	// ErrDuplicateKey 唯一键冲突（MySQL 1062），由 service 映射为业务错误码。
	ErrDuplicateKey = errors.New("duplicate key")
	// ErrStaleResource 乐观版本校验失败（updatedAt 与服务端不一致）。
	ErrStaleResource = errors.New("stale resource")
	// ErrTooManyRows 跨页全选结果超过上限。
	ErrTooManyRows = errors.New("too many rows")
)

// MemberRow 成员列表行。
type MemberRow struct {
	UserID      string
	Username    string
	DisplayName string
	DeptID      string
	DeptName    string
	Status      int8
	JoinedAt    int64
	RoleCount   int64
}

// CandidateRow 候选成员行。
type CandidateRow struct {
	UserID      string
	Username    string
	DisplayName string
	DeptID      string
	DeptName    string
}

// RoleRow 角色列表行（含聚合计数）。
type RoleRow struct {
	model.ClientRole
	PermissionCount int64
	MemberCount     int64
}

// PermissionRow 权限列表行（含聚合计数）。
type PermissionRow struct {
	model.ClientPermission
	ChildCount int64
	RoleCount  int64
}

// ClientWithCount 应用列表行（含成员数）。
type ClientWithCount struct {
	model.Client
	MemberCount int64
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return false
}

func lockUpdate() clause.Locking { return clause.Locking{Strength: "UPDATE"} }

// ---- 成员 ----

// CountClientMembers 统计应用成员数。
func (s *Store) CountClientMembers(ctx context.Context, clientID string) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.ClientMember{}).Where("client_id = ?", clientID).Count(&n).Error
	return n, err
}

func memberWhere(keyword, deptID string) string {
	var b strings.Builder
	if keyword != "" {
		b.WriteString(" AND (u.username LIKE CONCAT(?, '%') OR u.display_name LIKE CONCAT('%', ?, '%'))")
	}
	if deptID != "" {
		b.WriteString(" AND u.dept_id IN (SELECT o2.id FROM organizations o2 WHERE o2.id = ? OR o2.path LIKE CONCAT((SELECT o1.path FROM organizations o1 WHERE o1.id = ?), '%'))")
	}
	return b.String()
}

func keywordArgs(keyword, deptID string) []any {
	args := []any{}
	if keyword != "" {
		args = append(args, keyword, keyword)
	}
	if deptID != "" {
		args = append(args, deptID, deptID)
	}
	return args
}

// ListClientMembers 分页查询成员；keyword 对 username 前缀匹配、display_name 包含匹配。
func (s *Store) ListClientMembers(ctx context.Context, clientID, keyword, deptID string, page, pageSize int) ([]MemberRow, int64, error) {
	db := s.DB(ctx)
	countArgs := append([]any{clientID}, keywordArgs(keyword, deptID)...)
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM client_members m JOIN users u ON u.id = m.user_id WHERE m.client_id = ?`+memberWhere(keyword, deptID), countArgs...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	listArgs := append(countArgs, pageSize, (page-1)*pageSize)
	rows := []MemberRow{}
	err := db.Raw(`SELECT m.user_id, u.username, u.display_name, u.dept_id,
  COALESCE(o.name, '') AS dept_name, u.status, m.joined_at,
  (SELECT COUNT(*) FROM client_role_members rm WHERE rm.client_id = m.client_id AND rm.user_id = m.user_id) AS role_count
FROM client_members m
JOIN users u ON u.id = m.user_id
LEFT JOIN organizations o ON o.id = u.dept_id
WHERE m.client_id = ?`+memberWhere(keyword, deptID)+`
ORDER BY u.username ASC
LIMIT ? OFFSET ?`, listArgs...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListAllMemberIDs 跨页全选：返回当前筛选下的全部 userId，超过 limit 返回 ErrTooManyRows。
func (s *Store) ListAllMemberIDs(ctx context.Context, clientID, keyword, deptID string, limit int) ([]string, error) {
	args := append([]any{clientID}, keywordArgs(keyword, deptID)...)
	args = append(args, limit+1)
	var ids []string
	err := s.DB(ctx).Raw(`SELECT m.user_id FROM client_members m JOIN users u ON u.id = m.user_id
WHERE m.client_id = ?`+memberWhere(keyword, deptID)+` ORDER BY u.username ASC LIMIT ?`, args...).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		return nil, ErrTooManyRows
	}
	return ids, nil
}

// ListCandidates 候选用户：排除已是成员者与已停用账号；pageSize 由调用方限制 ≤50。
func (s *Store) ListCandidates(ctx context.Context, clientID, keyword, deptID string, page, pageSize int) ([]CandidateRow, int64, error) {
	db := s.DB(ctx)
	const base = ` FROM users u LEFT JOIN organizations o ON o.id = u.dept_id
WHERE u.status = 1 AND NOT EXISTS (SELECT 1 FROM client_members m2 WHERE m2.client_id = ? AND m2.user_id = u.id)` + ""
	where := ` AND 1 = 1` + memberWhere(keyword, deptID)
	args := append([]any{clientID}, keywordArgs(keyword, deptID)...)
	var total int64
	if err := db.Raw(`SELECT COUNT(*)`+base+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	listArgs := append(args, pageSize, (page-1)*pageSize)
	rows := []CandidateRow{}
	err := db.Raw(`SELECT u.id AS user_id, u.username, u.display_name, u.dept_id, COALESCE(o.name, '') AS dept_name`+
		base+where+` ORDER BY u.username ASC LIMIT ? OFFSET ?`, listArgs...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListAllCandidateIDs 候选的跨页全选。
func (s *Store) ListAllCandidateIDs(ctx context.Context, clientID, keyword, deptID string, limit int) ([]string, error) {
	args := append([]any{clientID}, keywordArgs(keyword, deptID)...)
	args = append(args, limit+1)
	var ids []string
	err := s.DB(ctx).Raw(`SELECT u.id FROM users u WHERE u.status = 1
AND NOT EXISTS (SELECT 1 FROM client_members m2 WHERE m2.client_id = ? AND m2.user_id = u.id)`+memberWhere(keyword, deptID)+
		` ORDER BY u.username ASC LIMIT ?`, args...).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		return nil, ErrTooManyRows
	}
	return ids, nil
}

// AddMembers 批量添加成员：跳过停用账号与已存在成员，返回新增数。
func (s *Store) AddMembers(ctx context.Context, clientID string, userIDs []string, addedBy string, now int64) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	var valid []string
	if err := s.DB(ctx).Model(&model.User{}).Where("id IN ? AND status = ?", userIDs, model.UserStatusActive).
		Pluck("id", &valid).Error; err != nil {
		return 0, err
	}
	if len(valid) == 0 {
		return 0, nil
	}
	rows := make([]model.ClientMember, 0, len(valid))
	for _, uid := range valid {
		rows = append(rows, model.ClientMember{ClientID: clientID, UserID: uid, AddedBy: &addedBy, JoinedAt: now})
	}
	res := s.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 500)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// AddMembersByDept 按部门（含子部门）整批加入，返回新增/已存在/已停用三类计数。
func (s *Store) AddMembersByDept(ctx context.Context, clientID, deptID string, includeSub bool, now int64) (added, existing, disabled int, err error) {
	db := s.DB(ctx)
	deptCond := `u.dept_id = ?`
	deptArgs := []any{deptID}
	if includeSub {
		deptCond = `u.dept_id IN (SELECT o2.id FROM organizations o2 WHERE o2.id = ? OR o2.path LIKE CONCAT((SELECT o1.path FROM organizations o1 WHERE o1.id = ?), '%'))`
		deptArgs = []any{deptID, deptID}
	}
	if err = db.Raw(`SELECT COUNT(*) FROM client_members m JOIN users u ON u.id = m.user_id
WHERE m.client_id = ? AND u.status = 1 AND `+deptCond, append([]any{clientID}, deptArgs...)...).Scan(&existing).Error; err != nil {
		return 0, 0, 0, err
	}
	if err = db.Raw(`SELECT COUNT(*) FROM users u WHERE u.status <> 1 AND `+deptCond, deptArgs...).Scan(&disabled).Error; err != nil {
		return 0, 0, 0, err
	}
	res := db.Exec(`INSERT IGNORE INTO client_members (client_id, user_id, added_by, joined_at)
SELECT ?, u.id, NULL, ? FROM users u WHERE u.status = 1 AND `+deptCond, append([]any{clientID, now}, deptArgs...)...)
	if res.Error != nil {
		return 0, 0, 0, res.Error
	}
	return int(res.RowsAffected), existing, disabled, nil
}

// RemoveMembers 批量移除成员，并在同一事务内解除其角色绑定。
func (s *Store) RemoveMembers(ctx context.Context, clientID string, userIDs []string) (removed, bindings, notMember int, err error) {
	if len(userIDs) == 0 {
		return 0, 0, 0, nil
	}
	var bindingsRemoved int64
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		if e := db.Model(&model.ClientRoleMember{}).Where("client_id = ? AND user_id IN ?", clientID, userIDs).
			Count(&bindingsRemoved).Error; e != nil {
			return e
		}
		if e := db.Where("client_id = ? AND user_id IN ?", clientID, userIDs).Delete(&model.ClientRoleMember{}).Error; e != nil {
			return e
		}
		res := db.Where("client_id = ? AND user_id IN ?", clientID, userIDs).Delete(&model.ClientMember{})
		if res.Error != nil {
			return res.Error
		}
		removed = int(res.RowsAffected)
		return nil
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return removed, int(bindingsRemoved), len(userIDs) - removed, nil
}

// IsClientMember 判断用户是否为该应用成员。
func (s *Store) IsClientMember(ctx context.Context, clientID, userID string) (bool, error) {
	var n int64
	err := s.DB(ctx).Model(&model.ClientMember{}).
		Where("client_id = ? AND user_id = ?", clientID, userID).Count(&n).Error
	return n > 0, err
}

// ListClientsForMember 返回用户以成员身份可访问的启用应用（FR-012）。
func (s *Store) ListClientsForMember(ctx context.Context, userID string) ([]model.Client, error) {
	var list []model.Client
	err := s.DB(ctx).Raw(`SELECT c.* FROM clients c
JOIN client_members m ON m.client_id = c.client_id
WHERE m.user_id = ? AND c.status = 1
ORDER BY c.created_at ASC`, userID).Scan(&list).Error
	return list, err
}

// ListClientsWithCount 应用列表（含成员数），hasMembers 为 nil 时不过滤。
func (s *Store) ListClientsWithCount(ctx context.Context, keyword string, hasMembers *bool, page, pageSize int) ([]ClientWithCount, int64, error) {
	db := s.DB(ctx)
	where := " WHERE 1 = 1"
	args := []any{}
	if keyword != "" {
		like := "%" + keyword + "%"
		where += " AND (c.client_id LIKE ? OR c.client_name LIKE ?)"
		args = append(args, like, like)
	}
	if hasMembers != nil {
		if *hasMembers {
			where += " AND EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = c.client_id)"
		} else {
			where += " AND NOT EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = c.client_id)"
		}
	}
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM clients c`+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []ClientWithCount{}
	err := db.Raw(`SELECT c.*, (SELECT COUNT(*) FROM client_members m WHERE m.client_id = c.client_id) AS member_count
FROM clients c`+where+` ORDER BY c.created_at ASC LIMIT ? OFFSET ?`,
		append(args, pageSize, (page-1)*pageSize)...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ---- 角色 ----

// ListClientRoles 角色列表（含权限数/成员数）。
func (s *Store) ListClientRoles(ctx context.Context, clientID string) ([]RoleRow, error) {
	rows := []RoleRow{}
	err := s.DB(ctx).Raw(`SELECT r.id, r.client_id, r.name, r.description, r.created_at, r.updated_at,
  (SELECT COUNT(*) FROM client_role_permissions rp WHERE rp.role_id = r.id) AS permission_count,
  (SELECT COUNT(*) FROM client_role_members rm WHERE rm.role_id = r.id) AS member_count
FROM client_roles r WHERE r.client_id = ? ORDER BY r.created_at ASC`, clientID).Scan(&rows).Error
	return rows, err
}

// GetClientRole 按 id 取角色（不存在返回 gorm.ErrRecordNotFound）。
func (s *Store) GetClientRole(ctx context.Context, clientID, roleID string) (*model.ClientRole, error) {
	var r model.ClientRole
	if err := s.DB(ctx).Where("client_id = ? AND id = ?", clientID, roleID).Take(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// RoleBindings 返回角色的 permissionIds 与 memberIds。
func (s *Store) RoleBindings(ctx context.Context, roleID string) (permIDs, memberIDs []string, err error) {
	db := s.DB(ctx)
	if err = db.Model(&model.ClientRolePermission{}).Where("role_id = ?", roleID).
		Order("permission_id").Pluck("permission_id", &permIDs).Error; err != nil {
		return nil, nil, err
	}
	if err = db.Model(&model.ClientRoleMember{}).Where("role_id = ?", roleID).
		Order("user_id").Pluck("user_id", &memberIDs).Error; err != nil {
		return nil, nil, err
	}
	if permIDs == nil {
		permIDs = []string{}
	}
	if memberIDs == nil {
		memberIDs = []string{}
	}
	return permIDs, memberIDs, nil
}

// CreateClientRole 插入角色；名称冲突返回 ErrDuplicateKey。
func (s *Store) CreateClientRole(ctx context.Context, r *model.ClientRole) error {
	if err := s.DB(ctx).Create(r).Error; err != nil {
		if isDuplicate(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

// UpdateClientRole 更新角色名/描述；expectedUpdatedAt 非空时做乐观版本校验。
func (s *Store) UpdateClientRole(ctx context.Context, clientID, roleID string, name, description *string, expected *int64, now int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var cur model.ClientRole
		if err := db.Clauses(lockUpdate()).Where("client_id = ? AND id = ?", clientID, roleID).Take(&cur).Error; err != nil {
			return err
		}
		if expected != nil && cur.UpdatedAt != *expected {
			return ErrStaleResource
		}
		updates := map[string]any{"updated_at": now}
		if name != nil {
			updates["name"] = *name
		}
		if description != nil {
			updates["description"] = *description
		}
		if err := db.Model(&model.ClientRole{}).Where("client_id = ? AND id = ?", clientID, roleID).Updates(updates).Error; err != nil {
			if isDuplicate(err) {
				return ErrDuplicateKey
			}
			return err
		}
		return nil
	})
}

// BumpRoleVersion 在事务内锁定角色、校验版本并刷新 updated_at。
// expected 为 nil 时不做校验（兼容脚本调用）；角色不存在返回 gorm.ErrRecordNotFound。
func (s *Store) BumpRoleVersion(ctx context.Context, clientID, roleID string, expected *int64, now int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var cur model.ClientRole
		if err := db.Clauses(lockUpdate()).Where("client_id = ? AND id = ?", clientID, roleID).Take(&cur).Error; err != nil {
			return err
		}
		if expected != nil && cur.UpdatedAt != *expected {
			return ErrStaleResource
		}
		return db.Model(&model.ClientRole{}).Where("client_id = ? AND id = ?", clientID, roleID).
			Update("updated_at", now).Error
	})
}

// DeleteClientRole 删除角色并返回被解除绑定的快照。
func (s *Store) DeleteClientRole(ctx context.Context, clientID, roleID string) (permIDs, memberIDs []string, err error) {
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		if e := db.Model(&model.ClientRolePermission{}).Where("role_id = ?", roleID).Pluck("permission_id", &permIDs).Error; e != nil {
			return e
		}
		if e := db.Model(&model.ClientRoleMember{}).Where("role_id = ?", roleID).Pluck("user_id", &memberIDs).Error; e != nil {
			return e
		}
		if e := db.Where("role_id = ?", roleID).Delete(&model.ClientRolePermission{}).Error; e != nil {
			return e
		}
		if e := db.Where("role_id = ?", roleID).Delete(&model.ClientRoleMember{}).Error; e != nil {
			return e
		}
		res := db.Where("client_id = ? AND id = ?", clientID, roleID).Delete(&model.ClientRole{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if permIDs == nil {
		permIDs = []string{}
	}
	if memberIDs == nil {
		memberIDs = []string{}
	}
	return permIDs, memberIDs, nil
}

// ---- 权限 ----

// ListClientPermissions 权限扁平列表（上限 1000，供前端组树）。
func (s *Store) ListClientPermissions(ctx context.Context, clientID string) ([]PermissionRow, error) {
	rows := []PermissionRow{}
	err := s.DB(ctx).Raw(`SELECT p.id, p.client_id, p.parent_id, p.code, p.name, p.description, p.sort, p.created_at, p.updated_at,
  (SELECT COUNT(*) FROM client_permissions c WHERE c.parent_id = p.id) AS child_count,
  (SELECT COUNT(*) FROM client_role_permissions rp WHERE rp.permission_id = p.id) AS role_count
FROM client_permissions p WHERE p.client_id = ? ORDER BY p.sort ASC, p.created_at ASC LIMIT 1000`, clientID).Scan(&rows).Error
	return rows, err
}

// GetClientPermission 按 id 取权限。
func (s *Store) GetClientPermission(ctx context.Context, clientID, permID string) (*model.ClientPermission, error) {
	var p model.ClientPermission
	if err := s.DB(ctx).Where("client_id = ? AND id = ?", clientID, permID).Take(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateClientPermission 插入权限；编码冲突返回 ErrDuplicateKey。
func (s *Store) CreateClientPermission(ctx context.Context, p *model.ClientPermission) error {
	if err := s.DB(ctx).Create(p).Error; err != nil {
		if isDuplicate(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

// UpdateClientPermission 更新权限字段；expectedUpdatedAt 非空时做版本校验。
func (s *Store) UpdateClientPermission(ctx context.Context, clientID, permID string, updates map[string]any, expected *int64, now int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var cur model.ClientPermission
		if err := db.Clauses(lockUpdate()).Where("client_id = ? AND id = ?", clientID, permID).Take(&cur).Error; err != nil {
			return err
		}
		if expected != nil && cur.UpdatedAt != *expected {
			return ErrStaleResource
		}
		updates["updated_at"] = now
		if err := db.Model(&model.ClientPermission{}).Where("client_id = ? AND id = ?", clientID, permID).Updates(updates).Error; err != nil {
			if isDuplicate(err) {
				return ErrDuplicateKey
			}
			return err
		}
		return nil
	})
}

// CountPermissionChildren 子节点数量（删除保护）。
func (s *Store) CountPermissionChildren(ctx context.Context, permID string) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.ClientPermission{}).Where("parent_id = ?", permID).Count(&n).Error
	return n, err
}

// DeleteClientPermission 删除权限并解除角色引用，返回被解除的角色 ID 快照。
func (s *Store) DeleteClientPermission(ctx context.Context, clientID, permID string) (roleIDs []string, err error) {
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		if e := db.Model(&model.ClientRolePermission{}).Where("permission_id = ?", permID).Pluck("role_id", &roleIDs).Error; e != nil {
			return e
		}
		if e := db.Where("permission_id = ?", permID).Delete(&model.ClientRolePermission{}).Error; e != nil {
			return e
		}
		res := db.Where("client_id = ? AND id = ?", clientID, permID).Delete(&model.ClientPermission{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return roleIDs, nil
}

// CountPermissionsInClient 校验一批权限 ID 是否都属于该应用。
func (s *Store) CountPermissionsInClient(ctx context.Context, clientID string, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var n int64
	err := s.DB(ctx).Model(&model.ClientPermission{}).Where("client_id = ? AND id IN ?", clientID, ids).Count(&n).Error
	return n, err
}

// ---- 授权（覆盖式） ----

// ReplaceRolePermissions 覆盖式设置角色权限，返回新增/移除数量。
func (s *Store) ReplaceRolePermissions(ctx context.Context, clientID, roleID string, ids []string, now int64) (added, removed int, err error) {
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var current []string
		if e := db.Model(&model.ClientRolePermission{}).Where("role_id = ?", roleID).Pluck("permission_id", &current).Error; e != nil {
			return e
		}
		want := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			want[id] = struct{}{}
		}
		have := make(map[string]struct{}, len(current))
		for _, id := range current {
			have[id] = struct{}{}
		}
		var toDelete, toInsert []string
		for id := range have {
			if _, ok := want[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		for id := range want {
			if _, ok := have[id]; !ok {
				toInsert = append(toInsert, id)
			}
		}
		if len(toDelete) > 0 {
			if e := db.Where("role_id = ? AND permission_id IN ?", roleID, toDelete).Delete(&model.ClientRolePermission{}).Error; e != nil {
				return e
			}
		}
		if len(toInsert) > 0 {
			rows := make([]model.ClientRolePermission, 0, len(toInsert))
			for _, id := range toInsert {
				rows = append(rows, model.ClientRolePermission{RoleID: roleID, PermissionID: id, ClientID: clientID, CreatedAt: now})
			}
			if e := db.CreateInBatches(&rows, 500).Error; e != nil {
				return e
			}
		}
		added, removed = len(toInsert), len(toDelete)
		return nil
	})
	return added, removed, err
}

// ReplaceRoleMembers 覆盖式设置角色成员；缺失的成员自动补入 client_members（INV-7）。
func (s *Store) ReplaceRoleMembers(ctx context.Context, clientID, roleID string, userIDs []string, now int64) (added, removed, autoJoined int, err error) {
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var current []string
		if e := db.Model(&model.ClientRoleMember{}).Where("role_id = ?", roleID).Pluck("user_id", &current).Error; e != nil {
			return e
		}
		want := make(map[string]struct{}, len(userIDs))
		for _, id := range userIDs {
			want[id] = struct{}{}
		}
		have := make(map[string]struct{}, len(current))
		for _, id := range current {
			have[id] = struct{}{}
		}
		var toDelete, toInsert []string
		for id := range have {
			if _, ok := want[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		for id := range want {
			if _, ok := have[id]; !ok {
				toInsert = append(toInsert, id)
			}
		}
		if len(toInsert) > 0 {
			members := make([]model.ClientMember, 0, len(toInsert))
			for _, id := range toInsert {
				members = append(members, model.ClientMember{ClientID: clientID, UserID: id, JoinedAt: now})
			}
			res := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&members, 500)
			if res.Error != nil {
				return res.Error
			}
			autoJoined = int(res.RowsAffected)
		}
		if len(toDelete) > 0 {
			if e := db.Where("role_id = ? AND user_id IN ?", roleID, toDelete).Delete(&model.ClientRoleMember{}).Error; e != nil {
				return e
			}
		}
		if len(toInsert) > 0 {
			rows := make([]model.ClientRoleMember, 0, len(toInsert))
			for _, id := range toInsert {
				rows = append(rows, model.ClientRoleMember{RoleID: roleID, UserID: id, ClientID: clientID, CreatedAt: now})
			}
			if e := db.CreateInBatches(&rows, 500).Error; e != nil {
				return e
			}
		}
		added, removed = len(toInsert), len(toDelete)
		return nil
	})
	return added, removed, autoJoined, err
}

// ---- userinfo 快照与令牌映射 ----

// SnapshotAppContext 返回用户在该应用内的角色名与权限编码（均已去重排序）。
func (s *Store) SnapshotAppContext(ctx context.Context, clientID, userID string) (roles, perms []string, err error) {
	db := s.DB(ctx)
	if err = db.Raw(`SELECT DISTINCT r.name AS name FROM client_roles r
JOIN client_role_members m ON m.role_id = r.id
WHERE m.client_id = ? AND m.user_id = ? ORDER BY r.name`, clientID, userID).Scan(&roles).Error; err != nil {
		return nil, nil, err
	}
	if err = db.Raw(`SELECT DISTINCT p.code AS code FROM client_permissions p
JOIN client_role_permissions rp ON rp.permission_id = p.id
JOIN client_role_members m ON m.role_id = rp.role_id
WHERE m.client_id = ? AND m.user_id = ? ORDER BY p.code`, clientID, userID).Scan(&perms).Error; err != nil {
		return nil, nil, err
	}
	if roles == nil {
		roles = []string{}
	}
	if perms == nil {
		perms = []string{}
	}
	return roles, perms, nil
}

// InsertAccessToken 写入令牌 → 应用映射（签发路径，失败即签发失败）。
func (s *Store) InsertAccessToken(ctx context.Context, tokenID, sub, clientID string, expiresAt, now int64) error {
	return s.DB(ctx).Create(&model.AccessToken{
		TokenID:   tokenID,
		Sub:       sub,
		ClientID:  clientID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}).Error
}

// CleanupExpiredAccessTokens 删除过期映射，单次最多 limit 行。
func (s *Store) CleanupExpiredAccessTokens(ctx context.Context, now int64, limit int) error {
	return s.DB(ctx).Exec("DELETE FROM access_tokens WHERE expires_at < ? LIMIT ?", now, limit).Error
}

// AccessTokenClientID 反查令牌所属应用；不存在返回 gorm.ErrRecordNotFound。
func (s *Store) AccessTokenClientID(ctx context.Context, tokenID string) (string, error) {
	var row struct {
		ClientID string `gorm:"column:client_id"`
	}
	if err := s.DB(ctx).Model(&model.AccessToken{}).Select("client_id").
		Where("token_id = ?", tokenID).Take(&row).Error; err != nil {
		return "", err
	}
	return row.ClientID, nil
}
