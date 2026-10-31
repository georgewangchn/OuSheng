package docs

import (
	"strings"
	"testing"
	"time"

	"ousheng/internal/model"
)

func ev(id string, at string) model.Evidence {
	return model.Evidence{Type: model.EvidenceDocument, Locator: "docs/acceptance/" + id + ".md", ObservedAt: at}
}

func doneItem(id, system string, evs ...model.Evidence) model.WorkItem {
	return model.WorkItem{ID: id, Type: model.TypeTask, System: system, Status: model.StatusDone, Evidence: evs}
}

func design(topic, doc string, related ...string) model.DesignInfo {
	return model.DesignInfo{
		Topic: topic,
		Design: model.DesignDoc{
			Status: model.DesignAgreed, Owner: "pm", Doc: doc, RelatedItems: related,
		},
	}
}

// D1 单一认领：强认领（design.doc）按路径精确命中。
func TestStrongClaimMatches(t *testing.T) {
	claims := Claims(
		[]model.DesignInfo{design("t1", "docs/design/a.md")},
		nil, nil,
	)
	if len(claims) != 1 || claims[0].Role != RoleDecision || claims[0].Owner != "t1" {
		t.Fatalf("claim derivation wrong: %+v", claims)
	}
	refs := Build([]File{{Path: "docs/design/a.md", Size: 100}}, claims)
	if len(refs) != 1 || !refs[0].Claimed {
		t.Fatalf("strong claim must match exactly: %+v", refs)
	}
	// 同名不同目录不得误命中（强认领是路径相等，不是 basename）：
	// 树外文件整体不被跟踪；树内不同文件不被认领。
	refs2 := Build([]File{
		{Path: "docs/other/a.md"},      // 树外：不跟踪
		{Path: "docs/design/other.md"}, // 树内但未被认领
	}, claims)
	if len(refs2) != 1 || refs2[0].Path != "docs/design/other.md" || refs2[0].Claimed {
		t.Fatalf("strong claim must be path-exact, not basename: %+v", refs2)
	}

	// 强认领落在工作区根时不产生扫描根（否则一个根级文档等于监控全仓），
	// 但该文件本身仍受跟踪——"认领指向的文件永远受跟踪"。
	rootClaims := Claims([]model.DesignInfo{design("t2", "PLAN.md")}, nil, nil)
	if got := Roots(rootClaims); len(got) != 0 {
		t.Fatalf("workspace-root claim must not create a root, got %v", got)
	}
	refs3 := Build([]File{{Path: "PLAN.md"}, {Path: "elsewhere/x.md"}}, rootClaims)
	if len(refs3) != 1 || refs3[0].Path != "PLAN.md" || !refs3[0].Claimed {
		t.Fatalf("claimed file must be tracked even without a root: %+v", refs3)
	}
}

// D3 未知即曝光：受监控树内未认领的 .md 是孤儿。
func TestOrphanDetected(t *testing.T) {
	claims := Claims([]model.DesignInfo{design("t1", "docs/design/a.md")}, nil, nil)
	refs := Build([]File{
		{Path: "docs/design/a.md"},
		{Path: "docs/design/b.md"}, // 无认领
	}, claims)
	orphans := Orphans(refs)
	if len(orphans) != 1 || orphans[0].Path != "docs/design/b.md" {
		t.Fatalf("expected exactly one orphan b.md, got %+v", orphans)
	}
}

// D3 推论：零认领的目录树自动豁免——树外文件不进孤儿判定。
func TestUnclaimedTreeIsExempt(t *testing.T) {
	claims := Claims([]model.DesignInfo{design("t1", "docs/design/a.md")}, nil, nil)
	// databuddy/ 与 product/ 两棵零认领的树：整树免打扰。
	refs := Build([]File{
		{Path: "docs/design/a.md"},
		{Path: "design/databuddy/x.md"},
		{Path: "design/databuddy/y.md"},
		{Path: "design/product/z.md"},
	}, claims)
	if len(refs) != 1 {
		t.Fatalf("only files under monitored roots should be tracked, got %+v", refs)
	}
}

// 边界 1：结构性文件豁免。
func TestStructuralFilesExempt(t *testing.T) {
	for _, p := range []string{"README.md", "INDEX.md", "docs/README.md", "a/b/INDEX.md"} {
		if !Exempt(p) {
			t.Fatalf("%s must be exempt", p)
		}
	}
	for _, p := range []string{"docs/design/a.md", "readme.md", "NOTES.md"} {
		if Exempt(p) {
			t.Fatalf("%s must NOT be exempt", p)
		}
	}
}

