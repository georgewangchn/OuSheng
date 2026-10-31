package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ousheng/internal/docs"
	"ousheng/internal/index"
	"ousheng/internal/machine"
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
// 契约正本（system.contract_doc，§30）在此并入 refs：repos.yaml 解析映射仓，
// 未映射/缺文件静默跳过（best-effort 读面，机器差异合法）。
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
	refs = append(refs, docs.ContractRefs(snap.Systems, contractRepoResolver(dir),
		func(repoDir, rel string) (int64, bool) {
			fi, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(rel)))
			if err != nil || fi.IsDir() {
				return 0, false
			}
			return fi.Size(), true
		})...)
	// git 时间一次批量取（逐条认领单起一次 git log 会把命令拖死）。
	var want []string
	for _, r := range refs {
		if r.Claimed && !r.External && !isContractRef(r.Path) {
			want = append(want, r.Path)
		}
	}
	times := repo.DocUpdated(want)
	for i := range refs {
		if t, ok := times[refs[i].Path]; ok {
			refs[i].Updated = t
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return docLayer{
		refs:   refs,
		stale:  docs.FindStale(refs, snap.Designs, snap.WorkItems),
		claims: claims,
		roots:  docs.Roots(claims),
	}
}

// isContractRef 报告 Ref 是否契约正本（"<system>:<rel>" 命名空间）。
// 判据：冒号出现在首个 "/" 之前且非首字符——工作区相对路径里的冒号
// （docs/a:b.md）冒号前必含 "/"，不误判（评审 🟡：原 Contains 会把这类
// 路径误踢出 DocUpdated 时间管道，陈旧判定静默失效）。
func isContractRef(p string) bool {
	i := strings.Index(p, ":")
	return i > 0 && !strings.Contains(p[:i], "/")
}

// contractRepoResolver 返回 system → 本机映射仓路径。只认 repos.yaml 显式映射
// （机器没映射 = 该机看不见契约正本，合法跳过）——不走工作区回退：契约正本
// 住系统仓，看板仓里同路径文件是另一回事，不冒认。
func contractRepoResolver(wsDir string) func(system string) (string, bool) {
	m := machine.LoadRepoMap(wsDir)
	return func(system string) (string, bool) {
		p, ok := m[system]
		if !ok || p == "" {
			return "", false
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return "", false
		}
		return p, true
	}
}

// bloatInputs 为 bloat 三信号收集内容与环比体积（仅 doc check 调用——
// converge 路径不付这笔 git/IO 成本）。prev 体积用 `git cat-file -s <rev>:<path>`
// 取 Days 天前 blob 字节数：每仓一次 rev-list，随后每文档一次 cat-file。
func bloatInputs(dir string, refs []docs.Ref, days int) (contents map[string][]byte, prev map[string]int64) {
	contents, prev = map[string][]byte{}, map[string]int64{}
	repoDirs := map[string]string{dir: dir} // 工作区仓
	for _, r := range refs {
		if !r.Claimed || r.External {
			continue
		}
		switch r.Role {
		case docs.RoleDecision, docs.RoleStatus, docs.RoleContract:
		default:
			continue
		}
		var repoDir, rel string
		if isContractRef(r.Path) {
			i := strings.Index(r.Path, ":")
			sys, rel2 := r.Path[:i], r.Path[i+1:]
			rd, ok := contractRepoResolver(dir)(sys)
			if !ok {
				continue
			}
			repoDir, rel = rd, rel2
		} else {
			repoDir, rel = dir, r.Path
		}
		if _, ok := repoDirs[repoDir]; !ok {
			repoDirs[repoDir] = repoDir
		}
		if b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel))); err == nil {
			contents[r.Path] = b
		}
	}
	if days <= 0 {
		return contents, prev
	}
	for repoDir := range repoDirs {
		out, err := gitOutput(repoDir, "rev-list", "-1", fmt.Sprintf("--before=%d days ago", days), "HEAD")
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		rev := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		for _, r := range refs {
			if !r.Claimed || r.External {
				continue
			}
			var rel string
			if isContractRef(r.Path) {
				i := strings.Index(r.Path, ":")
				sys := r.Path[:i]
				rd, ok := contractRepoResolver(dir)(sys)
				if !ok || rd != repoDir {
					continue
				}
				rel = r.Path[i+1:]
			} else if repoDir != dir {
				continue
			} else {
				rel = r.Path
			}
			if s, err := gitOutput(repoDir, "cat-file", "-s", rev+":"+rel); err == nil {
				if n, perr := strconv.ParseInt(strings.TrimSpace(s), 10, 64); perr == nil {
					prev[r.Path] = n
				}
			}
		}
	}
	return contents, prev
}

// gitOutput 在 dir 跑一条 git 命令（doc 层专用轻封装，best-effort）。
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	bloatBytes := fs.Int64("bloat-bytes", 0, "bloat signal 1: override ALL role tiers when >0 (takes precedence over --bloat-contract/decision; 0=per-role defaults, negative=off)")
	bloatContract := fs.Int64("bloat-contract", 30*1024, "bloat signal 1: max bytes per contract/status doc")
	bloatDecision := fs.Int64("bloat-decision", 50*1024, "bloat signal 1: max bytes per decision doc (方案正本可长)")
	bloatChangelog := fs.Int("bloat-changelog", 20, "bloat signal 2: max changelog/history section share in percent (0=off)")
	bloatChangelogBytes := fs.Int64("bloat-changelog-bytes", 8*1024, "bloat signal 2: absolute floor for the changelog section (小文件高占比不报；0=占比单门)")
	bloatGrowth := fs.Int64("bloat-growth", 20*1024, "bloat signal 3: max byte growth within --bloat-days (0=off)")
	bloatDays := fs.Int("bloat-days", 7, "bloat signal 3: growth window in days")
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
	opts := docs.BloatOpts{
		MaxBytes: *bloatBytes, MaxContractBytes: *bloatContract, MaxDecisionBytes: *bloatDecision,
		MaxStatusBytes: *bloatContract, MaxChangelog: *bloatChangelog, MaxChangelogBytes: *bloatChangelogBytes,
		MaxGrowth: *bloatGrowth, Days: *bloatDays,
	}
	// 全信号关闭时跳过 bloatInputs（评审 🔵：不白付 git rev-list/cat-file/读文件）。
	var contents map[string][]byte
	var prev map[string]int64
	sizeOff := opts.MaxBytes < 0 || (opts.MaxBytes == 0 && opts.MaxContractBytes <= 0 && opts.MaxDecisionBytes <= 0 && opts.MaxStatusBytes <= 0)
	if !sizeOff || opts.MaxChangelog > 0 || opts.MaxGrowth > 0 {
		contents, prev = bloatInputs(fs.Dir(), layer.refs, *bloatDays)
	}
	bloat := docs.Bloat(layer.refs, contents, prev, opts)
	if *asJSON {
		printJSON(stdout, map[string]any{"orphans": orphans, "stale": layer.stale, "tiny": tiny, "bloat": bloat})
		return 0
	}
	if len(orphans) == 0 && len(layer.stale) == 0 && len(tiny) == 0 && len(bloat) == 0 {
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
	for _, b := range bloat {
		fmt.Fprintf(stdout, "doc bloat: %s (%s, %s) [%s] %s\n", b.Path, b.Role, b.Owner, b.Kind, b.Detail)
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
