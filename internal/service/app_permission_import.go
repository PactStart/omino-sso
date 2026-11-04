package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
	"github.com/omino/omino-sso/internal/store/model"
)

// 应用权限点批量导入 / 导出（specs/011-app-permission-import）。
//
// 口径（改动前先看 spec 与 research）：
//   - 层级上限直接复用 maxPermissionDepth（与单条创建一致），不另设导入专用上限；
//   - 写入在单个事务内完成，且先锁 clients 行把同一应用的导入串行化；
//   - 父级合法性由内存映射保证，不逐节点查库（否则 1000 条会放大成十万级查询）；
//   - 导出不复用 ListClientPermissions（其 LIMIT 1000 会静默截断）。
const (
	maxImportPermissions = 1000
	maxImportBodyBytes   = 512 << 10
	maxImportIssues      = 20
	maxImportMovedItems  = 20
)

// 导入来源（query 参数 ?source=，非法值按 paste 处理）。
const (
	ImportSourcePaste = "paste"
	ImportSourceFile  = "file"
)

// MaxImportBodyBytes 供 handler 构造 http.MaxBytesReader（spec FR-013：512KB）。
const MaxImportBodyBytes = maxImportBodyBytes

// MaxImportPermissions 供 handler 生成"本次共 N 条，超过单次上限 M 条"的文案（spec FR-013）。
const MaxImportPermissions = maxImportPermissions

// ImportTooLargeError 表示"内容太多"，并携带成因，供 handler 给出区分文案（错误码不变）。
// Count > 0 表示单次条数超限（文案须含实际条数）；Count == 0 表示请求体积超限
// （此刻无法完整解析，规范禁止声称知道条数，只能给上限与成因提示）。
type ImportTooLargeError struct {
	Count int
}

func (e *ImportTooLargeError) Error() string {
	if e.Count > 0 {
		return fmt.Sprintf("本次共 %d 条，超过单次上限 %d 条", e.Count, maxImportPermissions)
	}
	return fmt.Sprintf("导入内容超过 %dKB 上限", maxImportBodyBytes>>10)
}

// Is 让 errors.Is(err, ErrAppPermissionImportTooLarge) 继续成立，映射表无需改动。
func (e *ImportTooLargeError) Is(target error) bool {
	return target == ErrAppPermissionImportTooLarge
}

// 导入相关业务错误。
var (
	// ErrAppPermissionImportInvalid 内容不合法（结构/字段/编码/重复/超深/超长/空/语法错）。
	ErrAppPermissionImportInvalid = errors.New("app permission import invalid")
	// ErrAppPermissionImportTooLarge 请求体或条数超限（无法解析或需裁剪后重试）。
	ErrAppPermissionImportTooLarge = errors.New("app permission import too large")
)

// ImportOutcome 供 handler 写聚合审计事件（spec FR-023）。
type ImportOutcome struct {
	AddedIDs   []string
	AddedCount int
	DocSha256  string
}

// importNode 解析后的权限点节点（保留 path 供问题定位）。
type importNode struct {
	path        string
	code        string
	name        string
	description *string
	sortVal     int
	sortSet     bool
	parent      *importNode
	children    []*importNode
}

// importDoc 解析后的文档。
type importDoc struct {
	version  int
	mode     string
	roots    []*importNode
	ordered  []*importNode
	total    int
	maxDepth int
}

type importParser struct {
	issues    []dto.PermissionImportIssue
	warnings  []dto.PermissionImportIssue
	seenCode  map[string]string
	seenLower map[string]string
	ordered   []*importNode
	total     int
	maxDepth  int
}

func newImportParser() *importParser {
	return &importParser{
		seenCode:  map[string]string{},
		seenLower: map[string]string{},
	}
}

func (p *importParser) issue(path, field, code, msg string) {
	p.issues = append(p.issues, dto.PermissionImportIssue{Path: path, Field: field, Code: code, Message: msg})
}

func (p *importParser) warn(path, field, code, msg string) {
	p.warnings = append(p.warnings, dto.PermissionImportIssue{Path: path, Field: field, Code: code, Message: msg})
}

