package service_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/service"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// mockFileStore 实现 store.FileStore，记录对象删除调用并返回确定 URL。
type mockFileStore struct {
	deletedKeys []string
	baseURL     string
}

func (m *mockFileStore) CreateUploadCredential(context.Context, string, string, int64) (*store.UploadCredential, error) {
	return nil, nil
}

func (m *mockFileStore) DeleteObject(_ context.Context, objectKey string) error {
	m.deletedKeys = append(m.deletedKeys, objectKey)
	return nil
}

func (m *mockFileStore) ResolvePublicURL(objectKey string) string {
	if m.baseURL != "" {
		return m.baseURL
	}
	return "https://cdn.example.com/" + objectKey
}

// fileEnv 文件上传集成测试环境（由 TEST_MYSQL_DSN 门控）。
type fileEnv struct {
	db     *gorm.DB
	st     *store.Store
	svc    *service.FileService
	fs     *mockFileStore
	audit  *service.Auditor
	cancel context.CancelFunc
}

func newFileEnv(t *testing.T) *fileEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(testDSNEnv))
	if dsn == "" {
		t.Skip("未设置 " + testDSNEnv + "，跳过文件上传集成测试")
	}
	db := openTestDB(t, dsn)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, nil)
	audit := service.NewAuditor(st, 64, logger)
	ctx, cancel := context.WithCancel(context.Background())
	go audit.Run(ctx)
	t.Cleanup(cancel)

	fs := &mockFileStore{}
	return &fileEnv{
		db:     db,
		st:     st,
		svc:    service.NewFileService(st, fs, audit),
		fs:     fs,
		audit:  audit,
		cancel: cancel,
	}
}

func (e *fileEnv) reset(t *testing.T) {
	t.Helper()
	for _, table := range []string{"files", "clients", "users", "audit_events"} {
		if err := e.db.Exec("TRUNCATE TABLE " + table).Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

// waitAuditEvent 轮询等待指定审计事件落库（Auditor 异步写入）。
func (e *fileEnv) waitAuditEvent(t *testing.T, eventType string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int64
		e.db.Model(&model.AuditEvent{}).Where("event_type = ?", eventType).Count(&n)
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待审计事件 %s 超时", eventType)
}

func (e *fileEnv) seedClient(t *testing.T, clientID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	c := &model.Client{
		ClientID:       clientID,
		ClientName:     "client-" + clientID,
		RedirectURIs:   model.StringList{"https://example.com/cb"},
		PostLogoutURIs: model.StringList{},
		GrantTypes:     model.StringList{"authorization_code"},
		Scopes:         model.StringList{"openid"},
		AccessTokenTTL: 900,
		RefreshTokenTTL: 2592000,
		TenantID:       "default",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := e.st.CreateClient(context.Background(), c); err != nil {
		t.Fatalf("创建 client 失败: %v", err)
	}
}

func (e *fileEnv) seedUser(t *testing.T, userID, username string) {
	t.Helper()
	now := time.Now().UnixMilli()
	u := &model.User{
		ID:                userID,
		Username:          username,
		DisplayName:       username,
		PasswordHash:      "x",
		DeptID:            "dept-0000000000000000000",
		TenantID:          "default",
		Roles:             model.StringList{},
		Status:            model.UserStatusActive,
		PasswordUpdatedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := e.st.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("创建 user 失败: %v", err)
	}
}

// TestConfirmUpload_AppLogo 确认上传回写 clients.logo_url 并发审计事件。
func TestConfirmUpload_AppLogo(t *testing.T) {
	e := newFileEnv(t)
	e.reset(t)
	e.seedClient(t, "c1")

	f, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene:    model.FileSceneAppLogo,
		Key:      "app_logo/01J9.png",
		Format:   "png",
		Size:     1024,
		TargetID: "c1",
		ActorID:  "u1",
	})
	if err != nil {
		t.Fatalf("ConfirmUpload 失败: %v", err)
	}
	if f.TargetID != "c1" || f.Scene != model.FileSceneAppLogo || f.Status != model.FileStatusActive {
		t.Fatalf("文件记录字段错误: %+v", f)
	}

	var c model.Client
	if err := e.db.Where("client_id = ?", "c1").Take(&c).Error; err != nil {
		t.Fatalf("查询 client 失败: %v", err)
	}
	wantURL := e.fs.ResolvePublicURL("app_logo/01J9.png")
	if c.LogoURL == nil || *c.LogoURL != wantURL {
		t.Fatalf("clients.logo_url = %v, 期望 %q", c.LogoURL, wantURL)
	}
	e.waitAuditEvent(t, model.EventFileUploaded)
}

// TestConfirmUpload_UserAvatar 确认上传回写 users.avatar_url。
func TestConfirmUpload_UserAvatar(t *testing.T) {
	e := newFileEnv(t)
	e.reset(t)
	e.seedUser(t, "u1", "zhangsan")

	_, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene:    model.FileSceneUserAvatar,
		Key:      "user_avatar/01J9.png",
		Format:   "png",
		Size:     1024,
		TargetID: "u1",
		ActorID:  "u1",
	})
	if err != nil {
		t.Fatalf("ConfirmUpload 失败: %v", err)
	}

	var u model.User
	if err := e.db.Where("id = ?", "u1").Take(&u).Error; err != nil {
		t.Fatalf("查询 user 失败: %v", err)
	}
	wantURL := e.fs.ResolvePublicURL("user_avatar/01J9.png")
	if u.AvatarURL == nil || *u.AvatarURL != wantURL {
		t.Fatalf("users.avatar_url = %v, 期望 %q", u.AvatarURL, wantURL)
	}
}

