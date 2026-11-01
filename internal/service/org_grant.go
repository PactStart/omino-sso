package service

import (
	"context"
	"errors"

	"github.com/omino/omino-sso/internal/store"
)

// 组织授权（010-app-access-org-grant）：把应用的门户可见性授予部门（可含子部门）。
// 本文件只做参数校验与错误映射；判定口径全部在 store 层（唯一实现，spec FR-004）。
// 注意：本特性是「门户可见性」，不是访问控制 —— 不参与 OIDC 授权判定（spec FR-029）。

var (
	// ErrOrgGrantLimit 单应用组织授权条数超过上限（spec FR-010）。
	ErrOrgGrantLimit = errors.New("org grant limit exceeded")
	// ErrOrgHasGrants 部门仍被组织授权引用，禁止删除（spec FR-028）。
	ErrOrgHasGrants = errors.New("org has grants")
)

// 组织授权上限（与 store.MaxOrgGrantsPerClient 保持一致）。
const (
	maxOrgGrantBatch  = 500
	maxOrgGrantTotal  = store.MaxOrgGrantsPerClient
	orgGrantHintLimit = 3
	orgGrantHintMin   = 20
)

// OrgGrantInput 单条授权输入。
type OrgGrantInput struct {
	OrgID      string
	IncludeSub bool
}

// OrgGrantItem 组织授权视图（含覆盖人数、停用覆盖数、独占人数）。
type OrgGrantItem struct {
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

// OrgGrantRef 成员命中的某条组织授权（成员来源列，spec FR-016）。
type OrgGrantRef struct {
	OrgID      string
	DeptName   string
	IncludeSub bool
}

// OrgGrantHint 存量迁移提示（spec FR-031）。
type OrgGrantHint struct {
	OrgID       string
	DeptName    string
	MemberCount int64
}

// ListOrgGrants 列出应用的组织授权。
func (s *AppAuthService) ListOrgGrants(ctx context.Context, clientID string) ([]OrgGrantItem, error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return nil, err
	}
	rows, err := s.st.ListOrgGrants(ctx, clientID)
	if err != nil {
		return nil, err
	}
	out := make([]OrgGrantItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, OrgGrantItem{
			OrgID: r.OrgID, DeptName: r.DeptName, DeptPath: r.DeptPath, IncludeSub: r.IncludeSub,
			GrantedBy: r.GrantedBy, GrantedAt: r.GrantedAt, UpdatedAt: r.UpdatedAt,
			CoveredUsers: r.CoveredUsers, CoveredDisabledUsers: r.CoveredDisabledUsers, UniqueUsers: r.UniqueUsers,
		})
	}
	return out, nil
}

// AddOrgGrants 新增/调整组织授权（幂等）。返回三态计数、本次涉及部门的合计覆盖人数与逐条变化。
func (s *AppAuthService) AddOrgGrants(ctx context.Context, clientID, actorID string, inputs []OrgGrantInput) (added, updated, unchanged int, coveredUsers int64, changes []store.OrgGrantChange, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return 0, 0, 0, 0, nil, err
	}
	dedup, ok := dedupOrgGrants(inputs)
	if !ok {
		return 0, 0, 0, 0, nil, ErrAppValidation
	}
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	storeInputs := make([]store.OrgGrantInput, 0, len(dedup))
	for _, d := range dedup {
		storeInputs = append(storeInputs, store.OrgGrantInput{OrgID: d.OrgID, IncludeSub: d.IncludeSub})
	}
	added, updated, unchanged, changes, err = s.st.AddOrgGrants(ctx, clientID, storeInputs, actor, nowMillis())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrOrgGrantLimit):
			return 0, 0, 0, 0, nil, ErrOrgGrantLimit
		case errors.Is(err, store.ErrOrgGrantDeptNotFound):
			return 0, 0, 0, 0, nil, ErrAppDeptNotFound
		default:
			return 0, 0, 0, 0, nil, err
		}
	}
	orgIDs := make([]string, 0, len(dedup))
	for _, d := range dedup {
		orgIDs = append(orgIDs, d.OrgID)
	}
	coveredUsers, err = s.st.CountUsersCoveredByOrgs(ctx, clientID, orgIDs)
	if err != nil {
		return 0, 0, 0, 0, nil, err
	}
	return added, updated, unchanged, coveredUsers, changes, nil
}

// RemoveOrgGrants 撤销组织授权。impactUsers 为「撤销将导致多少人不再可见」（删除前计算）。
func (s *AppAuthService) RemoveOrgGrants(ctx context.Context, clientID string, orgIDs []string) (removed, impactUsers, remainingVisibleUsers int, err error) {
	if err := s.ensureClient(ctx, clientID); err != nil {
		return 0, 0, 0, err
	}
	ids := dedupStrings(orgIDs)
	if len(ids) == 0 || len(ids) > maxOrgGrantBatch {
		return 0, 0, 0, ErrAppValidation
	}
	return s.st.RemoveOrgGrants(ctx, clientID, ids)
}

// OrgGrantHints 存量迁移提示：部门快照成员数达阈值且该部门未被授权（spec FR-031）。
func (s *AppAuthService) OrgGrantHints(ctx context.Context, clientID string) ([]OrgGrantHint, error) {
	rows, err := s.st.ListOrgGrantHints(ctx, clientID, orgGrantHintMin, orgGrantHintLimit)
	if err != nil {
		return nil, err
	}
	out := make([]OrgGrantHint, 0, len(rows))
	for _, r := range rows {
		out = append(out, OrgGrantHint{OrgID: r.OrgID, DeptName: r.DeptName, MemberCount: r.MemberCount})
	}
	return out, nil
}

// VisibleUserCount 应用的实际可见人数（成员 ∪ 组织授权覆盖，去重）。
func (s *AppAuthService) VisibleUserCount(ctx context.Context, clientID string) (int64, error) {
	return s.st.CountVisibleUsers(ctx, clientID)
}

// OrgGrantLimit 暴露上限给 handler 做提示文案。
func OrgGrantLimit() int { return maxOrgGrantTotal }

// dedupOrgGrants 去重（同一 orgId 只保留最后一次的 includeSubDepts）并校验批量上限。
func dedupOrgGrants(in []OrgGrantInput) ([]OrgGrantInput, bool) {
	if len(in) == 0 || len(in) > maxOrgGrantBatch {
		return nil, false
	}
	idx := make(map[string]int, len(in))
	out := make([]OrgGrantInput, 0, len(in))
	for _, g := range in {
		if g.OrgID == "" {
			return nil, false
		}
		if i, ok := idx[g.OrgID]; ok {
			out[i] = g
			continue
		}
		idx[g.OrgID] = len(out)
		out = append(out, g)
	}
	return out, true
}
