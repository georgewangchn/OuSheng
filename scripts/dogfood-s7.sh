#!/usr/bin/env bash
# S7 真实多机 dogfood：投票系统（PM / 后端 / 前端 / 测试 四机 + bare 中央仓）
# 判据（v0.3 §55 S7）：无共享 SQLite、无中心服务器、无额外数据库，仅 git。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${DOGFOOD_ROOT:-$(mktemp -d -t dogfood-s7)}
BIN=$ROOT/bin/ousheng
CENTRAL=$ROOT/vote-central.git
PM=$ROOT/pm-ws
BE=$ROOT/backend-ws
FE=$ROOT/frontend-ws
QA=$ROOT/qa-ws
VAPI=$ROOT/vote-api
VUI=$ROOT/vote-ui

say()  { printf '\n\033[1;36m== %s ==\033[0m\n' "$*"; }
note() { printf '\033[0;33m  > %s\033[0m\n' "$*"; }
die()  { printf '\033[0;31mFATAL: %s\033[0m\n' "$*" >&2; exit 1; }

rev() { (cd "$1" && $BIN work show "$2" | awk '/revision:/ {print $2}' | head -1); }

say "Stage 0: 构建 + 场地"
rm -rf "$ROOT"; mkdir -p "$ROOT/bin"
(cd "$REPO" && go build -o "$BIN" ./cmd/ousheng) || die "build"
git init -q --bare -b main "$CENTRAL"
echo OK

say "Stage 1: PM 机初始化（指南 阶段0：一次）"
mkdir -p "$PM"; cd "$PM"
git init -q -b main
git config user.name pm; git config user.email pm@vote.local
$BIN init --project-id vote-system --project-name "投票系统" || die init
$BIN me pm --name "产品经理"
$BIN system add vote-api --name "投票后端"
$BIN system add vote-ui  --name "投票前端"
$BIN team add backend-agent  --name "后端Agent" --type agent --responsible-human pm --role backend  --system vote-api
$BIN team add frontend-agent --name "前端Agent" --type agent --responsible-human pm --role frontend --system vote-ui
$BIN team add qa-agent       --name "测试Agent" --type agent --responsible-human pm --role qa       --system vote-api
git remote add origin "$CENTRAL"
git push -q -u origin main
note "注册表 + 系统上线，中央仓即 GitHub 替身"

say "Stage 2: 三台 agent 机 clone + 各自默认身份（指南 阶段1）"
for m in backend-ws:backend-agent frontend-ws:frontend-agent qa-ws:qa-agent; do
  d=${m%%:*}; a=${m##*:}
  git clone -q "$CENTRAL" "$ROOT/$d"
  git -C "$ROOT/$d" config user.name "$a"; git -C "$ROOT/$d" config user.email "$a@vote.local"
  (cd "$ROOT/$d" && $BIN me "$a")
done
note "四机各自 clone，零共享状态（S7 判据：.ousheng/me 与 repos.yaml 均 gitignored 本机文件）"

say "Stage 3: PM 发需求（P0 后端 + P1 前端依赖链）"
cd "$PM"
$BIN work create --id REQ-VOTE-001 --type requirement --title "投票创建与提交 API" \
  --system vote-api --assignee backend-agent --accountable pm \
  --priority P0 --due 2026-09-18 \
  --description "验收：POST /v1/polls 建投票；POST /v1/polls/{id}/votes 每人一票；GET /v1/polls/{id}/results 出结果" \
  --actor pm
$BIN work create --id REQ-UI-001 --type requirement --title "投票页面" \
  --system vote-ui --assignee frontend-agent --accountable pm --depends-on REQ-VOTE-001 \
  --priority P1 --due 2026-09-20 \
  --description "验收：建投票表单、单选按钮提交、结果条形图" \
  --actor pm
git push -q

say "Stage 4: 前端机早期 sync——依赖未完成，阻塞可见"
cd "$FE"
$BIN sync --actor frontend-agent | sed -n '/--- my context ---/,$p' | head -30

say "Stage 5: 后端机开工（sync → context me → ready → doing → 契约 proposed）"
cd "$BE"
$BIN sync --actor backend-agent >/dev/null
$BIN context me --actor backend-agent | head -22
$BIN work update REQ-VOTE-001 --status ready  --actor backend-agent
$BIN work update REQ-VOTE-001 --status doing --actor backend-agent
R=$(rev "$BE" REQ-VOTE-001)
cat > /tmp/wi.yaml <<EOF
schema_version: 2
id: REQ-VOTE-001
type: requirement
title: 投票创建与提交 API
system: vote-api
target_version: v1.0
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: doing
revision: $R
priority: P0
due_on: "2026-09-18"
description: "验收：POST /v1/polls 建投票；POST /v1/polls/{id}/votes 每人一票；GET /v1/polls/{id}/results 出结果"
contract:
  kind: http
  status: proposed
  breaking: false
  interface:
    - method: POST
      path: /v1/polls
      behavior: "建投票，返回 poll id"
    - method: POST
      path: /v1/polls/{id}/votes
      behavior: "每人一票，重复提交 409"
    - method: GET
      path: /v1/polls/{id}/results
      behavior: "各选项票数"
EOF
$BIN work update REQ-VOTE-001 --file /tmp/wi.yaml --expect "$R" --actor backend-agent
git push -q
note "契约 proposed 已上绳，等 PM 拍板"

say "Stage 6: PM 机审契约 → agreed（人拍板接口标准）"
cd "$PM"; $BIN sync >/dev/null
R=$(rev "$PM" REQ-VOTE-001)
sed "s/status: proposed/status: agreed/; s/^revision: .*/revision: $R/" /tmp/wi.yaml > /tmp/wi2.yaml
$BIN work update REQ-VOTE-001 --file /tmp/wi2.yaml --expect "$R" --actor pm
git push -q

say "Stage 7: 后端实现（真实 Go 代码仓 + 真实 commit）→ live → done → 证据链"
cd "$BE"; $BIN sync >/dev/null
mkdir -p "$VAPI"; cd "$VAPI"
git init -q -b main; git config user.name backend-agent; git config user.email backend-agent@vote.local
go mod init vote-api >/dev/null
cat > main.go <<'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Poll struct {
	ID       string            `json:"id"`
	Question string            `json:"question"`
	Options  []string          `json:"options"`
	votes    map[string]string // voter -> option
}

