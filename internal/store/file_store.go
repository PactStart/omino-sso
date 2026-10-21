package store

import (
	"context"
	"time"

	"github.com/omino/omino-sso/internal/store/model"
)

// FileFilter 文件记录筛选条件。
type FileFilter struct {
	Scene    string
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// CreateFile 插入文件记录。
func (s *Store) CreateFile(ctx context.Context, f *model.File) error {
	return s.DB(ctx).Create(f).Error
}

// GetFileByID 按主键查询。
func (s *Store) GetFileByID(ctx context.Context, id string) (*model.File, error) {
	var f model.File
	if err := s.DB(ctx).Where("id = ?", id).Take(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// GetActiveFileByTarget 查询某业务对象当前生效的文件记录（替换时用于清理旧记录）。
func (s *Store) GetActiveFileByTarget(ctx context.Context, scene, targetID string) (*model.File, error) {
	var f model.File
	if err := s.DB(ctx).
		Where("scene = ? AND target_id = ? AND status = ?", scene, targetID, model.FileStatusActive).
		Order("created_at DESC").Take(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFiles 分页查询文件记录。
func (s *Store) ListFiles(ctx context.Context, f FileFilter) ([]model.File, int64, error) {
	q := s.DB(ctx).Model(&model.File{})
	if f.Scene != "" {
		q = q.Where("scene = ?", f.Scene)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("url LIKE ? OR object_key LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.File
	err := q.Order("created_at DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&list).Error
	return list, total, err
}

// UpdateFileStatus 更新状态。
func (s *Store) UpdateFileStatus(ctx context.Context, id, status string) error {
	return s.DB(ctx).Model(&model.File{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": time.Now().UnixMilli()}).Error
}

// UpdateFileTargetID 回填文件记录的业务对象标识（注册应用后绑定 logo）。
func (s *Store) UpdateFileTargetID(ctx context.Context, id, targetID string) error {
	return s.DB(ctx).Model(&model.File{}).Where("id = ?", id).
		Updates(map[string]any{"target_id": targetID, "updated_at": time.Now().UnixMilli()}).Error
}

// ClearClientLogoIf 仅当当前 logo 仍指向该 url 时清空（避免误清新上传）。
func (s *Store) ClearClientLogoIf(ctx context.Context, clientID, url string, now int64) error {
	return s.DB(ctx).Model(&model.Client{}).Where("client_id = ? AND logo_url = ?", clientID, url).
		Updates(map[string]any{"logo_url": nil, "updated_at": now}).Error
}

// ClearUserAvatarIf 仅当当前头像仍指向该 url 时清空。
func (s *Store) ClearUserAvatarIf(ctx context.Context, userID, url string) error {
	return s.DB(ctx).Model(&model.User{}).Where("id = ? AND avatar_url = ?", userID, url).
		Updates(map[string]any{"avatar_url": nil, "updated_at": time.Now().UnixMilli()}).Error
}
