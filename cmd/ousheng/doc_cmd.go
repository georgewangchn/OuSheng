package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ousheng/internal/docs"
	"ousheng/internal/index"
	"ousheng/internal/state/gityaml"
)

// cmdDoc 是文档认领层的 CLI 入口（文档认领层设计 §4）。
//
//	ousheng doc map      户口本：受监控文档的认领态
//	ousheng doc check    审计：孤儿 + 陈旧明细
//	ousheng doc census   普查：未纳管区域（零认领的目录树）
//
// 三条命令全部只读。语义收在 internal/docs，本文件只做参数适配与输出
// （一语义一实现：converge 与 MCP 复用同一包）。
func cmdDoc(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ousheng doc <map|check|census> [flags]")
		return 2
	}
	switch args[0] {
	case "map":
		return docMap(args[1:], stdout, stderr)
	case "check":
		return docCheck(args[1:], stdout, stderr)
	case "census":
		return docCensus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown doc subcommand: %s\n", args[0])
		return 2
	}
}

// docLayer 组装一次认领层计算所需的全部输入。
type docLayer struct {
	refs   []docs.Ref
	stale  []docs.StaleFinding
	claims []docs.Claim
	roots  []string
}

// buildDocLayer 装载一次认领层计算（CLI 入口）。
func buildDocLayer(dir string) (docLayer, error) {
	_, snap, err := loadSnapshot(dir)
	if err != nil {
		return docLayer{}, err
	}
	return assembleDocLayer(dir, snap), nil
}

// assembleDocLayer 组装认领层——`ousheng doc` 三条命令与 converge 共用同一份
// 实现（一语义一实现：语义在 internal/docs，装载只此一处）。
func assembleDocLayer(dir string, snap index.Snapshot) docLayer {
	repo := gityaml.Open(dir)
	claims := gityaml.DocClaims(snap)
	files, err := repo.DocFiles()
	if err != nil || len(files) == 0 {
		// 非 git 仓，或 git 仓里还没有任何跟踪的 .md（刚 init 尚未提交）：
		// 退化为"只跟踪认领指向的文件"。不扫目录就没有孤儿判定
		// （无扫描面就无未知），但认领态与角色归属仍然可读，不响亮失败。
		files = existingClaimFiles(dir, claims)
	}
	refs := docs.Build(files, claims)
	// git 时间一次批量取（逐条认领单起一次 git log 会把命令拖死）。
	var want []string
	for _, r := range refs {
		if r.Claimed && !r.External {
			want = append(want, r.Path)
		}
	}
	times := repo.DocUpdated(want)
	for i := range refs {
		if t, ok := times[refs[i].Path]; ok {
			refs[i].Updated = t
		}
	}
	return docLayer{
		refs:   refs,
		stale:  docs.FindStale(refs, snap.Designs, snap.WorkItems),
		claims: claims,
		roots:  docs.Roots(claims),
	}
}

// existingClaimFiles 按文件系统核对认领路径：只有真实存在的文件才进清单。
// 认领可以指向尚未落地的文档——那不是错误，只是暂时无本可查。
// 结构性文件（README/INDEX）同样剔除——显式认领目录自述属误用，不纵容。
func existingClaimFiles(dir string, claims []docs.Claim) []docs.File {
	var out []docs.File
	for _, p := range docs.ClaimPaths(claims) {
		if docs.IsExternal(p) || docs.Exempt(p) {
			continue // 越出工作区 / 目录自述，本层不跟踪
		}
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		out = append(out, docs.File{Path: p, Size: fi.Size()})
	}
	return out
}

