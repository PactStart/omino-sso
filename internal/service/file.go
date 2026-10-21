package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// maxImageSize 单张图片大小上限（5MB）。
const maxImageSize = 5 * 1024 * 1024

// 文件上传相关哨兵错误，由 API 层映射为错误码。
var (
	ErrFileValidation    = errors.New("invalid upload params")
	ErrFileNotFound      = errors.New("file not found")
	ErrFileStatusInvalid = errors.New("invalid file status")
	ErrTargetNotFound    = errors.New("target not found")
)

// FileService 文件上传业务规则。
type FileService struct {
	st    *store.Store
	fs    store.FileStore
	audit *Auditor
}

// NewFileService 构造。
func NewFileService(st *store.Store, fs store.FileStore, audit *Auditor) *FileService {
	return &FileService{st: st, fs: fs, audit: audit}
}

var validFormats = map[string]bool{"jpg": true, "png": true, "webp": true}

func validScene(s string) bool {
	return s == model.FileSceneAppLogo || s == model.FileSceneUserAvatar
}

func validStatus(s string) bool {
	return s == model.FileStatusActive || s == model.FileStatusDisabled || s == model.FileStatusDeleted
}

// CreateUploadCredential 校验参数并签发直传凭证（key 由服务端生成，含场景前缀 + ULID）。
func (s *FileService) CreateUploadCredential(ctx context.Context, scene, format string, size int64) (*store.UploadCredential, error) {
	if !validScene(scene) || !validFormats[format] || size < 1 || size > maxImageSize {
		return nil, ErrFileValidation
	}
	key := fmt.Sprintf("%s/%s.%s", scene, id.New(), format)
	return s.fs.CreateUploadCredential(ctx, key, format, size)
}

// ConfirmUploadInput 上传确认入参。
type ConfirmUploadInput struct {
	Scene    string // app_logo | user_avatar
	Key      string // OSS 对象键
	Format   string // jpg | png | webp
	Size     int64  // 字节数
	TargetID string // app_logo=clientID；user_avatar=userID
	ActorID  string // 操作者（管理员代管时为管理员，自助时为本人）
}

// ConfirmUpload 写文件记录并回写业务字段，替换时清理旧记录与旧对象（FR-006/008/017）。
func (s *FileService) ConfirmUpload(ctx context.Context, in ConfirmUploadInput) (*model.File, error) {
	if !validScene(in.Scene) || !validFormats[in.Format] || in.Size < 1 || in.Size > maxImageSize {
		return nil, ErrFileValidation
	}
	if !strings.Contains("/"+in.Key, "/"+in.Scene+"/") {
		return nil, ErrFileValidation
	}
	url := s.fs.ResolvePublicURL(in.Key)
	now := nowMillis()

	// target_id 为空表示「先存文件、后绑定」（注册应用上传 logo 前尚无 clientID）。
	var old *model.File
	if in.TargetID != "" {
		var err error
		old, err = s.st.GetActiveFileByTarget(ctx, in.Scene, in.TargetID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	f := &model.File{
		ID:        id.New(),
		Provider:  "aliyun_oss",
		Scene:     in.Scene,
		TargetID:  in.TargetID,
		Format:    in.Format,
		Size:      in.Size,
		URL:       url,
		ObjectKey: in.Key,
		Status:    model.FileStatusActive,
		Creator:   in.ActorID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.st.Transaction(ctx, func(ctx context.Context) error {
		// 无 target 时仅落文件记录，业务字段待绑定后回写。
		if in.TargetID != "" {
			if err := s.applyBusinessField(ctx, in.Scene, in.TargetID, url); err != nil {
				return err
			}
		}
		return s.st.CreateFile(ctx, f)
	}); err != nil {
		return nil, err
	}

	// 替换：旧记录置 deleted，旧对象删除（best-effort）。
	if old != nil && old.ID != f.ID {
		_ = s.st.UpdateFileStatus(ctx, old.ID, model.FileStatusDeleted)
		_ = s.fs.DeleteObject(ctx, old.ObjectKey)
	}

	s.audit.Emit(ctx, Event{
		Type:   model.EventFileUploaded,
		UserID: in.ActorID,
		Fields: map[string]any{"scene": in.Scene, "targetId": in.TargetID, "fileId": f.ID},
	})
	return f, nil
}

// ListFiles 分页查询文件记录。
func (s *FileService) ListFiles(ctx context.Context, f store.FileFilter) ([]model.File, int64, error) {
	return s.st.ListFiles(ctx, f)
}

// UpdateFileStatus 更新状态：disabled 清空业务字段保留对象；deleted 清空字段并删对象；active 恢复写回。
func (s *FileService) UpdateFileStatus(ctx context.Context, fileID, status, actorID string) (*model.File, error) {
	if !validStatus(status) {
		return nil, ErrFileStatusInvalid
	}
	f, err := s.st.GetFileByID(ctx, fileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	switch status {
	case model.FileStatusDisabled:
		if err := s.clearBusinessField(ctx, f); err != nil {
			return nil, err
		}
	case model.FileStatusDeleted:
		if err := s.clearBusinessField(ctx, f); err != nil {
			return nil, err
		}
		_ = s.fs.DeleteObject(ctx, f.ObjectKey)
	case model.FileStatusActive:
		if err := s.applyBusinessField(ctx, f.Scene, f.TargetID, f.URL); err != nil {
			return nil, err
		}
	}
	if err := s.st.UpdateFileStatus(ctx, fileID, status); err != nil {
		return nil, err
	}
	s.audit.Emit(ctx, Event{
		Type:   model.EventFileStatusUpdated,
		UserID: actorID,
		Fields: map[string]any{"fileId": fileID, "status": status},
	})
	return s.st.GetFileByID(ctx, fileID)
}

// applyBusinessField 回写业务字段（logo_url / avatar_url）。
func (s *FileService) applyBusinessField(ctx context.Context, scene, targetID, url string) error {
	now := nowMillis()
	var err error
	switch scene {
	case model.FileSceneAppLogo:
		err = s.st.UpdateClientFields(ctx, targetID, map[string]any{"logo_url": url}, nil, now)
	case model.FileSceneUserAvatar:
		err = s.st.UpdateUserFields(ctx, targetID, map[string]any{"avatar_url": url})
	default:
		return ErrFileValidation
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTargetNotFound
	}
	return err
}

// clearBusinessField 清空业务字段（仅当仍指向该文件 url 时）。
func (s *FileService) clearBusinessField(ctx context.Context, f *model.File) error {
	switch f.Scene {
	case model.FileSceneAppLogo:
		return s.st.ClearClientLogoIf(ctx, f.TargetID, f.URL, nowMillis())
	case model.FileSceneUserAvatar:
		return s.st.ClearUserAvatarIf(ctx, f.TargetID, f.URL)
	default:
		return ErrFileValidation
	}
}
