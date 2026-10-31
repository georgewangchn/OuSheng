package gityaml

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ousheng/internal/model"
	"ousheng/internal/state"
)

// --- 交接快照（上下文交接方案 §H1，TASK-044）---

var handoverSegRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (r *Repo) handoverDir() string { return filepath.Join(r.root(), "handover") }

// ListHandover 扫描 .ousheng/handover/<system>/<actor>.md。
// 读面 best-effort：目录缺失 = 空；损坏条目跳过（不炸——降级路径），
// 快照正文不在此返回（正文只在 handover show 按需读）。
func (r *Repo) ListHandover() ([]model.HandoverInfo, error) {
	entries, err := os.ReadDir(r.handoverDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []model.HandoverInfo
	for _, e := range entries {
		if !e.IsDir() || !handoverSegRe.MatchString(e.Name()) {
			continue
		}
		system := e.Name()
		files, err := os.ReadDir(filepath.Join(r.handoverDir(), system))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			actor := strings.TrimSuffix(f.Name(), ".md")
			raw, err := os.ReadFile(filepath.Join(r.handoverDir(), system, f.Name()))
			if err != nil {
				continue
			}
			hf, _, derr := model.DecodeHandover(raw)
			if derr != nil {
				continue // 损坏跳过（doctor/手验兜底），不阻塞注入面
			}
			// 座位身份以文件名为准（frontmatter 可能漂移；show/claim 都按路径）。
			out = append(out, model.HandoverInfo{
				System: system, Actor: actor, SavedAt: hf.SavedAt, Size: int64(len(raw)),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].System != out[j].System {
			return out[i].System < out[j].System
		}
		return out[i].Actor < out[j].Actor
	})
	return out, nil
}

// GetHandover 读指定座位快照正文（show 用；缺文件 ErrNotFound）。
func (r *Repo) GetHandover(system, actor string) (model.HandoverFile, string, error) {
	if !handoverSegRe.MatchString(system) || !handoverSegRe.MatchString(actor) {
		return model.HandoverFile{}, "", fmt.Errorf("invalid handover seat %s/%s", system, actor)
	}
	raw, err := os.ReadFile(filepath.Join(r.handoverDir(), system, actor+".md"))
	if os.IsNotExist(err) {
		return model.HandoverFile{}, "", state.ErrNotFound
	}
	if err != nil {
		return model.HandoverFile{}, "", err
	}
	return model.DecodeHandover(raw)
}

// SaveHandover 写座位快照（覆盖式）+ git commit。
// 4KB 硬门在此（写路径 fail-closed）；git 历史即版本链（旧快照档案可回溯）。
func (r *Repo) SaveHandover(system, actor, body, msg string) (model.HandoverInfo, error) {
	if !handoverSegRe.MatchString(system) {
		return model.HandoverInfo{}, fmt.Errorf("invalid system %q (lowercase-hyphen)", system)
	}
	if !handoverSegRe.MatchString(actor) {
		return model.HandoverInfo{}, fmt.Errorf("invalid actor %q (lowercase-hyphen)", actor)
	}
	savedAt := model.NowRFC3339()
	raw := model.EncodeHandover(system, actor, savedAt, body)
	if len(raw) > model.HandoverMaxBytes {
		return model.HandoverInfo{}, fmt.Errorf("handover %d bytes > %d gate — 导航态超限即内容错居（细节去 design/单/architecture，此处只留指针）", len(raw), model.HandoverMaxBytes)
	}
	unlock, err := r.lock()
	if err != nil {
		return model.HandoverInfo{}, err
	}
	defer unlock()
	p := filepath.Join(r.handoverDir(), system, actor+".md")
	if err := writeFileMk(p, raw, 0o644); err != nil {
		return model.HandoverInfo{}, err
	}
	if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
		return model.HandoverInfo{}, err
	}
	if _, err := gitRun(r.Dir, "commit", "-m", msg); err != nil {
		return model.HandoverInfo{}, err
	}
	return model.HandoverInfo{System: system, Actor: actor, SavedAt: savedAt, Size: int64(len(raw))}, nil
}

// --- credentials 登记表（§H2，TASK-045）---

func (r *Repo) credentialsPath() string { return filepath.Join(r.root(), "credentials.yaml") }

// LoadCredentials 读登记表；缺失 = 空+false（opt-in：未登记零检查零噪音）。
// 损坏响亮报错（校验手写纪律：strict decode 在 model 层）。
func (r *Repo) LoadCredentials() (model.CredentialsFile, bool, error) {
	raw, err := os.ReadFile(r.credentialsPath())
	if os.IsNotExist(err) {
		return model.CredentialsFile{}, false, nil
	}
	if err != nil {
		return model.CredentialsFile{}, false, err
	}
	f, derr := model.DecodeCredentials(raw)
	if derr != nil {
		return model.CredentialsFile{}, true, derr
	}
	return f, true, nil
}
