package store

import (
	"context"
	"unicode/utf8"

	"gorm.io/gorm/clause"

	"github.com/omino/omino-sso/internal/store/model"
)

// ListOrganizations 返回全部组织（用于构建树）。
func (s *Store) ListOrganizations(ctx context.Context, tenantID string) ([]model.Organization, error) {
	q := s.DB(ctx).Model(&model.Organization{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var list []model.Organization
	err := q.Order("sort ASC, created_at ASC").Find(&list).Error
	return list, err
}

// GetOrganization 按主键查询。
func (s *Store) GetOrganization(ctx context.Context, id string) (*model.Organization, error) {
	var o model.Organization
	if err := s.DB(ctx).Where("id = ?", id).Take(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// CountUsersByDept 统计部门人数。
func (s *Store) CountUsersByDept(ctx context.Context) (map[string]int64, error) {
	type row struct {
		DeptID string
		N      int64
	}
	var rows []row
	err := s.DB(ctx).Model(&model.User{}).
		Select("dept_id, COUNT(*) AS n").Group("dept_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.DeptID] = r.N
	}
	return out, nil
}

// ---- 组织写操作的读原语 ----

// LockAllOrganizations 在事务内锁定并读取 organizations 全部行。
// 该表没有 tenant_id 索引（仅 idx_orgs_parent / idx_orgs_path），
// 因此 WHERE tenant_id = ? FOR UPDATE 会全表扫描并锁全表；
// 这里直接以 ORDER BY id 锁全表，实际效果是**组织写操作全局串行**。
// 调用方必须已处于 store.Transaction 中。
func (s *Store) LockAllOrganizations(ctx context.Context) ([]model.Organization, error) {
	var list []model.Organization
	err := s.DB(ctx).Model(&model.Organization{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("id ASC").
		Find(&list).Error
	return list, err
}

// LockOrganization 在事务内锁定单个组织行（账号归属写入侧使用，
// 与部门删除共用同一临界区，避免 dept_id 悬空）。
// 未找到返回 gorm.ErrRecordNotFound。
func (s *Store) LockOrganization(ctx context.Context, id string) (*model.Organization, error) {
	var o model.Organization
	err := s.DB(ctx).Model(&model.Organization{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Take(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrganizationByName 按"租户 + 父节点 + 名称"查找同级同名节点。
// name 必须用 BINARY 比较：organizations 的排序规则为 utf8mb4_0900_ai_ci
// （大小写不敏感），否则 Platform 与 platform 会被误判为同名。
// parentID 为 nil 表示根级，用 MySQL 的空值安全等值 <=>。
func (s *Store) GetOrganizationByName(ctx context.Context, tenantID string, parentID *string, name, excludeID string) (*model.Organization, error) {
	q := s.DB(ctx).Where("tenant_id = ? AND parent_id <=> ? AND BINARY name = ?", tenantID, parentID, name)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var o model.Organization
	if err := q.Take(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// HasChildOrganizations 判断是否存在直接子部门。
func (s *Store) HasChildOrganizations(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := s.DB(ctx).Model(&model.Organization{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountUsersByDeptID 统计直属该部门的账号数。
func (s *Store) CountUsersByDeptID(ctx context.Context, id string) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.User{}).Where("dept_id = ?", id).Count(&n).Error
	return n, err
}

// ListSubtree 返回以 pathPrefix 为前缀的整棵子树（含该节点自身）。
func (s *Store) ListSubtree(ctx context.Context, pathPrefix string) ([]model.Organization, error) {
	var list []model.Organization
	err := s.DB(ctx).Where("path LIKE ?", model.OrgPathPattern(pathPrefix)).
		Order("sort ASC, created_at ASC").
		Find(&list).Error
	return list, err
}

// CountUsersInSubtree 统计该部门及其全部子部门的账号数（单条聚合，不做 N+1）。
func (s *Store) CountUsersInSubtree(ctx context.Context, pathPrefix string) (int64, error) {
	var n int64
	err := s.DB(ctx).Table("users AS u").
		Joins("JOIN organizations AS o ON o.id = u.dept_id").
		Where("o.path LIKE ?", model.OrgPathPattern(pathPrefix)).
		Count(&n).Error
	return n, err
}

// ---- 组织写操作的写原语 ----

// CreateOrganization 插入组织节点。
func (s *Store) CreateOrganization(ctx context.Context, o *model.Organization) error {
	return s.DB(ctx).Create(o).Error
}

// UpdateOrganizationFields 局部更新组织节点（map 语义：只写提交的列）。
func (s *Store) UpdateOrganizationFields(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return s.DB(ctx).Model(&model.Organization{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateSubtreePaths 把整棵子树的物化路径从 oldPrefix 前缀平移到 newPrefix，
// 由单条 UPDATE 完成，不做逐行"读-改-写"。返回受影响行数（含自身）。
func (s *Store) UpdateSubtreePaths(ctx context.Context, oldPrefix, newPrefix string) (int64, error) {
	res := s.DB(ctx).Exec(
		"UPDATE organizations SET path = CONCAT(?, SUBSTRING(path, ?)) WHERE path LIKE ?",
		newPrefix, utf8.RuneCountInString(oldPrefix)+1, model.OrgPathPattern(oldPrefix),
	)
	return res.RowsAffected, res.Error
}

// DeleteOrganization 物理删除组织节点（前置条件由 service 校验）。
func (s *Store) DeleteOrganization(ctx context.Context, id string) error {
	return s.DB(ctx).Where("id = ?", id).Delete(&model.Organization{}).Error
}
