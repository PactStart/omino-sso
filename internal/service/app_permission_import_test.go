package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/omino/omino-sso/internal/api/dto"
	"github.com/omino/omino-sso/internal/store/model"
)

// 权限点导入的纯函数单测（specs/011-app-permission-import T007 / T016 / T017 / T024）。
// 全部不触库：解析、建计划、可回导判定都是内存计算。

func issueByCode(in []dto.PermissionImportIssue, code string) *dto.PermissionImportIssue {
	for i := range in {
		if in[i].Code == code {
			return &in[i]
		}
	}
	return nil
}

func strPtr(s string) *string { return &s }

func permRow(id, code string, parent *string) model.ClientPermission {
	return model.ClientPermission{ID: id, Code: code, Name: code, ParentID: parent, Sort: 0}
}

// T007：结构性错误必须逐项可定位（code + path，语法错误另给行号）。
func TestParseImportDocumentRejectsBadInput(t *testing.T) {
	longName := strings.Repeat("长", 65) // 每字 3 字节，远超 64 字节上限
	cases := []struct {
		name string
		body string
		code string
		path string
		line int // 0 表示不校验行号
	}{
		{"空内容", "", "empty", "", 0},
		{"纯空白", " \n\t ", "empty", "", 0},
		{"非法 JSON", "{\n  \"permissions\": [\n    {,}\n  ]\n}", "syntax_error", "", 3},
		{"根不是对象", `[1,2,3]`, "bad_type", "", 0},
		{"版本不支持", `{"version":2,"permissions":[{"code":"a","name":"A"}]}`, "bad_version", "version", 0},
		{"replace 模式", `{"mode":"replace","permissions":[{"code":"a","name":"A"}]}`, "bad_mode", "mode", 0},
		{"未知顶层字段", `{"permissionList":[],"permissions":[{"code":"a","name":"A"}]}`, "unknown_field", "permissionList", 0},
		{"未知节点字段", `{"permissions":[{"id":"x","code":"a","name":"A"}]}`, "unknown_field", "permissions[0].id", 0},
		{"编码含空格", `{"permissions":[{"code":"order read","name":"A"}]}`, "invalid_code", "permissions[0].code", 0},
		{"名称超长", `{"permissions":[{"code":"a","name":"` + longName + `"}]}`, "too_long", "permissions[0].name", 0},
		{"缺名称", `{"permissions":[{"code":"a"}]}`, "missing_field", "permissions[0].name", 0},
		{"children 非数组", `{"permissions":[{"code":"a","name":"A","children":"x"}]}`, "bad_type", "permissions[0].children", 0},
		{"permissions 为空", `{"permissions":[]}`, "empty", "permissions", 0},
		{"sort 非整数", `{"permissions":[{"code":"a","name":"A","sort":1.5}]}`, "bad_type", "permissions[0].sort", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, issues, _ := parseImportDocument([]byte(c.body))
			got := issueByCode(issues, c.code)
			if got == nil {
				t.Fatalf("未产生 %s 问题，实际 issues=%+v", c.code, issues)
			}
			if got.Path != c.path {
				t.Fatalf("%s 的 path = %q，期望 %q", c.code, got.Path, c.path)
			}
			if c.line > 0 {
				if got.Line == nil || *got.Line != c.line {
					t.Fatalf("语法错误行号 = %v，期望 %d", got.Line, c.line)
				}
			}
		})
	}
}

