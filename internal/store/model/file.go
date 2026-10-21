package model

// File 对应 files 表（AGENTS.md §6）。上传文件元数据，不含对象本体（对象在 OSS）。
type File struct {
	ID        string `gorm:"column:id;primaryKey"`
	Provider  string `gorm:"column:provider"`
	Scene     string `gorm:"column:scene"`
	TargetID  string `gorm:"column:target_id"`
	Format    string `gorm:"column:format"`
	Size      int64  `gorm:"column:size"`
	URL       string `gorm:"column:url"`
	ObjectKey string `gorm:"column:object_key"`
	Status    string `gorm:"column:status"`
	Creator   string `gorm:"column:creator"`
	CreatedAt int64  `gorm:"column:created_at"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (File) TableName() string { return "files" }

// 场景枚举（字符串，FR-013）。
const (
	FileSceneAppLogo    = "app_logo"
	FileSceneUserAvatar = "user_avatar"
)

// 状态枚举（字符串，FR-013）。
const (
	FileStatusActive   = "active"
	FileStatusDisabled = "disabled"
	FileStatusDeleted  = "deleted"
)