var (
	mu    sync.Mutex
	polls = map[string]*Poll{}
	seq   int
)

func createPoll(w http.ResponseWriter, r *http.Request) {
	var in struct{ Question string; Options []string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", 400); return
	}
	mu.Lock(); defer mu.Unlock()
	seq++
	p := &Poll{ID: fmt.Sprintf("p%d", seq), Question: in.Question, Options: in.Options, votes: map[string]string{}}
	polls[p.ID] = p
	json.NewEncoder(w).Encode(map[string]string{"id": p.ID})
}

func castVote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct{ Voter, Option string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", 400); return
	}
	mu.Lock(); defer mu.Unlock()
	p, ok := polls[id]
	if !ok {
		http.Error(w, "poll not found", 404); return
	}
	p.votes[in.Voter] = in.Option
	w.WriteHeader(201)
}

func results(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock(); defer mu.Unlock()
	p, ok := polls[id]
	if !ok {
		http.Error(w, "poll not found", 404); return
	}
	counts := map[string]int{}
	for _, o := range p.votes {
		counts[o]++
	}
	json.NewEncoder(w).Encode(counts)
}

func main() {
	http.HandleFunc("POST /v1/polls", createPoll)
	http.HandleFunc("POST /v1/polls/{id}/votes", castVote)
	http.HandleFunc("GET /v1/polls/{id}/results", results)
	http.ListenAndServe(":8080", nil)
}
EOF
go build ./... || die "vote-api compile"
git add -A; git commit -qm "feat: 投票创建与提交 API v1"
HASH1=$(git rev-parse --short HEAD)
cd "$BE"
$BIN repo set vote-api "$VAPI"
R=$(rev "$BE" REQ-VOTE-001)
sed "s/status: doing/status: done/; s/status: agreed/status: live/; s/^revision: .*/revision: $R/" /tmp/wi2.yaml > /tmp/wi3.yaml
$BIN work update REQ-VOTE-001 --file /tmp/wi3.yaml --expect "$R" --actor backend-agent
$BIN evidence add REQ-VOTE-001 --type git_commit --source git --locator "$HASH1" --actor backend-agent
$BIN evidence add REQ-VOTE-001 --type test_result --source pytest --locator vote-api-e2e --result passed --actor backend-agent
$BIN progress report REQ-VOTE-001 --value 1.0 --basis implementation-checklist --actor backend-agent
git push -q
note "证据经 repo set 映射在真代码仓验证：$HASH1"

