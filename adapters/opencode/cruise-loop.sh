#!/usr/bin/env bash
# cruise-loop.sh —— 巡航驱动器（巡航模式设计方案 §3.4）
# 外层定时器驱动而非 plugin 自主续命：kill 即停、每轮一 session 可审计、三闸可控。
# 用法: ./scripts/cruise-loop.sh <workspace> <actor> <code-repo> [轮顶=20] [间隔秒=300] [时窗=22:00-07:00]
#   workspace=看板仓（cruise plan 的 --dir）；code-repo=本座位代码仓（opencode 在此开session）
# 可选: OUSHENG_BIN / OC_BIN 覆盖二进制；CRUISE_EXTRA 透传 flag（如 --allow-new）。
# claude harness：OC_BIN 覆盖后请自行把 run 行改为 `claude -p "..."`（prompt 语义同）。
set -euo pipefail
WS=$1; ACTOR=$2; REPO=$3; MAXR=${4:-20}; GAP=${5:-300}; WIN=${6:-22:00-07:00}
if [ -z "$REPO" ]; then echo "[cruise] 缺 code-repo 参数——opencode 必须在代码仓开 session（在看板仓里干活=跑错地方）" >&2; exit 2; fi
OUSHENG=${OUSHENG_BIN:-ousheng}; OC=${OC_BIN:-opencode}
# 夜间省钱旋钮：OC_ARGS 透传给 opencode run（如 OC_ARGS="--model <便宜模型>"）。
OC_ARGS=${OC_ARGS:-}
EXTRA=${CRUISE_EXTRA:-}
run=0; idle=0
inwin() { python3 -c "
import datetime
n=datetime.datetime.now().time()
lo,hi=[datetime.datetime.strptime(t,'%H:%M').time() for t in '$WIN'.split('-')]
print('1' if (lo<=n<=hi) if lo<hi else (n>=lo or n<=hi) else '')"; }
while :; do
  run=$((run+1))
  [ $run -gt $MAXR ] && { echo "[cruise] 轮顶 $MAXR 达成，停"; break; }
  [ -z "$(inwin)" ] && { echo "[cruise] 时窗外 $WIN，停"; break; }
  # stderr 只进日志不进 prompt（复查修正：噪声混进指令源 = 数据面污染）
  PLAN=$("$OUSHENG" cruise plan --dir "$WS" --actor "$ACTOR" $EXTRA 2>/tmp/cruise-plan.err) || { echo "[cruise] plan 失败：$(cat /tmp/cruise-plan.err)"; break; }
  [ -s /tmp/cruise-plan.err ] && echo "[cruise] plan stderr: $(cat /tmp/cruise-plan.err)"
  if echo "$PLAN" | grep -q "cruise: idle"; then
    idle=$((idle+1)); [ $idle -ge 2 ] && { echo "[cruise] 连续 idle，停"; break; }
  else
    idle=0
    echo "[cruise] round $run → $OC"
    ( cd "$REPO" && $OC run $OC_ARGS --prompt "巡航执行（严格按 AGENTS.md 巡航协议节）：处理以下清单，一次一轮，触 human 门即挂起转下一项；收尾 ousheng handover save --system <你的系统> --actor $ACTOR 后 ousheng sync。

$PLAN" ) || true
  fi
  sleep "$GAP"
done
