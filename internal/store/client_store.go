package store

import (
	"context"

	"github.com/omino/omino-sso/internal/store/model"
)

// GetClient 按 client_id 查询（大小写敏感）。
func (s *Store) GetClient(ctx context.Context, clientID string) (*model.Client, error) {
	var c model.Client
	if err := s.DB(ctx).Where("client_id = ?", clientID).Take(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateClient 插入 client。
func (s *Store) CreateClient(ctx context.Context, c *model.Client) error {
	return s.DB(ctx).Create(c).Error
}

// ListClients 分页查询 client。
func (s *Store) ListClients(ctx context.Context, keyword string, page, pageSize int) ([]model.Client, int64, error) {
	q := s.DB(ctx).Model(&model.Client{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("client_id LIKE ? OR client_name LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Client
	err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// UpdateClientFields 在事务内校验版本并更新应用字段（expected 为 nil 时不校验）。
// 版本不一致返回 ErrStaleResource；应用不存在返回 gorm.ErrRecordNotFound。
func (s *Store) UpdateClientFields(ctx context.Context, clientID string, updates map[string]any, expected *int64, now int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		var cur model.Client
		if err := db.Clauses(lockUpdate()).Where("client_id = ?", clientID).Take(&cur).Error; err != nil {
			return err
		}
		if expected != nil && cur.UpdatedAt != *expected {
			return ErrStaleResource
		}
		updates["updated_at"] = now
		return db.Model(&model.Client{}).Where("client_id = ?", clientID).Updates(updates).Error
	})
}

// UpdateClientSecret 轮换密钥（只更新哈希）。
func (s *Store) UpdateClientSecret(ctx context.Context, clientID string, hash []byte, now int64) error {
	return s.DB(ctx).Model(&model.Client{}).Where("client_id = ?", clientID).
		Updates(map[string]any{"client_secret_hash": hash, "updated_at": now}).Error
}
