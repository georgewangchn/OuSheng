// Package docs 是文档认领层的语义核心（文档认领层设计 §四）。
//
// 定位：ousheng 是牵引绳，不做文档容器。本包不存任何文档内容，只回答两个
// 可机械判定的问题——
//
//	这篇文档归谁？（集合判定：有/无认领）
//	这篇文档还作不作数？（时间判定：文档提交 vs 认领方的 done 交付）
//
// 三条设计不变量在此落地：
//
//	D1 单一认领  —— 受管辖文档有且只有一个认领主体
//	D2 角色由来源 —— 角色是认领机制的属性，不存为字段
//	D3 未知即曝光 —— 受监控区域内的未认领 .md 必须报出
//
// 本包是纯函数，不做任何 I/O：文件清单与 git 时间由 state 层注入，
// 便于测试与复用（CLI / converge / MCP 三入口只做参数适配）。
//
// 扫描范围只有一条来源：认领关系。认领指向的文件永远受跟踪；
// 认领所在目录自动纳入监控；两者都不涉及的 .md 一律不打扰。
package docs

import (
	"path"
	"sort"
	"strings"
	"time"

	"ousheng/internal/model"
)

// Role 是文档角色。角色不由字段声明，而由**认领机制**唯一决定（D2）。
type Role string

const (
	// RoleDecision 设计方案——由 design.doc 认领，应随实现演进（陈旧可判）。
	RoleDecision Role = "decision"
	// RoleStatus 系统现状卡——由 .ousheng/architecture/<sys>.md 约定路径认领，
	// 应随系统演进（陈旧可判）。
	RoleStatus Role = "status"
	// RoleEvidence 验收/核验报告——由 work item 的 document 证据 locator 认领，
	// 是完成时快照，本就不该再变（陈旧不可判）。
	RoleEvidence Role = "evidence"
)

// Claim 是一条认领声明。认领不等于占有——文件仍在 git，
// 认领等于"我知道它归谁"。
type Claim struct {
	// Path 是工作区相对路径（可含 ../ 越界）。弱认领允许是
	// "docs/acceptance/x.md@e1aeb34" 这类带装饰的值。
	Path string
	Role Role
	// Owner 是认领主体：design topic / 系统 ID / work item ID。
	Owner string
	// Weak 标记弱认领（来自 evidence locator）。弱认领按 basename 匹配，
	// 因为 locator 常带 commit 后缀、且归档迁移目录后仍应算数。
	Weak bool
}

// File 是扫描到的一个文档（由 state 层提供，本包不读盘）。
type File struct {
	Path string
	Size int64
}

// Ref 是一篇受监控文档的认领状态。
type Ref struct {
	Path    string
	Role    Role
	Owner   string
	Claimed bool
	Size    int64
	// Updated 是文档的最后 git 提交时间；零值 = 拿不到（越界/未跟踪）。
	Updated time.Time
	// External 标记越出工作区的认领（越界者不参与陈旧判定）。
	External bool
}

// Norm 归一化工作区相对路径：统一 / 分隔并 Clean。
// 绝对路径原样返回（越界认领在 IsExternal 判定，不在此改写）。
func Norm(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if path.IsAbs(p) {
		return p
	}
	return path.Clean(p)
}

// IsExternal 报告认领是否越出工作区（../ 越界或绝对路径）。
// 越界认领合法但降级：只读、无 git 时间、不参与陈旧判定（§三 边界 2）。
func IsExternal(p string) bool {
	return strings.HasPrefix(Norm(p), "../") || path.IsAbs(Norm(p))
}

// Exempt 报告结构性文件——它们属于目录本身，不属于任何知识单元，
// 因此不参与孤儿判定（§三 边界 1）。
//
// 这是全包唯一硬编码的文件名白名单，且与项目无关：任何项目的
// 目录自述都叫这两个名字。
func Exempt(p string) bool {
	base := path.Base(Norm(p))
	return base == "README.md" || base == "INDEX.md"
}

// weakKey 从弱认领值提取匹配键：取 basename，并以第一个 ".md" 为界截断。
//
// 这条规则吸收实测中的两类脏值：
//
//	docs/acceptance/T-038-联合测试报告.md@e1aeb34        → T-038-联合测试报告.md
//	docs/acceptance/BUG-041-042-mlake查证与修复.md (commit 23c3fc2) → BUG-041-042-mlake查证与修复.md
//
// 注意**不剥日期**——日期是文件名本体（1023/1024 两篇核验报告必须区分）。
func weakKey(p string) string {
	s := path.Base(strings.TrimSpace(Norm(p)))
	if i := strings.Index(s, ".md"); i >= 0 {
		return s[:i+3]
	}
	return s
}

