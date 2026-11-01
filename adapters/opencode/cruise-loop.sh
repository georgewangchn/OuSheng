#!/usr/bin/env bash
# cruise-loop.sh —— 巡航驱动器（巡航模式设计方案 §3.4）
# 外层定时器驱动而非 plugin 自主续命：kill 即停、每轮一 session 可审计、三闸可控。
# 用法: ./scripts/cruise-loop.sh <workspace> <actor> [轮顶=20] [间隔秒=300] [时窗=22:00-07:00]
# 可选: OUSHENG_BIN / OC_BIN 覆盖二进制；CRUISE_EXTRA 透传 flag（如 --allow-new）。
# claude harness：OC_BIN 覆盖后请自行把 run 行改为 `claude -p "..."`（prompt 语义同）。
set -euo pipefail
WS=$1; ACTOR=$2; MAXR=${3:-20}; GAP=${4:-300}; WIN=${5:-22:00-07:00}
OUSHENG=${OUSHENG_BIN:-ousheng}; OC=${OC_BIN:-opencode}
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
  PLAN=$("$OUSHENG" cruise plan --dir "$WS" --actor "$ACTOR" $EXTRA 2>&1) || { echo "[cruise] plan 失败：$PLAN"; break; }
  if echo "$PLAN" | grep -q "cruise: idle"; then
    idle=$((idle+1)); [ $idle -ge 2 ] && { echo "[cruise] 连续 idle，停"; break; }
  else
    idle=0
    echo "[cruise] round $run → $OC"
    ( cd "$WS" && $OC run --prompt "巡航执行（严格按 AGENTS.md 巡航协议节）：处理以下清单，一次一轮，触 human 门即挂起转下一项；收尾 ousheng handover save --system <你的系统> --actor $ACTOR 后 ousheng sync。

$PLAN" ) || true
  fi
  sleep "$GAP"
done
