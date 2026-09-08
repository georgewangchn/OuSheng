// Package gityaml 是 state.Repository 的 Git + YAML 实现（v0.3 §27/§29）。
//
// 目录布局（workspace root = git root）：
//
//	.ousheng/
//	  project.yaml
//	  systems.yaml
//	  roles.yaml
//	  assignments.yaml
//	  actors/*.yaml
//	  work/*.yaml
//	  activity/YYYY-MM.jsonl
//	  cache/            ← gitignored，派生索引
//
// CAS 在本层执行（lock + read + compare + write + commit）；
// Git commit 不是 CAS 的替代（v0.3 §33）。
package gityaml

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"ousheng/internal/model"
	"ousheng/internal/state"
)

var workIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

const schemaV2 = 2

// Repo 实现 state.Repository。
type Repo struct {
	Dir         string
	SignCommits bool

	mu sync.Mutex
}

var _ state.Repository = (*Repo)(nil)

func Open(dir string) *Repo {
	r := &Repo{Dir: dir}
	if os.Getenv("OUSHENG_SIGN_COMMITS") == "1" {
		r.SignCommits = true
	}
	return r
}

// --- 布局 ---

func (r *Repo) root() string            { return filepath.Join(r.Dir, ".ousheng") }
func (r *Repo) workDir() string         { return filepath.Join(r.root(), "work") }
func (r *Repo) actorsDir() string       { return filepath.Join(r.root(), "actors") }
func (r *Repo) activityDir() string     { return filepath.Join(r.root(), "activity") }
func (r *Repo) cacheDir() string        { return filepath.Join(r.root(), "cache") }
func (r *Repo) projectPath() string     { return filepath.Join(r.root(), "project.yaml") }
func (r *Repo) systemsPath() string     { return filepath.Join(r.root(), "systems.yaml") }
func (r *Repo) rolesPath() string       { return filepath.Join(r.root(), "roles.yaml") }
func (r *Repo) assignmentsPath() string { return filepath.Join(r.root(), "assignments.yaml") }
func (r *Repo) workPath(id string) (string, error) {
	if !workIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid work item id %q", id)
	}
	return filepath.Join(r.workDir(), id+".yaml"), nil
}
func (r *Repo) actorPath(id string) string {
	return filepath.Join(r.actorsDir(), id+".yaml")
}

// --- Init ---

func (r *Repo) InitWorkspace(project model.Project) error {
	for _, d := range []string{r.workDir(), r.actorsDir(), r.activityDir(), r.cacheDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	pf := model.ProjectFile{SchemaVersion: 1, Project: project}
	if err := writeYAML(r.projectPath(), pf); err != nil {
		return err
	}
	for _, f := range []struct {
		path string
		body string
	}{
		{r.systemsPath(), "schema_version: 1\nsystems: []\n"},
		{r.rolesPath(), "schema_version: 1\nroles: []\n"},
		{r.assignmentsPath(), "schema_version: 1\nassignments: []\n"},
	} {
		if err := os.WriteFile(f.path, []byte(f.body), 0o644); err != nil {
			return err
		}
	}
	// cache 永不入 git（无论外层 .gitignore 怎么写）
	gi := filepath.Join(r.cacheDir(), ".gitignore")
	if err := os.WriteFile(gi, []byte("*\n!.gitignore\n"), 0o644); err != nil {
		return err
	}
	if err := r.gitInit(); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "commit", "-m", "ousheng: init workspace "+project.ID); err != nil {
		// 空仓库骨架重复 init 时可能无可提交内容——容忍
		if !strings.Contains(err.Error(), "nothing to commit") &&
			!strings.Contains(err.Error(), "no changes added to commit") {
			return err
		}
	}
	return nil
}

func (r *Repo) gitInit() error {
	if _, err := gitRun(r.Dir, "rev-parse", "--git-dir"); err == nil {
		return nil // 已是 git 仓库
	}
	if _, err := gitRun(r.Dir, "init", "-b", "main"); err != nil {
		if _, err2 := gitRun(r.Dir, "init"); err2 != nil {
			return err
		}
	}
	if _, err := gitRun(r.Dir, "config", "user.email", "board@ousheng.local"); err != nil {
		return fmt.Errorf("set user.email: %w", err)
	}
	if _, err := gitRun(r.Dir, "config", "user.name", "ousheng"); err != nil {
		return fmt.Errorf("set user.name: %w", err)
	}
	return nil
}

// --- Project / Registries ---

