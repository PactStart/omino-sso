package store

import (
	"context"

	"github.com/omino/omino-sso/internal/store/model"
)

// AuditFilter 审计查询条件。
type AuditFilter struct {
	UserID    string
	ClientID  string
	EventType string
	Result    string
	From      int64
	To        int64
	Page      int
	PageSize  int
}

// InsertAuditEvent 写入审计事件。
func (s *Store) InsertAuditEvent(ctx context.Context, e *model.AuditEvent) error {
	return s.DB(ctx).Create(e).Error
}

// ListAuditEvents 分页查询审计。
func (s *Store) ListAuditEvents(ctx context.Context, f AuditFilter) ([]model.AuditEvent, int64, error) {
	q := s.DB(ctx).Model(&model.AuditEvent{})
	if f.UserID != "" {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.ClientID != "" {
		q = q.Where("client_id = ?", f.ClientID)
	}
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.Result != "" {
		q = q.Where("result = ?", f.Result)
	}
	if f.From > 0 {
		q = q.Where("created_at >= ?", f.From)
	}
	if f.To > 0 {
		q = q.Where("created_at <= ?", f.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.AuditEvent
	err := q.Order("created_at DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&list).Error
	return list, total, err
}
