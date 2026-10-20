package store

import (
	"context"

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