// Claims 从既有数据派生认领集。不新增任何存储——认领是投影，不是新事实源。
//
//   - designs  : design.doc → RoleDecision
//   - systems  : .ousheng/architecture/<sys>.md → RoleStatus（约定路径）
//   - items    : type:document 证据 locator → RoleEvidence（弱认领）
func Claims(designs []model.DesignInfo, systems []string, items []model.WorkItem) []Claim {
	var out []Claim

	for _, d := range designs {
		if strings.TrimSpace(d.Design.Doc) == "" {
			continue
		}
		out = append(out, Claim{
			Path:  Norm(d.Design.Doc),
			Role:  RoleDecision,
			Owner: d.Topic,
		})
	}

	for _, sys := range systems {
		if strings.TrimSpace(sys) == "" {
			continue
		}
		out = append(out, Claim{
			Path:  Norm(path.Join(".ousheng", "architecture", sys+".md")),
			Role:  RoleStatus,
			Owner: sys,
		})
	}

	for _, w := range items {
		for _, e := range w.Evidence {
			if e.Type != model.EvidenceDocument {
				continue
			}
			loc := strings.TrimSpace(e.Locator)
			if loc == "" {
				continue
			}
			out = append(out, Claim{
				Path:  Norm(loc),
				Role:  RoleEvidence,
				Owner: w.ID,
				Weak:  true,
			})
		}
	}
	return out
}

