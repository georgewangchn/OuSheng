package main

// field_audit_test.go —— 2026-11-24 实栈复核锁集（档案 §56）。
//
// 取证面：真板（gitee kanban main，11-15→11-24，2332 提交）+ pm 机只读 clone。
// 每个锁对应一条实栈缺陷/证伪，不是推演预防（E4 判据驱动）：
//   ① 审计流一行冲突标记 → 全舰队「交付 0 单」静默 42h（pm.jsonl:726）
//   ② pm 机 clone 落后 2332 提交 → cruise status 把 9 天前旧板当现况（F1 缺口）
//   ③ 8 天 11 次板仓 merge（§48/§50-A 宣称灭绝的双写源未灭绝）
//   ④ cruise report 列全量历史 BUG（164 行收官墙，窗口参数形同虚设）
//   ⑤ heartbeat 把已撤座位/未注册名当「疑似停止」座位

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ousheng/adapters/opencode"
)

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestCruiseStatusSurvivesCorruptActivity（实锤①）：审计流带冲突标记时，
// status/heartbeat/report 必须照常投影好行——不得静默归零。
func TestCruiseStatusSurvivesCorruptActivity(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-001", "--status", "done", "--actor", "data")
	p := filepath.Join(dir, ".ousheng", "activity", time.Now().Format("2006-01"), "data.jsonl")
	if err := appendFile(p, "<<<<<<< HEAD\n=======\n>>>>>>> uat/main\n"); err != nil {
		t.Fatal(err)
	}
	so, se, code := runIn(t, dir, "cruise", "status", "--hours", "24")
	if code != 0 || !strings.Contains(so, "交付 1 单") {
		t.Fatalf("坏审计行不得让 status 归零（实栈：交付 0 单假读数 42h）: code=%d\n%s%s", code, so, se)
	}
	if so, _, code := runIn(t, dir, "cruise", "heartbeat"); code != 0 || !strings.Contains(so, "data") {
		t.Fatalf("heartbeat 必须在坏审计行下照常工作:\n%s", so)
	}
	so = mustRun(t, dir, "cruise", "report", "--hours", "24")
	if !strings.Contains(so, "| data |") {
		t.Fatalf("report 座位战果不得因坏审计行清空:\n%s", so)
	}
}

// ageRemoteContacts：把 git 的「远端接触痕迹」（FETCH_HEAD + 远端跟踪 reflog）
// 时间拨回——模拟 pm 机 clone 多日未 fetch。
func ageRemoteContacts(t *testing.T, dir string, ago time.Duration) {
	t.Helper()
	old := time.Now().Add(-ago)
	mark := func(p string, _ os.FileInfo, _ error) error {
		return os.Chtimes(p, old, old)
	}
	fh := filepath.Join(dir, ".git", "FETCH_HEAD")
	_ = os.WriteFile(fh, []byte(""), 0o644)
	_ = mark(fh, nil, nil)
	_ = filepath.Walk(filepath.Join(dir, ".git", "logs"), func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		return mark(p, fi, err)
	})
}

// twoClones：板仓双 clone（ws 有上游，ws2 推新提交）——双写源/陈旧 clone 的最小复现。
func twoClones(t *testing.T, ws string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitOK(t, t.TempDir(), "init", "-q", "--bare", bare)
	gitOK(t, ws, "remote", "add", "origin", bare)
	gitOK(t, ws, "push", "-q", "-u", "origin", "main")
	ws2 := filepath.Join(t.TempDir(), "ws2")
	gitOK(t, t.TempDir(), "clone", "-q", bare, ws2)
	gitOK(t, ws2, "config", "user.email", "t@t")
	gitOK(t, ws2, "config", "user.name", "t")
	return ws2
}

// TestCruiseStatusBoardFreshness（实锤②，F1 在晨检入口的落地）：只读观测面
// 必须带板龄——落后上游要点名，久未 fetch 要点名；新鲜态零输出（不添噪音）。
func TestCruiseStatusBoardFreshness(t *testing.T) {
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	ws2 := twoClones(t, ws)
	mustRun(t, ws2, "work", "create", "--id", "T-777", "--type", "task", "--title", "远端新单",
		"--system", "lake", "--description", "d", "--actor", "data")
	gitOK(t, ws2, "push", "-q", "origin", "main")

	// 新鲜态：未 fetch 时 tracking ref 与 HEAD 一致、接触痕迹新 → 零输出。
	if out := mustRun(t, ws, "cruise", "status"); strings.Contains(out, "旧板") || strings.Contains(out, "fetch") {
		t.Fatalf("新鲜 clone 不应出板龄噪音:\n%s", out)
	}
	// 落后上游（pm 机实况：clone 停在 11-15，origin/main 领先 2332）。
	gitOK(t, ws, "fetch", "-q", "origin")
	out := mustRun(t, ws, "cruise", "status")
	for _, want := range []string{"落后上游 1 提交", "旧板", "ousheng sync"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status 应点名板落后（缺 %q）:\n%s", want, out)
		}
	}
	js := mustRun(t, ws, "cruise", "status", "--json")
	if !strings.Contains(js, `"board_freshness"`) || !strings.Contains(js, "旧板") {
		t.Fatalf("JSON 面应携带板龄（驾驶舱/cron 消费）:\n%s", js)
	}
	out = mustRun(t, ws, "cruise", "report")
	if !strings.Contains(out, "落后上游 1 提交") {
		t.Fatalf("report 应携带板龄:\n%s", out)
	}
	// 拉齐后不再点名。
	gitOK(t, ws, "pull", "-q", "--ff-only")
	if out := mustRun(t, ws, "cruise", "status"); strings.Contains(out, "旧板") {
		t.Fatalf("拉齐后不应再报落后:\n%s", out)
	}
	// 久未 fetch（pm 机只读 clone 从不 pull，tracking ref 自身也陈旧）。
	ageRemoteContacts(t, ws, 72*time.Hour)
	out = mustRun(t, ws, "cruise", "status")
	if !strings.Contains(out, "fetch") || !strings.Contains(out, "72") {
		t.Fatalf("久未 fetch 应点名（tracking ref 自身可能陈旧）:\n%s", out)
	}
	ageRemoteContacts(t, ws, 2*time.Hour)
	if out := mustRun(t, ws, "cruise", "status"); strings.Contains(out, "fetch") {
		t.Fatalf("2h 内有远端接触不应点名:\n%s", out)
	}
}

