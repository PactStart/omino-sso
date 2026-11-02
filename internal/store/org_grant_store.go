package store

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/store/model"
)

// 组织授权（010-app-access-org-grant）的读写。
// 判定谓词只有一个来源：model.OrgGrantMatchSQL / model.OrgGrantMatchPlanSQL（spec FR-004）。

var (
	// ErrOrgGrantLimit 单应用组织授权条数超过上限。
	ErrOrgGrantLimit = errors.New("org grant limit exceeded")
	// ErrOrgGrantDeptNotFound 授权目标部门不存在（与 service.ErrAppDeptNotFound 同义，由 service 转换）。
	ErrOrgGrantDeptNotFound = errors.New("org grant dept not found")
)

// MaxOrgGrantsPerClient 单个应用的组织授权数量上限（spec FR-010）。
const MaxOrgGrantsPerClient = 500

// OrgGrantRow 组织授权列表行（含三个聚合计数）。
type OrgGrantRow struct {
	OrgID                string
	DeptName             string
	DeptPath             string
	IncludeSub           bool
	GrantedBy            *string
	GrantedAt            int64
	UpdatedAt            int64
	CoveredUsers         int64
	CoveredDisabledUsers int64
	UniqueUsers          int64
}

// OrgGrantInput 单条授权输入。
type OrgGrantInput struct {
	OrgID      string
	IncludeSub bool
}

// OrgGrantChange 一条授权在本次提交中的变化（供审计按条记录，spec FR-024）。
type OrgGrantChange struct {
	OrgID string
	// Kind 为 added / updated / unchanged。
	Kind string
	From bool
	To   bool
}

// OrgGrantRefRow 成员命中的某条组织授权（成员来源列）。
type OrgGrantRefRow struct {
	UserID     string
	OrgID      string
	DeptName   string
	IncludeSub bool
}

// OrgGrantHintRow 存量迁移提示：某部门在本应用中的成员数已达阈值且该部门未被授权。
type OrgGrantHintRow struct {
	OrgID       string
	DeptName    string
	MemberCount int64
}

