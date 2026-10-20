package store

import (
	"context"

	"gorm.io/gorm"
)

// 规范 §7 约束 3：/oauth2/token 链路必须共享同一事务。
// 事务通过 context 传递，Storage 层所有方法经 DB(ctx) 取用。

type txKey struct{}

// WithTx 把事务放入 context。
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// DB 返回当前 context 中的事务，否则返回基础连接。
// 必须始终链式调用 WithContext(ctx)，以传播超时与取消。
func (s *Store) DB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return s.db.WithContext(ctx)
}

// Transaction 在事务中执行 fn，若 context 已存在事务则复用。
func (s *Store) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return fn(ctx)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithTx(ctx, tx))
	})
}
