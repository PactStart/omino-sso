package store

import (
	"context"

	"github.com/omino/omino-sso/internal/store/model"
)

// SendRecordFilter 发送记录查询条件。
// 管理端查询强制带时间范围（From/To），以命中 created_at 前置索引（FR-014）。
type SendRecordFilter struct {
	Channel  string
	Scene    string
	Result   string
	From     int64
	To       int64
	Page     int
	PageSize int
}

// CreateSendRecord 同步写入一条发送记录（决策 9：保证 100% 可追溯）。
func (s *Store) CreateSendRecord(ctx context.Context, r *model.SendRecord) error {
	return s.DB(ctx).Create(r).Error
}

// ListSendRecords 分页查询发送记录（created_at 倒序）。
func (s *Store) ListSendRecords(ctx context.Context, f SendRecordFilter) ([]model.SendRecord, int64, error) {
	q := s.DB(ctx).Model(&model.SendRecord{})
	if f.Channel != "" {
		q = q.Where("channel = ?", f.Channel)
	}
	if f.Scene != "" {
		q = q.Where("scene = ?", f.Scene)
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
	var list []model.SendRecord
	err := q.Order("created_at DESC, id DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&list).Error
	return list, total, err
}

// DeleteSendRecordsBefore 分批物理删除 before 之前的发送记录，返回删除总行数。
// 单批受 batch 限制，循环至无可删数据，避免长事务与长锁（决策 8）。
func (s *Store) DeleteSendRecordsBefore(ctx context.Context, before int64, batch int) (int64, error) {
	return s.deleteBefore(ctx, "send_records", before, batch)
}

// DeleteAuditEventsBefore 分批物理删除 before 之前的审计日志。
func (s *Store) DeleteAuditEventsBefore(ctx context.Context, before int64, batch int) (int64, error) {
	return s.deleteBefore(ctx, "audit_events", before, batch)
}

func (s *Store) deleteBefore(ctx context.Context, table string, before int64, batch int) (int64, error) {
	if batch <= 0 {
		batch = 500
	}
	var total int64
	for {
		res := s.DB(ctx).Exec(
			"DELETE FROM "+table+" WHERE created_at < ? ORDER BY created_at LIMIT ?", before, batch)
		if res.Error != nil {
			return total, res.Error
		}
		n := res.RowsAffected
		total += n
		if n < int64(batch) {
			return total, nil
		}
	}
}