// parseImportDocument 解析导入文档。纯函数：不触库，可单测。
func parseImportDocument(data []byte) (*importDoc, []dto.PermissionImportIssue, []dto.PermissionImportIssue) {
	p := newImportParser()
	doc := &importDoc{version: 1, mode: "merge"}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		p.issue("", "", "empty", "内容为空，请粘贴或选择一个 JSON 文件")
		return doc, p.issues, p.warnings
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line := lineAt(data, se.Offset)
			p.issues = append(p.issues, dto.PermissionImportIssue{
				Path: "", Field: "", Code: "syntax_error",
				Message: fmt.Sprintf("内容不是合法的 JSON（第 %d 行附近）", line), Line: &line,
			})
			return doc, p.issues, p.warnings
		}
		p.issue("", "", "bad_type", "文档根必须是 JSON 对象（形如 {\"version\":1,\"permissions\":[...]}）")
		return doc, p.issues, p.warnings
	}

	// 顶层白名单（FR-006：未知字段一律拒绝）。
	for key := range root {
		switch key {
		case "version", "mode", "permissions":
		default:
			p.issue(key, key, "unknown_field", fmt.Sprintf("不支持的字段 %q，顶层只允许 version/mode/permissions", key))
		}
	}

	if raw, ok := root["version"]; ok {
		v, ok := parseInt(raw)
		if !ok || v != 1 {
			p.issue("version", "version", "bad_version", "仅支持 version 1")
		} else {
			doc.version = v
		}
	}
	if raw, ok := root["mode"]; ok {
		var mode string
		if err := json.Unmarshal(raw, &mode); err != nil {
			p.issue("mode", "mode", "bad_mode", "mode 只支持 \"merge\"")
		} else if mode != "merge" {
			msg := "mode 只支持 \"merge\""
			if mode == "replace" {
				msg = "暂不支持全量对齐（mode: \"replace\"），请使用 \"merge\""
			}
			p.issue("mode", "mode", "bad_mode", msg)
		} else {
			doc.mode = mode
		}
	}

	rawPerms, ok := root["permissions"]
	if !ok {
		p.issue("permissions", "permissions", "missing_field", "缺少 permissions 数组")
		return doc, p.issues, p.warnings
	}
	var perms []json.RawMessage
	if err := json.Unmarshal(rawPerms, &perms); err != nil {
		p.issue("permissions", "permissions", "bad_type", "permissions 必须是数组")
		return doc, p.issues, p.warnings
	}
	if len(perms) == 0 {
		p.issue("permissions", "permissions", "empty", "permissions 不能为空")
		return doc, p.issues, p.warnings
	}

	doc.roots = p.parseLevel(perms, "permissions", nil, 1)
	doc.ordered = p.ordered
	doc.total = p.total
	doc.maxDepth = p.maxDepth
	return doc, p.issues, p.warnings
}

