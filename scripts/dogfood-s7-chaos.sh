#!/usr/bin/env bash
# S7 多机 dogfood·混沌版：投票系统 + 12 个突发事件
# 事件清单：
#   ⚡1  需求变更（PM 中途改验收标准 + 升优先级）
#   ⚡2  线上事故紧急插队（P0 hotfix 打断进行中工作）
#   ⚡3  同单跨机并发写 → push 被拒 → rebase 真冲突 → 人拍板解
#   ⚡4  前端机器损毁 → re-clone 恢复
#   ⚡5  新成员中途加入 + work assign 移交
#   ⚡6  误取消 → 状态机重开边恢复
#   ⚡7  接口争议 → competing contracts → 人拍板取一
#   ⚡8  依赖被砍 → converge BLOCKED → 重定向依赖
#   ⚡9  伪造证据 → git adapter 拒绝
#   ⚡10 紧急回滚（contract live→deprecated + git revert）
#   ⚡11 虚报被抓（done 无证据 → converge warning → 补证据消警）
#   ⚡12 优先级重排（老板催移动端 P1→P0）
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${DOGFOOD_ROOT:-$(mktemp -d -t dogfood-chaos)}
BIN=$ROOT/bin/ousheng
CENTRAL=$ROOT/vote-central.git
PM=$ROOT/pm-ws; BE=$ROOT/backend-ws; FE=$ROOT/frontend-ws; QA=$ROOT/qa-ws; MO=$ROOT/mobile-ws
VAPI=$ROOT/vote-api; VUI=$ROOT/vote-ui

say()  { printf '\n\033[1;36m== %s ==\033[0m\n' "$*"; }
zap()  { printf '\n\033[1;31m⚡ 突发 %s\033[0m\n' "$*"; }
note() { printf '\033[0;33m  > %s\033[0m\n' "$*"; }
die()  { printf '\033[0;31mFATAL: %s\033[0m\n' "$*" >&2; exit 1; }
rev()  { (cd "$1" && $BIN work show "$2" | awk '/revision:/ {print $2}' | head -1); }

say "Stage 0: 构建 + 场地"
rm -rf "$ROOT"; mkdir -p "$ROOT/bin"
(cd "$REPO" && go build -o "$BIN" ./cmd/ousheng) || die build
git init -q --bare -b main "$CENTRAL"
echo OK

say "Stage 1: PM 初始化 + 发需求"
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
$BIN work create --id REQ-VOTE-001 --type requirement --title "投票创建与提交 API" \
  --system vote-api --assignee backend-agent --accountable pm --priority P0 --due 2026-09-18 \
  --description "验收：POST /v1/polls 建投票；POST /v1/polls/{id}/votes 每人一票；GET /v1/polls/{id}/results 出结果" \
  --actor pm
$BIN work create --id REQ-UI-001 --type requirement --title "投票页面" \
  --system vote-ui --assignee frontend-agent --accountable pm --depends-on REQ-VOTE-001 \
  --priority P1 --due 2026-09-20 \
  --description "验收：建投票表单、单选按钮提交、结果条形图" \
  --actor pm
git remote add origin "$CENTRAL"; git push -q -u origin main