// placeholders 生成 n 个逗号分隔的绑定占位符（用于 IN 列表，避免手工拼接值）。
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ListOrgGrants 列出应用的全部组织授权，并为每条算出覆盖人数 / 停用覆盖数 / 独占人数。
// 三者共用同一判定片段，只差在被统计的用户集合（spec FR-004/FR-005/FR-007）。
func (s *Store) ListOrgGrants(ctx context.Context, clientID string) ([]OrgGrantRow, error) {
	const covered = `SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id
		WHERE u.status = 1 AND ` + `%MATCH%`
	const disabled = `SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id
		WHERE u.status <> 1 AND ` + `%MATCH%`
	const unique = `SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id
		WHERE u.status = 1 AND %MATCH%
		  AND NOT EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = g.client_id AND m.user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM client_org_grants g2 JOIN organizations go2 ON go2.id = g2.org_id
		                  WHERE g2.client_id = g.client_id AND g2.org_id <> g.org_id
		                    AND ((g2.include_sub = 1 AND o.path LIKE CONCAT(go2.path, '%'))
		                      OR (g2.include_sub = 0 AND o.id = g2.org_id)))`

	match := model.OrgGrantMatchPlanSQL("g", "o")
	sub := func(tpl string) string { return strings.Replace(tpl, "%MATCH%", match, 1) }

	sql := `SELECT g.org_id AS org_id, go.name AS dept_name, go.path AS dept_path,
	   g.include_sub AS include_sub, g.granted_by AS granted_by,
	   g.granted_at AS granted_at, g.updated_at AS updated_at,
	   (` + sub(covered) + `) AS covered_users,
	   (` + sub(disabled) + `) AS covered_disabled_users,
	   (` + sub(unique) + `) AS unique_users
	FROM client_org_grants g
	JOIN organizations go ON go.id = g.org_id
	WHERE g.client_id = ?
	ORDER BY go.sort ASC, go.created_at ASC, g.granted_at ASC`

	var rows []OrgGrantRow
	if err := s.DB(ctx).Raw(sql, clientID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// AddOrgGrants 批量新增/调整组织授权（幂等）。
// 事务内先按 org_id 升序锁部门行，避免"校验后部门被并发删除"留下悬空 org_id；
// 上限判定与写入在同一事务内完成，因此不存在并发超限。
// 返回 added / updated（仅 includeSub 真变化）/ unchanged 三态计数。
func (s *Store) AddOrgGrants(ctx context.Context, clientID string, inputs []OrgGrantInput, actorID *string, now int64) (added, updated, unchanged int, changes []OrgGrantChange, err error) {
	if len(inputs) == 0 {
		return 0, 0, 0, nil, nil
	}
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)

		// 逐个部门加行锁（inputs 已在 service 层按 orgId 去重）：既校验存在性，
		// 也避免"校验后部门被并发删除"留下悬空 org_id。
		for _, in := range inputs {
			if _, e := s.LockOrganization(ctx, in.OrgID); e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return ErrOrgGrantDeptNotFound
				}
				return e
			}
		}

		var existing int64
		if e := db.Model(&model.ClientOrgGrant{}).Where("client_id = ?", clientID).Count(&existing).Error; e != nil {
			return e
		}

		toInsert := 0
		type pending struct {
			in        OrgGrantInput
			hasRow    bool
			oldSub    bool
			grantedAt int64
		}
		plans := make([]pending, 0, len(inputs))
		for _, in := range inputs {
			var row model.ClientOrgGrant
			e := db.Where("client_id = ? AND org_id = ?", clientID, in.OrgID).Take(&row).Error
			switch {
			case e == nil:
				plans = append(plans, pending{in: in, hasRow: true, oldSub: row.IncludeSub, grantedAt: row.GrantedAt})
			case errors.Is(e, gorm.ErrRecordNotFound):
				toInsert++
				plans = append(plans, pending{in: in})
			default:
				return e
			}
		}
		if existing+int64(toInsert) > MaxOrgGrantsPerClient {
			return ErrOrgGrantLimit
		}

		for _, p := range plans {
			switch {
			case !p.hasRow:
				row := model.ClientOrgGrant{
					ClientID: clientID, OrgID: p.in.OrgID, IncludeSub: p.in.IncludeSub,
					GrantedBy: actorID, GrantedAt: now, UpdatedAt: now,
				}
				if e := db.Create(&row).Error; e != nil {
					return e
				}
				added++
				changes = append(changes, OrgGrantChange{OrgID: p.in.OrgID, Kind: "added", To: p.in.IncludeSub})
			case p.oldSub != p.in.IncludeSub:
				if e := db.Model(&model.ClientOrgGrant{}).
					Where("client_id = ? AND org_id = ?", clientID, p.in.OrgID).
					Updates(map[string]any{"include_sub": p.in.IncludeSub, "updated_at": now}).Error; e != nil {
					return e
				}
				updated++
				changes = append(changes, OrgGrantChange{
					OrgID: p.in.OrgID, Kind: "updated", From: p.oldSub, To: p.in.IncludeSub,
				})
			default:
				// 完全幂等：不刷新 updated_at，也不产生审计事件（spec FR-003）。
				unchanged++
				changes = append(changes, OrgGrantChange{OrgID: p.in.OrgID, Kind: "unchanged", From: p.oldSub, To: p.in.IncludeSub})
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, nil, err
	}
	return added, updated, unchanged, changes, nil
}

