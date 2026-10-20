package model

// Organization 对应 organizations 表（AGENTS.md §6.2）。
type Organization struct {
	ID        string  `gorm:"column:id;primaryKey"`
	ParentID  *string `gorm:"column:parent_id"`
	Name      string  `gorm:"column:name"`
	Path      string  `gorm:"column:path"`
	Sort      int     `gorm:"column:sort"`
	TenantID  string  `gorm:"column:tenant_id"`
	CreatedAt int64   `gorm:"column:created_at"`
}

func (Organization) TableName() string { return "organizations" }