// TestCruiseStatusMergeFingerprint（实锤③）：板仓 merge 提交 = 双写源指纹——
// 单写源下 sync 只 ff，main 恒线性；窗口内出现 merge 必须曝光给 pm。
func TestCruiseStatusMergeFingerprint(t *testing.T) {
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	gitOK(t, ws, "config", "user.email", "t@t")
	gitOK(t, ws, "config", "user.name", "t")
	ws2 := twoClones(t, ws)
	// 两侧各自发号（真板形态：pm 侧与座位侧交错写）。对侧用 pm 身份写——审计流
	// 按 actor 分文件，两侧才不撞同一 jsonl（撞了就是「合并未决态」，归
	// TestWriteRejectedDuringPendingMerge 管）；这里要的是干净自动合并出的
	// merge 提交——它本身就是双写源指纹。
	mustRun(t, ws, "work", "create", "--id", "T-801", "--type", "task", "--title", "本侧单",
		"--system", "lake", "--description", "d", "--actor", "data")
	mustRun(t, ws2, "work", "create", "--id", "T-802", "--type", "task", "--title", "对侧单",
		"--system", "lake", "--description", "d", "--actor", "pm")
	gitOK(t, ws2, "push", "-q", "origin", "main")
	if out := mustRun(t, ws, "cruise", "status"); strings.Contains(out, "merge 提交") {
		t.Fatalf("线性历史不应报双写源:\n%s", out)
	}
	gitOK(t, ws, "pull", "-q", "--no-rebase", "origin", "main")
	out := mustRun(t, ws, "cruise", "status")
	for _, want := range []string{"merge 提交 1 次", "双写源"} {
		if !strings.Contains(out, want) {
			t.Fatalf("merge 提交应作为双写源指纹曝光（缺 %q）:\n%s", want, out)
		}
	}
	js := mustRun(t, ws, "cruise", "status", "--json")
	if !strings.Contains(js, `"merge_commits"`) {
		t.Fatalf("JSON 面应携带 merge 指纹:\n%s", js)
	}
	// 窗口必须真生效：`--since=240h` 这种写法 git 不认（实测 0 条）——10 天窗内
	// 刚发生的 merge 不能因日期语法错误而消失。
	if out := mustRun(t, ws, "cruise", "status", "--hours", "240"); !strings.Contains(out, "merge 提交 1 次") {
		t.Fatalf("--hours 240 仍应报窗口内的 merge（--since 日期语法锁）:\n%s", out)
	}
}

// TestGitSinceHoursIsValidGitDate（§56 复核抓回的潜伏缺陷）：`--since=12h` 不是
// git 日期语法——实测被当「本月 12 日」（全量 3364 条）/ 非法日（240h → 0 条）。
// 「仓夜间提交 N」行自 §34 起一直在报错数且无锁覆盖；与 merge 指纹共用单一出口。
func TestGitSinceHoursIsValidGitDate(t *testing.T) {
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	code := t.TempDir()
	gitOK(t, code, "init", "-q")
	gitOK(t, code, "config", "user.email", "t@t")
	gitOK(t, code, "config", "user.name", "t")
	old := time.Now().Add(-72 * time.Hour).Format(time.RFC3339)
	c := exec.Command("git", "-C", code, "commit", "-q", "--allow-empty", "-m", "old")
	c.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("old commit: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(code, "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, code, "add", "new.txt")
	gitOK(t, code, "commit", "-q", "-m", "new")
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "repos.yaml"), []byte("lake: "+code+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := mustRun(t, ws, "cruise", "status", "--hours", "240"); !strings.Contains(out, "lake 仓夜间提交 2") {
		t.Fatalf("240h 窗应含两笔提交（旧写法 240h → 0 条）:\n%s", out)
	}
	if out := mustRun(t, ws, "cruise", "status", "--hours", "1"); !strings.Contains(out, "lake 仓夜间提交 1") {
		t.Fatalf("1h 窗应只含新提交（旧写法 Nh → 本月 N 日，全量）:\n%s", out)
	}
}