// T007：合法文档的缺省口径（version=1 / mode=merge / sort 按书写顺序）。
func TestParseImportDocumentDefaults(t *testing.T) {
	body := `{"permissions":[
	  {"code":"order","name":"订单","children":[
	    {"code":"order.read","name":"查询"},
	    {"code":"order.write","name":"创建","sort":7}
	  ]},
	  {"code":"pay","name":"支付"}
	]}`
	doc, issues, warnings := parseImportDocument([]byte(body))
	if len(issues) != 0 {
		t.Fatalf("不应有问题，实际 %+v", issues)
	}
	if len(warnings) != 0 {
		t.Fatalf("不应有提示，实际 %+v", warnings)
	}
	if doc.version != 1 || doc.mode != "merge" {
		t.Fatalf("version/mode = %d/%q，期望 1/merge", doc.version, doc.mode)
	}
	if doc.total != 4 || doc.maxDepth != 2 {
		t.Fatalf("total/maxDepth = %d/%d，期望 4/2", doc.total, doc.maxDepth)
	}
	if len(doc.roots) != 2 {
		t.Fatalf("根节点数 = %d，期望 2", len(doc.roots))
	}
	if doc.roots[0].path != "permissions[0]" || doc.roots[0].children[1].path != "permissions[0].children[1]" {
		t.Fatalf("路径不符合预期：%q / %q", doc.roots[0].path, doc.roots[0].children[1].path)
	}
	// 省略 sort 时按同级书写顺序分配 0..n-1
	if doc.roots[0].sortVal != 0 || doc.roots[1].sortVal != 1 {
		t.Fatalf("根级缺省 sort = %d/%d，期望 0/1", doc.roots[0].sortVal, doc.roots[1].sortVal)
	}
	if doc.roots[0].children[0].sortVal != 0 || doc.roots[0].children[1].sortVal != 7 {
		t.Fatalf("子级 sort = %d/%d，期望 0/7",
			doc.roots[0].children[0].sortVal, doc.roots[0].children[1].sortVal)
	}
	if doc.roots[0].children[0].parent != doc.roots[0] {
		t.Fatal("子节点的 parent 未指向父节点")
	}
}

// T016：三层 12 条合法文档应全部通过并正确成树。
func TestParseImportDocumentThreeLevels(t *testing.T) {
	body := `{"version":1,"mode":"merge","permissions":[
	  {"code":"order","name":"订单","children":[
	    {"code":"order.read","name":"查询","children":[
	      {"code":"order.read.self","name":"查自己"},
	      {"code":"order.read.all","name":"查全部"}
	    ]},
	    {"code":"order.write","name":"创建","children":[
	      {"code":"order.write.self","name":"建自己"},
	      {"code":"order.write.all","name":"建全部"}
	    ]}
	  ]},
	  {"code":"pay","name":"支付","children":[
	    {"code":"pay.read","name":"查询","children":[
	      {"code":"pay.read.self","name":"查自己"},
	      {"code":"pay.read.all","name":"查全部"}
	    ]},
	    {"code":"pay.write","name":"创建","children":[
	      {"code":"pay.write.self","name":"建自己"},
	      {"code":"pay.write.all","name":"建全部"}
	    ]}
	  ]}
	]}`
	doc, issues, warnings := parseImportDocument([]byte(body))
	if len(issues) != 0 {
		t.Fatalf("12 条三层文档不应有问题：%+v", issues)
	}
	if len(warnings) != 0 {
		t.Fatalf("编码均以父级为前缀，不应有提示：%+v", warnings)
	}
	if doc.total != 14 || doc.maxDepth != 3 || len(doc.roots) != 2 {
		t.Fatalf("total/maxDepth/roots = %d/%d/%d，期望 14/3/2",
			doc.total, doc.maxDepth, len(doc.roots))
	}
	if len(doc.roots[0].children) != 2 || len(doc.roots[0].children[0].children) != 2 {
		t.Fatal("三层结构未被正确还原")
	}
}