// TestConfirmUpload_ReplacesOld 替换时旧记录置 deleted、旧对象删除、新记录生效。
func TestConfirmUpload_ReplacesOld(t *testing.T) {
	e := newFileEnv(t)
	e.reset(t)
	e.seedClient(t, "c1")

	if _, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene: model.FileSceneAppLogo, Key: "app_logo/old.png", Format: "png", Size: 1, TargetID: "c1", ActorID: "u1",
	}); err != nil {
		t.Fatalf("首次上传失败: %v", err)
	}
	if _, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene: model.FileSceneAppLogo, Key: "app_logo/new.png", Format: "png", Size: 1, TargetID: "c1", ActorID: "u1",
	}); err != nil {
		t.Fatalf("二次上传失败: %v", err)
	}

	var oldFile model.File
	if err := e.db.Where("object_key = ?", "app_logo/old.png").Take(&oldFile).Error; err != nil {
		t.Fatalf("查询旧记录失败: %v", err)
	}
	if oldFile.Status != model.FileStatusDeleted {
		t.Fatalf("旧记录应为 deleted，实际 %s", oldFile.Status)
	}
	if !slices.Contains(e.fs.deletedKeys, "app_logo/old.png") {
		t.Fatalf("旧对象应被删除，deletedKeys=%v", e.fs.deletedKeys)
	}

	var c model.Client
	if err := e.db.Where("client_id = ?", "c1").Take(&c).Error; err != nil {
		t.Fatalf("查询 client 失败: %v", err)
	}
	if c.LogoURL == nil || *c.LogoURL != e.fs.ResolvePublicURL("app_logo/new.png") {
		t.Fatalf("clients.logo_url 应指向新对象，实际 %v", c.LogoURL)
	}
}

// TestConfirmUpload_EmptyTarget 注册应用前上传（target 为空）仅落记录、不回写业务字段。
func TestConfirmUpload_EmptyTarget(t *testing.T) {
	e := newFileEnv(t)
	e.reset(t)

	f, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene: model.FileSceneAppLogo, Key: "app_logo/01J9.png", Format: "png", Size: 1, TargetID: "", ActorID: "u1",
	})
	if err != nil {
		t.Fatalf("ConfirmUpload 失败: %v", err)
	}
	if f.TargetID != "" {
		t.Fatalf("target 为空应落空 target_id，实际 %q", f.TargetID)
	}
	var n int64
	e.db.Model(&model.Client{}).Count(&n)
	if n != 0 {
		t.Fatalf("target 为空不应写 client，实际 %d 条", n)
	}
}

// TestUpdateFileStatus_Transitions active→disabled 清字段保留对象，→deleted 删对象。
func TestUpdateFileStatus_Transitions(t *testing.T) {
	e := newFileEnv(t)
	e.reset(t)
	e.seedClient(t, "c1")

	f, err := e.svc.ConfirmUpload(context.Background(), service.ConfirmUploadInput{
		Scene: model.FileSceneAppLogo, Key: "app_logo/logo.png", Format: "png", Size: 1, TargetID: "c1", ActorID: "u1",
	})
	if err != nil {
		t.Fatalf("ConfirmUpload 失败: %v", err)
	}

	if _, err := e.svc.UpdateFileStatus(context.Background(), f.ID, model.FileStatusDisabled, "admin"); err != nil {
		t.Fatalf("置 disabled 失败: %v", err)
	}
	var c model.Client
	if err := e.db.Where("client_id = ?", "c1").Take(&c).Error; err != nil {
		t.Fatalf("查询 client 失败: %v", err)
	}
	if c.LogoURL != nil {
		t.Fatalf("disabled 后 logo_url 应清空，实际 %v", c.LogoURL)
	}
	if slices.Contains(e.fs.deletedKeys, "app_logo/logo.png") {
		t.Fatalf("disabled 不应删对象，deletedKeys=%v", e.fs.deletedKeys)
	}

	if _, err := e.svc.UpdateFileStatus(context.Background(), f.ID, model.FileStatusDeleted, "admin"); err != nil {
		t.Fatalf("置 deleted 失败: %v", err)
	}
	if !slices.Contains(e.fs.deletedKeys, "app_logo/logo.png") {
		t.Fatalf("deleted 应删对象，deletedKeys=%v", e.fs.deletedKeys)
	}
	var f2 model.File
	if err := e.db.Where("id = ?", f.ID).Take(&f2).Error; err != nil {
		t.Fatalf("查询文件记录失败: %v", err)
	}
	if f2.Status != model.FileStatusDeleted {
		t.Fatalf("记录应为 deleted，实际 %s", f2.Status)
	}
	e.waitAuditEvent(t, model.EventFileStatusUpdated)
}
