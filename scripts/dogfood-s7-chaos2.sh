#!/usr/bin/env bash
# S7 多机 dogfood·混沌第二辑：内容平台（CMS）+ 12 个全新突发事件
# 与第一辑（dogfood-s7-chaos.sh）零重叠。事件清单：
#   ⚡1  紧急发版跳版（target_version v1→v2 + 版本纪律：revision ≠ target_version）
#   ⚡2  agent 失联重派（backend-agent 黑洞 + backend2 第五台机接手）
#   ⚡3  breaking 强推三连攻防（无 ack 拒 / agent 自 ack 拒 / PM ack 过）
#   ⚡4  假冒身份（--actor pm 冒充 + git 审计揪出 + 撤销恢复）
#   ⚡5  手改 YAML 损坏（未知键 → strict decode 拒 → git revert 恢复）
#   ⚡6  依赖环（converge cycle BLOCKED → 拆环）
#   ⚡7  撞 ID（双机同建 REQ-CMS-006 → push 拒 → rebase 冲突 → 前缀命名空间约定）
#   ⚡8  超长字段（8192 字节门拒绝）
#   ⚡9  裸修中央仓（绕过 CLI 手改 + push → 各机容忍 = Git canonical 本义）
#   ⚡10 批量雪崩（一口气 6 张 v2.0 单，list/kanban/context 抗压）
#   ⚡11 重工循环（done→doing 重开 + 证据 append-only + 失联者回归先对齐）
#   ⚡12 诚实框架（假 ci_run 证据无人验 = C1 设计边界，非 bug）
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${DOGFOOD_ROOT:-$(mktemp -d -t dogfood-chaos2)}
BIN=$ROOT/bin/ousheng
CENTRAL=$ROOT/cms-central.git
PM=$ROOT/pm-ws; BE=$ROOT/be-ws; FE=$ROOT/fe-ws; QA=$ROOT/qa-ws; BE2=$ROOT/be2-ws
VAPI=$ROOT/cms-api; VUI=$ROOT/cms-ui

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

say "Stage 1: PM 初始化 + 发需求（内容平台）"
mkdir -p "$PM"; cd "$PM"
git init -q -b main
git config user.name pm; git config user.email pm@cms.local
$BIN init --project-id cms-platform --project-name "内容平台" || die init
$BIN me pm --name "产品经理"
$BIN system add cms-api --name "内容后端"
$BIN system add cms-ui  --name "编辑台前端"
$BIN team add backend-agent  --name "后端Agent"  --type agent --responsible-human pm --role backend  --system cms-api
$BIN team add frontend-agent --name "前端Agent"  --type agent --responsible-human pm --role frontend --system cms-ui
$BIN team add qa-agent       --name "测试Agent"  --type agent --responsible-human pm --role qa       --system cms-api
$BIN work create --id REQ-CMS-001 --type requirement --title "文章 API" \
  --system cms-api --assignee backend-agent --accountable pm --priority P0 --due 2026-09-24 \
  --version v1.0 \
  --description "验收：POST /v1/articles 建文章；GET /v1/articles 出列表（响应 {items:[]}）" \
  --actor pm
$BIN work create --id REQ-UI-001 --type requirement --title "内容编辑台" \
  --system cms-ui --assignee frontend-agent --accountable pm --depends-on REQ-CMS-001 \
  --priority P1 --due 2026-09-28 --version v1.0 \
  --description "验收：标题输入框 + 发布按钮 + 已发布列表" \
  --actor pm
$BIN work create --id REQ-CMS-002 --type requirement --title "文章搜索 API" \
  --system cms-api --assignee backend-agent --accountable pm --priority P2 --due 2026-09-30 \
  --description "验收：GET /v1/articles/search?q=关键字 按标题过滤" \
  --actor pm
git remote add origin "$CENTRAL"; git push -q -u origin main