// T016：101 层必须在第 101 层被拒绝，且不再深入（避免刷屏）。
func TestParseImportDocumentTooDeep(t *testing.T) {
	const depth = 101
	node := fmt.Sprintf(`{"code":"p%d","name":"第%d层"}`, depth-1, depth)
	for i := depth - 2; i >= 0; i-- {
		node = fmt.Sprintf(`{"code":"p%d","name":"第%d层","children":[%s]}`, i, i+1, node)
	}
	body := `{"permissions":[` + node + `]}`

	doc, issues, _ := parseImportDocument([]byte(body))
	got := issueByCode(issues, "too_deep")
	if got == nil {
		t.Fatalf("101 层应报 too_deep，实际 %+v", issues)
	}
	if !strings.Contains(got.Message, "101") || !strings.Contains(got.Message, "100") {
		t.Fatalf("超深提示应含实际深度与上限：%q", got.Message)
	}
	if len(issues) != 1 {
		t.Fatalf("超深后不应继续产生问题（避免刷屏），实际 %+v", issues)
	}
	if doc.total != maxPermissionDepth {
		t.Fatalf("超深文档只应统计前 %d 层，实际 %d", maxPermissionDepth, doc.total)
	}
}

// T016：跨层重复编码必须同时指出两处位置。
func TestParseImportDocumentDuplicateCode(t *testing.T) {
	body := `{"permissions":[
	  {"code":"order","name":"订单","children":[{"code":"order.read","name":"查询"}]},
	  {"code":"pay","name":"支付","children":[{"code":"order.read","name":"再查"}]}
	]}`
	_, issues, _ := parseImportDocument([]byte(body))
	got := issueByCode(issues, "dup_code")
	if got == nil {
		t.Fatalf("跨层重复应报 dup_code，实际 %+v", issues)
	}
	if !strings.Contains(got.Message, "permissions[0].children[0].code") ||
		!strings.Contains(got.Message, "permissions[1].children[0].code") {
		t.Fatalf("重复提示应同时给出两处路径：%q", got.Message)
	}
}

// T016：前缀不符与大小写差异只提示、不阻断。
func TestParseImportDocumentWarningsDoNotBlock(t *testing.T) {
	prefixBody := `{"permissions":[
	  {"code":"order","name":"订单","children":[{"code":"pay.refund","name":"退款"}]}
	]}`
	doc, issues, warnings := parseImportDocument([]byte(prefixBody))
	if len(issues) != 0 {
		t.Fatalf("前缀提示不应阻断导入：%+v", issues)
	}
	if issueByCode(warnings, "prefix_hint") == nil {
		t.Fatalf("应给出 prefix_hint 提示，实际 %+v", warnings)
	}
	if doc.total != 2 {
		t.Fatalf("total = %d，期望 2", doc.total)
	}

	caseBody := `{"permissions":[
	  {"code":"order.read","name":"查询"},
	  {"code":"Order.Read","name":"查询2"}
	]}`
	doc, issues, warnings = parseImportDocument([]byte(caseBody))
	if len(issues) != 0 {
		t.Fatalf("大小写差异不应阻断：%+v", issues)
	}
	if issueByCode(warnings, "case_hint") == nil {
		t.Fatalf("应给出 case_hint 提示，实际 %+v", warnings)
	}
	if doc.total != 2 {
		t.Fatalf("大小写不同应作为两个权限点，total = %d", doc.total)
	}
}

