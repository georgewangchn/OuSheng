package gityaml

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ousheng/internal/model"
	"ousheng/internal/state"

	"gopkg.in/yaml.v3"
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
				System: system, Actor: actor, SavedAt: hf.SavedAt, Session: hf.Session, Size: int64(len(raw)),
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
// session：本代 opencode session id；空 = 换代信号（代际制，档案 §34）。
func (r *Repo) SaveHandover(system, actor, session, body, msg string) (model.HandoverInfo, error) {
	if !handoverSegRe.MatchString(system) {
		return model.HandoverInfo{}, fmt.Errorf("invalid system %q (lowercase-hyphen)", system)
	}
	if !handoverSegRe.MatchString(actor) {
		return model.HandoverInfo{}, fmt.Errorf("invalid actor %q (lowercase-hyphen)", actor)
	}
	// 注册表校验（评审 🟡：防幽灵座位——typo 系统会在 handover/ 下建幽灵目录、
	// context me 指针指空。system 必须已注册；actor 必须是已注册身份（含 human）。
	if _, err := r.GetSystem(system); err != nil {
		return model.HandoverInfo{}, fmt.Errorf("unknown system %q — 先 ousheng system add %s", system, system)
	}
	if _, err := r.GetActor(actor); err != nil {
		return model.HandoverInfo{}, fmt.Errorf("unknown actor %q — 先注册（ousheng me / team add）", actor)
	}
	savedAt := model.Now()
	raw := model.EncodeHandover(system, actor, savedAt, session, body)
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
	return model.HandoverInfo{System: system, Actor: actor, SavedAt: savedAt, Session: session, Size: int64(len(raw))}, nil
}

// AttachSession 补记本代 session id（驱动器侧：opencode run 结束后调用）。
// 只改 frontmatter 的 session 字段，正文与 saved_at 原样——正文是 agent 的
// 事实，驱动器只补自己知道的指针。session 为空 = 清除指针（强制换代）。
func (r *Repo) AttachSession(system, actor, session, msg string) error {
	if !handoverSegRe.MatchString(system) || !handoverSegRe.MatchString(actor) {
		return fmt.Errorf("invalid handover seat %s/%s", system, actor)
	}
	raw, err := os.ReadFile(filepath.Join(r.handoverDir(), system, actor+".md"))
	if err != nil {
		return err
	}
	hf, body, err := model.DecodeHandover(raw)
	if err != nil {
		return err
	}
	out := model.EncodeHandover(hf.System, hf.Actor, hf.SavedAt, session, body)
	// 幂等门（评审 🔴 实锤修复）：同指针重复 attach = 内容零变化——直接 no-op，
	// 不落 git。原实现靠匹配 git 输出容错，但工作区持根级 .ousheng.lock
	// （untracked）时 git 走 "nothing added to commit but untracked files
	// present" 变体，串匹配漏掉 → 第二次 attach 报错退出（§34 反事实③ 被证伪）。
	// 内容比对是唯一不依赖 locale/git 版本的判据。
	if bytes.Equal(out, raw) {
		return nil
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err := writeFileMk(filepath.Join(r.handoverDir(), system, actor+".md"), out, 0o644); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "commit", "-m", msg); err != nil {
		// 同 sid 连续 attach = 无变化空提交（评审 🔴：驱动器续代场景必触发）
		if !strings.Contains(err.Error(), "nothing to commit") &&
			!strings.Contains(err.Error(), "no changes added to commit") {
			return err
		}
	}
	return nil
}

// --- 在场度（档案 §34，P1）---

func (r *Repo) presencePath() string { return filepath.Join(r.root(), "presence.yaml") }

// SavePresence 写在场度（human 座位声明；入 git 全队同步）。
func (r *Repo) SavePresence(p model.Presence, msg string) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	body := fmt.Sprintf("schema_version: 1\nactor: %s\nstate: %s\nsince: %s\n", p.Actor, p.State, p.Since)
	if err := writeFileMk(r.presencePath(), []byte(body), 0o644); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "add", ".ousheng"); err != nil {
		return err
	}
	if _, err := gitRun(r.Dir, "commit", "-m", msg); err != nil {
		return err
	}
	return nil
}

// LoadPresence 读在场度；缺失/坏值 → away（悲观默认，档案 §34）。
func (r *Repo) LoadPresence() model.Presence {
	raw, err := os.ReadFile(r.presencePath())
	if err != nil {
		return model.Presence{State: model.PresenceAway, Since: ""}
	}
	var f struct {
		Actor string `yaml:"actor"`
		State string `yaml:"state"`
		Since string `yaml:"since"`
	}
	if yamlErr := yaml.Unmarshal(raw, &f); yamlErr != nil {
		return model.Presence{State: model.PresenceAway}
	}
	if f.State != model.PresencePresent {
		f.State = model.PresenceAway
	}
	return model.Presence{Actor: f.Actor, State: f.State, Since: f.Since}
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