// parseLevel 解析一层数组；depth 从 1（根级）开始。
func (p *importParser) parseLevel(items []json.RawMessage, pathPrefix string, parent *importNode, depth int) []*importNode {
	out := make([]*importNode, 0, len(items))
	for i, raw := range items {
		path := fmt.Sprintf("%s[%d]", pathPrefix, i)
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			p.issue(path, "", "bad_type", "该条权限点必须是 JSON 对象")
			continue
		}

		node := &importNode{path: path, parent: parent}

		for key := range obj {
			switch key {
			case "code", "name", "description", "sort", "children":
			default:
				p.issue(path+"."+key, key, "unknown_field",
					fmt.Sprintf("不支持的字段 %q，权限点只允许 code/name/description/sort/children", key))
			}
		}

		if v, ok := parseStringField(obj["code"]); ok {
			v = strings.TrimSpace(v)
			switch {
			case v == "":
				p.issue(path+".code", "code", "missing_field", "缺少 code")
			case len(v) > 64:
				p.issue(path+".code", "code", "too_long", fmt.Sprintf("code 最多 64 个字符，当前 %d", len(v)))
			case !permCodePattern.MatchString(v):
				p.issue(path+".code", "code", "invalid_code", "编码含非法字符，只允许字母、数字与 _.:-")
			default:
				node.code = v
			}
		} else if _, exists := obj["code"]; !exists {
			p.issue(path+".code", "code", "missing_field", "缺少 code")
		} else {
			p.issue(path+".code", "code", "bad_type", "code 必须是字符串")
		}

		if v, ok := parseStringField(obj["name"]); ok {
			v = strings.TrimSpace(v)
			switch {
			case v == "":
				p.issue(path+".name", "name", "missing_field", "缺少 name")
			case len(v) > 64:
				p.issue(path+".name", "name", "too_long", fmt.Sprintf("name 最多 64 字节（中文约 21 字），当前 %d 字节", len(v)))
			default:
				node.name = v
			}
		} else if _, exists := obj["name"]; !exists {
			p.issue(path+".name", "name", "missing_field", "缺少 name")
		} else {
			p.issue(path+".name", "name", "bad_type", "name 必须是字符串")
		}

		if rawDesc, exists := obj["description"]; exists {
			if v, ok := parseStringField(rawDesc); ok {
				v = strings.TrimSpace(v)
				if len(v) > 255 {
					p.issue(path+".description", "description", "too_long", fmt.Sprintf("description 最多 255 字节，当前 %d 字节", len(v)))
				} else if v != "" {
					node.description = &v
				}
			} else {
				p.issue(path+".description", "description", "bad_type", "description 必须是字符串")
			}
		}

		if rawSort, exists := obj["sort"]; exists {
			if v, ok := parseInt(rawSort); ok {
				node.sortVal = v
				node.sortSet = true
			} else {
				p.issue(path+".sort", "sort", "bad_type", "sort 必须是整数")
			}
		}

		// 跨层重复（FR-009）：同一文档内 code 必须全局唯一。
		if node.code != "" {
			if first, dup := p.seenCode[node.code]; dup {
				p.issue(path+".code", "code", "dup_code",
					fmt.Sprintf("编码 %q 重复：已出现在 %s，此处为 %s", node.code, first, path+".code"))
			} else {
				p.seenCode[node.code] = path + ".code"
			}
			lower := strings.ToLower(node.code)
			if first, dup := p.seenLower[lower]; dup && first != node.code {
				p.warn(path+".code", "code", "case_hint",
					fmt.Sprintf("与 %q 忽略大小写后相同，将作为两个不同权限点导入", first))
			} else if !dup {
				p.seenLower[lower] = node.code
			}
		}

		// 编码前缀只做提示，不阻断（FR-011）。
		if parent != nil && parent.code != "" && node.code != "" && !strings.HasPrefix(node.code, parent.code) {
			p.warn(path+".code", "code", "prefix_hint",
				fmt.Sprintf("编码未以父级编码 %q 为前缀，仅提示，不影响导入", parent.code))
		}

		// 深度（FR-012：与单条创建同一上限，超限即停，避免刷屏）。
		if depth > p.maxDepth {
			p.maxDepth = depth
		}
		if depth > maxPermissionDepth {
			p.issue(path, "", "too_deep",
				fmt.Sprintf("%s 的层级深度为 %d，超过上限 %d", path, depth, maxPermissionDepth))
			continue
		}

		p.total++
		p.ordered = append(p.ordered, node)

		if rawChildren, exists := obj["children"]; exists {
			var children []json.RawMessage
			if err := json.Unmarshal(rawChildren, &children); err != nil {
				p.issue(path+".children", "children", "bad_type", "children 必须是数组")
			} else if len(children) > 0 {
				node.children = p.parseLevel(children, path+".children", node, depth+1)
			}
		}

		out = append(out, node)
	}

	// 省略 sort 时按同级书写顺序分配 0..n-1（FR-008）。
	for i, n := range out {
		if !n.sortSet {
			n.sortVal = i
		}
	}
	return out
}

// planUpdate 一条待更新权限点的变更字段。
type planUpdate struct {
	id     string
	fields map[string]any
}

// importPlan 写入计划（父先子后）。
type importPlan struct {
	insert     []*model.ClientPermission
	update     []planUpdate
	addedIDs   []string
	unchanged  int
	moved      int
	movedItems []dto.PermissionImportMoved
	maxDepth   int
}

func (p *importPlan) added() int { return len(p.insert) }
func (p *importPlan) updated() int { return len(p.update) }