// T017：上级改写按输入整体重写、不成环，未出现在输入中的节点不动。
func TestBuildImportPlanRewritesParents(t *testing.T) {
	aID, bID := "01AAA", "01BBB"
	existing := []model.ClientPermission{
		permRow(aID, "A", nil),
		permRow(bID, "B", strPtr(aID)),
		permRow("01CCC", "C", nil), // 未出现在输入中
	}
	body := `{"permissions":[
	  {"code":"B","name":"B","children":[{"code":"A","name":"A"}]}
	]}`
	doc, issues, _ := parseImportDocument([]byte(body))
	if len(issues) != 0 {
		t.Fatalf("解析失败：%+v", issues)
	}

	plan := buildImportPlan(doc, existing)
	if plan.added() != 0 {
		t.Fatalf("不应新增，实际 %d", plan.added())
	}
	if plan.moved != 2 || len(plan.movedItems) != 2 {
		t.Fatalf("moved/movedItems = %d/%d，期望 2/2", plan.moved, len(plan.movedItems))
	}
	if plan.updated() != 2 {
		t.Fatalf("两条都应更新，实际 %d", plan.updated())
	}

	fieldsByID := map[string]map[string]any{}
	for _, u := range plan.update {
		fieldsByID[u.id] = u.fields
	}
	bFields, ok := fieldsByID[bID]
	if !ok {
		t.Fatal("B 未进入更新列表")
	}
	if v, exists := bFields["parent_id"]; !exists || v != nil {
		t.Fatalf("B 应被改到根级，parent_id = %v（exists=%v）", v, exists)
	}
	aFields, ok := fieldsByID[aID]
	if !ok {
		t.Fatal("A 未进入更新列表")
	}
	if v, _ := aFields["parent_id"].(string); v != bID {
		t.Fatalf("A 的 parent_id = %v，期望 %s", aFields["parent_id"], bID)
	}
	if _, touched := fieldsByID["01CCC"]; touched {
		t.Fatal("未出现在输入中的节点不得被更新")
	}
}

// T017：字段无变化计为 unchanged，不产生写操作；新增节点按父先子后进入 insert。
func TestBuildImportPlanIdempotentAndOrder(t *testing.T) {
	existing := []model.ClientPermission{permRow("01AAA", "A", nil)}
	body := `{"permissions":[
	  {"code":"A","name":"A","children":[
	    {"code":"A.1","name":"A.1","children":[{"code":"A.1.1","name":"A.1.1"}]}
	  ]}
	]}`
	doc, issues, _ := parseImportDocument([]byte(body))
	if len(issues) != 0 {
		t.Fatalf("解析失败：%+v", issues)
	}

	plan := buildImportPlan(doc, existing)
	if plan.unchanged != 1 || plan.updated() != 0 {
		t.Fatalf("unchanged/updated = %d/%d，期望 1/0", plan.unchanged, plan.updated())
	}
	if plan.added() != 2 {
		t.Fatalf("added = %d，期望 2", plan.added())
	}
	if plan.insert[0].Code != "A.1" || plan.insert[1].Code != "A.1.1" {
		t.Fatalf("新增顺序应为父先子后，实际 %q → %q", plan.insert[0].Code, plan.insert[1].Code)
	}
	if plan.insert[1].ParentID == nil || *plan.insert[1].ParentID != plan.insert[0].ID {
		t.Fatal("孙节点的 parent 未指向本次新增的父节点")
	}
	if len(plan.addedIDs) != 2 {
		t.Fatalf("addedIDs 数量 = %d，期望 2", len(plan.addedIDs))
	}
}

// T024：可回导判定必须与下载文件的 2 空格缩进字节一致（FR-019）。
func TestImportableMatchesDownloadBytes(t *testing.T) {
	nodes := []dto.PermissionImportNode{
		{Code: "order", Name: "订单", Children: []dto.PermissionImportNode{
			{Code: "order.read", Name: "查询"},
			{Code: "order.write", Name: "创建"},
		}},
	}
	file := exportFile{Version: 1, Mode: "merge", Permissions: nodes}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	compact, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("紧凑序列化失败：%v", err)
	}
	if len(compact) >= len(body) {
		t.Fatalf("缩进序列化（%d）应大于紧凑序列化（%d），否则本判定无意义", len(body), len(compact))
	}

	got := evaluateImportable(len(nodes), 2, len(body))
	if !got.Ok || len(got.Reasons) != 0 {
		t.Fatalf("可回导文档被判为不可回导：%+v", got)
	}
}

