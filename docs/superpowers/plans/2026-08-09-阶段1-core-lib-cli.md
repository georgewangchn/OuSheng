# 阶段1：core lib + `board` CLI 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 WTH 的 core lib（存储 / CAS / schema 校验 / 状态机 / DAG / 投影闸门 / 全局归约）与规范入口 `board` CLI，产出可独立运行的裸兜底工具。

**Architecture:** 单一 Go module。core 逻辑在 `internal/` 各包，`cmd/board` 是薄 CLI 前端。store = git 仓，一卡一 YAML 文件（`cards/<id>.yaml`）。校验手写（不拉 JSON Schema 库）。git 通过 `os/exec` 调用（不拉 go-git）。CAS 借 git 提交原子性 + 进程锁文件。

**Tech Stack:** Go 1.22+，`gopkg.in/yaml.v3`（唯一外部依赖），系统 `git`。

## Global Constraints

- Go 1.22+；唯一外部依赖 `gopkg.in/yaml.v3`；git 走 `os/exec`；不引入其他外部库。
- module 路径：`wth`。import 形如 `wth/internal/card`。
- store 布局：board 是一个 git 仓，卡片文件在 `cards/<id>.yaml`。
- 卡 id 正则：`^[a-z0-9][a-z0-9-]*$`。
- 卡文件最大 8192 字节（投影闸门尺寸上限）。
- status 枚举恒为 5 态：`proposed | agreed | live | verified | deprecated`。`broken`/`stuck` 不存为 status，是 converge 派生谓词。
- 每步 TDD：先写失败测试 → 跑证其失败 → 最小实现 → 跑证通过 → 提交。
- 提交信息用正常散文（非 caveman），结尾附 `Co-Authored-By: Claude <noreply@anthropic.com>`。

## File Structure

- `go.mod` — module 定义。
- `cmd/board/main.go` — CLI 入口，子命令 `init|read|write|converge|version`。
- `internal/card/card.go` — Card / Contract / Evidence / HumanAck 结构 + YAML 编解码（未知字段拒绝）。
- `internal/card/validate.go` — 量纲校验：字段规则 + 尺寸 + 禁内容（投影闸门）。
- `internal/lifecycle/machine.go` — status 合法迁移表。
- `internal/graph/dag.go` — depends_on 建图 + 环检测。
- `internal/store/store.go` — git-backed 存储：init / 读卡 / 写卡+提交 / 锁 / CAS。
- `internal/board/board.go` — actuator API：ReadBoard / WriteBoard / Converge，编排上述包。

---

### Task 1: 项目脚手架 + `board version`

**Files:**
- Create: `go.mod`
- Create: `cmd/board/main.go`
- Test: `cmd/board/main_test.go`

**Interfaces:**
- Consumes: 无
- Produces: 可运行二进制 `board`，`board version` 打印版本串 `wth board 0.1.0`。

- [ ] **Step 1: 写失败测试**

```go
// cmd/board/main_test.go
package main

import "testing"

func TestVersionString(t *testing.T) {
	if got := versionString(); got != "wth board 0.1.0" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./cmd/board/ -run TestVersionString -v`
Expected: 编译失败（`versionString` 未定义）或 module 未初始化。

- [ ] **Step 3: 最小实现**

```bash
go mod init wth
go get gopkg.in/yaml.v3
```

```go
// cmd/board/main.go
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func versionString() string { return "wth board " + version }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: board <init|read|write|converge|version>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(versionString())
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./cmd/board/ -run TestVersionString -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add go.mod go.sum cmd/board/main.go cmd/board/main_test.go
git commit -m "feat: scaffold go module and board version command"
```

---

### Task 2: Card 模型 + YAML 编解码（拒绝未知字段）

**Files:**
- Create: `internal/card/card.go`
- Test: `internal/card/card_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - 类型 `Status string`，常量 `Proposed|Agreed|Live|Verified|Deprecated`。
  - 结构 `Card{ID, Owner, Task string; Status Status; Version int; DependsOn []string; Contract Contract; Evidence *Evidence; HumanAck *HumanAck}`。
  - `Contract{Kind string; Breaking bool; Interface any}`；`Evidence{Probe, PassedAtCommit, By string}`；`HumanAck{Approver string; AtVersion int}`。
  - `func Decode(b []byte) (Card, error)` — 未知字段报错。
  - `func Encode(c Card) ([]byte, error)`。

- [ ] **Step 1: 写失败测试**

```go
// internal/card/card_test.go
package card

