package model

import "strings"

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

// OrgDepth 由物化路径推导层级（根级为 1）："/ID/" → 1，"/ID/ID/" → 2。
func OrgDepth(path string) int {
	return strings.Count(path, "/") - 1
}

// JoinOrgPath 拼接子节点的物化路径：父路径为空表示根级，结果为 "/" + id + "/"。
func JoinOrgPath(parentPath, id string) string {
	if parentPath == "" || parentPath == "/" {
		return "/" + id + "/"
	}
	return parentPath + id + "/"
}

// IsPathPrefix 判断 p 是否落在 prefix 子树内（含 prefix 自身），
// 用于移动时的成环判定与子树范围查询。
func IsPathPrefix(prefix, p string) bool {
	return strings.HasPrefix(p, prefix)
}

// OrgPathPattern 生成子树前缀匹配的 LIKE 模式。
// ULID 字符集为 [0-9A-HJKMNP-TV-Z]，不含 "%" 与 "_"，因此无需转义。
func OrgPathPattern(prefix string) string {
	return prefix + "%"
}