// T024：四种不可回导原因与边界值。
func TestEvaluateImportableReasons(t *testing.T) {
	cases := []struct {
		name    string
		count   int
		depth   int
		bodyLen int
		want    []string
	}{
		{"空文档", 0, 0, 2, []string{"empty"}},
		{"条数恰好上限", maxImportPermissions, 1, 10, nil},
		{"条数超限", maxImportPermissions + 1, 1, 10, []string{"too_many"}},
		{"深度恰好上限", 1, maxPermissionDepth, 10, nil},
		{"深度超限", 1, maxPermissionDepth + 1, 10, []string{"too_deep"}},
		{"体积恰好上限", 1, 1, maxImportBodyBytes, nil},
		{"体积超限", 1, 1, maxImportBodyBytes + 1, []string{"too_large"}},
		{"多处超限", maxImportPermissions + 1, maxPermissionDepth + 1, maxImportBodyBytes + 1,
			[]string{"too_many", "too_large", "too_deep"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evaluateImportable(c.count, c.depth, c.bodyLen)
			if len(got.Reasons) != len(c.want) {
				t.Fatalf("reasons = %v，期望 %v", got.Reasons, c.want)
			}
			for i := range c.want {
				if got.Reasons[i] != c.want[i] {
					t.Fatalf("reasons = %v，期望 %v", got.Reasons, c.want)
				}
			}
			if got.Ok != (len(c.want) == 0) {
				t.Fatalf("ok = %v，期望 %v", got.Ok, len(c.want) == 0)
			}
		})
	}
}

// 导出树必须按层级还原且不含内部标识（FR-019）。
func TestBuildExportTree(t *testing.T) {
	a := permRow("01A", "order", nil)
	b := permRow("01B", "order.read", strPtr("01A"))
	c := permRow("01C", "pay", nil)
	tree, depth := buildExportTree([]model.ClientPermission{a, b, c})
	if depth != 2 || len(tree) != 2 {
		t.Fatalf("depth/roots = %d/%d，期望 2/2", depth, len(tree))
	}
	if tree[0].Code != "order" || len(tree[0].Children) != 1 || tree[0].Children[0].Code != "order.read" {
		t.Fatalf("导出树结构不对：%+v", tree)
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	for _, banned := range []string{"01A", "01B", "01C", "parentId", "id\""} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("导出内容不得含内部标识 %q：%s", banned, raw)
		}
	}
}

// T046 / FR-013：512KB 必须覆盖「1000 条 × 编码/名称/描述全填满」的最坏合法载荷；
// 否则合法的最大批量会被自己的体积上限拒绝。本用例把这条口径锁死，防止常量或字段上限漂移。
func TestWorstCaseImportFitsBodyLimit(t *testing.T) {
	perms := make([]dto.PermissionImportNode, 0, maxImportPermissions)
	for i := 0; i < maxImportPermissions; i++ {
		desc := strings.Repeat("d", 255)
		perms = append(perms, dto.PermissionImportNode{
			Code:        strings.Repeat("c", 64),
			Name:        strings.Repeat("n", 64),
			Description: desc,
		})
	}
	// 编码全局唯一是 FR-009 的要求，这里让 1000 条互不相同：把尾部换成序号
	for i := range perms {
		suffix := fmt.Sprintf("%04d", i)
		perms[i].Code = strings.Repeat("c", 64-len(suffix)) + suffix
	}
	doc := map[string]any{"version": 1, "mode": "merge", "permissions": perms}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	// 与下载文件同一序列化口径（2 空格缩进）
	if len(body) >= MaxImportBodyBytes {
		t.Fatalf("最坏合法载荷 %d 字节 ≥ 上限 %d：上限不足以承载 1000 条满字段文档", len(body), MaxImportBodyBytes)
	}

	parsed, issues, _ := parseImportDocument(body)
	if len(issues) > 0 {
		t.Fatalf("最坏合法载荷不应有校验问题，实际 %d 条：%+v", len(issues), issues[:min(3, len(issues))])
	}
	if parsed.total != maxImportPermissions {
		t.Fatalf("解析条数 = %d，期望 %d", parsed.total, maxImportPermissions)
	}
}