// TestWriteRejectedDuringPendingMerge（§56）：板仓处于合并未决态（MERGE_HEAD 在）
// 时任何写路径必须 fail-loud 拒绝。机理：写路径的提交形态是 `git add .ousheng`
// + `git commit`（无 pathspec）——未决态下 git add 会把人尚未收口的冲突文件标
// 成「已解决」（冲突标记还在内容里），commit 直接落成 merge 提交：半解合并被
// 一笔无关写固化入库。11-16 T-206 卡面 / 11-22 pm.jsonl 两例冲突标记入库的同型
// 机制（那两笔是人手提交的，但座位机正是「人逐卡收口 × 座位持续写」的并发场）。
func TestWriteRejectedDuringPendingMerge(t *testing.T) {
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	gitOK(t, ws, "config", "user.email", "t@t")
	gitOK(t, ws, "config", "user.name", "t")
	ws2 := twoClones(t, ws)
	// 双写源：两侧各追加 evidence（卡面 + 审计流同文件追加）。ws 侧留一笔
	// **未推**提交，ws2 侧在旧基线上提交并推——ws pull 才是真分叉而非快进。
	mustRun(t, ws, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "local", "--actor", "data")
	mustRun(t, ws2, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "remote", "--actor", "data")
	gitOK(t, ws2, "push", "-q", "origin", "main")
	// 人来收口：pull 进合并未决态（sync 按设计不自动 rebase）。
	if err := exec.Command("git", "-C", ws, "pull", "-q", "--no-rebase", "origin", "main").Run(); err == nil {
		t.Skip("本机构造不出冲突（git 自动合并成功）——场景不成立")
	}
	if _, err := os.Stat(filepath.Join(ws, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("前置：板仓应处于合并未决态: %v", err)
	}
	// 座位的一笔无关写（新卡）——修复前它会成功并把半解合并固化成 merge 提交。
	out := mustRunFail(t, ws, "work", "create", "--id", "T-900", "--type", "task", "--title", "无关写",
		"--system", "lake", "--description", "d", "--actor", "data")
	if !strings.Contains(out, "合并未决态") {
		t.Fatalf("未决态写路径必须 fail-loud 拒绝并指引收口:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("写路径不得替人完成合并（MERGE_HEAD 应仍在）: %v", err)
	}
	if !fileContains(filepath.Join(ws, ".ousheng", "work", "T-001.yaml"), "<<<<<<<") {
		t.Fatalf("前置：冲突标记应仍在卡面（未被人收口）")
	}
	// 人逐卡收口并提交后，写路径恢复。
	gitOK(t, ws, "checkout", "--ours", "--", ".ousheng/work/T-001.yaml",
		filepath.Join(".ousheng/activity", time.Now().Format("2006-01"), "data.jsonl"))
	gitOK(t, ws, "add", "-A")
	gitOK(t, ws, "commit", "-q", "-m", "merge: 人逐卡收口")
	mustRun(t, ws, "work", "create", "--id", "T-900", "--type", "task", "--title", "无关写",
		"--system", "lake", "--description", "d", "--actor", "data")
	// 收口后的 merge 提交 = 双写源指纹，晨检必须曝光。
	if out := mustRun(t, ws, "cruise", "status"); !strings.Contains(out, "merge 提交 1 次") {
		t.Fatalf("收口 merge 应进双写源指纹:\n%s", out)
	}
}

func fileContains(p, needle string) bool {
	b, err := os.ReadFile(p)
	return err == nil && strings.Contains(string(b), needle)
}

// stateOf 取 plan 输出的 state 指纹行（§56 静止闸的驱动器接口）。
func stateOf(planOut string) string {
	for _, l := range strings.Split(planOut, "\n") {
		if strings.HasPrefix(l, "state: ") {
			return strings.TrimPrefix(l, "state: ")
		}
	}
	return ""
}

// TestCruisePlanStateFingerprint（§56，§51 证伪的修复面）：plan 输出「座位相关
// 事实」的内容指纹——驱动器静止闸的判据。它必须是「下一次起轮可能因此有活干」
// 的那组文件：相关则变、无关则不变、自身无易变输入（时间/attach/他域活动）。
//
// 被取代的旧尺（§42④ plan 文本 cksum）两头痛，11-21 真板实证：attach 行每轮
// 变 → 永不触发（四座 10-11 分钟一轮、零 delta 轮 50%）；他人往我的卡上落
// evidence 不改 plan 文本 → 该醒不醒。
func TestCruisePlanStateFingerprint(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	s1 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	if s1 == "" {
		t.Fatalf("plan 必须输出 state 指纹行")
	}
	// 稳定：连续两次 plan 指纹相同（无易变输入）。
	if s2 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data")); s2 != s1 {
		t.Fatalf("无事实变化时指纹必须稳定: %s vs %s", s1, s2)
	}
	if js := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--json"); !strings.Contains(js, `"state": "`+s1+`"`) {
		t.Fatalf("JSON 面应携带 state:\n%s", js)
	}
	// 无关变化不动指纹：新 backlog 卡（不在恢复/领活/反应任何一节）。
	mustRun(t, dir, "work", "create", "--id", "T-500", "--type", "task", "--title", "与我无关",
		"--system", "lake", "--description", "d", "--actor", "data")
	if s := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data")); s != s1 {
		t.Fatalf("无关卡不得改指纹（闸失效=永远不静止）: %s vs %s", s, s1)
	}
	// 相关变化①：pm 在我的在途卡上落 evidence（拍板/排位令的真实形态）。
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "pm-order",
		"--note", "pm 排位令", "--actor", "pm")
	s2 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	if s2 == s1 {
		t.Fatalf("我的卡被改动必须改指纹（否则 pm 的令到了座位不醒）")
	}
	// 相关变化②：依赖目标卡推进（我的 blocked 单等的 T-003）。
	mustRun(t, dir, "evidence", "add", "T-003", "--type", "manual_check", "--locator", "dep-moved", "--actor", "data")
	s3 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	if s3 == s2 {
		t.Fatalf("依赖目标卡变化必须改指纹（等的就是它）")
	}
	// 相关变化③：在场度（注册表）——门行为分叉的输入。
	os.WriteFile(filepath.Join(dir, ".ousheng", "presence.yaml"),
		[]byte("schema_version: 1\nactor: pm\nstate: present\nsince: "+time.Now().Format(time.RFC3339)+"\n"), 0o644)
	s4 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	if s4 == s3 {
		t.Fatalf("在场度变化必须改指纹（present/away 改变下一轮的门行为）")
	}
	// 相关变化④：领活池来新单（无主 ready）。
	os.WriteFile(filepath.Join(dir, ".ousheng", "work", "TASK-911.yaml"),
		[]byte("schema_version: 2\nid: TASK-911\ntype: task\ntitle: 新可领\nsystem: lake\ndescription: d\nstatus: ready\nrevision: 1\n"), 0o644)
	s5 := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	if s5 == s4 {
		t.Fatalf("领活池来新单必须改指纹")
	}
}