// RemoveOrgGrants 撤销组织授权。**先算后删**：impactUsers / remainingVisibleUsers 必须在
// 删除之前、同一事务内计算，否则两个值都会退化为 0（critique E5）。
func (s *Store) RemoveOrgGrants(ctx context.Context, clientID string, orgIDs []string) (removed, impactUsers, remainingVisibleUsers int, err error) {
	if len(orgIDs) == 0 {
		return 0, 0, 0, nil
	}
	err = s.Transaction(ctx, func(ctx context.Context) error {
		db := s.DB(ctx)
		ph := placeholders(len(orgIDs))

		matchTarget := model.OrgGrantMatchSQL("g", "go", "o")
		matchOther := model.OrgGrantMatchSQL("g2", "go2", "o")

		impactSQL := `SELECT
			COALESCE(SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = ? AND m.user_id = u.id)
			                   AND NOT EXISTS (SELECT 1 FROM client_org_grants g2 JOIN organizations go2 ON go2.id = g2.org_id
			                                   WHERE g2.client_id = ? AND g2.org_id NOT IN (` + ph + `) AND ` + matchOther + `)
			             THEN 1 ELSE 0 END), 0) AS impact_users,
			COALESCE(SUM(CASE WHEN EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = ? AND m.user_id = u.id)
			                   OR EXISTS (SELECT 1 FROM client_org_grants g2 JOIN organizations go2 ON go2.id = g2.org_id
			                              WHERE g2.client_id = ? AND g2.org_id NOT IN (` + ph + `) AND ` + matchOther + `)
			             THEN 1 ELSE 0 END), 0) AS remaining_users
		FROM users u
		JOIN organizations o ON o.id = u.dept_id
		WHERE u.status = 1
		  AND EXISTS (SELECT 1 FROM client_org_grants g JOIN organizations go ON go.id = g.org_id
		              WHERE g.client_id = ? AND g.org_id IN (` + ph + `) AND ` + matchTarget + `)`

		args := []any{clientID, clientID}
		args = append(args, toAnySlice(orgIDs)...)
		args = append(args, clientID, clientID)
		args = append(args, toAnySlice(orgIDs)...)
		args = append(args, clientID)
		args = append(args, toAnySlice(orgIDs)...)

		var sum struct {
			ImpactUsers    int64
			RemainingUsers int64
		}
		if e := db.Raw(impactSQL, args...).Scan(&sum).Error; e != nil {
			return e
		}

		delArgs := make([]any, 0, len(orgIDs)+1)
		delArgs = append(delArgs, clientID)
		delArgs = append(delArgs, toAnySlice(orgIDs)...)
		res := db.Exec(`DELETE FROM client_org_grants WHERE client_id = ? AND org_id IN (`+ph+`)`, delArgs...)
		if res.Error != nil {
			return res.Error
		}
		removed = int(res.RowsAffected)
		impactUsers = int(sum.ImpactUsers)
		remainingVisibleUsers = int(sum.RemainingUsers)
		return nil
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return removed, impactUsers, remainingVisibleUsers, nil
}

// HasOrgGrants 判断部门是否仍被任何应用的组织授权引用（部门删除保护，spec FR-028）。
func (s *Store) HasOrgGrants(ctx context.Context, orgID string) (bool, error) {
	var n int64
	err := s.DB(ctx).Model(&model.ClientOrgGrant{}).Where("org_id = ?", orgID).Count(&n).Error
	return n > 0, err
}

// CountOrgGrants 统计单应用的组织授权条数。
func (s *Store) CountOrgGrants(ctx context.Context, clientID string) (int64, error) {
	var n int64
	err := s.DB(ctx).Model(&model.ClientOrgGrant{}).Where("client_id = ?", clientID).Count(&n).Error
	return n, err
}

// CountVisibleUsers 统计应用的实际可见人数（手动成员 ∪ 组织授权覆盖，去重，仅在职启用）。
// 只在成员列表 page=1 时调用（critique E9）。
func (s *Store) CountVisibleUsers(ctx context.Context, clientID string) (int64, error) {
	match := model.OrgGrantMatchSQL("g", "go", "o")
	sql := `SELECT COUNT(*) FROM users u
	JOIN organizations o ON o.id = u.dept_id
	WHERE u.status = 1
	  AND (EXISTS (SELECT 1 FROM client_members m WHERE m.client_id = ? AND m.user_id = u.id)
	    OR EXISTS (SELECT 1 FROM client_org_grants g JOIN organizations go ON go.id = g.org_id
	               WHERE g.client_id = ? AND ` + match + `))`
	var n int64
	err := s.DB(ctx).Raw(sql, clientID, clientID).Scan(&n).Error
	return n, err
}

// CountUsersCoveredByOrgs 统计「被给定授权中任意一条覆盖」的在职启用账号数（多授权去重）。
// 用于 POST 组织授权的响应回执（contracts GT-2 的 coveredUsers）。
func (s *Store) CountUsersCoveredByOrgs(ctx context.Context, clientID string, orgIDs []string) (int64, error) {
	if len(orgIDs) == 0 {
		return 0, nil
	}
	match := model.OrgGrantMatchSQL("g", "go", "o")
	ph := placeholders(len(orgIDs))
	sql := `SELECT COUNT(*) FROM users u JOIN organizations o ON o.id = u.dept_id
	WHERE u.status = 1 AND EXISTS (
	  SELECT 1 FROM client_org_grants g JOIN organizations go ON go.id = g.org_id
	  WHERE g.client_id = ? AND g.org_id IN (` + ph + `) AND ` + match + `)`
	args := []any{clientID}
	args = append(args, toAnySlice(orgIDs)...)
	var n int64
	err := s.DB(ctx).Raw(sql, args...).Scan(&n).Error
	return n, err
}

// ListMemberOrgGrants 返回当页成员命中的组织授权（一条查询，无 N+1）。
// userIDs 由调用方限制在当页范围内（≤50）。
func (s *Store) ListMemberOrgGrants(ctx context.Context, clientID string, userIDs []string) ([]OrgGrantRefRow, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	match := model.OrgGrantMatchSQL("g", "go", "d")
	sql := `SELECT m.user_id AS user_id, g.org_id AS org_id, go.name AS dept_name, g.include_sub AS include_sub
	FROM client_members m
	JOIN users u ON u.id = m.user_id
	JOIN organizations d ON d.id = u.dept_id
	JOIN client_org_grants g ON g.client_id = m.client_id
	JOIN organizations go ON go.id = g.org_id
	WHERE m.client_id = ? AND m.user_id IN (` + placeholders(len(userIDs)) + `) AND ` + match + `
	ORDER BY go.sort ASC, go.created_at ASC`
	args := make([]any, 0, len(userIDs)+1)
	args = append(args, clientID)
	args = append(args, toAnySlice(userIDs)...)

	var rows []OrgGrantRefRow
	if err := s.DB(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListOrgGrantHints 返回"可改为组织授权"的只读提示（spec FR-031）：
// 某部门在本应用中的在职成员数 ≥ threshold，且该部门及其全部祖先均未被授权。
func (s *Store) ListOrgGrantHints(ctx context.Context, clientID string, threshold int, limit int) ([]OrgGrantHintRow, error) {
	db := s.DB(ctx)

	type deptCount struct {
		DeptID string
		N      int64
	}
	var counts []deptCount
	if err := db.Raw(`SELECT u.dept_id AS dept_id, COUNT(*) AS n
	FROM client_members m JOIN users u ON u.id = m.user_id
	WHERE m.client_id = ? AND u.status = 1
	GROUP BY u.dept_id
	HAVING COUNT(*) >= ?
	ORDER BY n DESC
	LIMIT 50`, clientID, threshold).Scan(&counts).Error; err != nil {
		return nil, err
	}
	if len(counts) == 0 {
		return nil, nil
	}

	var grants []model.ClientOrgGrant
	if err := db.Where("client_id = ?", clientID).Find(&grants).Error; err != nil {
		return nil, err
	}
	subGranted := map[string]bool{}
	exactGranted := map[string]bool{}
	for _, g := range grants {
		if g.IncludeSub {
			subGranted[g.OrgID] = true
		}
		exactGranted[g.OrgID] = true
	}

	ids := make([]string, 0, len(counts))
	for _, c := range counts {
		ids = append(ids, c.DeptID)
	}
	var orgs []model.Organization
	if err := db.Where("id IN ("+placeholders(len(ids))+")", toAnySlice(ids)...).Find(&orgs).Error; err != nil {
		return nil, err
	}
	nameByID := make(map[string]string, len(orgs))
	pathByID := make(map[string]string, len(orgs))
	for _, o := range orgs {
		nameByID[o.ID] = o.Name
		pathByID[o.ID] = o.Path
	}

	out := make([]OrgGrantHintRow, 0, limit)
	for _, c := range counts {
		path := pathByID[c.DeptID]
		if path == "" {
			continue
		}
		covered := false
		for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
			if seg == "" {
				continue
			}
			if subGranted[seg] {
				covered = true
				break
			}
			if seg == c.DeptID && exactGranted[seg] {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		out = append(out, OrgGrantHintRow{OrgID: c.DeptID, DeptName: nameByID[c.DeptID], MemberCount: c.N})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// toAnySlice 把 []string 转为 []any，便于拼接绑定参数。
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