// buildImportPlan 依据输入树与库内现状生成写入计划。纯内存计算，无 DB 交互。
func buildImportPlan(doc *importDoc, existing []model.ClientPermission) *importPlan {
	plan := &importPlan{}
	byCode := make(map[string]model.ClientPermission, len(existing))
	codeByID := make(map[string]string, len(existing))
	for _, row := range existing {
		byCode[row.Code] = row
		codeByID[row.ID] = row.Code
	}

	type queueItem struct {
		node     *importNode
		parentID *string
	}
	queue := make([]queueItem, 0, len(doc.roots))
	for _, root := range doc.roots {
		queue = append(queue, queueItem{node: root, parentID: nil})
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		node := item.node
		// 先确定本节点 ID（既有行用库里 ID，新增行用 ULID），再把子级排入队列（父先子后）。
		if row, ok := byCode[node.code]; ok {
			curParentCode := ""
			if row.ParentID != nil {
				curParentCode = codeByID[*row.ParentID]
			}
			inParentCode := ""
			if node.parent != nil {
				inParentCode = node.parent.code
			}
			fields := map[string]any{}
			if row.Name != node.name {
				fields["name"] = node.name
			}
			if !sameDesc(row.Description, node.description) {
				if node.description == nil {
					fields["description"] = nil
				} else {
					fields["description"] = *node.description
				}
			}
			if row.Sort != node.sortVal {
				fields["sort"] = node.sortVal
			}
			moved := curParentCode != inParentCode
			if moved {
				if item.parentID == nil {
					fields["parent_id"] = nil
				} else {
					fields["parent_id"] = *item.parentID
				}
				plan.moved++
				if len(plan.movedItems) < maxImportMovedItems {
					plan.movedItems = append(plan.movedItems, dto.PermissionImportMoved{
						Code: node.code, FromParent: curParentCode, ToParent: inParentCode,
					})
				}
			}
			if len(fields) == 0 {
				plan.unchanged++
			} else {
				plan.update = append(plan.update, planUpdate{id: row.ID, fields: fields})
			}
			selfID := row.ID
			for _, child := range node.children {
				pid := selfID
				queue = append(queue, queueItem{node: child, parentID: &pid})
			}
			continue
		}

		now := nowMillis()
		perm := &model.ClientPermission{
			ID:          id.New(),
			ClientID:    "",
			ParentID:    item.parentID,
			Code:        node.code,
			Name:        node.name,
			Description: node.description,
			Sort:        node.sortVal,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		plan.insert = append(plan.insert, perm)
		plan.addedIDs = append(plan.addedIDs, perm.ID)
		if plan.maxDepth < depthOf(node) {
			plan.maxDepth = depthOf(node)
		}
		selfID := perm.ID
		for _, child := range node.children {
			pid := selfID
			queue = append(queue, queueItem{node: child, parentID: &pid})
		}
	}
	return plan
}

func depthOf(n *importNode) int {
	d := 1
	for cur := n.parent; cur != nil; cur = cur.parent {
		d++
	}
	return d
}

func sameDesc(a *string, b *string) bool {
	av, bv := "", ""
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	return av == bv
}

// lineAt 把 json.SyntaxError.Offset 换算成 1 起始行号。
func lineAt(data []byte, offset int64) int {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return strings.Count(string(data[:offset]), "\n") + 1
}

func parseInt(raw json.RawMessage) (int, bool) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	v, err := n.Int64()
	if err != nil {
		return 0, false
	}
	if v > int64(^uint(0)>>1) || v < -int64(^uint(0)>>1)-1 {
		return 0, false
	}
	return int(v), true
}

func parseStringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// ensureClientActive 校验应用存在且启用（FR-022）。
func (s *AppAuthService) ensureClientActive(ctx context.Context, clientID string) error {
	c, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrClientNotFound
		}
		return err
	}
	if c.Status != 1 {
		return ErrAppClientDisabled
	}
	return nil
}

// AuthorizeImportConfirm 门户「确认导入」的鉴权（FR-021 / FR-025）。
//
// 与 AuthorizeAppAdmin 只差一处：应用停用时**不**在这里短路，而是把 ErrAppClientDisabled
// 交回 handler，由 handler 先写 1 条失败审计、再统一返回通用 403。
// 原因：FR-025 要求「应用不存在 / 已停用」这类业务拒绝可追溯，而门户的 AppAdmin 中间件
// 会在进入 handler 前就拒绝，导致门户侧永远写不出 failure 事件（admin 端会写）。
// 停用时分两步判：先确认调用者确实是该应用的管理员，避免任意登录用户把"某应用已停用"写进审计。
func (s *AppAuthService) AuthorizeImportConfirm(ctx context.Context, clientID, userID string) error {
	if clientID == "" || userID == "" {
		return ErrAppMemberNotFound
	}
	c, err := s.st.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrClientNotFound
		}
		return err
	}
	_, m, err := s.st.GetClientMembership(ctx, clientID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAppMemberNotFound
		}
		return err
	}
	if m == nil || !m.IsAdmin {
		return ErrAppNotAdmin
	}
	if c.Status != 1 {
		return ErrAppClientDisabled
	}
	return nil
}

