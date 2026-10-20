package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/store/model"
)

// UserFilter 用户列表筛选条件。
type UserFilter struct {
	Keyword  string
	DeptID   string
	Status   *int8
	Page     int
	PageSize int
}

// GetUserByID 按主键查询。
func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	if err := s.DB(ctx).Where("id = ?", id).Take(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByUsername 按用户名查询（大小写敏感，依赖列排序规则）。
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	if err := s.DB(ctx).Where("username = ?", username).Take(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser 插入用户。
func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	return s.DB(ctx).Create(u).Error
}

// UpdateUserFields 使用 map 更新，避免 GORM 忽略零值（规范 §8.3）。
func (s *Store) UpdateUserFields(ctx context.Context, id string, fields map[string]any) error {
	fields["updated_at"] = time.Now().UnixMilli()
	return s.DB(ctx).Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

// ListUsers 分页查询用户。
func (s *Store) ListUsers(ctx context.Context, f UserFilter) ([]model.User, int64, error) {
	q := s.DB(ctx).Model(&model.User{})
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	if f.DeptID != "" {
		q = q.Where("dept_id = ?", f.DeptID)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.User
	err := q.Order("created_at DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).
		Find(&list).Error
	return list, total, err
}

// CountUsers 用于判断是否需要初始化管理员。
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.User{}).Count(&n).Error
	return n, err
}

// IncrFailedAttempts 原子累加失败次数，达阈值时锁定。
func (s *Store) IncrFailedAttempts(ctx context.Context, id string, max int, lockUntil int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		if err := s.DB(ctx).Model(&model.User{}).Where("id = ?", id).
			UpdateColumn("failed_attempts", gorm.Expr("failed_attempts + 1")).Error; err != nil {
			return err
		}
		return s.DB(ctx).Model(&model.User{}).Where("id = ? AND failed_attempts >= ?", id, max).
			Updates(map[string]any{
				"locked_until":    lockUntil,
				"status":          model.UserStatusLocked,
				"failed_attempts": 0,
			}).Error
	})
}

// ResetFailedAttempts 登录成功后清零并解除锁定。
func (s *Store) ResetFailedAttempts(ctx context.Context, id string, now int64) error {
	return s.DB(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"failed_attempts": 0,
			"locked_until":    nil,
			"status":          model.UserStatusActive,
			"last_login_at":   now,
		}).Error
}
