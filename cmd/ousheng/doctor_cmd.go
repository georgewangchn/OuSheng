package main

// cmdDoctor：`ousheng doctor [--dir <代码仓>]`。部署标准自检（2026-09-20 裁决：
// 延后清单判据「多人反复踩配置坑」实锤转正——本会话三起：每会话权限弹窗、
// 旧 actor 残留、插件/协议段漂移无人察觉）。
//
// 三层检查：机器级（二进制/MCP）/ 座位级（五件套+全局放行）/ 工作区级
// （me/repo 映射有效性）。铁律：
//   - 检查不修复：修复单一入口 = `adapter install`（幂等）或对应 registry 命令；
//   - FAIL 必带修复命令（→ 后缀），任一 FAIL 退出码 1（可脚本化）；
//   - WARN 不影响退出码（MCP 缺失只降通道不降功能）；
//   - 用户全局配置解析失败降级 WARN 不是 FAIL（不碰用户配置原则，档案 §11）；
//   - plugin 逐字节比对（install 恒同步语义），opencode.json/package.json 只查
//     存在（install 保留语义）——检查口径对齐写入口径。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ousheng/adapters/opencode"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"context"
	"time"
)

// agentSeats 数板仓注册的 agent 型座位数（§37：me 缺失判据分形——多座位=合法
// 防御形态 WARN，单座位=缺省身份刚需 FAIL）。
func agentSeats(ws string) int {
	afs, err := gityaml.Open(ws).ListActors()
	if err != nil {
		return 0
	}
	n := 0
	for _, af := range afs {
		if af.Actor.Type == model.ActorAgent {
			n++
		}
	}
	return n
}

