// Package e2e 承载跨模块端到端验收（v0.3 §51 Phase 6：多机协作模拟）。
package e2e

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/internal/context"
	"ousheng/internal/model"
	"ousheng/internal/state"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
	"ousheng/internal/workspace"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// clone 的目标目录尚不存在：在父目录执行
	workdir := dir
	if len(args) > 0 && args[0] == "clone" {
		workdir = filepath.Dir(dir)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), workdir, err, out)
	}
	return string(out)
}

// §51 验收：Machine A 修改 → push；Machine B pull → index refresh；
// B 基于旧 revision 写入 → CAS conflict。
func TestMultiMachineSyncAndCASConflict(t *testing.T) {
	// Machine A：fixture workspace
	dirA := testfix.Setup(t)
	// Machine B：clone（第二台机器，只依赖 Git 同步，无共享 SQLite/中心服务）
	dirB := t.TempDir() + "/machine-b"
	gitIn(t, dirB, "clone", dirA, dirB)
	gitIn(t, dirB, "config", "user.email", "b@ousheng.local")
	gitIn(t, dirB, "config", "user.name", "machine-b")

	// B 初始状态 = A 状态
	repoB := gityaml.Open(dirB)
	wB, err := repoB.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if wB.Revision != 7 {
		t.Fatalf("machine B initial revision should be 7, got %d", wB.Revision)
	}

	// A 修改 BUG-017（status doing → testing，revision 8）
	svcA := workspace.New(gityaml.Open(dirA))
	wA, err := svcA.Repo.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	wA.Status = model.StatusTesting
	if _, err := svcA.Update(wA, wA.Revision, "backend-agent"); err != nil {
		t.Fatal(err)
	}

	// B pull + index refresh → 进入与 A 同一状态（§51 时序：先 pull）
	gitIn(t, dirB, "pull", "origin", "main")
	repoB2 := gityaml.Open(dirB)
	wB2, err := repoB2.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if wB2.Revision != 8 {
		t.Fatalf("machine B after pull should see revision 8, got %d", wB2.Revision)
	}

	// B 基于旧 revision（7，pull 前的缓存快照）写入 → 必须冲突
	svcB := workspace.New(repoB2)
	stale := wB // pull 前读到的 rev 7 快照
	stale.Title = "B 的过期修改"
	if _, err := svcB.Update(stale, 7, "backend-agent"); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("expected CAS conflict on stale revision, got %v", err)
	}

	// B 的 context 与 A 的 context 一致（同一 canonical state 的同构投影）
	cB, err := context.New(gityaml.Open(dirB))
	if err != nil {
		t.Fatal(err)
	}
	d, err := cB.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 8 || d.Status != model.StatusTesting {
		t.Fatalf("machine B context should see revision 8 testing, got rev %d %s", d.Revision, d.Status)
	}
	cA, err := context.New(gityaml.Open(dirA))
	if err != nil {
		t.Fatal(err)
	}
	mcA, err := cA.GetMyContext("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	mcB, err := cB.GetMyContext("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(mcA.ActiveWork) != len(mcB.ActiveWork) {
		t.Fatalf("context divergence: %d vs %d active work", len(mcA.ActiveWork), len(mcB.ActiveWork))
	}
	for i := range mcA.ActiveWork {
		if mcA.ActiveWork[i].Status != mcB.ActiveWork[i].Status {
			t.Fatalf("context divergence at %d: %s vs %s", i, mcA.ActiveWork[i].Status, mcB.ActiveWork[i].Status)
		}
	}

	// B 基于新 revision（8）写入 → 成功，且 A pull 后可见（双向）
	fresh := d.WorkItem
	fresh.Progress = &model.ProgressReport{
		Value: 0.9, Actor: "backend-agent", ReportedAt: model.Now(), Basis: model.BasisTestCases,
	}
	if _, err := svcB.ReportProgress("BUG-017", *fresh.Progress, 8); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dirA, "pull", dirB, "main")
	wA2, err := gityaml.Open(dirA).GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if wA2.Revision != 9 || wA2.Progress == nil || wA2.Progress.Value != 0.9 {
		t.Fatalf("machine A after pull should see revision 9 with progress 0.9, got rev %d %+v", wA2.Revision, wA2.Progress)
	}
}

// S7：第二台机器不需要共享 SQLite / 中心服务器 / 额外数据库。
// cache/index.db 不参与同步（gitignored），pull 后 rebuild 即恢复查询能力。
func TestSecondMachineZeroInfrastructure(t *testing.T) {
	dirA := testfix.Setup(t)
	dirB := t.TempDir() + "/machine-b"
	gitIn(t, dirB, "clone", dirA, dirB)

	// A 侧产生一个 SQLite 派生索引
	repoA := gityaml.Open(dirA)
	// A 再写一次，制造 A/B 状态差
	svcA := workspace.New(repoA)
	w, err := svcA.Repo.GetWorkItem("K8S-003")
	if err != nil {
		t.Fatal(err)
	}
	w.Status = model.StatusReady
	if _, err := svcA.Update(w, w.Revision, "wangwu"); err != nil {
		t.Fatal(err)
	}

	// B pull（不碰任何 SQLite）后内存索引直接可用
	gitIn(t, dirB, "pull", "origin", "main")
	c, err := context.New(gityaml.Open(dirB))
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.GetWorkItem("K8S-003")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != model.StatusReady {
		t.Fatalf("zero-infra machine should see ready, got %s", d.Status)
	}
}
