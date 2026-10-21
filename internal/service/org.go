package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 组织架构的固定规则（spec FR-019 / data-model §5）。
const (
	// MaxOrgDepth 组织层级上限，根级为第 1 级。
	MaxOrgDepth = 10
	// OrgDepthExceededMessage 层级超限的对外固定提示。
	OrgDepthExceededMessage = "层级不能超过 10 级"
	// DefaultOrgTenant 根级节点固定归属的租户。
	DefaultOrgTenant = "default"
	// maxOrgNameLen 与 organizations.name 的 VARCHAR(64) 一致。
	maxOrgNameLen = 64
	// maxOrgPathLen 与 organizations.path 的 VARCHAR(512) 一致；
	// 10 级上限下最长约 271 字符，此处仅作内部断言。
	maxOrgPathLen = 512
)

// 组织写操作的哨兵错误，由 API 层映射为登记的错误码（data-model §6）。
var (
	ErrOrgNotFound      = errors.New("organization not found")
	ErrOrgInvalidName   = errors.New("invalid organization name")
	ErrOrgInvalidSort   = errors.New("invalid organization sort")
	ErrOrgDepthExceeded = errors.New("organization depth exceeded")
	ErrOrgNameExists    = errors.New("organization name already exists")
	ErrOrgInvalidParent = errors.New("invalid parent organization")
	ErrOrgHasChildren   = errors.New("organization still has children")
	ErrOrgHasUsers      = errors.New("organization still has users")
)

// OrgService 组织架构写操作（业务规则层，不依赖 gin；审计由 API 层投递）。
type OrgService struct {
	st  *store.Store
	log *slog.Logger
}

// NewOrgService 构造。
func NewOrgService(st *store.Store, logger *slog.Logger) *OrgService {
	return &OrgService{st: st, log: logger}
}

// CreateOrgInput 创建组织节点入参。租户不接受客户端指定：由父链继承，根级为 default。
type CreateOrgInput struct {
	ParentID *string
	Name     string
	Sort     *int
}

// UpdateOrgInput 局部更新入参。ParentID 仅在 ParentIDSet 为 true 时生效，
// 此时 ParentID == nil 表示显式清空（移到根级）。
type UpdateOrgInput struct {
	Name        *string
	Sort        *int
	ParentID    *string
	ParentIDSet bool
}

// NormalizeOrgName 去除首尾空白（入库与比较前统一处理）。
func NormalizeOrgName(s string) string { return strings.TrimSpace(s) }

// ValidateOrgName 校验并返回规范化后的名称：trim 后 1–64 个字符，禁止控制字符。
func ValidateOrgName(raw string) (string, error) {
	name := NormalizeOrgName(raw)
	if name == "" {
		return "", ErrOrgInvalidName
	}
	if utf8.RuneCountInString(name) > maxOrgNameLen {
		return "", ErrOrgInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrOrgInvalidName
		}
	}
	return name, nil
}

// NormalizeOrgSort 校验排序值：缺省 0，必须落在 int32 范围（列类型为 INT）。
func NormalizeOrgSort(v *int) (int, error) {
	if v == nil {
		return 0, nil
	}
	if *v < math.MinInt32 || *v > math.MaxInt32 {
		return 0, ErrOrgInvalidSort
	}
	return *v, nil
}