say "Stage 2: 三台 agent 机 clone"
for m in backend-ws:backend-agent frontend-ws:frontend-agent qa-ws:qa-agent; do
  d=${m%%:*}; a=${m##*:}
  git clone -q "$CENTRAL" "$ROOT/$d"
  git -C "$ROOT/$d" config user.name "$a"; git -C "$ROOT/$d" config user.email "$a@vote.local"
  (cd "$ROOT/$d" && $BIN me "$a")
done

say "Stage 3: 后端开工 + 契约 proposed → PM agreed"
cd "$BE"; $BIN sync >/dev/null
$BIN work update REQ-VOTE-001 --status ready  --actor backend-agent
$BIN work update REQ-VOTE-001 --status doing --actor backend-agent
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
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	votes    map[string]string
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
	var counts map[string]int // 潜伏缺陷：nil map，空投票查询即 panic（500）
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
cd "$BE"; $BIN repo set vote-api "$VAPI"
R=$(rev "$BE" REQ-VOTE-001)
cat > /tmp/v1.yaml <<EOF
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
$BIN work update REQ-VOTE-001 --file /tmp/v1.yaml --expect "$R" --actor backend-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
R=$(rev "$PM" REQ-VOTE-001)
sed "s/status: proposed/status: agreed/; s/^revision: .*/revision: $R/" /tmp/v1.yaml > /tmp/v1b.yaml
$BIN work update REQ-VOTE-001 --file /tmp/v1b.yaml --expect "$R" --actor pm
git push -q

zap "2：线上事故——空投票查结果 500，紧急插队打断 v1"
cd "$QA"; $BIN sync >/dev/null
$BIN bug report --id BUG-INCIDENT-001 --title "线上事故：空投票查结果 500（紧急）" --system vote-api \
  --assignee backend-agent --detected-by qa-agent --accountable pm --actor qa-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
$BIN work update BUG-INCIDENT-001 --priority P0 --actor pm
git push -q
note "紧急度由人拍板：P0 插队"
cd "$BE"; $BIN sync >/dev/null
$BIN progress report REQ-VOTE-001 --value 0.4 --basis subtasks --actor backend-agent
$BIN work update BUG-INCIDENT-001 --status ready  --actor backend-agent
$BIN work update BUG-INCIDENT-001 --status doing --actor backend-agent
cd "$VAPI"
python3 - <<'EOF'
s = open("main.go").read()
s = s.replace("	var counts map[string]int // 潜伏缺陷：nil map，空投票查询即 panic（500）",
	"	counts := map[string]int{} // hotfix：修 nil map panic")
open("main.go", "w").write(s)
EOF
go build ./... || die "hotfix compile"
git add -A; git commit -qm "fix: 空投票查询 nil map panic（线上事故 hotfix）"
HOTHASH=$(git rev-parse --short HEAD)
cd "$BE"
$BIN work update BUG-INCIDENT-001 --status done --actor backend-agent
git push -q
note "后端仓促关单——没挂证据（事故场景常见偷懒）"

zap "11：虚报被抓——converge 警告 done 无证据"
cd "$PM"; $BIN sync >/dev/null
note "converge 输出（警告应可见）:"
$BIN converge
cd "$BE"; $BIN sync >/dev/null
$BIN evidence add BUG-INCIDENT-001 --type git_commit --source git --locator "$HOTHASH" --actor backend-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
note "补证后再 converge（警告应消失）:"
$BIN converge

say "Stage 4: 后端收尾 v1（契约 live → done → 证据）"
cd "$BE"; $BIN sync >/dev/null
V1HASH=$(git -C "$VAPI" rev-parse --short HEAD)
R=$(rev "$BE" REQ-VOTE-001)
sed "s/status: doing/status: done/; s/status: agreed/status: live/; s/^revision: .*/revision: $R/" /tmp/v1b.yaml > /tmp/v1c.yaml
$BIN work update REQ-VOTE-001 --file /tmp/v1c.yaml --expect "$R" --actor backend-agent
$BIN evidence add REQ-VOTE-001 --type git_commit --source git --locator "$V1HASH" --actor backend-agent
$BIN progress report REQ-VOTE-001 --value 1.0 --basis implementation-checklist --actor backend-agent
git push -q

zap "1+3：需求变更 × 同单跨机并发写——push 被拒、rebase 真冲突、人拍板"
cd "$FE"; $BIN sync >/dev/null
$BIN work update REQ-UI-001 --status ready  --actor frontend-agent
$BIN work update REQ-UI-001 --status doing --actor frontend-agent
note "前端同时自作主张降级 P2（分歧点）"
R=$(rev "$FE" REQ-UI-001)
cat > /tmp/ui-fe.yaml <<EOF
schema_version: 2
id: REQ-UI-001
type: requirement
title: 投票页面
system: vote-ui
assignee: frontend-agent
acting_role: frontend
accountable_human: pm
status: doing
revision: $R
priority: P2
due_on: "2026-09-20"
description: "验收：建投票表单、单选按钮提交、结果条形图"
EOF
# PM 需求变更（先 sync 再改：后端 Stage 4 已推进远端）
cd "$PM"; $BIN sync >/dev/null
cat > /tmp/ui-pm.yaml <<'EOF'
schema_version: 2
id: REQ-UI-001
type: requirement
title: 投票页面
system: vote-ui
assignee: frontend-agent
acting_role: frontend
accountable_human: pm
status: backlog
revision: 1
priority: P0
due_on: "2026-09-19"
description: "验收：建投票表单、单选按钮提交、结果条形图；新增：结果支持导出 CSV"
EOF
$BIN work update REQ-UI-001 --file /tmp/ui-pm.yaml --expect 1 --actor pm
git push -q
note "PM：需求变更（加 CSV 导出、P1→P0、due 提前）已 push"
# 前端带着旧认知提交并推送 → 被拒
cd "$FE"
$BIN work update REQ-UI-001 --file /tmp/ui-fe.yaml --expect "$R" --actor frontend-agent
if git push -q 2>/tmp/pusherr; then die "push 应被拒（远端已移动）"; fi
note "push 被拒（预期）: $(head -c 100 /tmp/pusherr | tr '\n' ' ')"
git pull --rebase -q 2>/dev/null || true
if git diff --name-only --diff-filter=U | grep -q .; then
  note "rebase 真冲突（同单同字段：PM 的 P0 vs 前端的 P2）"
  git rebase --abort
  note "处置 = CAS 哲学：不手 merge 历史，丢弃本地分歧 → sync 拿 PM 拍板后的最新事实 → 重放前端意图"
  git reset --hard -q origin/main
  $BIN sync >/dev/null
  $BIN work update REQ-UI-001 --status ready  --actor frontend-agent
  $BIN work update REQ-UI-001 --status doing --actor frontend-agent
fi
git push -q
cd "$QA"; $BIN sync >/dev/null
note "第三方机器验证合并结果:"
$BIN work show REQ-UI-001 | sed -n '1,14p'

zap "4：前端机器整台损毁 → re-clone 恢复"
rm -rf "$FE"
git clone -q "$CENTRAL" "$FE"
git -C "$FE" config user.name frontend-agent; git -C "$FE" config user.email frontend-agent@vote.local
cd "$FE"; $BIN me frontend-agent
$BIN repo set vote-ui "$VUI"
note "重克隆后 work list（状态全恢复）:"
$BIN work list --open | head -6
$BIN work update REQ-UI-001 --status done --actor frontend-agent
mkdir -p "$VUI"; cd "$VUI"
if [ ! -d .git ]; then
  git init -q -b main; git config user.name frontend-agent; git config user.email frontend-agent@vote.local
fi
cat > index.html <<'EOF'
<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>投票</title></head>
<body><h1>发起投票</h1>
<form id="f"><input id="q" placeholder="问题"><input id="o" placeholder="选项,逗号分隔"><button>创建</button></form>
<div id="poll"></div><h2>结果</h2><div id="r"></div>
<script>
  document.getElementById("f").onsubmit = async (e) => {
    e.preventDefault();
    const res = await fetch("/v1/polls", {method: "POST", headers: {"content-type": "application/json"},
      body: JSON.stringify({question: q.value, options: o.value.split(",")})});
    const {id} = await res.json(); poll.dataset.id = id;
    for (const t of ["A","B"]) { const b = document.createElement("button"); b.textContent = t;
      b.onclick = () => fetch(`/v1/polls/${id}/votes`, {method: "POST",
        headers: {"content-type": "application/json"}, body: JSON.stringify({voter: "me", option: t})});
      poll.appendChild(b); }
  };
</script></body></html>
EOF
git add -A; git commit -qm "feat: 投票页面（表单/投票/结果/CSV 导出入口）"
UIHASH=$(git rev-parse --short HEAD)
cd "$FE"
$BIN evidence add REQ-UI-001 --type git_commit --source git --locator "$UIHASH" --actor frontend-agent
$BIN progress report REQ-UI-001 --value 1.0 --basis implementation-checklist --actor frontend-agent
git push -q

zap "7：接口争议——后端提 offset 分页，PM 不买账（competing contracts）"
cd "$BE"; $BIN sync >/dev/null
$BIN work create --id REQ-VOTE-004 --type requirement --title "结果分页（offset 方案）" \
  --system vote-api --assignee backend-agent --accountable pm --priority P2 --due 2026-09-28 \
  --description "验收：GET results?page=N&size=M，offset 分页" --actor backend-agent
$BIN work update REQ-VOTE-004 --status ready --actor backend-agent
R=$(rev "$BE" REQ-VOTE-004)
cat > /tmp/p4.yaml <<EOF
schema_version: 2
id: REQ-VOTE-004
type: requirement
title: 结果分页（offset 方案）
system: vote-api
target_version: v1.1
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: doing
revision: $R
priority: P2
due_on: "2026-09-28"
description: "验收：GET results?page=N&size=M，offset 分页"
contract:
  kind: http
  status: proposed
  breaking: false
  interface:
    - method: GET
      path: /v1/polls/{id}/results
      behavior: "?page=N&size=M，返回 offset 窗口"
EOF
$BIN work update REQ-VOTE-004 --file /tmp/p4.yaml --expect "$R" --actor backend-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
note "PM 提出竞争方案：cursor 分页（两张 proposed 并挂，人拍板取其一）"
$BIN work create --id REQ-VOTE-005 --type requirement --title "结果分页（cursor 方案）" \
  --system vote-api --assignee backend-agent --accountable pm --priority P2 --due 2026-09-28 \
  --description "验收：GET results?cursor=X&limit=N，游标分页，深翻页不衰减" --actor pm
R=$(rev "$PM" REQ-VOTE-005)
cat > /tmp/p5.yaml <<EOF
schema_version: 2
id: REQ-VOTE-005
type: requirement
title: 结果分页（cursor 方案）
system: vote-api
target_version: v1.1
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: backlog
revision: $R
priority: P2
due_on: "2026-09-28"
description: "验收：GET results?cursor=X&limit=N，游标分页，深翻页不衰减"
contract:
  kind: http
  status: proposed
  breaking: false
  interface:
    - method: GET
      path: /v1/polls/{id}/results
      behavior: "?cursor=X&limit=N，返回 items+next_cursor，深翻页 O(1)"
EOF
$BIN work update REQ-VOTE-005 --file /tmp/p5.yaml --expect "$R" --actor pm
git push -q
note "拍板：cursor 方案胜出，offset 方案取消"
$BIN work update REQ-VOTE-004 --status cancelled --actor pm

zap "5：新成员中途加入 + 移交"
$BIN team add mobile-agent --name "移动端Agent" --type agent --responsible-human pm --role frontend --system vote-ui
$BIN work create --id REQ-UI-002 --type requirement --title "移动端结果列表" \
  --system vote-ui --assignee frontend-agent --accountable pm --depends-on REQ-VOTE-004 \
  --priority P1 --due 2026-09-26 \
  --description "验收：移动端 H5 结果列表，复用分页接口" --actor pm
$BIN work assign REQ-UI-002 --assignee mobile-agent --role frontend --actor pm
git push -q
git clone -q "$CENTRAL" "$MO"
git -C "$MO" config user.name mobile-agent; git -C "$MO" config user.email mobile-agent@vote.local
cd "$MO"; $BIN me mobile-agent
$BIN repo set vote-ui "$VUI"
note "新机器零基础设施上岗，context me:"
$BIN context me --actor mobile-agent | head -20

zap "6：测试误取消 → 状态机重开边恢复"
cd "$QA"; $BIN sync >/dev/null
$BIN work update REQ-UI-002 --status cancelled --actor qa-agent
git push -q
note "QA 手滑取消了 REQ-UI-002（想点的是别的单）"
cd "$PM"; $BIN sync >/dev/null
$BIN work update REQ-UI-002 --status backlog --actor pm
$BIN work update REQ-UI-002 --status ready   --actor pm
note "PM 重开：cancelled→backlog→ready，零数据丢失"

zap "8：依赖被砍——offset 单 cancelled，移动端依赖落空 → BLOCKED"
note "converge 输出（依赖目标 cancelled = 永不满足，应 BLOCKED）:"
$BIN converge || true
R=$(rev "$PM" REQ-UI-002)
cat > /tmp/ui2.yaml <<EOF
schema_version: 2
id: REQ-UI-002
type: requirement
title: 移动端结果列表
system: vote-ui
assignee: mobile-agent
acting_role: frontend
accountable_human: pm
status: ready
revision: $R
priority: P1
due_on: "2026-09-26"
depends_on:
  - REQ-VOTE-005
description: "验收：移动端 H5 结果列表，复用 cursor 分页接口"
EOF
$BIN work update REQ-UI-002 --file /tmp/ui2.yaml --expect "$R" --actor pm
note "PM 重定向依赖到 cursor 方案后再 converge:"
$BIN converge

zap "12：优先级重排——老板催移动端"
$BIN work update REQ-UI-002 --priority P0 --actor pm
git push -q

say "Stage 5: 后端实现 cursor 分页"
cd "$BE"; $BIN sync >/dev/null
R=$(rev "$BE" REQ-VOTE-005)
sed "s/status: proposed/status: agreed/; s/^revision: .*/revision: $R/" /tmp/p5.yaml > /tmp/p5b.yaml
$BIN work update REQ-VOTE-005 --file /tmp/p5b.yaml --expect "$R" --actor pm 2>/dev/null || \
$BIN work update REQ-VOTE-005 --file /tmp/p5b.yaml --expect "$R" --actor backend-agent
$BIN work update REQ-VOTE-005 --status ready  --actor backend-agent
$BIN work update REQ-VOTE-005 --status doing --actor backend-agent
cd "$VAPI"
python3 - <<'EOF'
s = open("main.go").read()
s = s.replace('''	json.NewEncoder(w).Encode(counts)
}''', '''	// cursor 分页：?cursor=&limit=，返回 items+next_cursor
	json.NewEncoder(w).Encode(map[string]any{"items": counts, "next_cursor": ""})
}''')
open("main.go", "w").write(s)
EOF
go build ./... || die "cursor compile"
git add -A; git commit -qm "feat: results cursor 分页"
V5HASH=$(git rev-parse --short HEAD)
cd "$BE"

zap "9：伪造证据——agent 想拿假 hash 蒙混"
if $BIN evidence add REQ-VOTE-005 --type git_commit --source git --locator deadbeef --actor backend-agent 2>/tmp/fakeev; then
  die "假证据竟然通过了"
else
  note "git adapter 拒绝（预期）: $(cat /tmp/fakeev | head -1)"
fi
$BIN evidence add REQ-VOTE-005 --type git_commit --source git --locator "$V5HASH" --actor backend-agent
R=$(rev "$BE" REQ-VOTE-005)
sed "s/status: backlog/status: done/; s/status: agreed/status: live/; s/^revision: .*/revision: $R/" /tmp/p5b.yaml > /tmp/p5c.yaml
$BIN work update REQ-VOTE-005 --file /tmp/p5c.yaml --expect "$R" --actor backend-agent
$BIN progress report REQ-VOTE-005 --value 1.0 --basis implementation-checklist --actor backend-agent
git push -q

say "Stage 6: 移动端收尾（新成员在真实压力下交付）"
cd "$MO"; $BIN sync >/dev/null
$BIN work update REQ-UI-002 --status doing --actor mobile-agent
cd "$VUI"
cat > mobile.html <<'EOF'
<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>结果</title></head>
<body><h1>移动端结果</h1><div id="list"></div>
<script>
  fetch(location.search.replace("?poll=", "/v1/polls/") + "/results?cursor=&limit=20")
    .then(r => r.json()).then(d => {
      for (const [k, v] of Object.entries(d.items || {})) {
        const p = document.createElement("div"); p.textContent = `${k}: ${v}`; list.appendChild(p);
      }
    });
</script></body></html>
EOF
git -C "$VUI" -c user.name=mobile-agent -c user.email=mobile-agent@vote.local add -A
git -C "$VUI" -c user.name=mobile-agent -c user.email=mobile-agent@vote.local commit -qm "feat: 移动端结果列表（cursor 分页）"
MOHASH=$(git rev-parse --short HEAD)
cd "$MO"
$BIN work update REQ-UI-002 --status done --actor mobile-agent
$BIN evidence add REQ-UI-002 --type git_commit --source git --locator "$MOHASH" --actor mobile-agent
$BIN progress report REQ-UI-002 --value 1.0 --basis implementation-checklist --actor mobile-agent
git push -q

zap "10：紧急回滚——消费方炸了，cursor 接口下线"
cd "$PM"; $BIN sync >/dev/null
cd "$VAPI"
git -C "$VAPI" -c user.name=pm -c user.email=pm@vote.local revert --no-edit HEAD >/dev/null
git -C "$VAPI" -c user.name=pm -c user.email=pm@vote.local commit -q --amend -m "revert: results cursor 分页（消费方不兼容，紧急下线）"
cd "$PM"
R=$(rev "$PM" REQ-VOTE-005)
cat > /tmp/p5d.yaml <<EOF
schema_version: 2
id: REQ-VOTE-005
type: requirement
title: 结果分页（cursor 方案）
system: vote-api
target_version: v1.1
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: done
revision: $R
priority: P2
due_on: "2026-09-28"
description: "验收：GET results?cursor=X&limit=N，游标分页，深翻页不衰减"
contract:
  kind: http
  status: deprecated
  breaking: false
  interface:
    - method: GET
      path: /v1/polls/{id}/results
      behavior: "?cursor=X&limit=N，返回 items+next_cursor，深翻页 O(1)"
evidence:
  - type: git_commit
    source: git
    locator: $V5HASH
progress:
  value: 1.0
  actor: backend-agent
  reported_at: "2026-09-14T18:00:00+08:00"
  basis: implementation-checklist
EOF
$BIN work update REQ-VOTE-005 --file /tmp/p5d.yaml --expect "$R" --actor pm
note "REQ-VOTE-004 的遗留 proposed 契约也要治理（closed+proposed 警告）:"
$BIN converge || true
R4=$(rev "$PM" REQ-VOTE-004)
sed "s/status: doing/status: cancelled/; s/status: proposed/status: deprecated/; s/^revision: .*/revision: $R4/" /tmp/p4.yaml > /tmp/p4b.yaml
$BIN work update REQ-VOTE-004 --file /tmp/p4b.yaml --expect "$R4" --actor pm
git push -q

say "Stage 7: 终局——五机独立 converge + 看板 + 突发清单"
for ws in "$PM" "$BE" "$FE" "$QA" "$MO"; do
  (cd "$ws" && $BIN sync >/dev/null 2>&1)
  printf '%-13s converge: %s\n' "$(basename "$ws")" "$( (cd "$ws" && $BIN converge) )"
done
echo
cd "$PM"
$BIN view kanban | head -30
echo
say "突发事件处置总账"
cat <<'SUMMARY'
  ⚡1  需求变更          → file 更新 + revision CAS，PM 单机落账
  ⚡2  紧急事故插队      → progress basis=interrupted-by-incident 留痕，hotfix 真实 commit
  ⚡3  同单跨机冲突      → push 拒绝 → pull --rebase 真冲突 → 人拍板（P0 胜出）→ revision 显式推进
  ⚡4  机器损毁          → re-clone + me + repo set，状态零丢失（一切在 git）
  ⚡5  新成员加入        → team add + work assign 移交 + 第五台机零基础设施上岗
  ⚡6  误取消            → cancelled→backlog 重开边，零数据丢失
  ⚡7  接口争议          → competing contracts（两张 proposed），人拍板取一
  ⚡8  依赖被砍          → 依赖目标 cancelled=永不满足 → converge BLOCKED 曝光 → PM 重定向 depends_on
  ⚡9  伪造证据          → git adapter 在真代码仓验证 locator，直接拒绝
  ⚡10 紧急回滚          → contract live→deprecated + 真实 git revert
  ⚡11 虚报被抓          → converge warning（done 无证据）→ 补证据 → 警告消失
  ⚡12 优先级重排        → quick flag P0，移动端 context me 可见
SUMMARY
say "CHAOS DOGFOOD 完成"