// TestCruisePlanWaitUntilChange（§56）：静止闸的「变到即醒」——--until-change
// 给定上轮指纹：事实未变则等满窗口（驱动器不空转起轮），事实变了立即返回
// （延迟 ≈ 一个 20s 探测步，而不是睡满退避窗）。
func TestCruisePlanWaitUntilChange(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	s := stateOf(mustRun(t, dir, "cruise", "plan", "--actor", "data"))
	// 未变：等满窗口（短窗 3s），返回的仍是指纹 S 的 plan。
	t0 := time.Now()
	out := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--wait", "3", "--until-change", s)
	if el := time.Since(t0); el < 3*time.Second {
		t.Fatalf("事实未变时应等满窗口（静止不空转），实际 %v", el)
	}
	if stateOf(out) != s {
		t.Fatalf("超时返回的 plan 指纹应仍为 %s:\n%s", s, out)
	}
	// 变了：早返回。1s 后改我在途卡的卡面（pm 落令的最小形态）。延迟上界 =
	// 一个 20s 探测步（拉取频率不变是既定设计），必须早于 30s 窗口到。
	card := filepath.Join(dir, ".ousheng", "work", "T-001.yaml")
	orig, _ := os.ReadFile(card)
	time.AfterFunc(800*time.Millisecond, func() {
		os.WriteFile(card, append(append([]byte{}, orig...), []byte("# pm 排位令\n")...), 0o644)
	})
	t0 = time.Now()
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data", "--wait", "30", "--until-change", s)
	if el := time.Since(t0); el >= 28*time.Second {
		t.Fatalf("事实变化应即醒（≤ 一个探测步，早于 30s 窗口），实际 %v", el)
	}
	if ns := stateOf(out); ns == "" || ns == s {
		t.Fatalf("即醒返回的应是指纹已变的 plan:\n%s", out)
	}
}