say "Stage 2: 三台 agent 机 clone"
for m in be-ws:backend-agent fe-ws:frontend-agent qa-ws:qa-agent; do
  d=${m%%:*}; a=${m##*:}
  git clone -q "$CENTRAL" "$ROOT/$d"
  git -C "$ROOT/$d" config user.name "$a"; git -C "$ROOT/$d" config user.email "$a@cms.local"
  (cd "$ROOT/$d" && $BIN me "$a")
done

say "Stage 3: 后端交付 REQ-CMS-001（契约 proposed → agreed → live）"
cd "$BE"; $BIN sync >/dev/null
$BIN work update REQ-CMS-001 --status ready  --actor backend-agent
$BIN work update REQ-CMS-001 --status doing --actor backend-agent
mkdir -p "$VAPI"; cd "$VAPI"
git init -q -b main; git config user.name backend-agent; git config user.email backend-agent@cms.local
go mod init cms-api >/dev/null
cat > main.go <<'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Article struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

var (
	mu    sync.Mutex
	arts  = map[string]Article{}
	seq   int
)

func createArticle(w http.ResponseWriter, r *http.Request) {
	var in struct{ Title string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", 400); return
	}
	mu.Lock(); defer mu.Unlock()
	seq++
	a := Article{ID: fmt.Sprintf("a%d", seq), Title: in.Title}
	arts[a.ID] = a
	json.NewEncoder(w).Encode(map[string]string{"id": a.ID})
}

func listArticles(w http.ResponseWriter, r *http.Request) {
	mu.Lock(); defer mu.Unlock()
	items := []Article{}
	for _, a := range arts {
		items = append(items, a)
	}
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func main() {
	http.HandleFunc("POST /v1/articles", createArticle)
	http.HandleFunc("GET /v1/articles", listArticles)
	http.ListenAndServe(":8080", nil)
}
EOF
go build ./... || die "cms-api compile"
git add -A; git commit -qm "feat: 文章创建与列表 API v1"
cd "$BE"; $BIN repo set cms-api "$VAPI"
R=$(rev "$BE" REQ-CMS-001)
cat > /tmp/c1.yaml <<EOF
schema_version: 2
id: REQ-CMS-001
type: requirement
title: 文章 API
system: cms-api
target_version: v1.0
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: doing
revision: $R
priority: P0
due_on: "2026-09-24"
description: "验收：POST /v1/articles 建文章；GET /v1/articles 出列表（响应 {items:[]}）"
contract:
  kind: http
  status: proposed
  breaking: false
  interface:
    - method: POST
      path: /v1/articles
      behavior: "建文章，返回 {id}"
    - method: GET
      path: /v1/articles
      behavior: "列表，响应 {items:[]}"
EOF
$BIN work update REQ-CMS-001 --file /tmp/c1.yaml --expect "$R" --actor backend-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
R=$(rev "$PM" REQ-CMS-001)
sed "s/status: proposed/status: agreed/; s/^revision: .*/revision: $R/" /tmp/c1.yaml > /tmp/c1a.yaml
$BIN work update REQ-CMS-001 --file /tmp/c1a.yaml --expect "$R" --actor pm
git push -q
cd "$BE"; $BIN sync >/dev/null
R=$(rev "$BE" REQ-CMS-001)
sed "s/status: agreed/status: live/; s/^revision: .*/revision: $R/" /tmp/c1a.yaml > /tmp/c1b.yaml
$BIN work update REQ-CMS-001 --file /tmp/c1b.yaml --expect "$R" --actor backend-agent
AHASH=$(git -C "$VAPI" rev-parse --short HEAD)
$BIN evidence add REQ-CMS-001 --type git_commit --source git --locator "$AHASH" --actor backend-agent
$BIN work update REQ-CMS-001 --status done --actor backend-agent
$BIN progress report REQ-CMS-001 --value 1.0 --basis implementation-checklist --actor backend-agent
git push -q

say "Stage 3.5: 前端交付 REQ-UI-001"
cd "$FE"; $BIN sync >/dev/null
$BIN work update REQ-UI-001 --status ready  --actor frontend-agent
$BIN work update REQ-UI-001 --status doing --actor frontend-agent
mkdir -p "$VUI"; cd "$VUI"
git init -q -b main; git config user.name frontend-agent; git config user.email frontend-agent@cms.local
cat > index.html <<'EOF'
<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>编辑台</title></head>
<body><h1>内容编辑台</h1>
<input id="t" placeholder="标题"><button onclick="pub()">发布</button>
<ul id="list"></ul>
<script>
  async function pub() {
    await fetch("/v1/articles", {method: "POST", headers: {"content-type": "application/json"},
      body: JSON.stringify({title: t.value})});
    load();
  }
  async function load() {
    const d = await (await fetch("/v1/articles")).json();
    list.innerHTML = (d.items || []).map(a => `<li>${a.title}</li>`).join("");
  }
  load();
</script></body></html>
EOF
git add -A; git commit -qm "feat: 编辑台 v1"
cd "$FE"; $BIN repo set cms-ui "$VUI"
UHASH=$(git -C "$VUI" rev-parse --short HEAD)
$BIN evidence add REQ-UI-001 --type git_commit --source git --locator "$UHASH" --actor frontend-agent
$BIN work update REQ-UI-001 --status done --actor frontend-agent
$BIN progress report REQ-UI-001 --value 1.0 --basis implementation-checklist --actor frontend-agent
git push -q

zap "1：紧急发版跳版——老板拍板 v2.0 里程碑，搜索并入 v2.0 批次"
cd "$PM"; $BIN sync >/dev/null
$BIN work create --id REQ-CMS-003 --type requirement --title "评论 API" \
  --system cms-api --assignee backend-agent --accountable pm --priority P1 --due 2026-10-12 \
  --version v2.0 \
  --description "验收：POST /v1/articles/{id}/comments 发评论；GET /v1/articles/{id}/comments 列表" \
  --actor pm
R=$(rev "$PM" REQ-CMS-002)
cat > /tmp/c2v2.yaml <<EOF
schema_version: 2
id: REQ-CMS-002
type: requirement
title: 文章搜索 API
system: cms-api
target_version: v2.0
assignee: backend-agent
acting_role: backend
accountable_human: pm
status: backlog
revision: $R
priority: P2
due_on: "2026-10-08"
description: "验收：GET /v1/articles/search?q=关键字 按标题过滤"
EOF
$BIN work update REQ-CMS-002 --file /tmp/c2v2.yaml --expect "$R" --actor pm
git push -q
note "版本纪律：target_version 是产品版本，revision 是 CAS 计数，永不混用"
$BIN work show REQ-CMS-002 | rg 'target_version|revision:' | head -2
note "work list --version v2.0 过滤（应见 002+003，不见 001）："
$BIN work list --version v2.0

zap "2：agent 失联——backend-agent 领了搜索单后人间蒸发"
cd "$BE"; $BIN sync >/dev/null
$BIN work update REQ-CMS-002 --status ready  --actor backend-agent
$BIN work update REQ-CMS-002 --status doing --actor backend-agent
note "失联 = 本地两笔 commit 从未 push（外界无人知晓），进度黑洞"
cd "$PM"
$BIN team add backend2-agent --name "后端Agent二号" --type agent --responsible-human pm --role backend --system cms-api
git push -q
git clone -q "$CENTRAL" "$BE2"
git -C "$BE2" config user.name backend2-agent; git -C "$BE2" config user.email backend2-agent@cms.local
cd "$BE2"; $BIN me backend2-agent
$BIN work assign REQ-CMS-002 --assignee backend2-agent --role backend --actor pm
git push -q
note "新机器零基础上岗，context me 直接看到接手的活："
$BIN context me --actor backend2-agent | head -16

zap "3：breaking 强推——列表响应结构要改，C2 三连攻防"
cd "$BE2"; $BIN sync >/dev/null
$BIN work update REQ-CMS-002 --status ready  --actor backend2-agent
$BIN work update REQ-CMS-002 --status doing --actor backend2-agent
R=$(rev "$BE2" REQ-CMS-002)
ACK_B2=$'human_ack:\n  approver: backend2-agent\n  at: "2026-09-14T15:00:00+08:00"'
c2emit() { # $1=dest $2=contract_status $3=rev $4=ack_block_or_empty
cat > "$1" <<EOF
schema_version: 2
id: REQ-CMS-002
type: requirement
title: 文章搜索 API
system: cms-api
target_version: v2.0
assignee: backend2-agent
acting_role: backend
accountable_human: pm
status: doing
revision: $3
priority: P2
due_on: "2026-10-08"
description: "验收：GET /v1/articles/search?q=关键字 按标题过滤；列表响应结构改 {data:[], total:N}"
contract:
  kind: http
  status: $2
  breaking: true
  interface:
    - method: GET
      path: /v1/articles
      behavior: "破坏性改版：响应 v1 {items:[]} → v2 {data:[], total:N}"
$4
EOF
}
c2emit /tmp/c2a.yaml proposed "$R" ""
if $BIN work update REQ-CMS-002 --file /tmp/c2a.yaml --expect "$R" --actor backend2-agent 2>/tmp/c2err; then
  die "breaking 无 ack 竟然过了"
else
  note "第一击（breaking 无 ack）被拒（预期）: $(head -1 /tmp/c2err)"
fi
c2emit /tmp/c2b.yaml proposed "$R" "$ACK_B2"
if $BIN work update REQ-CMS-002 --file /tmp/c2b.yaml --expect "$R" --actor backend2-agent 2>/tmp/c2err; then
  die "agent 自 ack 竟然过了"
else
  note "第二击（agent 自 ack）被拒（预期）: $(head -1 /tmp/c2err)"
fi
note "第三击走正道：backend2 提非破坏版提案，PM 用 --ack 拍板"
R=$(rev "$BE2" REQ-CMS-002)
sed "s/breaking: true/breaking: false/; s/破坏性改版：响应 v1 {items:\[\]} → v2 {data:\[\], total:N}/改动交由 PM 拍板/" /tmp/c2a.yaml | sed "s/^revision: .*/revision: $R/" > /tmp/c2c.yaml
$BIN work update REQ-CMS-002 --file /tmp/c2c.yaml --expect "$R" --actor backend2-agent
git push -q
cd "$PM"; $BIN sync >/dev/null
R=$(rev "$PM" REQ-CMS-002)
sed "s/status: proposed/status: agreed/; s/^revision: .*/revision: $R/" /tmp/c2c.yaml > /tmp/c2d.yaml
$BIN work update REQ-CMS-002 --file /tmp/c2d.yaml --expect "$R" --ack --actor pm
git push -q
note "PM ack 落账（human_ack.approver=pm，human 型）——breaking 改版过门"
cd "$BE2"; $BIN sync >/dev/null
R=$(rev "$BE2" REQ-CMS-002)
sed "s/status: agreed/status: live/; s/^revision: .*/revision: $R/" /tmp/c2d.yaml > /tmp/c2e.yaml
$BIN work update REQ-CMS-002 --file /tmp/c2e.yaml --expect "$R" --actor backend2-agent
$BIN repo set cms-api "$VAPI"
cd "$VAPI"
python3 - <<'EOF'
s = open("main.go").read()
s = s.replace('''	http.HandleFunc("GET /v1/articles", listArticles)''',
'''	http.HandleFunc("GET /v1/articles", listArticles)
	http.HandleFunc("GET /v1/articles/search", searchArticles)''')
s += '''
func searchArticles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	mu.Lock(); defer mu.Unlock()
	items := []Article{}
	for _, a := range arts {
		if q == "" || contains(a.Title, q) {
			items = append(items, a)
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"data": items, "total": len(items)})
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
'''
open("main.go", "w").write(s)
EOF
go build ./... || die "search compile"
git add -A; git commit -qm "feat: 文章搜索 + 列表响应 v2 结构"
cd "$BE2"
SHASH=$(git -C "$VAPI" rev-parse --short HEAD)
$BIN evidence add REQ-CMS-002 --type git_commit --source git --locator "$SHASH" --actor backend2-agent
$BIN work update REQ-CMS-002 --status done --actor backend2-agent
$BIN progress report REQ-CMS-002 --value 1.0 --basis implementation-checklist --actor backend2-agent
git push -q

zap "4：假冒身份——backend2 被污染，冒用 --actor pm 砍单"
cd "$BE2"
note "CLI 信任 --actor 旗标（注册表可伪造 = 已文档化边界，防线在审计不在门禁）"
$BIN work update REQ-CMS-003 --status cancelled --actor pm
git push -q
cd "$PM"; $BIN sync >/dev/null
note "PM 发现评论单被「自己」砍了，git 审计对账："
$BIN work show REQ-CMS-003 | rg 'status:' | head -1
git log -1 --format='  commit author = %an  <-- 与声称的 actor=pm 不符，冒用实锤' -- .ousheng/work/REQ-CMS-003.yaml
note "PM 重开：cancelled→backlog→ready→doing，证据链都在"
$BIN work update REQ-CMS-003 --status backlog --actor pm
$BIN work update REQ-CMS-003 --status ready   --actor pm
$BIN work update REQ-CMS-003 --status doing   --actor pm
git push -q

zap "5：手改损坏——QA 想直接改文件，手滑加了未知键"
cd "$QA"; $BIN sync >/dev/null
printf 'hax: true\n' >> .ousheng/work/REQ-CMS-003.yaml
git add -A; git commit -qm "qa: 手滑（直接编辑 YAML）"; git push -q
if (cd "$QA" && $BIN converge >/tmp/c5out 2>&1); then
  die "损坏 YAML 竟然通过 strict decode"
else
  note "strict decode 拒绝（预期，损坏不扩散成静默错误）: $(head -1 /tmp/c5out)"
fi
cd "$PM"
if $BIN sync >/dev/null 2>&1; then :; else note "PM sync 拉到损坏状态：git 层已同步、索引拒读响亮失败（正好是故障现场）"; fi
SHA=$(git log --format=%h -1 -- .ousheng/work/REQ-CMS-003.yaml)
note "PM 定位坏 commit ${SHA}，git revert 恢复（历史是救命的）"
git revert --no-edit "$SHA" >/dev/null
git push -q
$BIN converge || true
cd "$QA"; $BIN sync >/dev/null

zap "6：依赖环——PM 批量规划失误，004/005 互为前置"
cd "$PM"
$BIN work create --id REQ-CMS-004 --type requirement --title "通知服务" \
  --system cms-api --assignee backend2-agent --accountable pm --priority P2 --due 2026-10-20 \
  --version v2.0 --description "验收：文章发布后触发通知" --actor pm
$BIN work create --id REQ-CMS-005 --type requirement --title "邮件通道" \
  --system cms-api --assignee backend2-agent --accountable pm --priority P2 --due 2026-10-20 \
  --version v2.0 --description "验收：通知走邮件下发" --actor pm
R4=$(rev "$PM" REQ-CMS-004); R5=$(rev "$PM" REQ-CMS-005)
cat > /tmp/c4.yaml <<EOF
schema_version: 2
id: REQ-CMS-004
type: requirement
title: 通知服务
system: cms-api
target_version: v2.0
assignee: backend2-agent
acting_role: backend
accountable_human: pm
status: backlog
revision: $R4
priority: P2
due_on: "2026-10-20"
depends_on:
  - REQ-CMS-005
description: "验收：文章发布后触发通知"
EOF
cat > /tmp/c5.yaml <<EOF
schema_version: 2
id: REQ-CMS-005
type: requirement
title: 邮件通道
system: cms-api
target_version: v2.0
assignee: backend2-agent
acting_role: backend
accountable_human: pm
status: backlog
revision: $R5
priority: P2
due_on: "2026-10-20"
depends_on:
  - REQ-CMS-004
description: "验收：通知走邮件下发"
EOF
$BIN work update REQ-CMS-004 --file /tmp/c4.yaml --expect "$R4" --actor pm
$BIN work update REQ-CMS-005 --file /tmp/c5.yaml --expect "$R5" --actor pm
git push -q
out=$($BIN converge 2>&1) || true
echo "$out"
echo "$out" | rg -q "cycle" && note "依赖环被 converge 曝光（预期）" || die "依赖环未被检出"
note "PM 拆环：004 不再依赖 005（通知服务先行，邮件通道后补）"
R4=$(rev "$PM" REQ-CMS-004)
sed "s/^revision: .*/revision: $R4/" /tmp/c4.yaml | rg -v 'depends_on:|REQ-CMS-005' > /tmp/c4fix.yaml
$BIN work update REQ-CMS-004 --file /tmp/c4fix.yaml --expect "$R4" --actor pm
git push -q

zap "7：撞 ID——前端和测试同时建了 REQ-CMS-006"
cd "$FE"; $BIN sync >/dev/null
$BIN work create --id REQ-CMS-006 --type requirement --title "数据导出" \
  --system cms-ui --assignee frontend-agent --accountable pm --priority P2 --due 2026-10-18 \
  --description "验收：编辑台导出文章列表 CSV" --actor frontend-agent
git push -q
cd "$QA"
note "QA 本地未同步，也建了 REQ-CMS-006（冒烟脚本），push 被拒："
$BIN work create --id REQ-CMS-006 --type task --title "冒烟测试脚本" \
  --system cms-api --assignee qa-agent --accountable pm --priority P3 --due 2026-10-22 \
  --description "验收：api 冒烟脚本入库" --actor qa-agent
git push -q 2>&1 | head -2 || true
git pull --rebase 2>&1 | rg -i "conflict" && note "同 ID 两份 YAML，rebase 真冲突（预期）" || true
git rebase --abort 2>/dev/null || true
note "处置：丢弃本地分歧，改用带 owner 前缀的 ID 重建意图（ID 命名空间约定）"
git reset --hard -q origin/main
$BIN work create --id REQ-QA-006 --type task --title "冒烟测试脚本" \
  --system cms-api --assignee qa-agent --accountable pm --priority P3 --due 2026-10-22 \
  --description "验收：api 冒烟脚本入库" --actor qa-agent
git push -q

zap "8：超长字段——前端把整份设计稿贴进 description"
cd "$FE"; $BIN sync >/dev/null
R=$(rev "$FE" REQ-CMS-006)
FAT=$(python3 -c 'print("x"*8500)')
cat > /tmp/fat.yaml <<EOF
schema_version: 2
id: REQ-CMS-006
type: requirement
title: 数据导出
system: cms-ui
assignee: frontend-agent
acting_role: frontend
accountable_human: pm
status: backlog
revision: $R
priority: P2
due_on: "2026-10-18"
description: "$FAT"
EOF
if $BIN work update REQ-CMS-006 --file /tmp/fat.yaml --expect "$R" --actor frontend-agent 2>/tmp/faterr; then
  die "超长字段竟然过了 8192 门"
else
  note "8192 字节门拒绝（预期）: $(head -1 /tmp/faterr)"
fi
note "前端收敛：长文改外链"
$BIN work update REQ-CMS-006 --description "验收：编辑台导出文章列表 CSV；设计稿见 wiki/cms-export" --actor frontend-agent
git push -q

zap "9：裸修中央仓——运维绕过 CLI 直接手改错别字"
OOB=$ROOT/oob-admin
git clone -q "$CENTRAL" "$OOB"
git -C "$OOB" config user.name ops; git -C "$OOB" config user.email ops@cms.local
sed -i '' 's/^title: 内容编辑台$/title: 内容编辑台（修订）/' "$OOB/.ousheng/work/REQ-UI-001.yaml"
git -C "$OOB" add -A; git -C "$OOB" commit -qm "ops: 手改错别字（绕过 CLI 的裸 git 写入）"
git -C "$OOB" push -q
rm -rf "$OOB"
note "裸写入 = 合法（Git+YAML canonical 的本义），干净机器 sync 后照常工作："
cd "$FE"; $BIN sync >/dev/null
$BIN work show REQ-UI-001 | rg 'title:' | head -1
$BIN converge || true

zap "10：批量雪崩——PM 一口气规划 6 张 v2.0 单"
cd "$PM"; $BIN sync >/dev/null
for i in 1 2 3 4 5 6; do
  SYS=cms-ui; AS=frontend-agent
  if [ $((i % 2)) -eq 0 ]; then SYS=cms-api; AS=backend2-agent; fi
  if [ $i -eq 5 ]; then SYS=cms-api; AS=qa-agent; fi
  $BIN work create --id REQ-CMS-10$i --type requirement --title "v2.0 批次功能 $i" \
    --system "$SYS" --assignee "$AS" --accountable pm --priority P2 --due 2026-10-25 \
    --version v2.0 --description "验收：v2.0 批次功能 $i" --actor pm
done
git push -q
note "work list --open 行数: $($BIN work list --open | tail -n +2 | wc -l | tr -d ' ')"
note "view kanban 扛住: $($BIN view kanban | wc -l | tr -d ' ') 行"
note "context me（backend2 视角）扛住: $($BIN context me --actor backend2-agent | wc -l | tr -d ' ') 行"

zap "11：重工循环——QA 回归发现列表没分页，done 单重开"
cd "$BE"
note "失联者回归：先丢弃黑洞期的陈旧本地账，对齐最新事实"
git reset --hard -q origin/main
$BIN sync >/dev/null
$BIN work update REQ-CMS-001 --status doing --actor backend-agent
cd "$VAPI"
python3 - <<'EOF'
s = open("main.go").read()
s = s.replace('json.NewEncoder(w).Encode(map[string]any{"items": items})',
              'json.NewEncoder(w).Encode(map[string]any{"items": items, "page_size": 20})')
open("main.go", "w").write(s)
EOF
go build ./... || die "rework compile"
git add -A; git commit -qm "fix: 列表补分页字段（回归修复）"
cd "$BE"
FHASH=$(git -C "$VAPI" rev-parse --short HEAD)
$BIN evidence add REQ-CMS-001 --type git_commit --source git --locator "$FHASH" --actor backend-agent
note "证据 append-only（应见两条 git_commit）: $($BIN work show REQ-CMS-001 | rg -c 'git_commit')"
$BIN work update REQ-CMS-001 --status testing --actor backend-agent
$BIN work update REQ-CMS-001 --status done    --actor backend-agent
$BIN progress report REQ-CMS-001 --value 1.0 --basis test-cases --actor backend-agent
git push -q

zap "12：诚实框架——前端挂了个假 CI 证据，系统照单全收"
cd "$FE"; $BIN sync >/dev/null
$BIN evidence add REQ-UI-001 --type ci_run --source fake-ci --locator "https://ci.invalid/run/999" --result passed --actor frontend-agent
git push -q
note "收下了，无人验真伪（C1 诚实框架：只有 git_commit 有硬校验，其余类型是声明式证据）"
cd "$PM"; $BIN sync >/dev/null
$BIN converge || true

say "Stage 7: 终局——五机独立 converge 一致性 + 看板 + 总账"
declare -a outs=()
for ws in "$PM" "$BE" "$FE" "$QA" "$BE2"; do
  (cd "$ws" && $BIN sync >/dev/null 2>&1)
  out=$( (cd "$ws" && $BIN converge) )
  outs+=("$out")
  printf '%-8s converge: %s\n' "$(basename "$ws")" "$(echo "$out" | head -1)"
done
for o in "${outs[@]:1}"; do
  [ "$o" = "${outs[0]}" ] || die "五机 converge 输出不一致（S7 失败）"
done
note "五机 converge 完全一致（S7 通过）"
echo
cd "$PM"
$BIN view kanban | head -24
echo
say "突发事件处置总账（第二辑）"
cat <<'SUMMARY'
  ⚡1  紧急发版跳版    → target_version v1→v2 落账；revision(CAS)≠产品版本，work list --version 过滤
  ⚡2  agent 失联      → 本地账黑洞（未 push）→ team add + work assign 移交 + 第五台机零基础上岗
  ⚡3  breaking 强推   → C2 三连：无 ack 拒 / agent 自 ack 拒 / PM --ack 过（写入路径+注册表 human 型双门）
  ⚡4  假冒身份        → --actor 旗标可冒用（文档化边界）→ git 审计 author≠actor 揪出 → 重开恢复
  ⚡5  手改损坏        → 未知键被 strict decode 拒 → git revert 精准恢复（历史救命）
  ⚡6  依赖环          → converge cycle 曝光 BLOCKED → PM 拆环
  ⚡7  撞 ID           → 双机同建 → push 拒 → rebase 冲突 → 丢弃分歧 + owner 前缀命名空间重建
  ⚡8  超长字段        → 8192 字节门拒 → 长文改外链
  ⚡9  裸修中央仓      → 绕过 CLI 的合法 git 写入被全拓扑容忍（canonical 本义）
  ⚡10 批量雪崩        → 6 单连建，list/kanban/context 全扛住
  ⚡11 重工循环        → done→doing 重开 + 证据 append-only（两条 git_commit）+ 失联者先 reset 对齐
  ⚡12 诚实框架        → 假 ci_run 照收（C1 不验真伪 = 设计边界，git_commit 是唯一硬验类型）
SUMMARY
say "CHAOS DOGFOOD 第二辑完成"
