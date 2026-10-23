package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Store 聚合 MySQL 与 Redis 访问。
type Store struct {
	db  *gorm.DB
	rdb *redis.Client
}

// New 构造 Store。
func New(db *gorm.DB, rdb *redis.Client) *Store {
	return &Store{db: db, rdb: rdb}
}

// Ping 供 op.Storage.Health 使用：必须廉价，不开启事务、不获取锁。
func (s *Store) Ping(ctx context.Context) error {
	mysqlErr, redisErr := s.PingDetail(ctx)
	if mysqlErr != nil {
		return mysqlErr
	}
	return redisErr
}

// PingDetail 分别探测 MySQL 与 Redis，供 /readyz 输出 failing 明细（规范 §4.0 探针）。
// MySQL 不通时不再探测 Redis：依赖链上游已断，把 Redis 一起标红只会误导排障。
func (s *Store) PingDetail(ctx context.Context) (mysqlErr, redisErr error) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return err, nil
	}
	return nil, s.rdb.Ping(ctx).Err()
}

// Redis 暴露 redis 客户端给需要直接使用的服务（限流等）。
func (s *Store) Redis() *redis.Client { return s.rdb }

// DB 暴露 gorm 句柄（仅供 store 包内部与迁移工具使用）。
func (s *Store) RawDB() *gorm.DB { return s.db }
