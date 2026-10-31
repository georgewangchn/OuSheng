package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seatFor：HOME 隔离 + 座位工作区 + adapter install，返回 (repo, ws)。
// ws 补 git init（mkWorkspace 是手工骨架非 git 仓；凭据检查走 git 面需要）。
func seatFor(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	// 补 init 等价面：手工骨架无 gitignore，secrets 防线按 init 形态补上。
	os.WriteFile(filepath.Join(ws, ".ousheng", ".gitignore"), []byte("me\nrepos.yaml\nsecrets/\n"), 0o644)
	gitRun(t, ws, "init", "-q", "-b", "main")
	gitRun(t, ws, "config", "user.name", "t")
	gitRun(t, ws, "config", "user.email", "t@t.local")
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	return repo, ws
}

// writeSecrets：放一个本地凭据值文件（0600）。
func writeSecrets(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "secrets", name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeCredentials：写登记表（手改形态）。
func writeCredentials(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".ousheng", "credentials.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSecretsNeverTracked：init 首日 gitignore 含 secrets/；
// 误跟踪（git add -f）→ doctor FAIL。
func TestSecretsNeverTracked(t *testing.T) {
	repo, ws := seatFor(t)

	// init 首日防线（真 init 路径）。
	fresh := t.TempDir()
	mustRun(t, fresh, "init")
	gi, _ := os.ReadFile(filepath.Join(fresh, ".ousheng", ".gitignore"))
	if !strings.Contains(string(gi), "secrets/") {
		t.Fatalf("init gitignore must cover secrets/ from day one, got %q", gi)
	}

	// 健态：本地值存在（未跟踪）→ doctor 无 secrets FAIL。
	writeSecrets(t, ws, "lake.env", "DB_PASS=x\n")
	for _, l := range strings.Split(mustRun(t, repo, "doctor"), "\n") {
		if strings.Contains(l, "secrets") && strings.Contains(l, "FAIL") {
			t.Fatalf("untracked secrets must not fail:\n%s", l)
		}
	}

	// 事故态：强加进 git → FAIL。
	gitRun(t, ws, "add", "-f", ".ousheng/secrets/lake.env")
	if out := mustRunFail(t, repo, "doctor"); !strings.Contains(out, "已被 git 跟踪") {
		t.Fatalf("tracked secrets must FAIL with history-wash guidance:\n%s", out)
	}
}

// TestSecretPatternsWarn：高置信形态（PEM/token 前缀）→ WARN；
// 脱敏示例零误报。
func TestSecretPatternsWarn(t *testing.T) {
	repo, ws := seatFor(t)
	// 脱敏示例（零误报面）。
	writeCredentials(t, ws, "schema_version: 1\ncredentials:\n  - id: lake-db\n    system: lake\n    env: dev\n    desc: \"密码见 secrets（password: ***）\"\n    location: {kind: local-file, ref: secrets/lake.env#DB_PASS}\n")
	writeSecrets(t, ws, "lake.env", "DB_PASS=x\n")
	gitRun(t, ws, "add", ".ousheng")
	out := mustRun(t, repo, "doctor")
	if strings.Contains(out, "高置信凭据形态") {
		t.Fatalf("masked example must not trigger:\n%s", out)
	}

	// 真形态：PEM 私钥进了 design 正文 → WARN。
	d := filepath.Join(ws, ".ousheng", "designs", "leaky", "design.md")
	os.MkdirAll(filepath.Dir(d), 0o755)
	os.WriteFile(d, []byte("---\nstatus: draft\nowner: data\n---\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n"), 0o644)
	gitRun(t, ws, "add", ".ousheng")
	out = mustRun(t, repo, "doctor")
	if !strings.Contains(out, "高置信凭据形态") || !strings.Contains(out, "leaky/design.md") {
		t.Fatalf("PEM in rope files must WARN with locator:\n%s", out)
	}
}

// TestCredentialsDoctor：local-file 缺值 FAIL 清单；password-manager 缺 ref WARN；
// 无登记表零检查（opt-in）；坏登记表 FAIL。
func TestCredentialsDoctor(t *testing.T) {
	repo, ws := seatFor(t)

	// opt-in：无登记表 → 零凭据检查噪音。
	if out := mustRun(t, repo, "doctor"); strings.Contains(out, "凭据") {
		t.Fatalf("no registry must yield no credential checks:\n%s", out)
	}

	// local-file 缺值 → FAIL；补值 → PASS。
	writeCredentials(t, ws, "schema_version: 1\ncredentials:\n  - id: lake-db\n    system: lake\n    env: dev\n    location: {kind: local-file, ref: secrets/lake.env#DB_PASS}\n")
	gitRun(t, ws, "add", ".ousheng")
	out := mustRunFail(t, repo, "doctor")
	if !strings.Contains(out, "本机缺凭据值") || !strings.Contains(out, "lake-db") {
		t.Fatalf("missing local value must FAIL with list:\n%s", out)
	}
	writeSecrets(t, ws, "lake.env", "DB_PASS=x\n")
	if out := mustRun(t, repo, "doctor"); !strings.Contains(out, "凭据登记 1 条，本机值齐") {
		t.Fatalf("complete values must pass:\n%s", out)
	}

	// password-manager 缺 ref → WARN（不扣退出码）。
	writeCredentials(t, ws, "schema_version: 1\ncredentials:\n  - id: pro-db\n    system: lake\n    env: pro\n    location: {kind: password-manager}\n")
	gitRun(t, ws, "add", ".ousheng")
	out = mustRun(t, repo, "doctor")
	if !strings.Contains(out, "pro-db：password-manager 位置缺条目名") {
		t.Fatalf("pm-without-ref must WARN:\n%s", out)
	}

	// 坏登记表（非法 kind）→ FAIL（校验手写纪律）。
	writeCredentials(t, ws, "schema_version: 1\ncredentials:\n  - id: x\n    system: lake\n    location: {kind: inline-value, ref: hunter2}\n")
	gitRun(t, ws, "add", ".ousheng")
	if out := mustRunFail(t, repo, "doctor"); !strings.Contains(out, "credentials.yaml 非法") {
		t.Fatalf("bad registry must FAIL loudly:\n%s", out)
	}
}

// mustRunFail：期待非零退出码，返回 stdout（doctor FAIL 面断言用）。
func mustRunFail(t *testing.T, dir string, args ...string) string {
	t.Helper()
	so, se, code := runIn(t, dir, args...)
	if code == 0 {
		t.Fatalf("cli %v expected failure, got success:\n%s", args, so)
	}
	return so + se
}
