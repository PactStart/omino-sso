package store

import (
	"context"
	"errors"
	"time"

	"github.com/omino/omino-sso/internal/store/model"
)

// ErrCodeInvalid 授权码无效、过期或已被使用。
var ErrCodeInvalid = errors.New("authorization code is expired or already used")

// CodeRecord 是写入 auth_codes 的完整数据。
type CodeRecord struct {
	CodeHash            []byte
	ClientID            string
	UserID              string
	RedirectURI         string
	Scope               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	AuthTime            int64
	AMR                 []string
	ExpiresAt           int64
}

// InsertAuthCode 落库授权码（只存 SHA-256）。
func (s *Store) InsertAuthCode(ctx context.Context, rec CodeRecord) error {
	row := &model.AuthCode{
		CodeHash:            rec.CodeHash,
		ClientID:            rec.ClientID,
		UserID:              rec.UserID,
		RedirectURI:         rec.RedirectURI,
		Scope:               rec.Scope,
		CodeChallenge:       rec.CodeChallenge,
		CodeChallengeMethod: rec.CodeChallengeMethod,
		AuthTime:            rec.AuthTime,
		AMR:                 rec.AMR,
		ExpiresAt:           rec.ExpiresAt,
		Used:                false,
		CreatedAt:           time.Now().UnixMilli(),
	}
	if rec.Nonce != "" {
		row.Nonce = &rec.Nonce
	}
	return s.DB(ctx).Create(row).Error
}

// ConsumeAuthCode 原子消费授权码：单条 UPDATE + RowsAffected 判定（规范 §6.4）。
// 禁止先 SELECT 再 UPDATE。
func (s *Store) ConsumeAuthCode(ctx context.Context, codeHash []byte, now int64) (*model.AuthCode, error) {
	var out model.AuthCode
	err := s.Transaction(ctx, func(ctx context.Context) error {
		res := s.DB(ctx).Model(&model.AuthCode{}).
			Where("code_hash = ? AND used = 0 AND expires_at > ?", codeHash, now).
			Update("used", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrCodeInvalid
		}
		return s.DB(ctx).Where("code_hash = ?", codeHash).Take(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteExpiredCodes 清理过期授权码（定时任务）。
func (s *Store) DeleteExpiredCodes(ctx context.Context, before int64) error {
	return s.DB(ctx).Where("expires_at < ?", before).Delete(&model.AuthCode{}).Error
}

// FindAuthCode 只读查询授权码（不消费），用于提前校验 PKCE。
func (s *Store) FindAuthCode(ctx context.Context, codeHash []byte) (*model.AuthCode, error) {
	var out model.AuthCode
	if err := s.DB(ctx).Where("code_hash = ?", codeHash).Take(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}