// 弱认领：locator 带 commit 后缀 / 括号装饰时仍能匹配；日期是文件名本体，不剥。
func TestWeakClaimKey(t *testing.T) {
	cases := map[string]string{
		"docs/acceptance/T-038-联合测试报告.md@e1aeb34":                          "T-038-联合测试报告.md",
		"docs/acceptance/BUG-041-042-mlake查证与修复.md (mlake commit 23c3fc2)": "BUG-041-042-mlake查证与修复.md",
		"tech-talent-导入复核报告-1023.md":                                       "tech-talent-导入复核报告-1023.md",
		"pm-decision-20260928":                                             "pm-decision-20260928",
	}
	for in, want := range cases {
		if got := weakKey(in); got != want {
			t.Fatalf("weakKey(%q) = %q, want %q", in, got, want)
		}
	}
	// 日期不得被剥掉：1023 与 1024 是两篇不同文档。
	if weakKey("a-1023.md") == weakKey("a-1024.md") {
		t.Fatal("date suffix must be preserved: 1023 and 1024 are different docs")
	}
}

// 弱认领按 basename 匹配——文档归档迁移目录后仍算数（I3 要求落账后移 archive）。
//
// 注意 archive/ 下没有任何认领路径，因此不产生扫描根；迁移后的文件靠
// "认领直接指向"进入结果。它既不会被误报为孤儿，也不会让 census 把整个
// 归档目录报成未纳管区域——认领跟着 basename 走，不跟着目录走。
func TestWeakClaimSurvivesMove(t *testing.T) {
	items := []model.WorkItem{{
		ID: "T-038", Type: model.TypeTask, Status: model.StatusDone,
		Evidence: []model.Evidence{ev("T-038-复测报告-1021", "2026-10-21T10:00:00+08:00")},
	}}
	claims := Claims(nil, nil, items)
	refs := Build([]File{
		{Path: "docs/acceptance/T-038-复测报告-1021.md"},    // 认领路径所在目录
		{Path: "archive/acceptance/T-038-复测报告-1021.md"}, // 迁移后：目录无认领
		{Path: "archive/acceptance/T-039-另一份报告.md"},     // 迁移后仍无认领
	}, claims)
	if len(refs) != 2 {
		t.Fatalf("claimed files must be tracked wherever they live, refs=%+v", refs)
	}
	for _, r := range refs {
		if !r.Claimed {
			t.Fatalf("weak claim must survive directory move: %+v", r)
		}
	}
	if got := Orphans(refs); len(got) != 0 {
		t.Fatalf("archived evidence must not be reported as orphan: %+v", got)
	}
	// 归档目录里已认领的文件不得把整片区域推成"未纳管"。
	if got := Census([]File{{Path: "archive/acceptance/T-038-复测报告-1021.md"}}, claims, 1); len(got) != 0 {
		t.Fatalf("fully claimed archive dir must stay out of census: %+v", got)
	}
	// 但归档目录里混进一篇无主文档时，区域信号必须响——那正是 D3 的盲区。
	got := Census([]File{
		{Path: "archive/acceptance/T-038-复测报告-1021.md"},
		{Path: "archive/acceptance/T-039-另一份报告.md"},
	}, claims, 2)
	if len(got) != 1 || got[0].Dir != "archive/acceptance" || got[0].Files != 2 || got[0].Claimed != 1 {
		t.Fatalf("partially claimed archive dir must be reported: %+v", got)
	}
}

// 弱认领的 locator 无目录成分时不取根——对它取 dirname 会得到 "."，
// 等于把整个仓库拖进扫描。
func TestRootsSkipWeakClaims(t *testing.T) {
	items := []model.WorkItem{{
		ID: "BUG-026", Type: model.TypeBug, Status: model.StatusDone,
		Evidence: []model.Evidence{{
			Type: model.EvidenceDocument, Locator: "pm-schedule-20260928",
			ObservedAt: "2026-09-28T10:00:00+08:00",
		}},
	}}
	claims := Claims(nil, nil, items)
	if got := Roots(claims); len(got) != 0 {
		t.Fatalf("directoryless weak claims must not create scan roots, got %v", got)
	}
}

