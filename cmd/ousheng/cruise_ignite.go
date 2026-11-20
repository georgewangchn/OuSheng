package main

// cruise ignite —— 点火唯一入口（档案 §50-B，2026-11-16 A 级四座复盘拍板）。
//
// 判据触发：档案 §43-E 残留「pm 手跑再引资源事故 → 做统一入口」——11-13/11-14 两夜
// systemd 定时 × 座位脚本 × pm 会话手动三源并发实证（资源饱和 SSH 死、双驱动竞写）。
// §45 已裁「启动 = 人」，本命令把「人从哪个门进」收口为唯一门：
//
//	ousheng cruise ignite --seat <actor> [--stop]
//
// 四道前置（全 fail-closed，任何一道不过即拒）：
//  1. 座位归属：registry.machine = 本机（跨机座位拒——点火是本机执行位操作，
//     多机 = 每机一个入口，天然分布式，同 dispatch 判例）；
//  2. 座位仓定位：repos.yaml 映射 × 仓内 .opencode/ousheng.json actor 匹配；
//  3. 驱动器 canonical 门：scripts/cruise-loop.sh 与内嵌资产逐字节一致——
//     分叉驱动拒绝点火（§37「驱动器=协议资产」从纪律升为门）；
//  4. systemd 面：ousheng-cruise@.service 单元 + /etc/default/ousheng-cruise-<seat>
//     参数文件齐备（systemd = 监督 + journal 审计 + Restart 自愈，§34-D；裸 nohup
//     无监督形态不点火——那是 §34 复盘前的「驱动器单进程无监督」死法）。
//
// 启动仍 = 人（§45）：按钮/命令是人按的；本命令不做定时、不做自启、不代跑。
// 幂等：座位 flock 在驱动器内（§46 BUG-179 根治），重复 start = systemd no-op /
// flock 拒绝，不产生第二实例。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ousheng/adapters/opencode"
	"ousheng/internal/machine"
	"ousheng/internal/state/gityaml"
)

const (
	igniteUnitFile  = "ousheng-cruise@.service"
	igniteEnvPrefix = "ousheng-cruise-"
)