func docMap(args []string, stdout, stderr io.Writer) int {
	fs := newFS("doc map")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	layer, err := buildDocLayer(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *asJSON {
		printJSON(stdout, layer.refs)
		return 0
	}
	if len(layer.refs) == 0 {
		fmt.Fprintln(stdout, "no monitored documents (no claims yet — run `ousheng doc census` to find unclaimed regions)")
		return 0
	}
	fmt.Fprintf(stdout, "%-56s %-10s %-24s %-12s %s\n", "PATH", "ROLE", "OWNER", "UPDATED", "NOTE")
	for _, r := range layer.refs {
		role, owner := string(r.Role), r.Owner
		if !r.Claimed {
			role, owner = "—", "—（无认领）"
		}
		updated := "—"
		if !r.Updated.IsZero() {
			updated = r.Updated.Format("2006-01-02")
		}
		note := ""
		if !r.Claimed {
			note = "ORPHAN"
		} else if r.External {
			note = "EXTERNAL（无 git 时间，不判陈旧）"
		}
		fmt.Fprintf(stdout, "%-56s %-10s %-24s %-12s %s\n", truncPath(r.Path, 56), role, owner, updated, note)
	}
	fmt.Fprintf(stdout, "\nmonitored roots (auto-derived from claims): %s\n", strings.Join(layer.roots, ", "))
	return 0
}

func docCheck(args []string, stdout, stderr io.Writer) int {
	fs := newFS("doc check")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	layer, err := buildDocLayer(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	orphans := docs.Orphans(layer.refs)
	tiny := docs.Tiny(layer.refs)
	if *asJSON {
		printJSON(stdout, map[string]any{"orphans": orphans, "stale": layer.stale, "tiny": tiny})
		return 0
	}
	if len(orphans) == 0 && len(layer.stale) == 0 && len(tiny) == 0 {
		fmt.Fprintln(stdout, "doc check: clean — every monitored .md has exactly one claim, and no doc lags its owner")
		return 0
	}
	for _, o := range orphans {
		fmt.Fprintf(stdout, "doc orphan: %s\n", o.Path)
	}
	for _, s := range layer.stale {
		fmt.Fprintf(stdout, "doc stale: %s (%s, owned by %s) — %d done deliveries after doc update (latest %s at %s)\n",
			s.Path, s.Role, s.Owner, s.Count, s.LatestID, s.Latest.Format("2006-01-02"))
	}
	for _, r := range tiny {
		fmt.Fprintf(stdout, "doc tiny: %s (%dB < %dB) — decision doc suspiciously small (stub, not a plan?)\n",
			r.Path, r.Size, docs.TinyFloor)
	}
	return 0
}

func docCensus(args []string, stdout, stderr io.Writer) int {
	fs := newFS("doc census")
	asJSON := fs.Bool("json", false, "output JSON")
	minFiles := fs.Int("min", 3, "minimum .md count to report a region")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	_, snap, err := loadSnapshot(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repo := gityaml.Open(fs.Dir())
	files, err := repo.DocFiles()
	if err != nil {
		fmt.Fprintln(stdout, "doc census: not a git repository — nothing to census")
		return 0
	}
	claims := gityaml.DocClaims(snap)
	regions := docs.Census(files, claims, *minFiles)
	if *asJSON {
		printJSON(stdout, regions)
		return 0
	}
	if len(regions) == 0 {
		fmt.Fprintf(stdout, "doc census: clean — no unclaimed region with ≥%d .md files\n", *minFiles)
		return 0
	}
	fmt.Fprintf(stdout, "%-48s %s\n", "UNCLAIMED REGION", ".MD FILES")
	for _, r := range regions {
		fmt.Fprintf(stdout, "%-48s %d\n", r.Dir, r.Files)
	}
	fmt.Fprintln(stdout, "\nthese trees sit outside every claim-derived root and still hold unclaimed docs:")
	fmt.Fprintln(stdout, "claim one doc there (design doc / evidence locator) and the tree joins monitoring.")
	return 0
}

// truncPath 截断过长的路径，避免表格被撑爆。
func truncPath(p string, n int) string {
	if len(p) <= n {
		return p
	}
	return "…" + p[len(p)-n+3:]
}