// TestCLIHelperProcess：让测试二进制自 exec 充当 `ousheng` 可执行文件（驱动器
// 行为锁需要真二进制——进程内 run() 不是一个可被 bash 调用的命令）。
// 用法：OUSheng_CLI_HELPER=1 <testbin> -test.run '^TestCLIHelperProcess$' -- <args…>
func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("OUSheng_CLI_HELPER") != "1" {
		return
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

// runDriverOnce：真跑一份驱动器脚本（轮顶 4、WAIT=1s、stub opencode），返回各轮
// 起跑时刻（毫秒）与 journal。poke=true 时在第 2 轮起跑 300ms 后往我在途卡落一笔
// 「pm 排位令」（事实变化）。行为锁与缺陷回放共用（§58）。
func runDriverOnce(t *testing.T, script string, poke bool) (ms []int64, journal string) {
	t.Helper()
	for _, bin := range []string{"bash", "python3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s 不可用", bin)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("取不到测试二进制路径: %v", err)
	}
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	box := t.TempDir()
	// ousheng 包装：转调测试二进制（进程内 run）。
	oush := filepath.Join(box, "ousheng")
	os.WriteFile(oush, []byte("#!/bin/sh\nexec \""+exe+"\" -test.run '^TestCLIHelperProcess$' -- \"$@\"\n"), 0o755)
	// stub opencode：记录每轮起跑时刻（毫秒），回驱动器要解析的 sessionID / cache.read。
	stamp := filepath.Join(box, "stamps")
	oc := filepath.Join(box, "opencode")
	os.WriteFile(oc, []byte("#!/bin/sh\n"+
		"python3 -c 'import time; print(int(time.time()*1000))' >> \""+stamp+"\"\n"+
		"echo '{\"sessionID\":\"sid-stub\",\"cache\":{\"read\":1000}}'\n"), 0o755)
	// 驱动器：座位 flock / 并发槽改到私有目录（多测试并行不互撞）。
	sh := filepath.Join(box, "cruise-loop.sh")
	body := strings.ReplaceAll(script, "/tmp/opencode/cruise-seat-", filepath.Join(box, "seat-"))
	body = strings.ReplaceAll(body, "/tmp/cruise-sem.", filepath.Join(box, "sem-"))
	os.WriteFile(sh, []byte(body), 0o755)

	cmd := exec.Command("bash", sh, ws, "data", "lake", box, "4", "1", "00:00-00:00")
	cmd.Env = append(os.Environ(),
		"OUSheng_CLI_HELPER=1", "OUSHENG_BIN="+oush, "OC_BIN="+oc, "CONCURRENCY_MAX=16")
	var jour bytes.Buffer
	cmd.Stdout, cmd.Stderr = &jour, &jour
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(ws, ".ousheng", "work", "T-001.yaml")
	poked := make(chan struct{})
	go func() {
		defer close(poked)
		if !poke {
			return
		}
		for i := 0; i < 400; i++ { // ≤ 20s
			if b, err := os.ReadFile(stamp); err == nil && strings.Count(string(b), "\n") >= 2 {
				time.Sleep(300 * time.Millisecond)
				orig, _ := os.ReadFile(card)
				os.WriteFile(card, append(append([]byte{}, orig...), []byte("# pm 排位令\n")...), 0o644)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("驱动器应以轮顶正常退出: %v\n%s", err, jour.String())
		}
	case <-time.After(90 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("驱动器超时未退:\n%s", jour.String())
	}
	<-poked
	st, _ := os.ReadFile(stamp)
	for _, f := range strings.Fields(string(st)) {
		v, _ := strconv.ParseInt(f, 10, 64)
		ms = append(ms, v)
	}
	return ms, jour.String()
}

// TestCruiseLoopQuietGate（§56，驱动器行为锁——真跑 cruise-loop.sh + stub opencode）：
// §51 证伪面：一句话 prompt 之后驱动器仍是「10 分钟一轮、一半零 delta」。静止闸
// 必须做到：①事实未变 → 轮间隔按阶梯拉开（不再等间隔起空轮）；②事实变了 →
// 下一个探测点即醒（不是睡满退避窗）。
//
// 时间线（WAIT=1s，轮顶 4）：r1 →(睡1+静止等1)→ r2 →(睡1，期间卡面被改 → 不静止)
// → r3 →(睡1+静止等1)→ r4。
func TestCruiseLoopQuietGate(t *testing.T) {
	ms, j := runDriverOnce(t, string(opencode.CruiseLoopSh), true)
	// 两次静止等待；事实变化后 SAME 归零，阶梯从 #1 重启（变化后不该沿用旧退避）。
	if strings.Count(j, "静止轮 #1") != 2 {
		t.Fatalf("驱动器 journal 应有两次静止等待（r2 前、r4 前）:\n%s", j)
	}
	if len(ms) != 4 {
		t.Fatalf("应恰好 4 轮（轮顶）: %d\n%s", len(ms), j)
	}
	d21, d32, d43 := ms[1]-ms[0], ms[2]-ms[1], ms[3]-ms[2]
	// ① 事实未变：睡 1s + 静止等 1s（旧形态等间隔 ≈1s）。
	if d21 < 1800 || d43 < 1800 {
		t.Fatalf("静止轮必须付「基线睡眠 + 静止等待」（r2-r1=%dms, r4-r3=%dms）:\n%s", d21, d43, j)
	}
	// ② 事实变了：r2 后 300ms 卡面被改，r3 应在基线睡眠后立即起（≈1s），
	// 而不是再付一次静止等待（≥2s）。
	if d32 >= 1700 {
		t.Fatalf("事实变化应即醒（r3-r2=%dms，应 ≈ 基线睡眠 1s）:\n%s", d32, j)
	}
}

// TestLockReplayDriverQuietGate（档案 §58 缺陷回放）：把「静止等待被拆掉」这个
// 缺陷形态（等价于 §42④ 退避从未触发时的行为：事实没变也等间隔重起空轮）施加到
// 驱动器副本上——行为锁的判据（轮间隔 ≥ 基线睡眠 + 静止等待）必须变红。
// 锚点对不上 = 驱动器已变形 → 响亮失败，逼着重做回放。
func TestLockReplayDriverQuietGate(t *testing.T) {
	src := string(opencode.CruiseLoopSh)
	anchor := `--until-change "$STATE"`
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("回放锚点失效（驱动器已变形）——按新形态重做缺陷回放: %d", strings.Count(src, anchor))
	}
	sabotaged := strings.Replace(src, anchor, "", 1)
	ms, j := runDriverOnce(t, sabotaged, false)
	if len(ms) != 4 {
		t.Fatalf("回放未跑完 4 轮:\n%s", j)
	}
	for i := 1; i < len(ms); i++ {
		if d := ms[i] - ms[i-1]; d >= 1800 {
			t.Fatalf("回放未复现缺陷（第 %d 轮间隔 %dms，缺陷形态应 ≈ 基线睡眠 1s 等间隔）:\n%s", i+1, d, j)
		}
	}
	if strings.Contains(j, "静止轮") && strings.Contains(j, "最长") {
		// 静止轮仍会打印「等待变化」，但 plan 不再阻塞——日志在、行为没了：正是
		// 「字符串锁 / 日志在位 ≠ 行为成立」的驱动器版。
		t.Logf("缺陷形态下 journal 仍有「静止轮」字样——日志在位不代表闸在工作:\n%s", j)
	}
}

// TestConvergeFoldsProgressMemo（§57 拍板）：「done 但 progress 未满」同类备忘折叠成
// 一行（真板 18 条 warning 占 11 条）——可动作项不被淹没，清单仍可见。
func TestConvergeFoldsProgressMemo(t *testing.T) {
	dir := cruiseFixture(t)
	for _, id := range []string{"T-801", "T-802", "T-803"} {
		mustRun(t, dir, "work", "create", "--id", id, "--type", "task", "--title", "备忘记账 "+id,
			"--system", "lake", "--description", "d", "--actor", "data")
		mustRun(t, dir, "work", "assign", id, "--assignee", "data", "--role", "dev", "--actor", "data")
		for _, st := range []string{"ready", "doing"} {
			mustRun(t, dir, "work", "update", id, "--status", st, "--actor", "data")
		}
		mustRun(t, dir, "evidence", "add", id, "--type", "manual_check", "--locator", "x", "--actor", "data")
		mustRun(t, dir, "work", "update", id, "--status", "done", "--actor", "data")
	}
	mustRun(t, dir, "progress", "report", "T-801", "--value", "0.9", "--actor", "data", "--basis", "manual")
	mustRun(t, dir, "progress", "report", "T-802", "--value", "0.6", "--actor", "data", "--basis", "manual")
	so, _, _ := runIn(t, dir, "converge")
	if strings.Count(so, "done but progress reported") != 0 {
		t.Fatalf("备忘类不应逐条列出:\n%s", so)
	}
	if !strings.Contains(so, "备忘 2 单") || !strings.Contains(so, "T-801 90%") || !strings.Contains(so, "T-802 60%") {
		t.Fatalf("备忘应折叠成一行计数+清单:\n%s", so)
	}
}

// TestWebCruiseHidesStateLine：plan 的 `state:` 指纹是驱动器静止闸的机械接口
// （§56「不进 prompt，不给人读」）——驾驶舱各座位 plan 摘要不得把它显示给 pm
// （呈现层中文、数据层工程语义）。
func TestWebCruiseHidesStateLine(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	body := webGet(t, dir, "/cruise")
	if strings.Contains(body, "state: ") {
		t.Fatalf("驾驶舱不应把驱动器接口行显示给人:\n%s", body[:min(len(body), 400)])
	}
	if !strings.Contains(body, "1·resume") {
		t.Fatalf("plan 摘要本体应仍在:\n%s", body[:min(len(body), 400)])
	}
}

// TestOversizedCardAllowsNonGrowthWrites（§57 拍板，8KB 尺寸门推演结论）：
// 超限卡（既成事实形态：merge 膨胀/手改/历史遗留）只拒增长型写——
// 关单（status→done）、瘦身放行；新 evidence、继续增长拒并指引。
// 真板实锤：T-193「8KB 满载死锁（此前任何 CLI 写入被拒）」、T-152 十九次瘦身、
// 99 张卡 >7KB——门值不动（§18 骨架原则），只修「写冻结」这个不变式破口。
func TestOversizedCardAllowsNonGrowthWrites(t *testing.T) {
	dir := cruiseFixture(t)
	big := "schema_version: 2\nid: T-950\ntype: task\ntitle: 顶格单\ndescription: |\n  " +
		strings.Repeat("长", 3000) + "\nsystem: lake\ndetected_by: data\nassignee: data\nstatus: doing\nrevision: 9\n" +
		"evidence:\n    - type: manual_check\n      locator: x\n      source: data\n"
	path := filepath.Join(dir, ".ousheng", "work", "T-950.yaml")
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Size() <= 8192 {
		t.Fatalf("test setup: %d bytes, need > 8192", fi.Size())
	}
	// 关单：revision 9→10 进位 +1 字节，仍须放行（写冻结死锁的正反面）。
	out := mustRun(t, dir, "work", "update", "T-950", "--status", "done", "--actor", "data")
	if !strings.Contains(out, "done") {
		t.Fatalf("超限卡关单必须放行:\n%s", out)
	}
	// 新 evidence：拒（骨架原则），报错带下一步。
	if out := mustRunFail(t, dir, "evidence", "add", "T-950", "--type", "manual_check", "--locator", "y", "--actor", "data"); !strings.Contains(out, "骨架原则") {
		t.Fatalf("超限卡新增 evidence 必须拒并指引:\n%s", out)
	}
	// 继续增长（--file 改长描述）：拒。
	b, _ := os.ReadFile(path)
	longer := strings.Replace(string(b), "title: 顶格单", "title: 顶格单"+strings.Repeat("长", 300), 1)
	lp := filepath.Join(t.TempDir(), "longer.yaml")
	os.WriteFile(lp, []byte(longer), 0o644)
	if out := mustRunFail(t, dir, "work", "update", "T-950", "--file", lp, "--expect", "10", "--actor", "data"); !strings.Contains(out, "增长") {
		t.Fatalf("超限卡继续增长必须拒:\n%s", out)
	}
	// 瘦身（解冻正道）：放行，且此后恢复普通写。
	slim := "schema_version: 2\nid: T-950\ntype: task\ntitle: 顶格单（已瘦身）\ndescription: d\nsystem: lake\nassignee: data\nstatus: done\nrevision: 10\nevidence:\n    - type: document\n      locator: docs/evidence/T-950.md\n      source: data\n"
	sp := filepath.Join(t.TempDir(), "slim.yaml")
	os.WriteFile(sp, []byte(slim), 0o644)
	mustRun(t, dir, "work", "update", "T-950", "--file", sp, "--expect", "10", "--actor", "data")
	mustRun(t, dir, "evidence", "add", "T-950", "--type", "manual_check", "--locator", "z", "--actor", "data")
}

// TestWebTokenFromEnv（§57）：非回环绑定的令牌可来自 env（常驻单元不让令牌进
// 命令行）；回环绑定不吃 env——「回环免 token」不因环境里碰巧有变量而变；
// 两头都没有仍然 fail-closed。
func TestWebTokenFromEnv(t *testing.T) {
	t.Setenv("OUSHENG_WEB_TOKEN", "")
	if err := validateWebBind("0.0.0.0:7878", webToken("", "0.0.0.0:7878")); err == nil {
		t.Fatalf("非回环无令牌必须拒（§50-E 不因 §57 松动）")
	}
	t.Setenv("OUSHENG_WEB_TOKEN", "from-env")
	if err := validateWebBind("0.0.0.0:7878", webToken("", "0.0.0.0:7878")); err != nil {
		t.Fatalf("env 令牌应满足非回环绑定: %v", err)
	}
	if got := webToken("explicit", "0.0.0.0:7878"); got != "explicit" {
		t.Fatalf("显式 --token 优先: %q", got)
	}
	if got := webToken("", "127.0.0.1:7878"); got != "" {
		t.Fatalf("回环绑定不应吃 env 令牌: %q", got)
	}
}

// captureProcessStderr：接管 os.Stderr（公共前置的提示直写进程 stderr——与 gityaml
// 读面警告同款），跑 fn，返回捕获内容。
func captureProcessStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stderr = old
	w.Close()
	return <-done
}

