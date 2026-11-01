package model

// ClientOrgGrant 对应 client_org_grants 表（010-app-access-org-grant，AGENTS.md §6.12）。
// 一条记录表示「某应用对某部门（可含子部门）开放门户可见性」。
// 只存 org_id，不存 org_path：部门迁父时子树 path 整体平移，快照会指向错误子树。
type ClientOrgGrant struct {
	ClientID   string  `gorm:"column:client_id;primaryKey"`
	OrgID      string  `gorm:"column:org_id;primaryKey"`
	IncludeSub bool    `gorm:"column:include_sub"`
	GrantedBy  *string `gorm:"column:granted_by"`
	GrantedAt  int64   `gorm:"column:granted_at"`
	UpdatedAt  int64   `gorm:"column:updated_at"`
}

func (ClientOrgGrant) TableName() string { return "client_org_grants" }

// OrgGrantMatchSQL 返回「授权行 g 是否覆盖部门 d」的 SQL 片段，是本特性的**唯一判定实现**
// （spec FR-004：覆盖人数与可见性不得各算一套）。
//
// 三个参数只接受调用方写死的表别名，片段本身不含任何用户输入，参数一律走绑定，
// 因此不构成注入面。语义：
//
//	include_sub = 1：d 落在 g 指向部门的整棵子树内（物化路径前缀匹配）
//	include_sub = 0：d 就是 g 指向的部门本级（等值匹配，避免前缀误命中子孙）
func OrgGrantMatchSQL(g, grantOrg, d string) string {
	return "((" + g + ".include_sub = 1 AND " + d + ".path LIKE CONCAT(" + grantOrg + ".path, '%'))" +
		" OR (" + g + ".include_sub = 0 AND " + d + ".id = " + g + ".org_id))"
}

// OrgGrantMatchPlanSQL 是 OrgGrantMatchSQL 的变体：授权部门与用户部门来自同一张
// organizations 表的 `o`，用于「覆盖人数」这类按授权聚合的查询（grantOrg 与 d 同源）。
func OrgGrantMatchPlanSQL(g, o string) string {
	return "((" + g + ".include_sub = 1 AND " + o + ".path LIKE CONCAT((SELECT p.path FROM organizations p WHERE p.id = " + g + ".org_id), '%'))" +
		" OR (" + g + ".include_sub = 0 AND " + o + ".id = " + g + ".org_id))"
}
