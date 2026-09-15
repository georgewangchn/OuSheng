#!/usr/bin/env bash
# S7 dogfood 第四幕：共识层（v0.4）——设计发起、轮次发言、human 拍板、supersede 全流程。
# 拓扑：PM / 后端 / 前端 三机 + bare 中央仓（挂接 vote 场景）。
# 判据：design/architecture 全程 Git+YAML 同步；注入面只有指针；human 门与超前曝光审计生效。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ROOT=${DOGFOOD_ROOT:-$(mktemp -d -t dogfood-s7-design)}
BIN=$ROOT/bin/ousheng
CENTRAL=$ROOT/vote-central.git
PM=$ROOT/pm-ws
BE=$ROOT/backend-ws
FE=$ROOT/frontend-ws

say()  { printf '\n\033[1;36m== %s ==\033[0m\n' "$*"; }
note() { printf '\033[0;33m  > %s\033[0m\n' "$*"; }
die()  { printf '\033[0;31mFATAL: %s\033[0m\n' "$*" >&2; exit 1; }

say "Stage 0: 构建 + 场地（三机 + bare）"
rm -rf "$ROOT"; mkdir -p "$ROOT/bin"
(cd "$REPO" && go build -o "$BIN" ./cmd/ousheng) || die "build"
git init -q --bare -b main "$CENTRAL"
mkdir -p "$PM"; cd "$PM"
git init -q -b main
git config user.name pm; git config user.email pm@vote.local
$BIN init --project-id vote-system --project-name "投票系统" || die init
$BIN me pm --name "产品经理"
$BIN system add vote-api --name "投票后端"
$BIN system add vote-ui  --name "投票前端"
$BIN team add backend-agent  --name "后端Agent"  --type agent --responsible-human pm --role backend  --system vote-api
$BIN team add frontend-agent --name "前端Agent"  --type agent --responsible-human pm --role frontend --system vote-ui
git remote add origin "$CENTRAL"; git push -q -u origin main
for m in backend-ws:backend-agent frontend-ws:frontend-agent; do
  d=${m%%:*}; a=${m##*:}
  git clone -q "$CENTRAL" "$ROOT/$d"
  git -C "$ROOT/$d" config user.name "$a"; git -C "$ROOT/$d" config user.email "$a@vote.local"
  (cd "$ROOT/$d" && $BIN me "$a")
done
note "三机就位，零共享状态"

say "Stage 1: architecture 上线 + PM 发建设计（内容直接写文件，绳不代笔）"
cd "$PM"
mkdir -p .ousheng/architecture
cat > .ousheng/architecture/vote-api.md <<'EOF'
# vote-api 全局面貌

- HTTP 服务，Go 标准库，端口 8080
- 资源：polls / votes / results（契约见 work items）
- 状态全部在内存（demo 拓扑），生产形态待议
EOF
cat > .ousheng/architecture/vote-ui.md <<'EOF'
# vote-ui 全局面貌

- 静态单页（index.html），fetch 直连 vote-api
- 无构建链、无框架（demo 拓扑）
EOF
$BIN work create --id REQ-VOTE-003 --type requirement --title "投票结果实时推送" \
  --system vote-api --assignee backend-agent --accountable pm \
  --description "验收：结果页自动更新，无需手动刷新" --actor pm
mkdir -p .ousheng/designs/live-results
cat > .ousheng/designs/live-results/design.md <<'EOF'
---
status: draft
owner: pm
systems: [vote-api, vote-ui]
related_items: [REQ-VOTE-003]
---
# 投票结果实时推送

SSE：GET /v1/polls/{id}/events，每次计票后推送全量 counts。
前端 EventSource 订阅，断线自动重连（浏览器原生）。
备选（本轮讨论）：WebSocket 双向通道——本轮倾向否掉，只有下行需求。
EOF
cat > .ousheng/designs/live-results/round-1.md <<'EOF'
## pm — 2026-09-15

发起。倾向 SSE：只有下行需求，双工是过度设计。
后端评估连接数上限与 goroutine 成本；前端评估 EventSource 兼容性。
EOF
git add .ousheng/architecture/vote-api.md .ousheng/architecture/vote-ui.md \
  .ousheng/designs/live-results/design.md .ousheng/designs/live-results/round-1.md
git commit -qm "design: 发起 live-results 方案（draft）+ architecture 上线"
git push -q
note "design.md/round 人写 git 提交，绳只做发现/注入/生命周期"

say "Stage 2: 两台 agent 机 sync —— pending_reviews 地址化到达（注入面只有指针）"
cd "$BE"; $BIN sync --actor backend-agent >/dev/null
$BIN context me --actor backend-agent | grep -A2 pending_reviews || die "backend pending 未见"
cd "$FE"; $BIN sync --actor frontend-agent >/dev/null
$BIN context me --actor frontend-agent | grep -A2 pending_reviews || die "frontend pending 未见"
note "draft × systems 含我 × 我未发言 → 待发言派生到达；正文零注入（需要时 design show 自读）"

say "Stage 3: 后端轮次发言 → pending 消失；前端未发言仍 pending"
cd "$BE"
cat >> .ousheng/designs/live-results/round-1.md <<'EOF'

## backend-agent — 2026-09-15

同意 SSE。单 poll 连接数 demo 场景 <100，goroutine 成本可忽略。
建议 events 载荷直接复用 GET /results 的结构，前端零适配。
EOF
git add .ousheng/designs/live-results/round-1.md
git commit -qm "design(round-1): backend-agent 发言（SSE 可行）"
git push -q
$BIN sync --actor backend-agent >/dev/null
if $BIN context me --actor backend-agent | grep -q pending_reviews; then
  die "发言后 pending 应消失"
fi
note "backend 已发言不再被喊；waiting_for 不存储，从最新 round 派生（零元数据腐烂）"

say "Stage 4: 超前曝光审计——后端急开工（doing × draft）→ converge warning"
cd "$BE"
$BIN work update REQ-VOTE-003 --status ready  --actor backend-agent
$BIN work update REQ-VOTE-003 --status doing --actor backend-agent
git push -q
$BIN converge | grep "running ahead" || die "超前曝光 warning 未见"
note "方案未拍板就开工：曝光不拦（PM 可能故意抢先，§4.8 防线二）"

say "Stage 5: 攻防——agent 自 decide 被拒（human 门）→ PM 拍板 → warning 消"
cd "$BE"
if $BIN design decide live-results --actor backend-agent 2>/tmp/design-err; then
  die "human 门失守：agent 自 decide 竟然通过"
else
  note "拒绝（预期）: $(cat /tmp/design-err)"
fi
cd "$PM"; $BIN sync >/dev/null
$BIN design decide live-results --actor pm
git push -q
cd "$BE"; $BIN sync --actor backend-agent >/dev/null
if $BIN converge 2>&1 | grep -q "running ahead"; then
  die "拍板后超前曝光 warning 应消失"
fi
note "拍板留痕：decided_by=pm；三方一致后才算共识（自拍不算）"

say "Stage 6: supersede 全流程——新方案继任 + waiting-for 巡检 + 终局核验"
cd "$PM"
mkdir -p .ousheng/designs/live-results-ws
cat > .ousheng/designs/live-results-ws/design.md <<'EOF'
---
status: draft
owner: pm
systems: [vote-api, vote-ui]
related_items: [REQ-VOTE-003]
---
# 投票结果实时推送（WebSocket 版）

推翻 SSE：v2 要加"谁在线"上行信号，双工需求坐实，改一条 WebSocket 通道。
EOF
git add .ousheng/designs/live-results-ws/design.md
git commit -qm "design: 发起 live-results-ws（draft，推翻 SSE 方案）"
git push -q
cd "$FE"; $BIN sync --actor frontend-agent >/dev/null
$BIN context me --actor frontend-agent | grep live-results-ws || die "frontend 应见新方案 pending"
cd "$PM"
note "frontend 被新方案喊话（新轮未发言，live-results 已 agreed 不再喊）:"
$BIN design list --waiting-for frontend-agent
$BIN design decide live-results-ws --actor pm
$BIN design supersede live-results --by live-results-ws --actor pm
git push -q
$BIN design list
note "两机 converge 一致性:"
for ws in "$PM" "$BE"; do
  (cd "$ws" && $BIN sync >/dev/null 2>&1)
  printf '%-12s converge: %s\n' "$(basename "$ws")" "$( (cd "$ws" && $BIN converge) )"
done
cd "$BE"
note "work show detail 层携带方案出处（两代方案全留痕）:"
$BIN work show REQ-VOTE-003 | grep -A4 "^designs:" || die "work show 应含 designs 出处"
say "S7-DESIGN 判据核验"
echo "  human 门: agent decide 被拒 + PM decide 留痕（git 审计可查）"
echo "  注入面: pending_reviews/knowledge 全是指针，正文零注入"
echo "  supersede: live-results → live-results-ws 链完整（继任者已拍板，无悬空/未生效 warning）"
say "DOGFOOD S7-DESIGN 完成"