import "testing"

func TestDecodeRoundTrip(t *testing.T) {
	in := []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n")
	c, err := Decode(in)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.ID != "a" || c.Status != Proposed || c.Contract.Kind != "http" {
		t.Fatalf("bad decode: %+v", c)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	in := []byte("id: a\nowner: b\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\nsecret_memory: leak\n")
	if _, err := Decode(in); err == nil {
		t.Fatal("expected error on unknown field")
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/card/ -v`
Expected: 编译失败（`Decode` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/card/card.go
package card

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

type Status string

const (
	Proposed   Status = "proposed"
	Agreed     Status = "agreed"
	Live       Status = "live"
	Verified   Status = "verified"
	Deprecated Status = "deprecated"
)

type Contract struct {
	Kind      string `yaml:"kind"`
	Breaking  bool   `yaml:"breaking"`
	Interface any    `yaml:"interface"`
}

type Evidence struct {
	Probe          string `yaml:"probe"`
	PassedAtCommit string `yaml:"passed_at_commit"`
	By             string `yaml:"by"`
}

type HumanAck struct {
	Approver  string `yaml:"approver"`
	AtVersion int    `yaml:"at_version"`
}

type Card struct {
	ID        string    `yaml:"id"`
	Owner     string    `yaml:"owner"`
	Task      string    `yaml:"task"`
	Status    Status    `yaml:"status"`
	Version   int       `yaml:"version"`
	DependsOn []string  `yaml:"depends_on,omitempty"`
	Contract  Contract  `yaml:"contract"`
	Evidence  *Evidence `yaml:"evidence,omitempty"`
	HumanAck  *HumanAck `yaml:"human_ack,omitempty"`
}

func Decode(b []byte) (Card, error) {
	var c Card
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Card{}, err
	}
	return c, nil
}

func Encode(c Card) ([]byte, error) {
	return yaml.Marshal(c)
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/card/ -v`
Expected: PASS（两个测试）

- [ ] **Step 5: 提交**

```bash
git add internal/card/card.go internal/card/card_test.go
git commit -m "feat: card model with yaml codec rejecting unknown fields"
```

---

### Task 3: 量纲校验 + 投影闸门（字段规则 + 尺寸 + 禁内容）

**Files:**
- Create: `internal/card/validate.go`
- Test: `internal/card/validate_test.go`

**Interfaces:**
- Consumes: `card.Card`（Task 2）
- Produces:
  - `const MaxCardBytes = 8192`
  - `func Validate(c Card, raw []byte) error` — 一处校验字段规则、尺寸、禁内容；不合规返回非 nil error。
  - 规则：id 匹配 `^[a-z0-9][a-z0-9-]*$`；owner/task 非空；status 属 5 枚举；version ≥ 0；contract.kind 属 `{http,cli,lib,event}`；`status==verified` 必须 `Evidence!=nil` 且三字段非空；`contract.breaking==true` 必须 `HumanAck!=nil`；`len(raw) ≤ MaxCardBytes`；raw 不含围栏代码块 ```` ``` ```` 或 `Traceback (most recent call last)`。

- [ ] **Step 1: 写失败测试**

```go
// internal/card/validate_test.go
package card

import (
	"strings"
	"testing"
)

func base() Card {
	return Card{ID: "a", Owner: "backend", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestValidateOK(t *testing.T) {
	c := base()
	if err := Validate(c, []byte("id: a")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateBadID(t *testing.T) {
	c := base()
	c.ID = "Bad_ID"
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected id error")
	}
}

func TestValidateVerifiedNeedsEvidence(t *testing.T) {
	c := base()
	c.Status = Verified
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected evidence required")
	}
}

func TestValidateBreakingNeedsHumanAck(t *testing.T) {
	c := base()
	c.Contract.Breaking = true
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected human_ack required")
	}
}

func TestValidateRejectsCodeBlock(t *testing.T) {
	c := base()
	raw := []byte("task: |\n  " + strings.Repeat("`", 3) + "go\n  leak\n")
	if err := Validate(c, raw); err == nil {
		t.Fatal("expected forbidden-content error")
	}
}

func TestValidateRejectsOversize(t *testing.T) {
	c := base()
	if err := Validate(c, make([]byte, MaxCardBytes+1)); err == nil {
		t.Fatal("expected size error")
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/card/ -run TestValidate -v`
Expected: 编译失败（`Validate`/`MaxCardBytes` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/card/validate.go
package card

import (
	"fmt"
	"regexp"
	"strings"
)

const MaxCardBytes = 8192

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var kinds = map[string]bool{"http": true, "cli": true, "lib": true, "event": true}

var statuses = map[Status]bool{Proposed: true, Agreed: true, Live: true, Verified: true, Deprecated: true}

var forbidden = []string{"```", "Traceback (most recent call last)"}

func Validate(c Card, raw []byte) error {
	if !idRe.MatchString(c.ID) {
		return fmt.Errorf("invalid id %q", c.ID)
	}
	if strings.TrimSpace(c.Owner) == "" {
		return fmt.Errorf("owner required")
	}
	if strings.TrimSpace(c.Task) == "" {
		return fmt.Errorf("task required")
	}
	if !statuses[c.Status] {
		return fmt.Errorf("invalid status %q", c.Status)
	}
	if c.Version < 0 {
		return fmt.Errorf("version must be >= 0")
	}
	if !kinds[c.Contract.Kind] {
		return fmt.Errorf("invalid contract.kind %q", c.Contract.Kind)
	}
	if c.Status == Verified {
		if c.Evidence == nil || c.Evidence.Probe == "" || c.Evidence.PassedAtCommit == "" || c.Evidence.By == "" {
			return fmt.Errorf("status=verified requires complete evidence")
		}
	}
	if c.Contract.Breaking && c.HumanAck == nil {
		return fmt.Errorf("contract.breaking=true requires human_ack")
	}
	if len(raw) > MaxCardBytes {
		return fmt.Errorf("card exceeds %d bytes", MaxCardBytes)
	}
	for _, f := range forbidden {
		if strings.Contains(string(raw), f) {
			return fmt.Errorf("forbidden content: %q", f)
		}
	}
	return nil
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/card/ -v`
Expected: PASS（全部）

- [ ] **Step 5: 提交**

```bash
git add internal/card/validate.go internal/card/validate_test.go
git commit -m "feat: dimension validation and projection gate"
```

---

### Task 4: 生命周期状态机（合法迁移）

**Files:**
- Create: `internal/lifecycle/machine.go`
- Test: `internal/lifecycle/machine_test.go`

**Interfaces:**
- Consumes: `card.Status`（Task 2）
- Produces: `func CanTransition(from, to card.Status) bool` — 允许合法迁移与同态（内容编辑不改 status）。
  - 合法边：`proposed→agreed`，`agreed→live`，`live→verified`，`verified→live`（探针回退），任意 `→deprecated`。同态 `x→x` 允许。

- [ ] **Step 1: 写失败测试**

```go
// internal/lifecycle/machine_test.go
package lifecycle

import (
	"testing"

	"wth/internal/card"
)

func TestLegalTransitions(t *testing.T) {
	ok := [][2]card.Status{
		{card.Proposed, card.Agreed}, {card.Agreed, card.Live},
		{card.Live, card.Verified}, {card.Verified, card.Live},
		{card.Live, card.Deprecated}, {card.Verified, card.Deprecated},
		{card.Live, card.Live},
	}
	for _, e := range ok {
		if !CanTransition(e[0], e[1]) {
			t.Errorf("expected %s->%s legal", e[0], e[1])
		}
	}
}

func TestIllegalTransitions(t *testing.T) {
	bad := [][2]card.Status{
		{card.Proposed, card.Verified}, {card.Deprecated, card.Live},
		{card.Proposed, card.Live},
	}
	for _, e := range bad {
		if CanTransition(e[0], e[1]) {
			t.Errorf("expected %s->%s illegal", e[0], e[1])
		}
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/lifecycle/ -v`
Expected: 编译失败（`CanTransition` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/lifecycle/machine.go
package lifecycle

import "wth/internal/card"

var edges = map[card.Status]map[card.Status]bool{
	card.Proposed: {card.Agreed: true, card.Deprecated: true},
	card.Agreed:   {card.Live: true, card.Deprecated: true},
	card.Live:     {card.Verified: true, card.Deprecated: true},
	card.Verified: {card.Live: true, card.Deprecated: true},
}

func CanTransition(from, to card.Status) bool {
	if from == to {
		return true
	}
	return edges[from][to]
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/lifecycle/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/lifecycle/
git commit -m "feat: lifecycle state machine transitions"
```

---

### Task 5: DAG 环检测

**Files:**
- Create: `internal/graph/dag.go`
- Test: `internal/graph/dag_test.go`

**Interfaces:**
- Consumes: 无（接收 `map[string][]string` 邻接表：id → depends_on）
- Produces: `func FindCycle(deps map[string][]string) []string` — 有环返回环上节点序列，无环返回 nil。

- [ ] **Step 1: 写失败测试**

```go
// internal/graph/dag_test.go
package graph

import "testing"

func TestNoCycle(t *testing.T) {
	deps := map[string][]string{"a": {"b"}, "b": {"c"}, "c": nil}
	if got := FindCycle(deps); got != nil {
		t.Fatalf("expected no cycle, got %v", got)
	}
}

func TestCycle(t *testing.T) {
	deps := map[string][]string{"a": {"b"}, "b": {"a"}}
	if got := FindCycle(deps); got == nil {
		t.Fatal("expected cycle")
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/graph/ -v`
Expected: 编译失败（`FindCycle` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/graph/dag.go
package graph

const (
	white = 0
	gray  = 1
	black = 2
)

func FindCycle(deps map[string][]string) []string {
	color := map[string]int{}
	var stack []string
	var dfs func(n string) []string
	dfs = func(n string) []string {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range deps[n] {
			switch color[m] {
			case gray:
				return append(stack, m)
			case white:
				if c := dfs(m); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return nil
	}
	for n := range deps {
		if color[n] == white {
			if c := dfs(n); c != nil {
				return c
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/graph/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/graph/
git commit -m "feat: DAG cycle detection"
```

---

### Task 6: git-backed 存储（init / 读全部 / 写卡+提交）

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `card.Card`、`card.Decode`、`card.Encode`（Task 2）
- Produces:
  - `type Store struct{ Dir string }`
  - `func Open(dir string) *Store`
  - `func (s *Store) Init() error` — `git init` + 建 `cards/` 目录。
  - `func (s *Store) List() ([]card.Card, error)` — 读 `cards/*.yaml` 全部并解码。
  - `func (s *Store) Get(id string) (card.Card, []byte, bool, error)` — 返回卡、原始字节、是否存在。
  - `func (s *Store) commit(id string, raw []byte, msg string) error` — 写文件 + `git add` + `git commit`（内部）。
  - `func gitRun(dir string, args ...string) (string, error)`（内部 helper）。

- [ ] **Step 1: 写失败测试**

```go
// internal/store/store_test.go
package store

import (
	"testing"

	"wth/internal/card"
)

func newCard(id string) card.Card {
	return card.Card{ID: id, Owner: "backend", Task: "t", Status: card.Proposed, Version: 1,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestInitAndCommitAndList(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	c := newCard("a")
	raw, _ := card.Encode(c)
	if err := s.commit("a", raw, "add a"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("bad list: %+v", got)
	}
	_, _, ok, err := s.Get("a")
	if err != nil || !ok {
		t.Fatalf("get a: ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/store/ -v`
Expected: 编译失败（`Open`/`Init` 等未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/store/store.go
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wth/internal/card"
)

type Store struct{ Dir string }

func Open(dir string) *Store { return &Store{Dir: dir} }

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (s *Store) cardsDir() string { return filepath.Join(s.Dir, "cards") }
func (s *Store) path(id string) string {
	return filepath.Join(s.cardsDir(), id+".yaml")
}

func (s *Store) Init() error {
	if err := os.MkdirAll(s.cardsDir(), 0o755); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "init"); err != nil {
		return err
	}
	// 本地身份，保证测试环境可提交
	_, _ = gitRun(s.Dir, "config", "user.email", "board@wth.local")
	_, _ = gitRun(s.Dir, "config", "user.name", "board")
	return nil
}

func (s *Store) List() ([]card.Card, error) {
	entries, err := os.ReadDir(s.cardsDir())
	if err != nil {
		return nil, err
	}
	var out []card.Card
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.cardsDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		c, err := card.Decode(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) Get(id string) (card.Card, []byte, bool, error) {
	b, err := os.ReadFile(s.path(id))
	if os.IsNotExist(err) {
		return card.Card{}, nil, false, nil
	}
	if err != nil {
		return card.Card{}, nil, false, err
	}
	c, err := card.Decode(b)
	if err != nil {
		return card.Card{}, nil, false, err
	}
	return c, b, true, nil
}

func (s *Store) commit(id string, raw []byte, msg string) error {
	if err := os.WriteFile(s.path(id), raw, 0o644); err != nil {
		return err
	}
	if _, err := gitRun(s.Dir, "add", filepath.Join("cards", id+".yaml")); err != nil {
		return err
	}
	_, err := gitRun(s.Dir, "commit", "-m", msg)
	return err
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/store/
git commit -m "feat: git-backed card store with init, list, get, commit"
```

---

### Task 7: CAS + 进程锁（原子写）

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/cas_test.go`

**Interfaces:**
- Consumes: `Store`（Task 6）
- Produces:
  - `var ErrConflict = errors.New("version conflict")`
  - `func (s *Store) Write(c card.Card, expectedVersion int, validate func(card.Card, []byte) error, msg string) (card.Card, error)` — 锁内做：读现值 → CAS 比对 → 版本 +1 → 重编码 → 对**最终字节**跑 `validate`（nil 则跳过） → git 提交。CAS 不符返回 `ErrConflict`。新卡 `expectedVersion==0`。
  - 说明：`validate` 回调让上层（Task 8）把量纲校验落在**含新版本号的最终字节**上，且整个读-校-写在锁内原子。锁文件 `.board.lock`（`O_CREATE|O_EXCL`）序列化本地进程；分布式 CAS（git push 拒绝）留阶段4。

- [ ] **Step 1: 写失败测试**

```go
// internal/store/cas_test.go
package store

import (
	"errors"
	"testing"
)

func TestWriteBumpsVersion(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	c := newCard("a")
	w, err := s.Write(c, 0, nil, "add")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.Version != 1 {
		t.Fatalf("want version 1, got %d", w.Version)
	}
}

func TestWriteConflictOnStaleExpected(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	c := newCard("a")
	_, _ = s.Write(c, 0, nil, "add")      // now version 1
	_, err := s.Write(c, 0, nil, "again") // stale expected
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/store/ -run TestWrite -v`
Expected: 编译失败（`Write`/`ErrConflict` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// 追加到 internal/store/store.go
import "errors"   // 合并进已有 import 块

var ErrConflict = errors.New("version conflict")

func (s *Store) lock() (func(), error) {
	lp := filepath.Join(s.Dir, ".board.lock")
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("board locked: %w", err)
	}
	return func() { f.Close(); os.Remove(lp) }, nil
}

func (s *Store) Write(c card.Card, expectedVersion int, validate func(card.Card, []byte) error, msg string) (card.Card, error) {
	unlock, err := s.lock()
	if err != nil {
		return card.Card{}, err
	}
	defer unlock()

	cur, _, ok, err := s.Get(c.ID)
	if err != nil {
		return card.Card{}, err
	}
	curVer := 0
	if ok {
		curVer = cur.Version
	}
	if expectedVersion != curVer {
		return card.Card{}, ErrConflict
	}
	c.Version = curVer + 1
	out, err := card.Encode(c)
	if err != nil {
		return card.Card{}, err
	}
	if validate != nil {
		if err := validate(c, out); err != nil {
			return card.Card{}, err
		}
	}
	if err := s.commit(c.ID, out, msg); err != nil {
		return card.Card{}, err
	}
	return c, nil
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/store/ -v`
Expected: PASS（全部）

- [ ] **Step 5: 提交**

```bash
git add internal/store/
git commit -m "feat: CAS write with process lock and version bump"
```

---

### Task 8: Board actuator API（ReadBoard / WriteBoard）

**Files:**
- Create: `internal/board/board.go`
- Test: `internal/board/board_test.go`

**Interfaces:**
- Consumes: `store.Store`（6/7）、`card.Validate`（3）、`lifecycle.CanTransition`（4）
- Produces:
  - `type Board struct{ store *store.Store }`；`func New(dir string) *Board`；`func (b *Board) Init() error`。
  - `type Scope struct{ ID, Owner, Status, Kind string }`（空串为通配）。
  - `func (b *Board) ReadBoard(s Scope) ([]card.Card, error)` — 读全部再按 Scope 过滤。
  - `func (b *Board) WriteBoard(c card.Card, expectedVersion int) (card.Card, error)` — 校验状态迁移（新卡须 `proposed`）+ 委托 `store.Write`（锁内 CAS + 量纲校验回调）。非法迁移或校验失败返回 error；版本冲突返回 `store.ErrConflict`。

> 迁移检查在锁外读旧态，CAS 在锁内按 version 裁定：若期间发生写，version 变、CAS 拒、上层重读，故不产生错误接受。

- [ ] **Step 1: 写失败测试**

```go
// internal/board/board_test.go
package board

import (
	"testing"

	"wth/internal/card"
)

func mk(id string) card.Card {
	return card.Card{ID: id, Owner: "backend", Task: "t", Status: card.Proposed, Version: 1,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestWriteReadFilter(t *testing.T) {
	b := New(t.TempDir())
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WriteBoard(mk("a"), 0); err != nil {
		t.Fatalf("write a: %v", err)
	}
	got, err := b.ReadBoard(Scope{Owner: "backend"})
	if err != nil || len(got) != 1 {
		t.Fatalf("read: %v n=%d", err, len(got))
	}
	if none, _ := b.ReadBoard(Scope{Owner: "frontend"}); len(none) != 0 {
		t.Fatalf("expected 0 for frontend, got %d", len(none))
	}
}

func TestWriteRejectsIllegalTransition(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	a, _ := b.WriteBoard(mk("a"), 0) // proposed v1
	a.Status = card.Verified          // proposed->verified illegal
	if _, err := b.WriteBoard(a, 1); err == nil {
		t.Fatal("expected illegal transition error")
	}
}

func TestWriteRejectsInvalidCard(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := mk("a")
	c.Contract.Kind = "grpc" // not in enum
	if _, err := b.WriteBoard(c, 0); err == nil {
		t.Fatal("expected validation error")
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/board/ -v`
Expected: 编译失败（`New` 等未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/board/board.go
package board

import (
	"fmt"

	"wth/internal/card"
	"wth/internal/lifecycle"
	"wth/internal/store"
)

type Board struct{ store *store.Store }

func New(dir string) *Board { return &Board{store: store.Open(dir)} }

func (b *Board) Init() error { return b.store.Init() }

type Scope struct{ ID, Owner, Status, Kind string }

func (s Scope) match(c card.Card) bool {
	if s.ID != "" && c.ID != s.ID {
		return false
	}
	if s.Owner != "" && c.Owner != s.Owner {
		return false
	}
	if s.Status != "" && string(c.Status) != s.Status {
		return false
	}
	if s.Kind != "" && c.Contract.Kind != s.Kind {
		return false
	}
	return true
}

func (b *Board) ReadBoard(s Scope) ([]card.Card, error) {
	all, err := b.store.List()
	if err != nil {
		return nil, err
	}
	var out []card.Card
	for _, c := range all {
		if s.match(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (b *Board) WriteBoard(c card.Card, expectedVersion int) (card.Card, error) {
	prev, _, ok, err := b.store.Get(c.ID)
	if err != nil {
		return card.Card{}, err
	}
	if ok {
		if !lifecycle.CanTransition(prev.Status, c.Status) {
			return card.Card{}, fmt.Errorf("illegal transition %s->%s", prev.Status, c.Status)
		}
	} else if c.Status != card.Proposed {
		return card.Card{}, fmt.Errorf("new card must start in proposed")
	}
	msg := fmt.Sprintf("board: write %s -> %s", c.ID, c.Status)
	return b.store.Write(c, expectedVersion, card.Validate, msg)
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/board/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/board/board.go internal/board/board_test.go
git commit -m "feat: board actuator ReadBoard and WriteBoard"
```

---

### Task 9: Converge 全局归约

**Files:**
- Create: `internal/board/converge.go`
- Test: `internal/board/converge_test.go`

**Interfaces:**
- Consumes: `Board.ReadBoard`（8）、`graph.FindCycle`（5）、`card`（2）
- Produces:
  - `type Convergence struct{ Status string; Blockers []string; Cycle []string }`（Status ∈ `CONVERGED|IN_PROGRESS|STUCK`）。
  - `func (b *Board) Converge() (Convergence, error)`。
  - 阶段1 判定：环 → STUCK+Cycle；`broken`（verified 卡的某依赖为 deprecated）→ STUCK+Blockers；有 `proposed` 或非全 verified → IN_PROGRESS；全 verified ∧ 各带 evidence ∧ 无 proposed ∧ 无 broken ∧ 无环 → CONVERGED。
  - 说明：时基 `stuck`（停滞 N 周期）需周期时钟，阶段1 未建，留后续阶段。

- [ ] **Step 1: 写失败测试**

```go
// internal/board/converge_test.go
package board

import (
	"testing"

	"wth/internal/card"
)

func verified(id string) card.Card {
	return card.Card{ID: id, Owner: "o", Task: "t", Status: card.Verified, Version: 1,
		Contract: card.Contract{Kind: "http", Interface: []any{}},
		Evidence: &card.Evidence{Probe: "p", PassedAtCommit: "c", By: "frontend"}}
}

func TestConvergeConverged(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	// proposed -> agreed -> live -> verified
	c := mk("a")
	c, _ = b.WriteBoard(c, 0)
	c.Status = card.Agreed
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Live
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Verified
	c.Evidence = &card.Evidence{Probe: "p", PassedAtCommit: "x", By: "frontend"}
	if _, err := b.WriteBoard(c, c.Version); err != nil {
		t.Fatalf("to verified: %v", err)
	}
	got, err := b.Converge()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "CONVERGED" {
		t.Fatalf("want CONVERGED, got %s %+v", got.Status, got)
	}
}

func TestConvergeInProgressWhenProposed(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	_, _ = b.WriteBoard(mk("a"), 0) // proposed
	got, _ := b.Converge()
	if got.Status != "IN_PROGRESS" {
		t.Fatalf("want IN_PROGRESS, got %s", got.Status)
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./internal/board/ -run TestConverge -v`
Expected: 编译失败（`Converge` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// internal/board/converge.go
package board

import (
	"wth/internal/card"
	"wth/internal/graph"
)

type Convergence struct {
	Status   string
	Blockers []string
	Cycle    []string
}

func (b *Board) Converge() (Convergence, error) {
	cards, err := b.ReadBoard(Scope{})
	if err != nil {
		return Convergence{}, err
	}
	byID := map[string]card.Card{}
	deps := map[string][]string{}
	for _, c := range cards {
		byID[c.ID] = c
		deps[c.ID] = c.DependsOn
	}
	if cyc := graph.FindCycle(deps); cyc != nil {
		return Convergence{Status: "STUCK", Cycle: cyc}, nil
	}

	var blockers []string
	allVerified := true
	hasProposed := false
	for _, c := range cards {
		if c.Status == card.Proposed {
			hasProposed = true
		}
		if c.Status != card.Verified {
			allVerified = false
		}
		if c.Status == card.Verified {
			if c.Evidence == nil {
				blockers = append(blockers, c.ID+": verified without evidence")
			}
			for _, d := range c.DependsOn {
				if dep, ok := byID[d]; ok && dep.Status == card.Deprecated {
					blockers = append(blockers, c.ID+": broken, depends on deprecated "+d)
				}
			}
		}
	}
	if len(blockers) > 0 {
		return Convergence{Status: "STUCK", Blockers: blockers}, nil
	}
	if len(cards) > 0 && allVerified && !hasProposed {
		return Convergence{Status: "CONVERGED"}, nil
	}
	return Convergence{Status: "IN_PROGRESS"}, nil
}
```

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./internal/board/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/board/converge.go internal/board/converge_test.go
git commit -m "feat: global convergence reduction over board"
```

---

### Task 10: `board` CLI 子命令接线（init / read / write / converge）

**Files:**
- Modify: `cmd/board/main.go`
- Test: `cmd/board/cli_test.go`

**Interfaces:**
- Consumes: `board.Board`（8/9）
- Produces:
  - `func run(args []string, stdout, stderr io.Writer) int` — 解析子命令并执行，返回退出码。`main` 调 `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`。
  - 子命令：
    - `init [dir]` — 默认当前目录。
    - `read [--dir d] [--owner o] [--status s] [--kind k] [--id i]` — 打印匹配卡（YAML）。
    - `write --dir d --file f.yaml --expect N` — 解码文件写板；打印新版本或错误。
    - `converge [--dir d]` — 打印 `Status` 与 blockers/cycle。
    - `version` — 保留（Task 1）。

- [ ] **Step 1: 写失败测试**

```go
// cmd/board/cli_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInitWriteReadConverge(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer

	if code := run([]string{"init", dir}, &out, &errb); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errb.String())
	}

	cardFile := filepath.Join(dir, "a.yaml")
	os.WriteFile(cardFile, []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)

	out.Reset()
	errb.Reset()
	if code := run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &out, &errb); code != 0 {
		t.Fatalf("write code=%d err=%s", code, errb.String())
	}

	out.Reset()
	if code := run([]string{"read", "--dir", dir, "--owner", "backend"}, &out, &errb); code != 0 {
		t.Fatalf("read code=%d", code)
	}
	if !strings.Contains(out.String(), "id: a") {
		t.Fatalf("read output missing card: %s", out.String())
	}

	out.Reset()
	if code := run([]string{"converge", "--dir", dir}, &out, &errb); code != 0 {
		t.Fatalf("converge code=%d", code)
	}
	if !strings.Contains(out.String(), "IN_PROGRESS") {
		t.Fatalf("converge output: %s", out.String())
	}
}
```

- [ ] **Step 2: 跑测试证其失败**

Run: `go test ./cmd/board/ -run TestCLIInitWriteReadConverge -v`
Expected: 编译失败（`run` 未定义）。

- [ ] **Step 3: 最小实现**

```go
// cmd/board/main.go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"wth/internal/board"
	"wth/internal/card"
)

const version = "0.1.0"

func versionString() string { return "wth board " + version }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: board <init|read|write|converge|version>")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, versionString())
		return 0
	case "init":
		dir := "."
		if len(args) > 1 {
			dir = args[1]
		}
		if err := board.New(dir).Init(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "read":
		fs := flag.NewFlagSet("read", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		owner := fs.String("owner", "", "filter owner")
		status := fs.String("status", "", "filter status")
		kind := fs.String("kind", "", "filter kind")
		id := fs.String("id", "", "filter id")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		cards, err := board.New(*dir).ReadBoard(board.Scope{ID: *id, Owner: *owner, Status: *status, Kind: *kind})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, c := range cards {
			b, _ := card.Encode(c)
			fmt.Fprintf(stdout, "---\n%s", b)
		}
		return 0
	case "write":
		fs := flag.NewFlagSet("write", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		file := fs.String("file", "", "card yaml file")
		expect := fs.Int("expect", 0, "expected version (CAS)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		c, err := card.Decode(raw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		w, err := board.New(*dir).WriteBoard(c, *expect)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "written %s version %d\n", w.ID, w.Version)
		return 0
	case "converge":
		fs := flag.NewFlagSet("converge", flag.ContinueOnError)
		dir := fs.String("dir", ".", "board dir")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		res, err := board.New(*dir).Converge()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		out, _ := yaml.Marshal(res)
		fmt.Fprint(stdout, string(out))
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
```

> 注意：Task 1 的 `main_test.go::TestVersionString` 仍通过（`versionString` 未变）。

- [ ] **Step 4: 跑测试证其通过**

Run: `go test ./... -v`
Expected: 全仓 PASS

- [ ] **Step 5: 提交**

```bash
git add cmd/board/main.go cmd/board/cli_test.go
git commit -m "feat: board CLI subcommands init read write converge"
```

---

## Self-Review

**Spec 覆盖（对照设计方案 §2/§3/§5）：**
- §2 卡片 schema 量纲 → Task 2（模型）+ Task 3（校验）：id/owner/task/status/version/contract/evidence/human_ack、未知字段拒、尺寸、禁内容。✓
- §3 `read_board` → Task 8 `ReadBoard` + Scope 过滤。✓
- §3 `write_board` 四门（CAS / schema / 状态机 / breaking 限速）→ Task 7（CAS+锁）+ Task 3（schema，含 breaking→human_ack）+ Task 4/8（状态机）。✓
- §3 `converge` 纯归约 → Task 9。✓
- §5 生命周期状态机 → Task 4。✓
- §5.3 DAG 环检测 → Task 5 + Task 9 接线。✓
- §2 投影硬闸门落 core lib → Task 3 `Validate` 在 `store.Write` 锁内回调执行。✓
- store = git 仓，一卡一文件 → Task 6。✓

**阶段1 明确不覆盖（留后续阶段，非遗漏）：**
- 时基 `stuck`（停滞 N 周期）需周期时钟——阶段2+。
- 签名验签（GPG）——阶段4（§6 F2）。
- 分布式 CAS（git push 拒绝）——阶段4 联邦。
- MCP server / opencode 接入——阶段2。

**类型一致性：** `Card`/`Contract`/`Evidence`/`HumanAck` 字段跨 Task 2→3→6→8 一致；`store.Write` 回调签名 `func(card.Card, []byte) error` 与 Task 8 传入的 `card.Validate` 一致；`Scope` 字段与 CLI `read` flag 一致；`Convergence.Status` 串值 `CONVERGED|IN_PROGRESS|STUCK` 与测试断言一致。✓

**占位符扫描：** 无 TBD/TODO；各步均含实际代码。✓

---

## Execution Handoff

计划完成，存于 `docs/superpowers/plans/2026-08-09-阶段1-core-lib-cli.md`。两种执行方式：

1. **Subagent-Driven（推荐）** — 每任务派新 subagent，任务间复核，快速迭代。
2. **Inline Execution** — 本会话内批量执行，检查点复核。

选哪种？
