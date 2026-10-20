package store

import (
	"context"

	"github.com/omino/omino-sso/internal/store/model"
)

// LastAccessByClient 返回用户在各 client 上最近一次成功获取令牌的时间。
func (s *Store) LastAccessByClient(ctx context.Context, userID string) (map[string]int64, error) {
	type row struct {
		ClientID string
		LastAt   int64
	}
	var rows []row
	err := s.DB(ctx).Model(&model.AuditEvent{}).
		Select("client_id, MAX(created_at) AS last_at").
		Where("user_id = ? AND client_id IS NOT NULL AND result = ?", userID, "success").
		Group("client_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ClientID] = r.LastAt
	}
	return out, nil
}
