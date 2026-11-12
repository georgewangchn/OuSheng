package main

// team list / --machine 锁（档案 §40，pm session 冷启动实栈发现：
// 座位→机器拓扑绳上无处查——跨机协作执行位寻址缺口）。

import (
	"strings"
	"testing"

	"ousheng/internal/testfix"
)

func TestTeamListCmd(t *testing.T) {
	dir := testfix.Setup(t)
	mustRun(t, dir, "me", "pm", "--name", "PM")
	mustRun(t, dir, "system", "add", "s")
	mustRun(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "s", "--machine", "aliyun-a")
	mustRun(t, dir, "team", "add", "ui-local", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "s", "--machine", "rdc-06(10.100.1.34)")

	out := mustRun(t, dir, "team", "list")
	for _, want := range []string{"pm", "data", "aliyun-a", "ui-local", "rdc-06(10.100.1.34)", "human", "agent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("team list missing %q:\n%s", want, out)
		}
	}
	// 未登记 machine 的座位显占位不空白（列对齐可读）
	if !strings.Contains(out, "—") {
		t.Fatalf("未登记 machine 应回退占位:\n%s", out)
	}
}

func TestTeamAddMachineAndReject(t *testing.T) {
	dir := testfix.Setup(t)
	mustRun(t, dir, "me", "pm")
	mustRun(t, dir, "system", "add", "s")
	// 非法 machine（含空格）拒
	if _, _, code := runIn(t, dir, "team", "add", "bad", "--type", "agent", "--responsible-human", "pm", "--machine", "bad machine"); code == 0 {
		t.Fatal("非法 machine 必须拒")
	}
	// 先建 data，再撞 --machine new-host → exists + 不更新提示
	mustRun(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "pm")
	out, se, _ := runIn(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "pm", "--machine", "new-host")
	if !strings.Contains(out, "exists") || !strings.Contains(se, "不随本命令更新") {
		t.Fatalf("已存在座位应提示 exists(stdout)+不更新(stderr):\nout:%s\nerr:%s", out, se)
	}
}