// TestEntryFreshnessDefaultOn（§58 P2）：入口板龄自检由构造覆盖——任何经 fs.Dir()
// 取工作区的命令（读面 / 写面 / agent 注入面）在落后上游的板上都提示；健康态零输出；
// 自带自检的入口（cruise status）不重复；拉齐后静默。
func TestEntryFreshnessDefaultOn(t *testing.T) {
	ws := cruiseFixture(t)
	mkCruiseBoard(t, ws)
	ws2 := twoClones(t, ws)
	run := func(args ...string) string {
		return captureProcessStderr(t, func() { runCLIRaw(append(args, "--dir", ws)) })
	}
	if out := run("work", "list"); strings.Contains(out, "落后上游") {
		t.Fatalf("健康 clone 不应有板龄提示（噪音=狼来了）:\n%s", out)
	}
	mustRun(t, ws2, "work", "create", "--id", "T-777", "--type", "task", "--title", "远端新单",
		"--system", "lake", "--description", "d", "--actor", "data")
	gitOK(t, ws2, "push", "-q", "origin", "main")
	gitOK(t, ws, "fetch", "-q", "origin")
	// 读面 / agent 注入面 / 写面 / 审计面——四类入口都走默认。
	for _, args := range [][]string{
		{"work", "list"},
		{"context", "me", "--actor", "data"},
		{"evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--actor", "data"},
		{"converge"},
	} {
		if out := run(args...); !strings.Contains(out, "[warn] 本地板落后上游 1 提交") {
			t.Fatalf("%v：落后板上必须有板龄提示（默认接，覆盖由构造得出）:\n%q", args, out)
		}
	}
	// 措辞不得误触驱动器的等待白名单（§48 以「分叉」识别等待模式）。
	if out := run("converge"); strings.Contains(out, "分叉") {
		t.Fatalf("公共前置提示不得含「分叉」:\n%s", out)
	}
	// 自带自检的入口不经公共前置重复提示（它的板龄在 stdout）。
	if out := run("cruise", "status"); strings.Contains(out, "落后上游") {
		t.Fatalf("豁免入口不应重复提示:\n%s", out)
	}
	// 上面那笔测试写入在 ws 落了本地提交——与 ws2 的新单分叉，pull --ff-only 必败：
	// 这正是「落后板上写入」的真实危害（§48）。撤掉它再验证「拉齐后静默」。
	gitOK(t, ws, "reset", "-q", "--hard", "HEAD~1")
	gitOK(t, ws, "pull", "-q", "--ff-only")
	if out := run("work", "list"); strings.Contains(out, "落后上游") {
		t.Fatalf("拉齐后不应再提示:\n%s", out)
	}
}