// ValidateImport 校验预览：只读，不写权限点、不写审计（FR-002）。
func (s *AppAuthService) ValidateImport(ctx context.Context, clientID string, data []byte) (*dto.PermissionImportPreview, error) {
	if len(data) == 0 {
		return &dto.PermissionImportPreview{
			Ok:         false,
			Issues:     []dto.PermissionImportIssue{{Path: "", Field: "", Code: "empty", Message: "内容为空，请粘贴或选择一个 JSON 文件"}},
			IssueTotal: 1,
			Warnings:   []dto.PermissionImportIssue{},
			MovedItems: []dto.PermissionImportMoved{},
		}, nil
	}
	if len(data) > maxImportBodyBytes {
		return nil, &ImportTooLargeError{}
	}
	if err := s.ensureClientActive(ctx, clientID); err != nil {
		return nil, err
	}

	doc, issues, warnings := parseImportDocument(data)
	if doc.total > maxImportPermissions {
		issues = append(issues, dto.PermissionImportIssue{
			Path: "permissions", Field: "permissions", Code: "too_many",
			Message: fmt.Sprintf("本次共 %d 条，超过单次上限 %d 条，请裁剪后重试", doc.total, maxImportPermissions),
		})
	}

	preview := &dto.PermissionImportPreview{
		Ok:           len(issues) == 0,
		Total:        doc.total,
		Issues:       []dto.PermissionImportIssue{},
		Warnings:     []dto.PermissionImportIssue{},
		MovedItems:   []dto.PermissionImportMoved{},
		IssueTotal:   len(issues),
		WarningTotal: len(warnings),
	}
	if len(issues) > 0 {
		// 校验不通过时不做半截统计（ok=false 时计数归零，避免误导）。
		preview.Issues = truncateIssues(issues)
		preview.Warnings = truncateIssues(warnings)
		return preview, nil
	}

	existing, err := s.st.ListClientPermissionsForExport(ctx, clientID)
	if err != nil {
		return nil, err
	}
	plan := buildImportPlan(doc, existing)
	preview.Added = plan.added()
	preview.Updated = plan.updated()
	preview.Unchanged = plan.unchanged
	preview.Moved = plan.moved
	preview.MovedItems = plan.movedItems
	preview.Warnings = truncateIssues(warnings)
	return preview, nil
}

func truncateIssues(in []dto.PermissionImportIssue) []dto.PermissionImportIssue {
	if in == nil {
		return []dto.PermissionImportIssue{}
	}
	if len(in) <= maxImportIssues {
		return in
	}
	return in[:maxImportIssues]
}

// ConfirmImport 确认导入：单事务内按 code 幂等合并（新增 + 更新，不删除）。
func (s *AppAuthService) ConfirmImport(ctx context.Context, clientID string, data []byte, source string) (*dto.PermissionImportResult, *ImportOutcome, error) {
	if len(data) > maxImportBodyBytes {
		return nil, nil, &ImportTooLargeError{}
	}
	if err := s.ensureClientActive(ctx, clientID); err != nil {
		return nil, nil, err
	}

	doc, issues, _ := parseImportDocument(data)
	if len(issues) > 0 {
		return nil, nil, ErrAppPermissionImportInvalid
	}
	if doc.total > maxImportPermissions {
		return nil, nil, &ImportTooLargeError{Count: doc.total}
	}

	sum := sha256.Sum256(data)
	outcome := &ImportOutcome{DocSha256: hex.EncodeToString(sum[:])}
	result := &dto.PermissionImportResult{Source: normalizeImportSource(source)}

	err := s.st.Transaction(ctx, func(ctx context.Context) error {
		if err := s.st.LockClientRow(ctx, clientID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrClientNotFound
			}
			return err
		}
		existing, err := s.st.LockClientPermissions(ctx, clientID)
		if err != nil {
			return err
		}
		plan := buildImportPlan(doc, existing)

		now := nowMillis()
		for _, p := range plan.insert {
			p.ClientID = clientID
		}
		if err := s.st.CreateClientPermissions(ctx, plan.insert); err != nil {
			if errors.Is(err, store.ErrDuplicateKey) {
				return ErrAppResourceStale
			}
			return err
		}
		for _, u := range plan.update {
			if err := s.st.UpdateClientPermissionFields(ctx, clientID, u.id, u.fields, now); err != nil {
				if errors.Is(err, store.ErrDuplicateKey) {
					return ErrAppResourceStale
				}
				return err
			}
		}

		result.Added = plan.added()
		result.Updated = plan.updated()
		result.Unchanged = plan.unchanged
		result.Moved = plan.moved
		result.Total = doc.total
		outcome.AddedIDs = plan.addedIDs
		outcome.AddedCount = len(plan.addedIDs)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return result, outcome, nil
}

// exportFile 导出文件的序列化形态：字段顺序固定为 version/mode/permissions，
// 保证 importable 的体积判定与前端下载内容（JSON.stringify 保序）逐字节一致。
type exportFile struct {
	Version     int                    `json:"version"`
	Mode        string                 `json:"mode"`
	Permissions []dto.PermissionImportNode `json:"permissions"`
}

// ExportPermissions 导出为与导入同构的文档，并自证"能否回导"（FR-019）。
func (s *AppAuthService) ExportPermissions(ctx context.Context, clientID string) (*dto.PermissionExportDocument, error) {
	if err := s.ensureClientActive(ctx, clientID); err != nil {
		return nil, err
	}
	rows, err := s.st.ListClientPermissionsForExport(ctx, clientID)
	if err != nil {
		return nil, err
	}

	tree, maxDepth := buildExportTree(rows)
	file := exportFile{Version: 1, Mode: "merge", Permissions: tree}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}

	return &dto.PermissionExportDocument{
		Version:     1,
		Mode:        "merge",
		Importable:  evaluateImportable(len(rows), maxDepth, len(body)),
		Permissions: tree,
	}, nil
}