func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	fs := &flagSetWithDir{FlagSet: newFlagSet("doctor")}
	fs.dir = fs.String("dir", ".", "code repo dir (default: .)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	fails := 0
	check := func(status, msg string) {
		fmt.Fprintf(stdout, "[%-4s] %s\n", status, msg)
		if status == "FAIL" {
			fails++
		}
	}

	// --- 机器级 ---
	check("PASS", versionString())
	if _, err := exec.LookPath("ousheng-mcp"); err == nil {
		check("PASS", "ousheng-mcp 在 PATH")
	} else {
		check("WARN", "ousheng-mcp 不在 PATH——MCP 工具通道不可用（CLI 通道不受影响）")
	}

	// --- 座位级 + 工作区定位 ---
	cfgPath := filepath.Join(dir, ".opencode", "ousheng.json")
	ws, actor := "", ""
	hasSeatCfg := false
	if raw, err := os.ReadFile(cfgPath); err == nil {
		hasSeatCfg = true
		var mc struct {
			Workspace string `json:"workspace"`
			Actor     string `json:"actor"`
			System    string `json:"system"`
		}
		if err := json.Unmarshal(raw, &mc); err != nil {
			check("FAIL", fmt.Sprintf("ousheng.json 非法（%v）→ ousheng adapter install", err))
		} else {
			ws, actor = mc.Workspace, mc.Actor
			// 代际链完整性（评审 §36 Y7）：system 空 → plugin 的 handover attach
			// 静默跳过（session 代际指针断，退化为每轮新代——可用但费上下文）。
			// 未映射属合法形态（install 不猜），故 WARN 不 FAIL。
			if mc.System == "" {
				check("WARN", "ousheng.json 无 system 字段——代际 attach 不生效（退化为每轮新代）→ ousheng adapter install --system <系统>（或 repos.yaml 映射后重跑）")
			}
		}
	} else if adapterAssetsPresent(dir) {
		check("FAIL", "检测到 adapter 资产但无 .opencode/ousheng.json（旧形态残留）→ ousheng adapter install --workspace <工作区>")
	} else {
		check("INFO", "当前目录非 adapter 座位（无 .opencode/ousheng.json）")
	}
	if ws == "" {
		ws = resolveAdapterWorkspace("", dir)
	}

	// 座位级：工作区/身份/plugin/协议段/全局放行
	if ws != "" {
		if _, err := os.Stat(filepath.Join(ws, ".ousheng", "project.yaml")); err != nil {
			check("FAIL", fmt.Sprintf("工作区 %s 无效（缺 .ousheng/project.yaml）→ ousheng adapter install --workspace <工作区>", ws))
		} else if hasSeatCfg && actor != "" {
			if af, err := gityaml.Open(ws).GetActor(actor); err != nil {
				check("FAIL", fmt.Sprintf("ousheng.json actor=%s 未注册 → ousheng adapter install --actor <身份>（或 team add）", actor))
			} else {
				check("PASS", fmt.Sprintf("座位身份 actor=%s（workspace=%s）已注册", actor, ws))
				// §41 座位漂移检测：registry.machine 已登记且 ≠ 本机 hostname →
				// 座位搬家未更新或旧机座位残留（搬机 runbook：新机 join + adapter
				// install + 手改 registry machine；旧机清 .opencode/ousheng.json）。
				if m := af.Actor.Machine; m != "" {
					hn, _ := os.Hostname()
					base := m
					if i := strings.Index(m, "("); i > 0 {
						base = m[:i]
					}
					if base != hn && !strings.Contains(m, hn) {
						check("WARN", fmt.Sprintf("座位 %s 的 machine=%s ≠ 本机 %s——搬家未更新 registry 或旧机座位残留", actor, m, hn))
					}
				}
			}
		}
		switch globalPermissionStatus(ws) {
		case "allow":
			check("PASS", "全局放行 "+ws+"/** 已配置")
		case "unparsable":
			check("WARN", "全局配置解析失败（JSONC 等）——按档案 §11 手动确认 external_directory")
		default:
			check("FAIL", "全局放行缺 "+ws+"/** → ousheng adapter install")
		}
	}
	if hasSeatCfg {
		pluginPath := filepath.Join(dir, ".opencode", "plugins", "ousheng-sampler.ts")
		if raw, err := os.ReadFile(pluginPath); err != nil {
			check("FAIL", "插件缺失（.opencode/plugins/ousheng-sampler.ts）→ ousheng adapter install")
		} else {
			// 按本机 opencode 版本选 v1/v2 资产比对（与 adapter install 同源）
			expected := opencode.PluginTS
			label := "v2"
			if !opencodeIsV2() {
				expected = opencode.PluginV1TS
				label = "v1"
			}
			if string(raw) != string(expected) {
				check("FAIL", "插件与内嵌资产"+label+"不一致（版本漂移）→ ousheng adapter install")
			} else {
				check("PASS", "插件与内嵌资产"+label+"逐字节一致")
			}
		}
		for _, a := range []string{filepath.Join(dir, ".opencode", "opencode.json"), filepath.Join(dir, ".opencode", "package.json")} {
			if _, err := os.Stat(a); err != nil {
				check("FAIL", "adapter 资产缺失 "+filepath.Base(a)+" → ousheng adapter install")
			}
		}
		block := agentsBegin + "\n" + strings.TrimRight(string(opencode.AgentsMD), "\n") + "\n" + agentsEnd
		if raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")); err != nil {
			check("FAIL", "AGENTS.md 未装 → ousheng adapter install")
		} else if !strings.Contains(string(raw), agentsBegin) {
			check("FAIL", "AGENTS.md 缺标记段 → ousheng adapter install")
		} else if !strings.Contains(string(raw), block) {
			check("FAIL", "AGENTS.md 协议段过期（升级后未重跑）→ ousheng adapter install")
		} else {
			check("PASS", "AGENTS.md 协议段 = 当前版本")
		}

		// §50-C 驱动器漂移检查：装过 --cruise（scripts/cruise-loop.sh 存在）即逐字节
		// 比对 canonical。可选资产语义不变（未装不查）；装了分叉 = FAIL——历史上三次
		// 手搓 fork 丢 attach/隔离/三闸（§37/§47），「机械锁修了 ≠ 在跑的脚本修了」，
		// 部署校验的最后一公里在此收口（同插件逐字节先例）。
		if raw, err := os.ReadFile(filepath.Join(dir, "scripts", "cruise-loop.sh")); err == nil {
			if string(raw) != string(opencode.CruiseLoopSh) {
				check("FAIL", "巡航驱动器与 canonical 分叉（手改/fork 漂移——§37/§47 事故形态）→ ousheng adapter install --cruise 重跑取回（驱动器=协议资产，改动走产品仓）")
			} else {
				check("PASS", "巡航驱动器 = canonical（逐字节一致）")
			}
		}

		// 座位资产不得入库（2026-09-21 裁决，档案 §13）：二进制内嵌分发为单一来源，
		// 入库副本必漂移；gitignore 不回溯已跟踪文件，故单列检查。
		if out, err := gitOut(dir, "ls-files", "--", ".opencode/opencode.json", ".opencode/ousheng.json",
			".opencode/package.json", ".opencode/package-lock.json", ".opencode/bun.lock", ".opencode/plugins"); err == nil {
			if tracked := strings.TrimSpace(out); tracked != "" {
				check("FAIL", "座位资产被入库（"+strings.ReplaceAll(tracked, "\n", " ")+"）→ git rm --cached <路径>（档案 §13）")
			} else {
				check("PASS", "座位资产未入库（gitignore 生效）")
			}
		}
	}

	// --- 工作区级：me / repo 映射 ---
	if ws != "" {
		// T-226 盲区④（档案 §54，2026-11-21 pm 授权修复）：板仓多 remote 水位差
		// ——拓扑分裂可查形态。事故：pm 推 A 源、席位拉 B 源，各源 sync「ok」但互缺
		// → 席位整夜看旧板盲跑（11-16 修过、11-20 复发 = 「再发一次即做」判据触发）。
		// 机械锁：多 remote 时代水位差 WARN；单 remote 无从比较不查（诚实边界）。
		// §55 H2 追加：remote URL 交叉——自环（remote 指本仓自身路径）/多名同址冗余。
		// §55 review 🔴1：hard（remote 独有=看旧板风险）WARN；soft（未推/不可达）
		// 提示级。🔴4：URL 自环检查不限多 remote——单 origin 指自己（11-21 实锤形态）同样要抓。
		if n, hard, soft := boardRemoteDrift(ws, true); n >= 2 {
			if len(hard) > 0 {
				check("WARN", "板仓多 remote 水位差（hard：remote 有本地缺的新，T-226 盲区④）："+strings.Join(hard, "；")+"——先 git pull <干道> 收口（看旧板风险）")
			} else if len(soft) > 0 {
				check("PASS", "板仓多 remote 无缺新（soft："+strings.Join(soft, "；")+"——记得推平）")
			} else {
				check("PASS", "板仓多 remote 水位一致（"+fmt.Sprint(n)+" 源同源）")
			}
		}
		// §55 review 🔴4：URL 交叉不限多 remote——单 origin 指自己（11-21 实锤形态）同样要抓。
		if urlOut, err := gitOutErr(ws, "remote", "-v"); err == nil {
			urls := map[string][]string{}
			for _, l := range strings.Split(urlOut, "\n") {
				f := strings.Fields(strings.TrimSpace(l))
				// 行格式：name<TAB>url (fetch|push)——按 fetch 侧去重统计。
				if len(f) >= 3 && strings.HasSuffix(f[2], "(fetch)") {
					urls[f[1]] = append(urls[f[1]], f[0])
				}
			}
			for url, names := range urls {
				if len(names) > 1 {
					check("WARN", "板仓 remote 冗余："+strings.Join(names, "/")+" 同址 "+url+"——合并为一名，防半更新")
				}
				if p := strings.TrimPrefix(url, "file://"); strings.HasPrefix(p, "/") || strings.HasPrefix(url, "/") {
					if abs, err := filepath.Abs(p); err == nil {
						if same, _ := filepath.EvalSymlinks(abs); same != "" {
							if wsAbs, _ := filepath.EvalSymlinks(ws); same == wsAbs {
								check("WARN", "板仓 remote 自环："+strings.Join(names, "/")+" 指向本仓自身路径——fetch/push 恒空转，指向干道")
							}
						}
					}
				}
			}
		}
		if _, err := os.Stat(filepath.Join(ws, ".ousheng", "project.yaml")); err == nil {
			if raw, err := os.ReadFile(mePath(ws)); err == nil {
				me := strings.TrimSpace(string(raw))
				if me == "" {
					check("FAIL", "me 为空 → ousheng me <id>")
				} else if _, err := gityaml.Open(ws).GetActor(me); err != nil {
					check("FAIL", fmt.Sprintf("me=%s 未注册 → ousheng me <id>", me))
				} else {
					check("PASS", "me="+me+" 已注册")
				}
			} else {
				// §37 收紧（2026-11-12）：多座位机的 me 缺失 = 合法防御形态，降
				// WARN；单座位机仍 FAIL（缺省身份刚需）。多座位判定用**机器级双
				// 信号**（板级 agent 注册 >1 且本机 repos.yaml 映射 >1）——板级
				// 单信号会把多机协作板（agent 分布多台机）在单座位机上误报 WARN。
				if agentSeats(ws) > 1 && len(loadRepoMap(ws)) > 1 {
					check("WARN", "板仓无 me 且多座位共享（合法防御形态，BUG-136）——写命令一律显式 --actor")
				} else {
					check("FAIL", "工作区未设默认身份 → ousheng me <id>")
				}
			}
			repos := loadRepoMap(ws)
			for sys, path := range repos {
				if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
					check("FAIL", fmt.Sprintf("repo 映射 %s → %s 失效（非 git 仓或不存在）→ ousheng repo set %s <路径>", sys, path, sys))
				} else {
					check("PASS", "repo 映射 "+sys+" → "+path)
				}
			}
			// 审计桶（2026-09-21，档案 §14）：不可归属写的机器可查形态。历史条目存在
			// 不 FAIL（审计记录不可删），但新写不应再产生（身份解析 + 写路径硬门已修）。
			if files, _ := filepath.Glob(filepath.Join(ws, ".ousheng", "activity", "*", "unknown.jsonl")); len(files) > 0 {
				lines := 0
				for _, f := range files {
					if b, err := os.ReadFile(f); err == nil {
						if s := strings.TrimSpace(string(b)); s != "" {
							lines += strings.Count(s, "\n") + 1
						}
					}
				}
				check("WARN", fmt.Sprintf("审计桶 unknown.jsonl 存在（%d 文件 / %d 条）——历史不可归属写；新写已被拒，查身份解析链", len(files), lines))
			} else {
				check("PASS", "无 unknown 审计桶（所有 activity 可归属）")
			}
			// 水位（无 fetch，按本地上游引用给建议）：未推/落后/分叉都是协作断点
			// （BUG-001 事故：工单烂在本地全舰队不可见，档案 §10）。
			if out, err := gitOut(ws, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
				if f := strings.Fields(out); len(f) == 2 {
					behind, ahead := atoiSafe(f[0]), atoiSafe(f[1])
					switch {
					case ahead > 0 && behind > 0:
						check("WARN", fmt.Sprintf("工作区与上游分叉（未推 %d / 落后 %d）→ 人工 git pull --rebase 后再 sync", ahead, behind))
					case ahead > 0:
						check("WARN", fmt.Sprintf("工作区有 %d 个未推提交 → ousheng sync 推送收口（未推提交全舰队不可见）", ahead))
					case behind > 0:
						check("WARN", fmt.Sprintf("工作区落后上游 %d 个提交 → ousheng sync 拉齐", behind))
					default:
						check("PASS", "工作区水位与上游一致")
					}
				}
			}

			// --- 凭据三查（上下文交接方案 §H2，TASK-045）---
			// ①② 无条件（事故不分是否登记）；③ opt-in（无登记表零检查零噪音）。
			// 值永不入 git 是硬线：检查是机械锁，不靠自觉。
			if out, err := gitOut(ws, "ls-files", "--", ".ousheng/secrets"); err == nil && strings.TrimSpace(out) != "" {
				n := len(strings.Fields(out))
				check("FAIL", fmt.Sprintf("secrets 已被 git 跟踪（%d 文件）——值入 git 事故：立即轮换凭据 + git filter-repo 洗历史（历史永存，删文件不够）", n))
			}
			if hits := scanSecretPatterns(ws); len(hits) > 0 {
				check("WARN", fmt.Sprintf("高置信凭据形态出现在绳上文件（%s）——值只属于 secrets/ 本地文件或密码管理器", strings.Join(hits, ", ")))
			}
			creds, hasRegistry, err := gityaml.Open(ws).LoadCredentials()
			if err != nil {
				check("FAIL", fmt.Sprintf("credentials.yaml 非法（%v）——登记表只做位置索引，值不入册", err))
			} else if hasRegistry {
				var missing []string
				for _, c := range creds.Credentials {
					switch c.Location.Kind {
					case "local-file":
						ref := c.Location.Ref
						filePart := ref
						if i := strings.Index(ref, "#"); i > 0 {
							filePart = ref[:i]
						}
						// 位置层唯一合法值区是 secrets/（复查修正：ref 非此前缀时
						// 不猜测改写路径——WARN 修正 ref，人改登记表）。
						if filePart == "" {
							filePart = "secrets/" + c.System + ".env"
						} else if !strings.HasPrefix(filePart, "secrets/") {
							check("WARN", fmt.Sprintf("凭据 %s：local-file ref %q 应相对 secrets/（如 secrets/%s.env#KEY）", c.ID, filePart, c.System))
							continue
						}
						if _, serr := os.Stat(filepath.Join(ws, ".ousheng", filepath.FromSlash(filePart))); serr != nil {
							missing = append(missing, fmt.Sprintf("%s（%s）", c.ID, filePart))
						}
					case "password-manager":
						if strings.TrimSpace(c.Location.Ref) == "" {
							check("WARN", fmt.Sprintf("凭据 %s：password-manager 位置缺条目名（ref）", c.ID))
						}
					}
				}
				if len(missing) > 0 {
					check("FAIL", fmt.Sprintf("本机缺凭据值：%s → 手工灌 .ousheng/secrets/（0600；re-clone 后一次性重灌）", strings.Join(missing, ", ")))
				} else {
					check("PASS", fmt.Sprintf("凭据登记 %d 条，本机值齐", len(creds.Credentials)))
				}
			}
		}
	}

	if fails > 0 {
		fmt.Fprintf(stdout, "doctor: %d 项 FAIL（修复后重跑；adapter 类一键 ousheng adapter install）\n", fails)
		return 1
	}
	fmt.Fprintln(stdout, "doctor: 全部通过")
	return 0
}