// TestHasUpstreamFast：公共前置的零进程预判——没配上游（单机 / 夹具）读文件即判；
// 有上游回落 git 仍能给数；形态不明（非 git 目录按「无上游」处理，与 git 行为一致）。
func TestHasUpstreamFast(t *testing.T) {
	ws := cruiseFixture(t)
	if known, has := hasUpstreamFast(ws); !known || has {
		t.Fatalf("无 remote 的工作区应判定「已知无上游」: %v %v", known, has)
	}
	if boardBehind(ws) != "" {
		t.Fatalf("无上游不应产生板龄信号")
	}
	twoClones(t, ws)
	if known, has := hasUpstreamFast(ws); !known || !has {
		t.Fatalf("配了上游的 clone 应判定「有上游」: %v %v", known, has)
	}
	if known, has := hasUpstreamFast(t.TempDir()); !known || has {
		t.Fatalf("非 git 目录按无上游处理: %v %v", known, has)
	}
}

// TestCruiseReportScalesClosedItems（实锤④）：晨报的需求/缺陷清单——进行中与
// 窗口内收官逐列，历史收官折叠成一行计数（真板：164 行「✅ 收官」墙）。
func TestCruiseReportScalesClosedItems(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	for i := 100; i < 125; i++ {
		card := fmt.Sprintf("schema_version: 2\nid: BUG-%03d\ntype: bug\ntitle: 历史缺陷\ndescription: d\nsystem: lake\ndetected_by: data\nstatus: done\nrevision: 2\nevidence:\n    - type: manual_check\n      locator: x\n", i)
		if err := os.WriteFile(filepath.Join(dir, ".ousheng", "work", fmt.Sprintf("BUG-%03d.yaml", i)), []byte(card), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, dir, "bug", "report", "--id", "BUG-301", "--title", "在途缺陷", "--system", "lake", "--detected-by", "data")
	mustRun(t, dir, "bug", "report", "--id", "BUG-302", "--title", "本窗收官", "--system", "lake", "--detected-by", "data")
	mustRun(t, dir, "evidence", "add", "BUG-302", "--type", "git_commit", "--locator", "HEAD", "--actor", "data")
	mustRun(t, dir, "work", "assign", "BUG-302", "--assignee", "data", "--role", "dev", "--actor", "data")
	for _, st := range []string{"ready", "doing", "done"} {
		mustRun(t, dir, "work", "update", "BUG-302", "--status", st, "--actor", "data")
	}

	out := mustRun(t, dir, "cruise", "report", "--hours", "24")
	for _, want := range []string{"进行中", "| BUG-301 |", "本窗收官", "| BUG-302 |", "历史收官 25 单"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report 缺 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| BUG-101 |") || strings.Contains(out, "| BUG-124 |") {
		t.Fatalf("历史收官单不得逐列（164 行收官墙实锤）:\n%s", out)
	}
	// JSON 面保持全量（cron/web 消费方不受呈现层折叠影响）。
	js := mustRun(t, dir, "cruise", "report", "--hours", "24", "--json")
	if !strings.Contains(js, "BUG-101") || !strings.Contains(js, "BUG-301") {
		t.Fatalf("JSON 面应保持全量 items:\n%s", js)
	}
}

// TestCruiseHeartbeatRegisteredSeatsOnly（实锤⑤）：已撤座位/未注册名的审计流
// 残留不得冒充「疑似停止」的座位（真板：ousheng / data-local 永久 ⚠）。
func TestCruiseHeartbeatRegisteredSeatsOnly(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	p := filepath.Join(dir, ".ousheng", "activity", time.Now().Format("2006-01"), "ghost-seat.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"ts":%q,"actor":"ghost-seat","action":"evidence_added","work_item":"T-001","detail":"历史残留"}`+"\n", time.Now().Add(-48*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, dir, "cruise", "heartbeat", "--min", "30")
	if strings.Contains(out, "ghost-seat") {
		t.Fatalf("未注册名的审计残留不得冒充座位:\n%s", out)
	}
	if !strings.Contains(out, "data") {
		t.Fatalf("注册座位必须仍在:\n%s", out)
	}
}