// evaluateImportable 判定导出文件能否回导（spec FR-019）。
// bodyLen 必须是**下载文件**（2 空格缩进）的字节数，不得用 compact 序列化结果，
// 否则会出现"服务端说可回导、下载后被 413 拒"的假承诺。
func evaluateImportable(count, maxDepth, bodyLen int) dto.PermissionExportable {
	reasons := make([]string, 0, 4)
	if count == 0 {
		reasons = append(reasons, "empty")
	}
	if count > maxImportPermissions {
		reasons = append(reasons, "too_many")
	}
	if bodyLen > maxImportBodyBytes {
		reasons = append(reasons, "too_large")
	}
	if maxDepth > maxPermissionDepth {
		reasons = append(reasons, "too_deep")
	}
	return dto.PermissionExportable{Ok: len(reasons) == 0, Reasons: reasons}
}

// buildExportTree 把扁平行组装成嵌套结构；返回树与最大深度（空树为 0）。
func buildExportTree(rows []model.ClientPermission) ([]dto.PermissionImportNode, int) {
	if len(rows) == 0 {
		return []dto.PermissionImportNode{}, 0
	}
	idToNode := make(map[string]*dto.PermissionImportNode, len(rows))
	childrenOf := make(map[string][]string, len(rows))
	rootIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		node := &dto.PermissionImportNode{Code: row.Code, Name: row.Name, Description: derefStr(row.Description)}
		idToNode[row.ID] = node
		if row.ParentID == nil {
			rootIDs = append(rootIDs, row.ID)
		} else {
			childrenOf[*row.ParentID] = append(childrenOf[*row.ParentID], row.ID)
		}
	}

	var build func(id string, depth int) (dto.PermissionImportNode, int)
	build = func(id string, depth int) (dto.PermissionImportNode, int) {
		node := *idToNode[id]
		maxDepth := depth
		if kids, ok := childrenOf[id]; ok && len(kids) > 0 {
			node.Children = make([]dto.PermissionImportNode, 0, len(kids))
			for _, kid := range kids {
				child, d := build(kid, depth+1)
				if d > maxDepth {
					maxDepth = d
				}
				node.Children = append(node.Children, child)
			}
		}
		return node, maxDepth
	}

	out := make([]dto.PermissionImportNode, 0, len(rootIDs))
	maxDepth := 0
	for _, id := range rootIDs {
		node, d := build(id, 1)
		if d > maxDepth {
			maxDepth = d
		}
		out = append(out, node)
	}
	return out, maxDepth
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// normalizeImportSource 白名单化来源：非法值一律按 paste（research R1）。
func normalizeImportSource(source string) string {
	switch source {
	case ImportSourceFile:
		return ImportSourceFile
	default:
		return ImportSourcePaste
	}
}