// adapterAssetsPresent：无 ousheng.json 但有 plugin/协议段等资产 = 旧形态残留。
func adapterAssetsPresent(dir string) bool {
	for _, p := range []string{
		filepath.Join(dir, ".opencode", "plugins", "ousheng-sampler.ts"),
		filepath.Join(dir, ".opencode", "opencode.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// globalPermissionStatus：只读检查（doctor 不写任何配置）。
// allow=已放行；unparsable=用户全局配置解析失败；missing=无放行规则。
func globalPermissionStatus(ws string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "missing"
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		return "missing"
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "unparsable"
	}
	perm, ok := cfg["permission"].(map[string]any)
	if cfg["permission"] != nil && !ok {
		return "unparsable"
	}
	ext, ok := perm["external_directory"].(map[string]any)
	if perm["external_directory"] != nil && !ok {
		return "unparsable"
	}
	if ext[ws+"/**"] == "allow" {
		return "allow"
	}
	return "missing"
}

// gitOut：在 dir 跑 git 子命令并返回 stdout（非 git 仓/无 upstream 等由调用方忽略）。
func gitOut(dir string, args ...string) (string, error) {
	return gitOutErr(dir, args...)
}

// boardRemoteDrift（T-226 盲区④机械锁，§55 review 修订）：板仓多 remote 水位差。
// 返回 (remote 数, hard, soft)。**语义分离（review 🔴1）**：hard = behind>0（remote
// 有本地没有 = 席位看旧板的真实风险）或确定性异常（无当前分支/HEAD 异常）；
// soft = 仅 ahead（没推，低危——归档镜像常态）或 fetch 不可达（网络态，不确定）。
// doFetch=false 轻量形态（sync/web——基于现有 tracking ref，毫秒级）。
func boardRemoteDrift(ws string, doFetch bool) (int, []string, []string) {
	out, err := gitOutErr(ws, "remote")
	if err != nil {
		return 0, nil, nil
	}
	var remotes []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if r := strings.TrimSpace(l); r != "" {
			remotes = append(remotes, r)
		}
	}
	if len(remotes) < 2 {
		return len(remotes), nil, nil
	}
	var hard, soft []string
	// 分支动态取（pm 2026-11-21 直觉命中：硬编码 main 会在 master 分支 remote
	// 上「无 main 跳过」漏检；detached HEAD 本身即异常态）。
	branchOut, err := gitOutErr(ws, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(branchOut) == "" || strings.TrimSpace(branchOut) == "HEAD" {
		return len(remotes), append(hard, "本仓 HEAD 分支异常（detached/不可解析）——人工确认"), soft
	}
	branch := strings.TrimSpace(branchOut)
	for _, r := range remotes {
		if doFetch {
			if _, err := gitOutTimeout(ws, 30*time.Second, "fetch", "-q", r); err != nil {
				soft = append(soft, r+": fetch 不可达/超时（网络/权限）——人工确认")
				continue
			}
		}
		cnt, err := gitOutErr(ws, "rev-list", "--left-right", "--count", "HEAD..."+r+"/"+branch)
		if err != nil {
			hard = append(hard, r+": 无 "+branch+" 分支——分支不一致（T-226 拓扑面）")
			continue
		}
		c := strings.Fields(strings.TrimSpace(cnt))
		if len(c) != 2 {
			continue
		}
		if c[1] != "0" {
			hard = append(hard, fmt.Sprintf("%s 独有 %s（本地缺新=席位看旧板风险，T-226 盲区④）", r, c[1]))
		} else if c[0] != "0" {
			soft = append(soft, fmt.Sprintf("%s 落后本地 %s 条（未推，低危——归档镜像常态）", r, c[0]))
		}
	}
	return len(remotes), hard, soft
}
// gitOutErr：在 dir 跑 git 子命令并返回 stdout 与错误（fetch/rev-list 等带失败语义
// 的调用走此；gitOut 保留为忽略错误的旧签名）。
func gitOutErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

// gitOutTimeout（漏洞审计 2026-11-21 🔴）：网络类 git 操作（fetch/pull/push）带
// 超时——TCP 半开（不拒连只 hang）曾可无限阻塞 plan/boardRemoteDrift → 座位僵死，
// 自检链自己变成死锁源。超时=ContextDeadlineExceeded 按失败处理（fail-visible）。
func gitOutTimeout(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	gitBin := envOr("OUSHENG_GIT_BIN", "git")
	cmd := exec.CommandContext(ctx, gitBin, append([]string{"-C", dir}, args...)...)
	// 测试实证（TestGitOutTimeout）：ctx 杀 git 父进程后，stdout 管道仍被其
	// 子进程（ssh/spawn）吊住，Output 会一直等到子孙退出——超时「生效但慢死」。
	// WaitDelay：ctx 取消后 100ms 强制关管道释放调用方。
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("git %v 超时（%v，网络半开嫌疑）", args[0], timeout)
	}
	return string(out), err
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// secretPatterns 是高置信凭据形态（上下文交接方案 §H2 锁 2.5——check-secrets
// 先例产品化）。只收高置信防误报：脱敏示例（***）、普通 password: 字样不命中。
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`),
}

// scanSecretPatterns 扫 .ousheng 下 git 跟踪的文本文件，返回命中（file:line 形态，≤5 条）。
// 只读 git 已跟踪面（本机未跟踪的草稿不扫——那是人的私有区）。
func scanSecretPatterns(ws string) []string {
	out, err := gitOut(ws, "ls-files", "--", ".ousheng")
	if err != nil {
		return nil
	}
	var hits []string
	for _, f := range strings.Fields(out) {
		if !strings.HasSuffix(f, ".md") && !strings.HasSuffix(f, ".yaml") && !strings.HasSuffix(f, ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(f)))
		if err != nil || len(b) > 1<<20 {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, re := range secretPatterns {
				if re.MatchString(line) {
					hits = append(hits, fmt.Sprintf("%s:%d", f, i+1))
					if len(hits) >= 5 {
						return hits
					}
				}
			}
		}
	}
	return hits
}
