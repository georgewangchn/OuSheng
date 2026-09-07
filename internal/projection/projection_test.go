package projection

import (
	"bytes"
	"strings"
	"testing"

	"ousheng/internal/context"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

func proj(t *testing.T) *Service {
	t.Helper()
	dir := testfix.Setup(t)
	c, err := context.New(gityaml.Open(dir))
	if err != nil {
		t.Fatal(err)
	}
	return New(c)
}

func TestKanban(t *testing.T) {
	p := proj(t)
	cols := p.Kanban()
	var statuses []string
	for _, c := range cols {
		statuses = append(statuses, string(c.Status))
	}
	// fixtures: doing×2 backlog×1 ready×1
	if strings.Join(statuses, ",") != "backlog,ready,doing" {
		t.Fatalf("columns wrong: %v", statuses)
	}

	var buf bytes.Buffer
	RenderKanban(&buf, cols)
	out := buf.String()
	// §21 卡片要素：System/Version/Role/Actor/Human/Progress(reported)/Blocker
	for _, want := range []string{
		"== DOING (2) ==",
		"[FEAT-CDC-001] CDC 增量同步",
		"System    datax-backend (DataX Backend)",
		"Version   v2.0",
		"Role      backend",
		"Actor     backend-agent",
		"Human     zhangsan",
		"70% reported (implementation-checklist)",
		"Blocker   K8S-003",
		"[BUG-017] CDC checkpoint 恢复失败",
		"== BACKLOG (1) ==",
		"[K8S-003] K8s Runtime 升级阻塞排查",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("kanban missing %q:\n%s", want, out)
		}
	}
}

func TestKanbanEmpty(t *testing.T) {
	dir := testfix.Setup(t)
	testfix.StripWork(t, dir)
	c, err := context.New(gityaml.Open(dir))
	if err != nil {
		t.Fatal(err)
	}
	p := New(c)
	var buf bytes.Buffer
	RenderKanban(&buf, p.Kanban())
	if !strings.Contains(buf.String(), "(no work items)") {
		t.Fatalf("empty kanban wrong: %q", buf.String())
	}
}

func TestVersionView(t *testing.T) {
	p := proj(t)
	blocks := p.VersionView("v2.0")
	if len(blocks) != 3 {
		t.Fatalf("expected 3 system blocks, got %d", len(blocks))
	}
	var buf bytes.Buffer
	RenderVersionView(&buf, blocks)
	out := buf.String()
	for _, want := range []string{
		"datax-backend / v2.0",
		"doing 2",
		"Reported Progress:",
		"FEAT-CDC-001  70% (implementation-checklist)",
		"BUG-017  30% (manual)",
		"Blockers: K8S-003",
		"lakehouse-k8s / v2.0",
		"datax-ui / v2.0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("version view missing %q:\n%s", want, out)
		}
	}
	// 不合成虚假总百分比（§19/§24）
	if strings.Contains(out, "Total") || strings.Contains(strings.ToLower(out), "overall") {
		t.Fatalf("version view must not fabricate aggregate percentage:\n%s", out)
	}
}

func TestProjectSummary(t *testing.T) {
	p := proj(t)
	sum := p.ProjectSummary()
	if sum.Project != "smart-lakehouse" || sum.Name != "智能湖仓" {
		t.Fatalf("project wrong: %+v", sum)
	}
	if sum.Total != 4 || sum.Counts["doing"] != 2 || sum.Counts["backlog"] != 1 || sum.Counts["ready"] != 1 {
		t.Fatalf("counts wrong: %+v", sum)
	}
	if len(sum.Blockers) != 1 || sum.Blockers[0] != "K8S-003" {
		t.Fatalf("blockers wrong: %v", sum.Blockers)
	}
	var buf bytes.Buffer
	RenderProjectSummary(&buf, sum)
	if !strings.Contains(buf.String(), "smart-lakehouse (智能湖仓)") {
		t.Fatalf("summary render wrong:\n%s", buf.String())
	}
}