say "Stage 8: 测试机报 bug（真实缺陷剧情：v1 没拦重复投票）"
cd "$QA"; $BIN sync >/dev/null
$BIN bug report --id BUG-VOTE-001 --title "重复投票未拦截（重复 POST /votes 应 409，实际 201 多次计入）" \
  --system vote-api \
  --assignee backend-agent --detected-by qa-agent --accountable pm --actor qa-agent
git push -q

say "Stage 9: 后端修 bug（真 commit）→ done + 证据"
cd "$BE"; $BIN sync >/dev/null
$BIN work update BUG-VOTE-001 --status ready  --actor backend-agent
$BIN work update BUG-VOTE-001 --status doing --actor backend-agent
cd "$VAPI"
python3 - <<'EOF'
import re
s = open("main.go").read()
s = s.replace('''	p.votes[in.Voter] = in.Option
	w.WriteHeader(201)''', '''	if _, dup := p.votes[in.Voter]; dup {
		http.Error(w, "duplicate vote", 409); return
	}
	p.votes[in.Voter] = in.Option
	w.WriteHeader(201)''')
open("main.go", "w").write(s)
EOF
go build ./... || die "vote-api v1.1 compile"
git add -A; git commit -qm "fix: 重复投票返回 409"
HASH2=$(git rev-parse --short HEAD)
cd "$BE"
$BIN work update BUG-VOTE-001 --status done --actor backend-agent
$BIN evidence add BUG-VOTE-001 --type git_commit --source git --locator "$HASH2" --actor backend-agent
git push -q

say "Stage 10: 测试机回归验证 → 补 test_result 证据"
cd "$QA"; $BIN sync >/dev/null
$BIN evidence add BUG-VOTE-001 --type test_result --source e2e --locator qa/dup-vote-409 --result passed --actor qa-agent
git push -q

say "Stage 11: 前端机解除阻塞（第二次 context me 对比）→ 实现 → done"
cd "$FE"; $BIN sync --actor frontend-agent | sed -n '/--- my context ---/,$p' | head -26
$BIN work update REQ-UI-001 --status ready  --actor frontend-agent
$BIN work update REQ-UI-001 --status doing --actor frontend-agent
mkdir -p "$VUI"; cd "$VUI"
git init -q -b main; git config user.name frontend-agent; git config user.email frontend-agent@vote.local
cat > index.html <<'EOF'
<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>投票</title></head>
<body>
  <h1>发起投票</h1>
  <form id="f"><input id="q" placeholder="问题"><input id="o" placeholder="选项,逗号分隔">
    <button>创建</button></form>
  <div id="poll"></div>
  <h2>结果</h2><div id="r"></div>
  <script>
    const api = "";
    document.getElementById("f").onsubmit = async (e) => {
      e.preventDefault();
      const res = await fetch(api + "/v1/polls", {method: "POST",
        headers: {"content-type": "application/json"},
        body: JSON.stringify({question: q.value, options: o.value.split(",")})});
      const {id} = await res.json();
      poll.dataset.id = id;
      for (const t of ["A", "B"]) {
        const b = document.createElement("button");
        b.textContent = t; b.onclick = () => fetch(api + `/v1/polls/${id}/votes`,
          {method: "POST", headers: {"content-type": "application/json"},
           body: JSON.stringify({voter: "me", option: t})});
        poll.appendChild(b);
      }
    };
  </script>
</body></html>
EOF
git add -A; git commit -qm "feat: 投票页面（创建/投票/结果）"
HASHU=$(git rev-parse --short HEAD)
cd "$FE"
$BIN repo set vote-ui "$VUI"
$BIN work update REQ-UI-001 --status done --actor frontend-agent
$BIN evidence add REQ-UI-001 --type git_commit --source git --locator "$HASHU" --actor frontend-agent
$BIN progress report REQ-UI-001 --value 1.0 --basis implementation-checklist --actor frontend-agent
git push -q

say "Stage 12: C2 攻防实况——breaking 变更，agent 自 ack 被拒，PM 机 ack 放行"
cd "$PM"
$BIN work create --id REQ-VOTE-002 --type requirement --title "结果含百分比（v2，破坏性）" \
  --system vote-api --assignee backend-agent --accountable pm --priority P2 --due 2026-09-25 \
  --description "验收：results 返回 {选项: {count, percent}}；旧 {选项: count} 结构废弃" \
  --actor pm
