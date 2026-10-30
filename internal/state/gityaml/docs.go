package gityaml

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ousheng/internal/docs"
	"ousheng/internal/index"
)

// TrackedMarkdown 返回 git 跟踪的全部 .md 路径（相对工作区根）。
//
// 用 `git ls-files` 而非文件系统遍历：天然遵守 .gitignore，
// 不需要另做排除面（对标参考、构建产物、依赖目录全部自动出局）。
// 非 git 仓返回空——文档认领层降级为"仅 design.doc 可判"，不响亮失败。
//
// `-c core.quotepath=false` 是必需的：中文文件名默认被 git 转义成
// `"docs/\345\255\246..."` 这种八进制引用形态，不关掉 quotepath 的话
// 整个中文文档生态对本层完全不可见（实测实栈 38 篇 .md 中 24 篇是中文名）。
func (r *Repo) TrackedMarkdown() ([]string, error) {
	out, err := gitRun(r.Dir, "-c", "core.quotepath=false", "ls-files", "*.md")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, filepath.ToSlash(line))
	}
	sort.Strings(files)
	return files, nil
}

// DocUpdated 返回给定路径的最后 git 提交时间（%cI），按路径索引。
//
// 单次 `git log --format=... --name-only` 遍历历史，取每个路径最后一次
// 出现对应的提交时间——避免逐文件 `git log -1` 的 N 次子进程。
// 无历史的路径（新建未提交）退 file mtime；git 不可用则缺项。
//
// best-effort 语义与 ArchitectureUpdated 一致：这是 warning 面的派生输入，
// 缺位只导致该文档不参与陈旧判定，不构成数据损坏。
func (r *Repo) DocUpdated(paths []string) map[string]time.Time {
	out := map[string]time.Time{}
	if len(paths) == 0 {
		return out
	}
	// 过滤掉越界/绝对路径：git 在工作区内跑，外部路径取不到时间。
	var local []string
	for _, p := range paths {
		if path.IsAbs(p) || strings.HasPrefix(p, "../") {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(p))); err != nil {
			continue
		}
		local = append(local, p)
	}
	if len(local) == 0 {
		return out
	}

	args := append([]string{"-c", "core.quotepath=false", "log", "--format=commit %cI", "--name-only"}, "--")
	args = append(args, local...)
	raw, err := gitRun(r.Dir, args...)
	if err != nil {
		// 非仓库/无历史：mtime 兜底，让新建文档不误报滞后。
		for _, p := range local {
			if fi, serr := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(p))); serr == nil {
				out[p] = fi.ModTime()
			}
		}
		return out
	}

	// git log 输出是时间倒序：每个路径第一次出现即其最后提交。
	var cur time.Time
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "commit ") {
			if t, perr := time.Parse(time.RFC3339, strings.TrimPrefix(line, "commit ")); perr == nil {
				cur = t
			}
			continue
		}
		p := filepath.ToSlash(line)
		if _, ok := out[p]; !ok && !cur.IsZero() {
			out[p] = cur
		}
	}
	for _, p := range local {
		if _, ok := out[p]; !ok {
			if fi, serr := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(p))); serr == nil {
				out[p] = fi.ModTime()
			}
		}
	}
	return out
}

// DocFiles 组装文档认领层所需的扫描输入（文件清单 + 体积）。
// 只收 .md：drawio/svg 等图形载体不参与孤儿判定（认领层设计 §4.1）。
// 结构性文件（README/INDEX，§三 边界 1）在此剔除——目录自述不属任何
// 知识单元，不进 refs 就不会被报孤儿（Census 侧另有同款过滤兜底）。
func (r *Repo) DocFiles() ([]docs.File, error) {
	tracked, err := r.TrackedMarkdown()
	if err != nil {
		return nil, err
	}
	out := make([]docs.File, 0, len(tracked))
	for _, p := range tracked {
		if docs.Exempt(p) {
			continue
		}
		fi, serr := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(p)))
		if serr != nil {
			continue
		}
		out = append(out, docs.File{Path: p, Size: fi.Size()})
	}
	return out, nil
}

// DocClaims 从既有数据派生认领集（design.doc + architecture 约定路径 +
// work item document 证据 locator）。不新增任何存储。
func DocClaims(snap index.Snapshot) []docs.Claim {
	return docs.Claims(snap.Designs, snap.Architecture, snap.WorkItems)
}