// 弱认领自带目录成分时同样取根——验收报告所在的目录本就是文档堆积处，
// 不取根就等于放任那里长出无主文档。
func TestPathShapedWeakClaimCreatesRoot(t *testing.T) {
	items := []model.WorkItem{{
		ID: "T-038", Type: model.TypeTask, Status: model.StatusDone,
		Evidence: []model.Evidence{ev("T-038-复测报告-1021", "2026-10-21T10:00:00+08:00")},
	}}
	claims := Claims(nil, nil, items)
	if got := Roots(claims); len(got) != 1 || got[0] != "docs/acceptance" {
		t.Fatalf("path-shaped weak claim must create its dir as root, got %v", got)
	}
	// 装饰（commit 后缀 / 括号备注）不得影响取根。
	dirty := Claims(nil, nil, []model.WorkItem{{
		ID: "BUG-041", Type: model.TypeBug, Status: model.StatusDone,
		Evidence: []model.Evidence{{
			Type:       model.EvidenceDocument,
			Locator:    "docs/acceptance/BUG-041-042-mlake查证与修复.md (mlake commit 23c3fc2)",
			ObservedAt: "2026-10-01T10:00:00+08:00",
		}},
	}})
	if got := Roots(dirty); len(got) != 1 || got[0] != "docs/acceptance" {
		t.Fatalf("decorated locator must still yield its dir, got %v", got)
	}
}

// .ousheng/ 是绳自己的地盘，不得成为扫描根。
func TestRootsSkipOushengDir(t *testing.T) {
	claims := Claims(nil, []string{"data"}, nil)
	if got := Roots(claims); len(got) != 0 {
		t.Fatalf(".ousheng/architecture claims must not create roots, got %v", got)
	}
}

// 边界 2：越界认领合法但降级——EXTERNAL 标记，不参与陈旧判定。
func TestExternalClaimDegraded(t *testing.T) {
	if !IsExternal("../mlake/docs/ARCHITECTURE.md") || !IsExternal("/abs/path.md") {
		t.Fatal("expected external")
	}
	if IsExternal("docs/design/a.md") {
		t.Fatal("workspace-relative path must not be external")
	}
	// 越界认领在工作区外，取不到工作区内的扫描根。
	ext := []Claim{{Path: "../mlake/docs/ARCHITECTURE.md", Role: RoleStatus, Owner: "mlake"}}
	if got := Roots(ext); len(got) != 0 {
		t.Fatalf("external claims must not create roots, got %v", got)
	}
	items := []model.WorkItem{doneItem("T-1", "data",
		model.Evidence{Type: model.EvidenceManualCheck, ObservedAt: "2026-10-20T10:00:00+08:00"})}
	stale := FindStale([]Ref{{
		Path: "../mlake/docs/ARCHITECTURE.md", Role: RoleStatus, Owner: "mlake",
		Claimed: true, External: true,
		Updated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}}, nil, items)
	if len(stale) != 0 {
		t.Fatalf("external docs must not be judged stale (no git time), got %+v", stale)
	}
}

// decision 角色：文档提交早于 topic 管辖的 done 单 → 陈旧，计数正确。
func TestStaleDecision(t *testing.T) {
	items := []model.WorkItem{
		doneItem("T-031", "data", model.Evidence{Type: model.EvidenceManualCheck, ObservedAt: "2026-10-10T10:00:00+08:00"}),
		doneItem("T-034", "data", model.Evidence{Type: model.EvidenceManualCheck, ObservedAt: "2026-10-20T10:00:00+08:00"}),
		doneItem("T-099", "data", model.Evidence{Type: model.EvidenceManualCheck, ObservedAt: "2026-11-01T10:00:00+08:00"}), // 不在 related_items
	}
	claims := Claims([]model.DesignInfo{design("layered", "docs/design/l.md", "T-031", "T-034")}, nil, items)
	refs := Build([]File{{Path: "docs/design/l.md", Size: 9}}, claims)
	refs[0].Updated = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	stale := FindStale(refs, []model.DesignInfo{design("layered", "docs/design/l.md", "T-031", "T-034")}, items)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale finding, got %+v", stale)
	}
	if stale[0].Count != 1 || stale[0].LatestID != "T-034" {
		t.Fatalf("only T-034 is after doc time; T-031 before, T-099 out of scope: %+v", stale[0])
	}
}