// Create 创建组织节点。
// 事务内顺序固定为"锁 → 读 → 校验 → 写"（contracts §0）。
func (s *OrgService) Create(ctx context.Context, in CreateOrgInput) (*model.Organization, error) {
	name, err := ValidateOrgName(in.Name)
	if err != nil {
		return nil, err
	}
	sort, err := NormalizeOrgSort(in.Sort)
	if err != nil {
		return nil, err
	}
	wantParent := normalizeOrgID(in.ParentID)

	var created *model.Organization
	err = s.withWriteTx(ctx, func(ctx context.Context) error {
		orgs, err := s.st.LockAllOrganizations(ctx)
		if err != nil {
			return err
		}
		index := indexOrgs(orgs)

		var parent *model.Organization
		if wantParent != nil {
			p, ok := index[*wantParent]
			if !ok {
				return ErrOrgInvalidParent
			}
			parent = p
		}

		tenant := DefaultOrgTenant
		parentPath := ""
		var parentID *string
		depth := 1
		if parent != nil {
			tenant = parent.TenantID
			if tenant == "" {
				tenant = DefaultOrgTenant
			}
			parentPath = parent.Path
			pid := parent.ID
			parentID = &pid
			depth = model.OrgDepth(parent.Path) + 1
		}
		if depth > MaxOrgDepth {
			return ErrOrgDepthExceeded
		}

		if _, err := s.st.GetOrganizationByName(ctx, tenant, parentID, name, ""); err == nil {
			return ErrOrgNameExists
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		newID := id.New()
		path := model.JoinOrgPath(parentPath, newID)
		if len(path) > maxOrgPathLen {
			// 10 级上限下不可达；命中说明 path 规则被破坏。
			return fmt.Errorf("organization path overflow: len=%d", len(path))
		}
		o := &model.Organization{
			ID:        newID,
			ParentID:  parentID,
			Name:      name,
			Path:      path,
			Sort:      sort,
			TenantID:  tenant,
			CreatedAt: nowMillis(),
		}
		if err := s.st.CreateOrganization(ctx, o); err != nil {
			return err
		}
		created = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 局部更新组织节点（重命名 / 调序 / 移动）。
// 未提交的字段保持不变；Name/Sort/ParentID 全部未提交时不执行任何 UPDATE（幂等）。
func (s *OrgService) Update(ctx context.Context, orgID string, in UpdateOrgInput) (*model.Organization, error) {
	var name string
	if in.Name != nil {
		n, err := ValidateOrgName(*in.Name)
		if err != nil {
			return nil, err
		}
		name = n
	}
	var sort int
	if in.Sort != nil {
		v, err := NormalizeOrgSort(in.Sort)
		if err != nil {
			return nil, err
		}
		sort = v
	}
	wantParent := normalizeOrgID(in.ParentID)

	var updated *model.Organization
	err := s.withWriteTx(ctx, func(ctx context.Context) error {
		orgs, err := s.st.LockAllOrganizations(ctx)
		if err != nil {
			return err
		}
		index := indexOrgs(orgs)
		cur, ok := index[orgID]
		if !ok {
			return ErrOrgNotFound
		}

		fields := map[string]any{}
		next := *cur

		if in.Name != nil && name != cur.Name {
			if _, err := s.st.GetOrganizationByName(ctx, cur.TenantID, cur.ParentID, name, cur.ID); err == nil {
				return ErrOrgNameExists
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			fields["name"] = name
			next.Name = name
		}
		if in.Sort != nil && sort != cur.Sort {
			fields["sort"] = sort
			next.Sort = sort
		}

		if in.ParentIDSet && !sameOrgParent(cur.ParentID, wantParent) {
			var newParent *model.Organization
			if wantParent != nil {
				p, ok := index[*wantParent]
				if !ok {
					return ErrOrgInvalidParent
				}
				// 自身与后代都不能作为新上级（成环）。
				if model.IsPathPrefix(cur.Path, p.Path) {
					return ErrOrgInvalidParent
				}
				// 租户不可由客户端指定：跨租户挂接在接口层不可表达。
				if p.TenantID != cur.TenantID {
					return ErrOrgInvalidParent
				}
				newParent = p
			}

			newDepth, parentPath := 1, ""
			if newParent != nil {
				newDepth = model.OrgDepth(newParent.Path) + 1
				parentPath = newParent.Path
			}
			if newDepth+subtreeHeight(cur.Path, orgs)-1 > MaxOrgDepth {
				return ErrOrgDepthExceeded
			}

			oldPrefix := cur.Path
			newPrefix := model.JoinOrgPath(parentPath, cur.ID)
			if len(newPrefix) > maxOrgPathLen {
				return fmt.Errorf("organization path overflow: len=%d", len(newPrefix))
			}
			if _, err := s.st.UpdateSubtreePaths(ctx, oldPrefix, newPrefix); err != nil {
				return err
			}
			// 子树路径已搬迁，再改自身 parent_id（map 中显式 nil 会写 SQL NULL）。
			fields["parent_id"] = wantParent
			next.ParentID = wantParent
			next.Path = newPrefix
		}

		if len(fields) == 0 {
			updated = &next
			return nil
		}
		if err := s.st.UpdateOrganizationFields(ctx, cur.ID, fields); err != nil {
			return err
		}
		updated = &next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete 物理删除组织节点：仅当既无子部门、也无直属账号时允许。
func (s *OrgService) Delete(ctx context.Context, orgID string) error {
	return s.withWriteTx(ctx, func(ctx context.Context) error {
		orgs, err := s.st.LockAllOrganizations(ctx)
		if err != nil {
			return err
		}
		if _, ok := indexOrgs(orgs)[orgID]; !ok {
			return ErrOrgNotFound
		}
		hasChildren, err := s.st.HasChildOrganizations(ctx, orgID)
		if err != nil {
			return err
		}
		if hasChildren {
			return ErrOrgHasChildren
		}
		n, err := s.st.CountUsersByDeptID(ctx, orgID)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrOrgHasUsers
		}
		return s.st.DeleteOrganization(ctx, orgID)
	})
}

// Tree 返回构建组织树所需的全部节点与每个节点的**直属**人数
// （树节点的 userCount 语义为直属人数，与既有响应保持一致；
// 子树汇总只出现在写操作的详情响应中）。
func (s *OrgService) Tree(ctx context.Context, tenantID string) ([]model.Organization, map[string]int64, error) {
	orgs, err := s.st.ListOrganizations(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	counts, err := s.st.CountUsersByDept(ctx)
	if err != nil {
		return nil, nil, err
	}
	return orgs, counts, nil
}

// Name 返回组织节点名称；节点不存在时返回空串。
// 仅供 API 层在写操作前取审计摘要用，不参与业务判定。
func (s *OrgService) Name(ctx context.Context, orgID string) string {
	o, err := s.st.GetOrganization(ctx, orgID)
	if err != nil {
		return ""
	}
	return o.Name
}

// UserCountInSubtree 返回该节点及其全部子部门的账号数（用于详情响应）。
func (s *OrgService) UserCountInSubtree(ctx context.Context, orgID string) (int64, error) {
	o, err := s.st.GetOrganization(ctx, orgID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrOrgNotFound
		}
		return 0, err
	}
	return s.st.CountUsersInSubtree(ctx, o.Path)
}

// withWriteTx 用一条事务包裹组织写操作。
// 空表上并发创建会因 supremum 间隙锁兼容而互相升级为死锁（1213），
// 或长时间等待（1205），故对这两种错误重试一次；重试对调用方不可见，
// 重试后仍失败才向上抛出（由 API 层映射为 500）。
func (s *OrgService) withWriteTx(ctx context.Context, fn func(ctx context.Context) error) error {
	const maxAttempts = 2
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = s.st.Transaction(ctx, fn)
		if err == nil || !isRetryableTxError(err) {
			return err
		}
		if attempt+1 < maxAttempts {
			s.log.Warn("organization write retry after lock conflict", "err", err)
		}
	}
	return err
}

// isRetryableTxError 判定 MySQL 死锁（1213）与锁等待超时（1205）。
func isRetryableTxError(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1213 || me.Number == 1205
	}
	return false
}

func indexOrgs(orgs []model.Organization) map[string]*model.Organization {
	out := make(map[string]*model.Organization, len(orgs))
	for i := range orgs {
		out[orgs[i].ID] = &orgs[i]
	}
	return out
}

// subtreeHeight 返回以 prefix 为根的子树高度（仅自身时为 1）。
func subtreeHeight(prefix string, orgs []model.Organization) int {
	base := model.OrgDepth(prefix)
	height := 1
	for i := range orgs {
		if !model.IsPathPrefix(prefix, orgs[i].Path) {
			continue
		}
		if h := model.OrgDepth(orgs[i].Path) - base + 1; h > height {
			height = h
		}
	}
	return height
}

// normalizeOrgID 把空串与仅空白的父节点标识视为"无父节点"（根级）。
func normalizeOrgID(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// sameOrgParent 判断父节点是否未变化（两者均指根级视为相同）。
func sameOrgParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