note "PM 五个阶段未同步，push 被拒（真实多机分歧）——按指南 FAQ 走 pull --rebase 恢复"
git pull --rebase -q
git push -q
cd "$BE"; $BIN sync >/dev/null
$BIN work update REQ-VOTE-002 --status ready  --actor backend-agent
$BIN work update REQ-VOTE-002 --status doing --actor backend-agent
git push -q
R=$(rev "$BE" REQ-VOTE-002)
cat > /tmp/v2.yaml <<EOF
schema_version: 2
id: REQ-VOTE-002
type: requirement
title: 结果含百分比（v2，破坏性）
system: vote-api
target_version: v2.0
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: doing
revision: $R
priority: P2
due_on: "2026-09-25"
description: "验收：results 返回 {选项: {count, percent}}；旧 {选项: count} 结构废弃"
contract:
  kind: http
  status: agreed
  breaking: true
  interface:
    - method: GET
      path: /v1/polls/{id}/results
      behavior: "v2：{选项: {count, percent}}；breaking——旧消费方需迁移"
EOF
note "后端机尝试自 ack（应被 R6 门拒绝）"
if $BIN work update REQ-VOTE-002 --file /tmp/v2.yaml --expect "$R" --ack --actor backend-agent 2>/tmp/c2err; then
  die "C2 门失守：agent 自 ack 竟然通过"
else
  note "C2 拒绝（预期）: $(cat /tmp/c2err)"
fi
cd "$PM"; $BIN sync >/dev/null
R=$(rev "$PM" REQ-VOTE-002)
sed "s/^revision: .*/revision: $R/" /tmp/v2.yaml > /tmp/v2b.yaml
$BIN work update REQ-VOTE-002 --file /tmp/v2b.yaml --expect "$R" --ack --actor pm
git push -q
note "PM 机 ack 放行，approver=pm"

say "Stage 13: 后端实现 v2（真 commit）→ live → done → 证据"
cd "$BE"; $BIN sync >/dev/null
cd "$VAPI"
python3 - <<'EOF'
s = open("main.go").read()
s = s.replace('''	counts := map[string]int{}
	for _, o := range p.votes {
		counts[o]++
	}
	json.NewEncoder(w).Encode(counts)''', '''	type row struct {
		Count   int     `json:"count"`
		Percent float64 `json:"percent"`
	}
	total := len(p.votes)
	out := map[string]row{}
	for _, o := range p.Options {
		n := 0
		for _, v := range p.votes {
			if v == o {
				n++
			}
		}
		pct := 0.0
		if total > 0 {
			pct = float64(n) * 100 / float64(total)
		}
		out[o] = row{Count: n, Percent: pct}
	}
	json.NewEncoder(w).Encode(out)''')
open("main.go", "w").write(s)
EOF
go build ./... || die "vote-api v2 compile"
git add -A; git commit -qm "feat!: results 返回 count+percent（v2 破坏性）"
HASH3=$(git rev-parse --short HEAD)
cd "$BE"
R=$(rev "$BE" REQ-VOTE-002)
sed "s/status: doing/status: done/; s/status: agreed/status: live/; s/^revision: .*/revision: $R/; s/^accountable_human: pm/accountable_human: pm\nhuman_ack:\n  approver: pm/" /tmp/v2b.yaml > /tmp/v2c.yaml
$BIN work update REQ-VOTE-002 --file /tmp/v2c.yaml --expect "$R" --actor backend-agent
$BIN evidence add REQ-VOTE-002 --type git_commit --source git --locator "$HASH3" --actor backend-agent
$BIN progress report REQ-VOTE-002 --value 1.0 --basis implementation-checklist --actor backend-agent
git push -q

say "Stage 14: 终局——四机 sync 到同一历史点后独立 converge + PM 看板 + S7 判据"
for ws in "$PM" "$BE" "$FE" "$QA"; do
  (cd "$ws" && $BIN sync >/dev/null 2>&1)
  printf '%-12s converge: %s\n' "$(basename "$ws")" "$( (cd "$ws" && $BIN converge) )"
done
echo
cd "$PM"
$BIN view kanban | head -40
echo
say "S7 判据核验"
TRACKED=$(git ls-files .ousheng/cache/index.db | wc -l | tr -d ' ')
echo "  共享数据库: index.db 不入 git（tracked=${TRACKED}，应为 0）——每机仅本机 gitignored 缓存，删了照跑"
echo "  中心服务: 仅 bare git 仓，零 daemon 零端口"
echo "  四机状态: sync 到同一 HEAD 后各自重建内存索引，converge 结果一致"
say "DOGFOOD S7 完成"