// evidence 角色是完成时快照，陈旧不可判（D2）。
func TestEvidenceRoleNeverStale(t *testing.T) {
	items := []model.WorkItem{{
		ID: "T-038", Type: model.TypeTask, Status: model.StatusDone,
		Evidence: []model.Evidence{ev("T-038-report", "2026-10-20T10:00:00+08:00")},
	}}
	// 直接构造 Ref：evidence 角色认领的文档即使远早于交付，也不判陈旧。
	refs := []Ref{{
		Path: "docs/acceptance/T-038-report.md", Role: RoleEvidence, Owner: "T-038", Claimed: true,
		Updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	stale := FindStale(refs, nil, items)
	if len(stale) != 0 {
		t.Fatalf("evidence role is a done-time snapshot and must never be judged stale, got %+v", stale)
	}
}

// status 角色：系统 done 单晚于 architecture 卡更新 → 陈旧。
func TestStaleStatus(t *testing.T) {
	items := []model.WorkItem{
		doneItem("T-029", "data", model.Evidence{Type: model.EvidenceManualCheck, ObservedAt: "2026-10-29T10:00:00+08:00"}),
	}
	claims := Claims(nil, []string{"data"}, items)
	refs := Build([]File{{Path: ".ousheng/architecture/data.md"}}, claims)
	// architecture 路径在 .ousheng 下，不构成扫描根；此处直接构造 Ref 测语义。
	refs = []Ref{{Path: ".ousheng/architecture/data.md", Role: RoleStatus, Owner: "data", Claimed: true,
		Updated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}
	stale := FindStale(refs, nil, items)
	if len(stale) != 1 || stale[0].Count != 1 || stale[0].Owner != "data" {
		t.Fatalf("status role must be judged stale against system done items: %+v", stale)
	}
}

// census：零认领且 .md 数达标的目录树报为未纳管区域；已纳管树不报。
func TestCensusFindsUnclaimedRegion(t *testing.T) {
	claims := Claims([]model.DesignInfo{design("t1", "docs/design/a.md")}, nil, nil)
	files := []File{
		{Path: "docs/design/a.md"}, // 已认领
		{Path: "docs/design/b.md"},
		{Path: "design/databuddy/1.md"},
		{Path: "design/databuddy/2.md"},
		{Path: "design/databuddy/3.md"},
		{Path: "design/databuddy/4.md"},
		{Path: "README.md"}, // 结构性豁免
	}
	regions := Census(files, claims, 3)
	if len(regions) != 1 || regions[0].Dir != "design/databuddy" || regions[0].Files != 4 {
		t.Fatalf("census must report the zero-claim tree with >=3 files, got %+v", regions)
	}
	// 阈值以下不报。
	if got := Census(files, claims, 10); len(got) != 0 {
		t.Fatalf("below threshold must stay silent, got %+v", got)
	}
}

// 单调性：认领数增加不应让孤儿数增加。
func TestClaimingRemovesOrphan(t *testing.T) {
	base := []File{{Path: "docs/design/a.md"}, {Path: "docs/design/b.md"}}
	// 零认领时整棵树不受监控（自动豁免），所以先用一条锚点认领把树纳入监控。
	anchor := []model.DesignInfo{design("t-anchor", "docs/design/a.md")}
	before := Orphans(Build(base, Claims(anchor, nil, nil)))
	after := Orphans(Build(base, Claims([]model.DesignInfo{
		design("t-anchor", "docs/design/a.md"),
		design("t1", "docs/design/b.md"),
	}, nil, nil)))
	if len(before) != 1 || before[0].Path != "docs/design/b.md" {
		t.Fatalf("anchor claimed, b.md must be the only orphan, got %+v", before)
	}
	if len(after) != 0 {
		t.Fatalf("claiming b.md must leave no orphan, got %+v", after)
	}
}

// Tiny：500B 下限只盯 decision（doc-lifecycle 方案裁决 4）。
func TestTinyDecisionOnly(t *testing.T) {
	refs := []Ref{
		{Path: "docs/design/a.md", Role: RoleDecision, Owner: "t1", Claimed: true, Size: 120},
		{Path: "docs/design/b.md", Role: RoleDecision, Owner: "t2", Claimed: true, Size: 5000},
		{Path: "docs/acceptance/r.md", Role: RoleEvidence, Owner: "T-1", Claimed: true, Size: 80},
		{Path: "docs/design/ghost.md", Size: 100}, // 未认领不判 tiny（先治无主）
	}
	got := Tiny(refs)
	if len(got) != 1 || got[0].Path != "docs/design/a.md" {
		t.Fatalf("tiny must flag only small claimed decisions, got %v", got)
	}
}

// 边界 1（结构性文件豁免）在扫描入口的落地：README/INDEX 不进 refs，
// 也就不会被报孤儿（实栈实锤：docs/infocards/README.md 曾进 orphan 清单）。
func TestExemptFilesNeverOrphan(t *testing.T) {
	files := []File{
		{Path: "docs/infocards/README.md", Size: 100},
		{Path: "docs/design/INDEX.md", Size: 100},
		{Path: "docs/design/plan.md", Size: 100},
	}
	claims := []Claim{{Path: "docs/design/plan.md", Role: RoleDecision, Owner: "t1"}}
	// 调用方约定：扫描输入先剔 Exempt（gityaml.DocFiles / existingClaimFiles 双侧落实）。
	var filtered []File
	for _, f := range files {
		if !Exempt(f.Path) {
			filtered = append(filtered, f)
		}
	}
	refs := Build(filtered, claims)
	if got := Orphans(refs); len(got) != 0 {
		t.Fatalf("exempt files must not orphan, got %v", got)
	}
}

// --- doc bloat 三信号（TASK-039，档案 §30） ---

func TestChangelogRatio(t *testing.T) {
	big := "## 架构\n" + strings.Repeat("x", 3000) + "\n## Changelog\n" + strings.Repeat("y", 2000) + "\n"
	if r := ChangelogRatio([]byte(big)); r < 0.39 || r > 0.41 {
		t.Fatalf("changelog ratio ≈40%% expected, got %.2f", r)
	}
	// 子节也属 changelog；中文「变更记录」同判。
	cn := "# 卡\n正文\n## 变更记录\n- a\n### 2026\n- b\n"
	if r := ChangelogRatio([]byte(cn)); r == 0 {
		t.Fatalf("chinese 变更记录 section must count, got 0")
	}
	if r := ChangelogRatio([]byte("# 只有正文\n无节\n")); r != 0 {
		t.Fatalf("no section must be 0, got %.2f", r)
	}
}

func TestContractRefs(t *testing.T) {
	systems := []model.System{
		{ID: "data", ContractDoc: "datax/INFO-CARD.md"},
		{ID: "mlake", ContractDoc: "deploy/docs/INFO-CARD.md"},
		{ID: "ui"}, // 未声明 → 无 ref
	}
	refs := ContractRefs(systems,
		func(sys string) (string, bool) {
			if sys == "data" {
				return "/repos/datax-data", true
			}
			return "", false // mlake 本机未映射 → 跳过
		},
		func(repoDir, rel string) (int64, bool) {
			if repoDir == "/repos/datax-data" && rel == "datax/INFO-CARD.md" {
				return 216088, true
			}
			return 0, false
		})
	if len(refs) != 1 || refs[0].Path != "data:datax/INFO-CARD.md" || refs[0].Role != RoleContract || refs[0].Owner != "data" || refs[0].Size != 216088 {
		t.Fatalf("contract refs wrong: %+v", refs)
	}
	if !refs[0].Claimed {
		t.Fatalf("contract doc is claimed by definition")
	}
}

func TestBloatThreeSignals(t *testing.T) {
	refs := []Ref{
		{Path: "data:datax/INFO-CARD.md", Role: RoleContract, Owner: "data", Claimed: true, Size: 216088},
		{Path: "docs/design/慢涨.md", Role: RoleDecision, Owner: "t1", Claimed: true, Size: 55 * 1024},
		{Path: "docs/design/健康.md", Role: RoleDecision, Owner: "t2", Claimed: true, Size: 8 * 1024},
		{Path: "docs/acceptance/r.md", Role: RoleEvidence, Owner: "T-1", Claimed: true, Size: 900 * 1024}, // 快照不判
		{Path: "docs/design/孤儿.md", Size: 500 * 1024},                                                     // 未认领不判（先治无主）
	}
	contents := map[string][]byte{
		"data:datax/INFO-CARD.md": []byte("## 服务面\n" + strings.Repeat("a", 80000) + "\n## Changelog\n" + strings.Repeat("b", 130000) + "\n"),
		"docs/design/健康.md":       []byte("# 方案\n" + strings.Repeat("c", 8000)),
	}
	prev := map[string]int64{
		"docs/design/慢涨.md": 10 * 1024, // +30KB/7d → growth
		"docs/design/健康.md": 7 * 1024,  // +1KB → 健康
	}
	got := Bloat(refs, contents, prev, BloatOpts{MaxContractBytes: 30 * 1024, MaxDecisionBytes: 50 * 1024, MaxStatusBytes: 30 * 1024, MaxChangelog: 20, MaxChangelogBytes: 8 * 1024, MaxGrowth: 20 * 1024, Days: 7})
	kinds := map[string]int{}
	for _, f := range got {
		kinds[f.Path+"/"+f.Kind]++
	}
	if kinds["data:datax/INFO-CARD.md/size"] != 1 {
		t.Fatalf("216KB contract must trigger size, got %v", got)
	}
	if kinds["data:datax/INFO-CARD.md/changelog"] != 1 {
		t.Fatalf("59%%-ish changelog must trigger, got %v", got)
	}
	if kinds["docs/design/慢涨.md/growth"] != 1 {
		t.Fatalf("+30KB/7d must trigger growth, got %v", got)
	}
	if kinds["docs/design/慢涨.md/size"] != 1 {
		t.Fatalf("55KB decision over 50KB tier, got %v", got)
	}
	for _, f := range got {
		if f.Path == "docs/design/健康.md" || f.Path == "docs/acceptance/r.md" || f.Path == "docs/design/孤儿.md" {
			t.Fatalf("healthy/evidence/unclaimed must stay silent, got %+v", f)
		}
	}
}

// TASK-042 双门与分级：
// ① changelog 绝对量下限——小文件高占比（53% 但 6.6KB）不报，大文件高占比报；
// ② 体积分级——contract 35KB 报、decision 35KB 不报、decision 55KB 报。
func TestBloatTieredAndAbsoluteFloor(t *testing.T) {
	small := "## 服务面\n" + strings.Repeat("a", 5800) + "\n## Changelog\n" + strings.Repeat("b", 6600) + "\n"     // ~12KB, 53%
	big := "## 服务面\n" + strings.Repeat("a", 10*1024) + "\n## Changelog\n" + strings.Repeat("b", 17*1024) + "\n" // ~27KB, 59%
	refs := []Ref{
		{Path: "m:small.md", Role: RoleContract, Owner: "m", Claimed: true, Size: int64(len(small))},
		{Path: "m:big.md", Role: RoleContract, Owner: "m", Claimed: true, Size: int64(len(big))},
		{Path: "d:35.md", Role: RoleDecision, Owner: "d", Claimed: true, Size: 35 * 1024},
		{Path: "d:55.md", Role: RoleDecision, Owner: "d", Claimed: true, Size: 55 * 1024},
		{Path: "c:35.md", Role: RoleContract, Owner: "c", Claimed: true, Size: 35 * 1024},
	}
	contents := map[string][]byte{"m:small.md": []byte(small), "m:big.md": []byte(big)}
	o := BloatOpts{MaxContractBytes: 30 * 1024, MaxDecisionBytes: 50 * 1024, MaxStatusBytes: 30 * 1024,
		MaxChangelog: 20, MaxChangelogBytes: 8 * 1024}
	got := Bloat(refs, contents, nil, o)
	has := func(p, k string) bool {
		for _, f := range got {
			if f.Path == p && f.Kind == k {
				return true
			}
		}
		return false
	}
	if has("m:small.md", "changelog") {
		t.Fatalf("small file high ratio must not trigger (absolute floor):\n%v", got)
	}
	if !has("m:big.md", "changelog") {
		t.Fatalf("big file high ratio must trigger:\n%v", got)
	}
	if has("d:35.md", "size") {
		t.Fatalf("35KB decision under 50KB tier must stay silent:\n%v", got)
	}
	if !has("d:55.md", "size") || !has("c:35.md", "size") {
		t.Fatalf("55KB decision / 35KB contract must trigger:\n%v", got)
	}
	// MaxBytes<0 = 显式关①（分级全跳）。
	o.MaxBytes = -1
	got2 := Bloat(refs, contents, nil, o)
	for _, f := range got2 {
		if f.Kind == "size" {
			t.Fatalf("MaxBytes<0 must disable size signal:\n%v", got2)
		}
	}
}