func cruiseIgnite(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise ignite")
	seat := fs.String("seat", "", "seat actor（本机座位，ousheng team list 可查 machine 列）")
	stop := fs.Bool("stop", false, "停止该座位巡航（systemctl stop）")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	if *seat == "" {
		fmt.Fprintln(stderr, "--seat required（本机座位；ousheng team list 查 machine 列）")
		return 2
	}
	ws := fs.Dir()
	repo := gityaml.Open(ws)
	af, err := repo.GetActor(*seat)
	if err != nil {
		fmt.Fprintf(stderr, "座位 %s 未注册 → ousheng team list\n", *seat)
		return 1
	}
	// 门 1：座位归属本机（比对逻辑与 doctor §41 座位漂移同款）。
	if m := af.Actor.Machine; m != "" && !machineMatchesHost(m) {
		fmt.Fprintf(stderr, "座位 %s 常驻 %s ≠ 本机——点火只对本机座位（到该机执行；多机=每机一个入口，档案 §50-B）\n", *seat, m)
		return 1
	}
	// 门 2：座位仓定位（repos.yaml 映射 × ousheng.json actor 匹配）。
	repoDir := seatRepoDir(ws, *seat)
	if repoDir == "" {
		fmt.Fprintf(stderr, "座位 %s 的代码仓未映射 → ousheng repo set <system> <路径> 后 ousheng adapter install --cruise\n", *seat)
		return 1
	}
	// 门 3：驱动器 canonical（逐字节）。
	drv := filepath.Join(repoDir, "scripts", "cruise-loop.sh")
	raw, err := os.ReadFile(drv)
	if err != nil {
		fmt.Fprintf(stderr, "驱动器缺失（%s）→ ousheng adapter install --cruise（先装再点火）\n", drv)
		return 1
	}
	if !bytes.Equal(raw, opencode.CruiseLoopSh) {
		fmt.Fprintf(stderr, "驱动器与 canonical 分叉（手改/fork 漂移，档案 §37）→ ousheng adapter install --cruise 重跑取回后再点火\n")
		return 1
	}
	// 门 4：板仓多 remote 水位一致（T-226 盲区④，档案 §54）——拓扑分裂拒点火。
	// 事故形态：pm 推 A 源、席位拉 B 源，各源 sync「ok」但互缺 → 席位整夜看旧板
	// 盲跑（11-16 修过、11-20 复发 =「再发一次即做」判据触发）。fail-closed。
	if n, drifts := boardRemoteDrift(ws, true); n >= 2 && len(drifts) > 0 {
		fmt.Fprintf(stderr, "板仓多 remote 水位差（拓扑分裂，T-226 盲区④）：%s——先确认干道并推平（git push <干道> main / pull 齐平）再点火\n", strings.Join(drifts, "；"))
		return 1
	}
	// 门 4：systemd 面（单元 + 参数文件；测试经 env 旋钮注入临时路径）。
	unitDir := envOr("OUSHENG_SYSTEMD_DIR", "/etc/systemd/system")
	etcDir := envOr("OUSHENG_ETC_DEFAULT", "/etc/default")
	envFile := filepath.Join(etcDir, igniteEnvPrefix+*seat)
	unit := filepath.Join(unitDir, igniteUnitFile)
	if _, err := os.Stat(unit); err != nil {
		fmt.Fprintf(stderr, "systemd 单元缺失（%s）→ cp %s/scripts/ousheng-cruise@.service %s && systemctl daemon-reload（档案 §50-B：无监督形态不点火）\n", unit, repoDir, unitDir)
		return 1
	}
	if _, err := os.Stat(envFile); err != nil {
		fmt.Fprintf(stderr, "参数文件缺失（%s）→ 按 scripts/ousheng-cruise@.service 头注释建 OUSHENG_CRUISE_ARGS 后再点火\n", envFile)
		return 1
	}
	// §55 H3：截止参数有效性——已过期的截止必是错参数（点火即停，11-20 实锤
	// 「22 分钟后过期」险情）。fail-closed。
	envBody, _ := os.ReadFile(envFile)
	var argsLine string
	for _, l := range strings.Split(string(envBody), "\n") {
		if strings.HasPrefix(l, "OUSHENG_CRUISE_ARGS=") {
			argsLine = strings.Trim(strings.TrimPrefix(l, "OUSHENG_CRUISE_ARGS="), "\"")
		}
	}
	if parts := strings.Fields(argsLine); len(parts) >= 9 {
		if dl, err := time.Parse("2006-01-02 15:04", parts[7]+" "+parts[8]); err == nil && !dl.After(time.Now()) {
			fmt.Fprintf(stderr, "截止参数 %s %s 已过期（点火即停）→ 先改 %s 的 OUSHENG_CRUISE_ARGS 截止再点火\n", parts[7], parts[8], envFile)
			return 1
		}
	}
	// §55 H4：模型路由退化警告——CODE==WAIT 且含 flash = 弱模型干编码
	//（11-15 事故机械化；不拒——可能是 pm 有意的省钱配置，但必须可见）。
	envOf := func(key string) string {
		for _, l := range strings.Split(string(envBody), "\n") {
			if strings.HasPrefix(l, key+"=") {
				return strings.TrimSpace(strings.TrimPrefix(l, key+"="))
			}
		}
		return ""
	}
	if code, wait := envOf("OC_MODEL_CODE"), envOf("OC_MODEL_WAIT"); code != "" && code == wait && strings.Contains(code, "flash") {
		fmt.Fprintf(stdout, "[warn] 模型路由退化：OC_MODEL_CODE=OC_MODEL_WAIT=%s（值守模型干编码，11-15 事故形态）——确认是有意配置\n", code)
	}
	// §55 H1b：座位仓分支一致性——当前分支 ≠ origin 默认分支 = 巡航可能在
	// 错代码上干活（警告不拒：dev 分支干活是合法形态，data 仓先例）。
	if cur, _ := gitOutErr(repoDir, "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(cur) != "" {
		if def, _ := gitOutErr(repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); strings.TrimSpace(def) != "" {
			want := strings.TrimPrefix(strings.TrimSpace(def), "origin/")
			if strings.TrimSpace(cur) != want {
				fmt.Fprintf(stdout, "[warn] 座位仓当前分支 %s ≠ origin 默认分支 %s——巡航将在非默认分支干活，确认有意\n", strings.TrimSpace(cur), want)
			}
		}
	}
	action := "start"
	if *stop {
		action = "stop"
	}
	sysctl := envOr("OUSHENG_SYSTEMCTL", "systemctl")
	out, err := exec.Command(sysctl, action, "ousheng-cruise@"+*seat).CombinedOutput()
	if len(bytes.TrimSpace(out)) > 0 {
		fmt.Fprintf(stdout, "%s\n", strings.TrimSpace(string(out)))
	}
	if err != nil {
		fmt.Fprintf(stderr, "systemctl %s 失败：%v\n", action, err)
		return 1
	}
	// 非门提醒：在场度过夜 = agent 空等烧 token（11-13→16 实锤：present 挂三天）。
	if p := repo.LoadPresence(); p.State == "present" && presenceStaleHours(p.Since) > 12 {
		fmt.Fprintf(stdout, "提醒：PM present 已挂 %.0f 小时——夜间请 pm away（agent 问完等 = 空等烧 token，档案 §50-G）\n", presenceStaleHours(p.Since))
	}
	verb := "点火"
	if *stop {
		verb = "停火"
	}
	fmt.Fprintf(stdout, "cruise %s：seat=%s（%s；参数 %s；日志 journalctl -u ousheng-cruise@%s -f）\n", verb, *seat, action, envFile, *seat)
	return 0
}

// seatRepoDir 在 repos.yaml 映射中定位座位仓：仓内 .opencode/ousheng.json 的
// actor == seat 即命中（adapter install 写入，机器本地事实）。
func seatRepoDir(ws, seat string) string {
	for _, dir := range machine.LoadRepoMap(ws) {
		raw, err := os.ReadFile(filepath.Join(dir, ".opencode", "ousheng.json"))
		if err != nil {
			continue
		}
		var cfg map[string]any
		if json.Unmarshal(raw, &cfg) != nil {
			continue
		}
		if a, _ := cfg["actor"].(string); a == seat {
			return dir
		}
	}
	return ""
}

// machineMatchesHost：registry.machine（可带 "(IP)" 注记）与本机 hostname 比对
// （与 doctor §41 座位漂移同款语义，单点提出来两处共用）。
func machineMatchesHost(m string) bool {
	hn, _ := os.Hostname()
	base := m
	if i := strings.Index(m, "("); i > 0 {
		base = m[:i]
	}
	return base == hn || strings.Contains(m, hn)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// presenceStaleHours：present 声明距今小时数（坏值按 0——提醒非门，不 fail）。
func presenceStaleHours(since string) float64 {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil || t.IsZero() {
		return 0
	}
	return time.Since(t).Hours()
}
