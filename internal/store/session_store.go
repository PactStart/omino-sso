package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/omino/omino-sso/internal/store/model"
)

// ErrSessionNotFound 会话不存在或已过期。
var ErrSessionNotFound = errors.New("session not found")

const sessionKeyPrefix = "sess:"

func (s *Store) sessionKey(sid string) string { return sessionKeyPrefix + sid }

// CreateSession 写入 Redis（主存）与 MySQL（审计副本）。
func (s *Store) CreateSession(ctx context.Context, sess *model.Session) error {
	amr, _ := json.Marshal([]string(sess.AMR))
	ttl := time.Until(time.UnixMilli(sess.ExpiresAt))
	if ttl <= 0 {
		return errors.New("session already expired")
	}
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, s.sessionKey(sess.SID), map[string]any{
		"user_id":      sess.UserID,
		"tenant_id":    sess.TenantID,
		"amr":          string(amr),
		"auth_time":    sess.AuthTime,
		"ip":           sess.IP,
		"created_at":   sess.CreatedAt,
		"last_seen_at": sess.LastSeenAt,
		"expires_at":   sess.ExpiresAt,
	})
	pipe.Expire(ctx, s.sessionKey(sess.SID), ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return s.DB(ctx).Create(sess).Error
}

// GetSession 读取会话并校验过期时间。
func (s *Store) GetSession(ctx context.Context, sid string) (*model.Session, error) {
	m, err := s.rdb.HGetAll(ctx, s.sessionKey(sid)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrSessionNotFound
	}
	geti := func(k string) int64 {
		v, _ := strconv.ParseInt(m[k], 10, 64)
		return v
	}
	sess := &model.Session{
		SID:        sid,
		UserID:     m["user_id"],
		TenantID:   m["tenant_id"],
		AuthTime:   geti("auth_time"),
		IP:         m["ip"],
		CreatedAt:  geti("created_at"),
		LastSeenAt: geti("last_seen_at"),
		ExpiresAt:  geti("expires_at"),
	}
	if sess.ExpiresAt <= time.Now().UnixMilli() {
		_ = s.DeleteSession(ctx, sid)
		return nil, ErrSessionNotFound
	}
	if m["amr"] != "" {
		var amr []string
		_ = json.Unmarshal([]byte(m["amr"]), &amr)
		sess.AMR = amr
	}
	return sess, nil
}

// TouchSession 刷新 last_seen_at（不延长过期时间）。
func (s *Store) TouchSession(ctx context.Context, sid string, now int64) {
	_ = s.rdb.HSet(ctx, s.sessionKey(sid), "last_seen_at", now).Err()
}

// DeleteSession 删除 Redis 会话并标记 MySQL 副本。
func (s *Store) DeleteSession(ctx context.Context, sid string) error {
	if err := s.rdb.Del(ctx, s.sessionKey(sid)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return s.DB(ctx).Model(&model.Session{}).Where("sid = ?", sid).
		Updates(map[string]any{"revoked": true}).Error
}

// DeleteSessionsByUser 吊销用户全部会话（改密 / 登出）。
func (s *Store) DeleteSessionsByUser(ctx context.Context, userID string) error {
	var sids []string
	if err := s.DB(ctx).Model(&model.Session{}).
		Where("user_id = ? AND revoked = 0", userID).Pluck("sid", &sids).Error; err != nil {
		return err
	}
	for _, sid := range sids {
		_ = s.rdb.Del(ctx, s.sessionKey(sid)).Err()
	}
	return s.DB(ctx).Model(&model.Session{}).Where("user_id = ? AND revoked = 0", userID).
		Updates(map[string]any{"revoked": true}).Error
}

// DeleteOtherSessions 吊销用户除 keepSID 之外的全部会话（已登录重置密码场景）。
func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepSID string) error {
	var sids []string
	if err := s.DB(ctx).Model(&model.Session{}).
		Where("user_id = ? AND revoked = 0 AND sid <> ?", userID, keepSID).Pluck("sid", &sids).Error; err != nil {
		return err
	}
	for _, sid := range sids {
		_ = s.rdb.Del(ctx, s.sessionKey(sid)).Err()
	}
	return s.DB(ctx).Model(&model.Session{}).
		Where("user_id = ? AND revoked = 0 AND sid <> ?", userID, keepSID).
		Updates(map[string]any{"revoked": true}).Error
}

// ListSessionsByUser 返回用户未失效的会话（MySQL 副本）。
func (s *Store) ListSessionsByUser(ctx context.Context, userID string, now int64) ([]model.Session, error) {
	var list []model.Session
	err := s.DB(ctx).Where("user_id = ? AND revoked = 0 AND expires_at > ?", userID, now).
		Order("last_seen_at DESC").Find(&list).Error
	return list, err
}
