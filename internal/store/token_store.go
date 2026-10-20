package store

import (
	"context"
	"errors"

	"github.com/omino/omino-sso/internal/store/model"
)

// ErrRefreshReused 表示检测到 refresh token 重放。
var ErrRefreshReused = errors.New("refresh token reuse detected")

// InsertRefreshToken 插入 refresh token（只存 SHA-256）。
func (s *Store) InsertRefreshToken(ctx context.Context, rt *model.RefreshToken) error {
	return s.DB(ctx).Create(rt).Error
}

// GetRefreshTokenByHash 按哈希查询。
func (s *Store) GetRefreshTokenByHash(ctx context.Context, tokenHash []byte) (*model.RefreshToken, error) {
	var rt model.RefreshToken
	if err := s.DB(ctx).Where("token_hash = ?", tokenHash).Take(&rt).Error; err != nil {
		return nil, err
	}
	return &rt, nil
}

// RevokeFamily 吊销整条 token 链（重放检测触发）。
func (s *Store) RevokeFamily(ctx context.Context, familyID string, now int64) error {
	return s.DB(ctx).Model(&model.RefreshToken{}).
		Where("family_id = ? AND revoked = 0", familyID).
		Updates(map[string]any{"revoked": true, "revoked_at": now}).Error
}

// RotateRefreshToken 事务内完成"吊销旧 token + 写入新 token"。
// 旧 token 必须处于未吊销状态，用 RowsAffected 判定，避免并发下双发。
func (s *Store) RotateRefreshToken(ctx context.Context, oldID string, next *model.RefreshToken, now int64) error {
	return s.Transaction(ctx, func(ctx context.Context) error {
		res := s.DB(ctx).Model(&model.RefreshToken{}).
			Where("id = ? AND revoked = 0", oldID).
			Updates(map[string]any{"revoked": true, "revoked_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrRefreshReused
		}
		return s.DB(ctx).Create(next).Error
	})
}

// RevokeRefreshTokensByUser 登出时吊销某用户在某 client 下的全部 refresh token。
func (s *Store) RevokeRefreshTokensByUser(ctx context.Context, userID, clientID string, now int64) error {
	q := s.DB(ctx).Model(&model.RefreshToken{}).Where("user_id = ? AND revoked = 0", userID)
	if clientID != "" {
		q = q.Where("client_id = ?", clientID)
	}
	return q.Updates(map[string]any{"revoked": true, "revoked_at": now}).Error
}

// RevokeRefreshTokenByHash 撤销单个 refresh token（/oauth2/revoke）。
func (s *Store) RevokeRefreshTokenByHash(ctx context.Context, tokenHash []byte, now int64) error {
	return s.DB(ctx).Model(&model.RefreshToken{}).
		Where("token_hash = ? AND revoked = 0", tokenHash).
		Updates(map[string]any{"revoked": true, "revoked_at": now}).Error
}

// DeleteExpiredRefreshTokens 清理过期记录。
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context, before int64) error {
	return s.DB(ctx).Where("expires_at < ?", before).Delete(&model.RefreshToken{}).Error
}