// ClaimPaths 返回认领声明指向的路径候选，剥掉 locator 装饰：
//
//	强认领  docs/design/a.md             → docs/design/a.md
//	弱认领  docs/acceptance/x.md@e1aeb34 → docs/acceptance/x.md
//	弱认领  docs/x/y.md (commit 23c3fc2) → docs/x/y.md
//	弱认领  pm-decision-20260928         → 无法解析为路径，跳过
//
// 纯字符串推导，不读盘。唯一用途是无 git 环境下按文件系统核对认领
// （见 CLI 层的降级路径）——有 git 时扫描面由 `git ls-files` 提供，
// 远大于本函数的结果。
func ClaimPaths(claims []Claim) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range claims {
		p := strings.TrimSpace(c.Path)
		if c.Weak {
			i := strings.Index(p, ".md")
			if i < 0 {
				continue // 不是路径形态，无从核对
			}
			p = p[:i+3]
		}
		n := Norm(p)
		if n == "" || n == "." || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// Roots 由认领关系推导受监控目录（D3）。
//
// 硬编码目录清单是错解——每个项目目录结构不同，写死就是把某个项目的
// 习惯变成产品约束。正解是自动推导：
//
//	某目录树里有至少一篇已认领文档 → 该树自动纳入监控
//	某目录树零认领 → 自动豁免（参考区/产品材料区天然免打扰）
//
// 取根只看认领路径的目录成分，并按认领强度分级：
//
//	强认领（design.doc，经字段校验）        → 取直接目录
//	弱认领（evidence locator，自由文本）    → 必须自带目录成分才取根
//
// 后者是必需的：locator 常写成 "pm-decision-20260928" 这种无目录的值，
// 对它取 dirname 会得到 "."，进而把整个仓库拖进扫描。而形如
// "docs/acceptance/T-038-复测报告-1021.md@e1aeb34" 的 locator 确实指向
// 一个文档堆积处，应当取根。
//
// 落在工作区根的认领不取根（无论强弱）——一个根级文档不该把全仓拖进
// 扫描；该文件本身仍受跟踪，见 Build 的第②类。
//
// `.ousheng/` 下的根一律丢弃——那是绳自己的地盘，内容由绳自己管。
func Roots(claims []Claim) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range claims {
		dir, ok := claimDir(c.Path, !c.Weak)
		if !ok {
			continue
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// claimDir 返回认领声明所在的目录；ok=false 表示该声明不产生扫描根。
//
// strong 区分认领强度：强认领经字段校验、语义明确；弱认领是人工填写的
// 自由文本，形态不定，判别必须更保守——
//
//	docs/acceptance/x.md@e1aeb34          → docs/acceptance
//	docs/x/BUG-041-042-mlake查证与修复.md (commit 23c3fc2) → docs/x
//	pm-decision-20260928                  → 无目录成分，不取
//	NOTES.md                              → 落在工作区根，不取
//	../mlake/docs/ARCHITECTURE.md         → 越界，不取
//	/abs/path.md、https://example.com/a.md → 不是工作区内路径，不取
//	.ousheng/architecture/data.md         → 绳自己的地盘，不取
//
// 这条判别决定扫描面，因此宁可漏判不可误判：漏判的代价是一片区域不被扫
// （census 仍会曝光它），误判的代价是把整个仓库拖进扫描。
func claimDir(p string, strong bool) (string, bool) {
	s := strings.TrimSpace(p)
	if s == "" || strings.Contains(s, "://") {
		return "", false
	}
	if IsExternal(s) {
		return "", false
	}
	dir := path.Dir(Norm(s))
	if dir == ".ousheng" || strings.HasPrefix(dir, ".ousheng/") {
		return "", false
	}
	if dir == "." || dir == "/" || dir == "" {
		// 落在工作区根本身：只有强认领可能到这里，且一律不取根
		// （否则一个根级文档等于监控全仓）。文件本身由 Build 第②类兜住。
		return "", false
	}
	if !strong && !strings.Contains(Norm(s), "/") {
		return "", false
	}
	return dir, true
}

// Under 报告路径 p 是否在目录 root 之下（root 为 "." 表示整个工作区）。
func Under(p, root string) bool {
	p = Norm(p)
	root = Norm(root)
	if root == "." {
		return !strings.HasPrefix(p, ".ousheng/") && p != ".ousheng"
	}
	return p == root || strings.HasPrefix(p, root+"/")
}

// Build 把扫描到的文件清单与认领集合成 Ref 集。
//
// 进入结果的有两类文件：
//
//	① 落在受监控根之下的文件（根由 Roots 从认领路径推导）；
//	② 被某条认领直接指向的文件——无论它落在哪里。
//
// ② 不可省，它是"认领指向的文件永远受跟踪"这条不变量的落地：
// 弱认领按 basename 匹配，所以文档被归档迁移（I3 要求落账后移 archive）
// 后仍然算数；强认领落在工作区根时不产生根，也靠②兜住。
//
// 树外且无认领的文件不进结果——它们天然不受管，不做孤儿判定
// （否则参考区/产品材料区会被误报）。
func Build(files []File, claims []Claim) []Ref {
	strong, weak := splitClaims(claims)
	roots := Roots(claims)
	refs := make([]Ref, 0, len(files))
	for _, f := range files {
		p := Norm(f.Path)
		c, ok := matchClaim(p, strong, weak)
		if !ok && !monitored(p, roots) {
			continue
		}
		r := Ref{Path: p, Size: f.Size}
		if ok {
			r.Claimed, r.Role, r.Owner = true, c.Role, c.Owner
			r.External = IsExternal(c.Path)
		}
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs
}

// splitClaims 把认领集拆成强（路径相等命中）与弱（basename 命中）两张表。
func splitClaims(claims []Claim) (strong, weak map[string]Claim) {
	strong, weak = map[string]Claim{}, map[string]Claim{}
	for _, c := range claims {
		if c.Weak {
			weak[weakKey(c.Path)] = c
			continue
		}
		strong[Norm(c.Path)] = c
	}
	return strong, weak
}

// matchClaim 返回路径 p 的认领方：先强（路径相等）后弱（basename）。
// 同一文件被强弱两条认领同时命中时强认领胜出——它更精确（D1）。
func matchClaim(p string, strong, weak map[string]Claim) (Claim, bool) {
	if c, ok := strong[p]; ok {
		return c, true
	}
	if k := weakKey(p); k != "" {
		if c, ok := weak[k]; ok {
			return c, true
		}
	}
	return Claim{}, false
}

// monitored 报告路径是否落在任一受监控根之下。
func monitored(p string, roots []string) bool {
	for _, r := range roots {
		if Under(p, r) {
			return true
		}
	}
	return false
}

// Orphans 返回受监控区域内未被认领的文档（D3：未知即曝光）。
// 结构性文件已由调用方在扫描前剔除。
func Orphans(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if !r.Claimed {
			out = append(out, r)
		}
	}
	return out
}

// TinyFloor 是方案文档的体积下限（doc-lifecycle 方案 §十一 裁决 4：
// 500B，违反报 warning 不拒——一页纸类方案实测存在，只曝光不拦截）。
const TinyFloor = 500

// Tiny 返回疑似指针壳的方案文档：decision 角色且体积低于下限。
// 只盯 decision——evidence 报告可短（完成时快照一句话合法），
// architecture 卡实践上无小者，越界认领拿不到 Size。
func Tiny(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if r.Claimed && r.Role == RoleDecision && !r.External && r.Size > 0 && r.Size < TinyFloor {
			out = append(out, r)
		}
	}
	return out
}

// StaleFinding 是一篇文档滞后于其认领方 done 交付的曝光。
type StaleFinding struct {
	Path     string
	Role     Role
	Owner    string
	DocTime  time.Time
	Latest   time.Time
	LatestID string
	Count    int
}

// FindStale 计算陈旧文档：文档最后更新时间早于认领方 done 单的最晚证据时间。
//
// 覆盖面必须诚实：decision 角色的认领方是 topic，比较基准是该 topic 的
// `related_items`；若 topic 未申报 related_items，则无从比较、静默跳过。
// 这不是缺陷被掩盖，是 D2 的推论——认领关系决定可比性。
func FindStale(refs []Ref, designs []model.DesignInfo, items []model.WorkItem) []StaleFinding {
	byID := map[string]model.WorkItem{}
	for _, w := range items {
		byID[w.ID] = w
	}

	// design topic → 其管辖的 done 单（经 related_items）。
	topicItems := map[string][]model.WorkItem{}
	for _, d := range designs {
		if d.Design.Status == model.DesignSuperseded || d.Design.Status == model.DesignWithdrawn {
			continue
		}
		for _, id := range d.Design.RelatedItems {
			w, ok := byID[id]
			if !ok || w.Status != model.StatusDone {
				continue
			}
			topicItems[d.Topic] = append(topicItems[d.Topic], w)
		}
	}

	// system → 其 done 单。
	sysItems := map[string][]model.WorkItem{}
	for _, w := range items {
		if w.Status != model.StatusDone || w.System == "" {
			continue
		}
		sysItems[w.System] = append(sysItems[w.System], w)
	}

	var out []StaleFinding
	for _, r := range refs {
		if !r.Claimed || r.Updated.IsZero() || r.External {
			continue
		}
		var pool []model.WorkItem
		switch r.Role {
		case RoleDecision:
			pool = topicItems[r.Owner]
		case RoleStatus:
			pool = sysItems[r.Owner]
		default:
			continue // RoleEvidence 是完成时快照，陈旧不可判
		}
		if len(pool) == 0 {
			continue
		}
		var latest time.Time
		var latestID string
		count := 0
		for _, w := range pool {
			at, ok := LatestEvidenceTime(w)
			if !ok {
				continue
			}
			if at.After(r.Updated) {
				count++
			}
			if at.After(latest) {
				latest, latestID = at, w.ID
			}
		}
		if count == 0 {
			continue
		}
		out = append(out, StaleFinding{
			Path:     r.Path,
			Role:     r.Role,
			Owner:    r.Owner,
			DocTime:  r.Updated,
			Latest:   latest,
			LatestID: latestID,
			Count:    count,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// LatestEvidenceTime 返回 work item 证据中最晚的 observed_at。
// 无证据/全部不可解析 = false。
func LatestEvidenceTime(w model.WorkItem) (time.Time, bool) {
	var latest time.Time
	for _, e := range w.Evidence {
		t, err := time.Parse(time.RFC3339, e.ObservedAt)
		if err != nil {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest, !latest.IsZero()
}

// Region 是一个未纳管区域：目录树下有若干 .md 却零认领（§4.4 census）。
type Region struct {
	Dir   string
	Files int
	// Claimed 是同一树下的认领数；>0 说明已纳管，不会进结果。
	Claimed int
}

// Census 发现未纳管区域——D3 的盲区：零认领的目录树永不被扫，
// 若有人在那里堆文档，永远不可见。
//
// 普查不报文件级孤儿（那是 firehose），报**区域级**信号：一条推动人处理一片。
// 判定条件是"该目录不在任何受监控根之下，且存在未认领文件"：
// 全部文件都有认领的目录不报——归档迁走的验收报告仍被 locator 的
// basename 命中，报它就是制造噪声。
func Census(files []File, claims []Claim, minFiles int) []Region {
	strong, weak := splitClaims(claims)
	roots := Roots(claims)
	tree := map[string]*Region{}
	for _, f := range files {
		p := Norm(f.Path)
		if path.IsAbs(p) || Exempt(p) {
			continue
		}
		if p == ".ousheng" || strings.HasPrefix(p, ".ousheng/") {
			continue // 绳自己的地盘，不参与普查
		}
		dir := path.Dir(p)
		if dir == "." || dir == "" {
			continue // 工作区根的散文件不成"区域"
		}
		r := tree[dir]
		if r == nil {
			r = &Region{Dir: dir}
			tree[dir] = r
		}
		r.Files++
		if _, ok := matchClaim(p, strong, weak); ok {
			r.Claimed++
		}
	}
	var out []Region
	for _, r := range tree {
		// 已在受监控根之下 = 已纳管（树内未认领文件由 orphan 负责）。
		if monitored(r.Dir, roots) {
			continue
		}
		if r.Claimed >= r.Files || r.Files < minFiles {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Dir < out[j].Dir
	})
	return out
}