func (r *Repo) GetProject() (model.Project, error) {
	b, err := os.ReadFile(r.projectPath())
	if os.IsNotExist(err) {
		return model.Project{}, fmt.Errorf("not an ousheng workspace (missing %s): %w", r.projectPath(), state.ErrNotFound)
	}
	if err != nil {
		return model.Project{}, err
	}
	pf, err := model.DecodeProjectFile(b)
	if err != nil {
		return model.Project{}, err
	}
	return pf.Project, nil
}

func (r *Repo) ListSystems() ([]model.System, error) {
	b, err := os.ReadFile(r.systemsPath())
	if os.IsNotExist(err) {
		return nil, nil // bootstrap：允许空注册表
	}
	if err != nil {
		return nil, err
	}
	f, err := model.DecodeSystemsFile(b)
	if err != nil {
		return nil, err
	}
	return f.Systems, nil
}

func (r *Repo) GetSystem(id string) (model.System, error) {
	systems, err := r.ListSystems()
	if err != nil {
		return model.System{}, err
	}
	for _, s := range systems {
		if s.ID == id {
			return s, nil
		}
	}
	return model.System{}, fmt.Errorf("system %q: %w", id, state.ErrNotFound)
}

func (r *Repo) ListRoles() ([]model.Role, error) {
	b, err := os.ReadFile(r.rolesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := model.DecodeRolesFile(b)
	if err != nil {
		return nil, err
	}
	return f.Roles, nil
}

func (r *Repo) ListAssignments() ([]model.Assignment, error) {
	b, err := os.ReadFile(r.assignmentsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := model.DecodeAssignmentsFile(b)
	if err != nil {
		return nil, err
	}
	return f.Assignments, nil
}

func (r *Repo) ListActors() ([]model.ActorFile, error) {
	entries, err := os.ReadDir(r.actorsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []model.ActorFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		f, err := r.GetActor(strings.TrimSuffix(e.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Actor.ID < out[j].Actor.ID })
	return out, nil
}

var actorIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (r *Repo) GetActor(id string) (model.ActorFile, error) {
	if !actorIDRe.MatchString(id) {
		return model.ActorFile{}, fmt.Errorf("invalid actor id %q: %w", id, state.ErrNotFound)
	}
	b, err := os.ReadFile(r.actorPath(id))
	if os.IsNotExist(err) {
		return model.ActorFile{}, fmt.Errorf("actor %q: %w", id, state.ErrNotFound)
	}
	if err != nil {
		return model.ActorFile{}, err
	}
	return model.DecodeActorFile(b)
}

// --- WorkItem ---

func (r *Repo) GetWorkItem(id string) (model.WorkItem, error) {
	p, err := r.workPath(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return model.WorkItem{}, fmt.Errorf("work item %q: %w", id, state.ErrNotFound)
	}
	if err != nil {
		return model.WorkItem{}, err
	}
	return model.DecodeWorkItem(b)
}

func (r *Repo) ListWorkItems() ([]model.WorkItem, error) {
	entries, err := os.ReadDir(r.workDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []model.WorkItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(r.workDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		w, err := model.DecodeWorkItem(b)
		if err != nil {
			return nil, fmt.Errorf("work/%s: %w", e.Name(), err)
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CreateWorkItem：新对象 revision=1，expect 隐含 0。存在即 ErrExists。
func (r *Repo) CreateWorkItem(w model.WorkItem, acts []model.Activity, msg string) (model.WorkItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lock()
	if err != nil {
		return model.WorkItem{}, err
	}
	defer unlock()

	if _, err := r.GetWorkItem(w.ID); err == nil {
		return model.WorkItem{}, fmt.Errorf("work item %q: %w", w.ID, state.ErrExists)
	} else if !errors.Is(err, state.ErrNotFound) {
		return model.WorkItem{}, err
	}
	w.SchemaVersion = schemaV2
	w.Revision = 1
	raw, err := model.EncodeWorkItem(w)
	if err != nil {
		return model.WorkItem{}, err
	}
	if err := model.ValidateWorkItem(w, raw); err != nil {
		return model.WorkItem{}, err
	}
	if err := r.commitWork(w, acts, raw, msg); err != nil {
		return model.WorkItem{}, err
	}
	return w, nil
}

// UpdateWorkItem：CAS。expectRevision 必须等于当前 revision，否则 ErrConflict。
func (r *Repo) UpdateWorkItem(w model.WorkItem, expectRevision int, acts []model.Activity, msg string) (model.WorkItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lock()
	if err != nil {
		return model.WorkItem{}, err
	}
	defer unlock()

	cur, err := r.GetWorkItem(w.ID)
	if errors.Is(err, state.ErrNotFound) {
		return model.WorkItem{}, err
	}
	if err != nil {
		return model.WorkItem{}, err
	}
	if cur.Revision != expectRevision {
		return model.WorkItem{}, fmt.Errorf("%w: work item %s at revision %d, expected %d",
			state.ErrConflict, w.ID, cur.Revision, expectRevision)
	}
	w.SchemaVersion = schemaV2
	w.Revision = cur.Revision + 1
	raw, err := model.EncodeWorkItem(w)
	if err != nil {
		return model.WorkItem{}, err
	}
	if err := model.ValidateWorkItem(w, raw); err != nil {
		return model.WorkItem{}, err
	}
	if err := r.commitWork(w, acts, raw, msg); err != nil {
		return model.WorkItem{}, err
	}
	return w, nil
}

// ImportWorkItem 迁移专用：以给定 revision 原样落盘（§35 card.version → revision）。
// 只允许尚不存在的 id；字段校验照常执行。
func (r *Repo) ImportWorkItem(w model.WorkItem, acts []model.Activity, msg string) (model.WorkItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lock()
	if err != nil {
		return model.WorkItem{}, err
	}
	defer unlock()

	if _, err := r.GetWorkItem(w.ID); err == nil {
		return model.WorkItem{}, fmt.Errorf("work item %q: %w", w.ID, state.ErrExists)
	} else if !errors.Is(err, state.ErrNotFound) {
		return model.WorkItem{}, err
	}
	w.SchemaVersion = schemaV2
	if w.Revision < 1 {
		w.Revision = 1
	}
	raw, err := model.EncodeWorkItem(w)
	if err != nil {
		return model.WorkItem{}, err
	}
	if err := model.ValidateWorkItem(w, raw); err != nil {
		return model.WorkItem{}, err
	}
	if err := r.commitWork(w, acts, raw, msg); err != nil {
		return model.WorkItem{}, err
	}
	return w, nil
}

// commitWork 在同一把锁内写 work 文件 + activity + 单次 git commit。
func (r *Repo) commitWork(w model.WorkItem, acts []model.Activity, raw []byte, msg string) error {
	p, err := r.workPath(w.ID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return err
	}
	if err := r.AppendActivity(acts, ""); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
		return err
	}
	args := []string{"commit", "-m", msg}
	if r.SignCommits {
		args = append(args, "-S")
	}
	_, err = gitRun(r.Dir, args...)
	return err
}

// --- Activity ---

// activityPath 按 TS 所在月份分桶。
func activityPath(dir string, ts string) string {
	month := "unknown"
	if len(ts) >= len("2006-01") {
		month = ts[:7]
	}
	return filepath.Join(dir, month+".jsonl")
}

func (r *Repo) AppendActivity(acts []model.Activity, msg string) error {
	if len(acts) == 0 {
		return nil
	}
	if err := os.MkdirAll(r.activityDir(), 0o755); err != nil {
		return err
	}
	byFile := map[string][]model.Activity{}
	var order []string
	for _, a := range acts {
		p := activityPath(r.activityDir(), a.TS)
		if _, ok := byFile[p]; !ok {
			order = append(order, p)
		}
		byFile[p] = append(byFile[p], a)
	}
	for _, p := range order {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		for _, a := range byFile[p] {
			b, err := json.Marshal(a)
			if err != nil {
				f.Close()
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
	}
	if msg != "" {
		if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
			return err
		}
		_, err := gitRun(r.Dir, "commit", "-m", msg)
		return err
	}
	return nil
}

func (r *Repo) ListActivity() ([]model.Activity, error) {
	entries, err := os.ReadDir(r.activityDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(r.activityDir(), e.Name()))
		}
	}
	sort.Strings(files)
	var out []model.Activity
	for _, f := range files {
		acts, err := model.LoadActivityFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, acts...)
	}
	return out, nil
}

// --- Lock（进程互斥 + 陈锁检测，语义与 v1 store 一致）---

func (r *Repo) lock() (func(), error) {
	lp := filepath.Join(r.Dir, ".ousheng.lock")
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if isStaleLock(lp) {
			os.Remove(lp)
			f, err = os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		}
		if err != nil {
			return nil, fmt.Errorf("workspace locked: %w", err)
		}
	}
	fmt.Fprintf(f, "%d", os.Getpid())
	f.Close()
	return func() { os.Remove(lp) }, nil
}

func isStaleLock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return true
	}
	if runtime.GOOS == "windows" {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return true
	}
	return false
}

// --- helpers ---

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func writeYAML(path string, v any) error {
	b, err := yamlMarshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