// T051 / SC-005：1000 条 / 5 层输入的耗时基线（纯函数部分，不含 DB）。
// 预览 = 解析 + 建计划；确认 = 同一份解析 + 与库内现状做 diff，故两个基准分别覆盖冷启动与全量无变化。

// fiveLevelDoc 构造 5 层共 1000 条的唯一编码文档：5 + 20 + 80 + 320 + 575。
func fiveLevelDoc() []dto.PermissionImportNode {
	const (
		roots      = 5
		branch     = 4
		l4Nodes    = roots * branch * branch * branch // 320
		doubleLeaf = 255                              // 前 255 个 L4 各带 2 个叶子，其余各 1 个
	)
	rootsNodes := make([]dto.PermissionImportNode, 0, roots)
	l4Index := 0
	for i := 0; i < roots; i++ {
		l2 := make([]dto.PermissionImportNode, 0, branch)
		for j := 0; j < branch; j++ {
			l3 := make([]dto.PermissionImportNode, 0, branch)
			for k := 0; k < branch; k++ {
				l4 := make([]dto.PermissionImportNode, 0, branch)
				for m := 0; m < branch; m++ {
					leaves := 1
					if l4Index < doubleLeaf {
						leaves = 2
					}
					l4Index++
					l5 := make([]dto.PermissionImportNode, 0, leaves)
					for n := 0; n < leaves; n++ {
						l5 = append(l5, dto.PermissionImportNode{
							Code: fmt.Sprintf("p.%d.%d.%d.%d.%d", i, j, k, m, n),
							Name: fmt.Sprintf("动作%d", n),
						})
					}
					l4 = append(l4, dto.PermissionImportNode{
						Code: fmt.Sprintf("p.%d.%d.%d.%d", i, j, k, m), Name: "模块", Children: l5,
					})
				}
				l3 = append(l3, dto.PermissionImportNode{
					Code: fmt.Sprintf("p.%d.%d.%d", i, j, k), Name: "子域", Children: l4,
				})
			}
			l2 = append(l2, dto.PermissionImportNode{
				Code: fmt.Sprintf("p.%d.%d", i, j), Name: "域", Children: l3,
			})
		}
		rootsNodes = append(rootsNodes, dto.PermissionImportNode{
			Code: fmt.Sprintf("p.%d", i), Name: "系统", Children: l2,
		})
	}
	return rootsNodes
}

func fiveLevelBody(tb testing.TB) []byte {
	tb.Helper()
	body, err := json.Marshal(map[string]any{
		"version": 1, "mode": "merge", "permissions": fiveLevelDoc(),
	})
	if err != nil {
		tb.Fatalf("序列化失败：%v", err)
	}
	return body
}

func BenchmarkImportPreview1000x5(b *testing.B) {
	body := fiveLevelBody(b)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, issues, _ := parseImportDocument(body)
		if len(issues) > 0 {
			b.Fatalf("基准文档不应有校验问题：%+v", issues[:min(3, len(issues))])
		}
		if plan := buildImportPlan(doc, nil); plan.added() != 1000 {
			b.Fatalf("新增数 = %d，期望 1000", plan.added())
		}
	}
}

// 全量无变化的确认导入：解析 + 与库内 1000 行逐条 diff（不含事务与写库）。
func BenchmarkImportConfirmUnchanged1000x5(b *testing.B) {
	body := fiveLevelBody(b)
	seedDoc, issues, _ := parseImportDocument(body)
	if len(issues) > 0 {
		b.Fatalf("基准文档不应有校验问题")
	}
	seedPlan := buildImportPlan(seedDoc, nil)
	existing := make([]model.ClientPermission, 0, len(seedPlan.insert))
	for _, row := range seedPlan.insert {
		existing = append(existing, *row)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, _, _ := parseImportDocument(body)
		plan := buildImportPlan(doc, existing)
		if plan.added() != 0 || plan.unchanged != 1000 {
			b.Fatalf("无变化导入的计数不对：added=%d unchanged=%d", plan.added(), plan.unchanged)
		}
	}
}
