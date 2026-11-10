#!/usr/bin/env bash
# cruise-loop.sh —— 巡航驱动器 v2（档案 §34：P0 代际续跑 / P2 活到即醒+自愈）
# 用法: ./scripts/cruise-loop.sh <kanban工作区> <actor> <system> <代码仓> [轮顶=20] [wait秒=600] [时窗=22:00-07:00]
# 环境旋钮: OUSHENG_BIN / OC_BIN / OC_ARGS（全局透传）
#   OC_MODEL_WAIT  = 值守/轻活模型（如 glm/glm-5.3-flash）——默认不传=全局默认
#   OC_MODEL_CODE  = 编码/强推理模型（如 glm/glm-5.3）——默认不传=全局默认
#   CRUISE_EXTRA   = plan 透传（如 --allow-new）
# 模型路由（档案 §35 P5，PM 实栈双层修正产品化）：
#   cruise plan 是纯 CLI（零 LLM），无需模型；opencode run 按任务类型选模型。
#   实现：OC_ARGS 基础上按 plan 中的活类型动态加 --model。
# 挂载: 手动 / cron / systemd（见 scripts/ousheng-cruise@.service 模板）
set -uo pipefail
WS=$1; ACTOR=$2; SYSTEM=$3; REPO=$4; MAXR=${5:-20}; WAIT=${6:-600}; WIN=${7:-22:00-07:00}
[ -z "$REPO" ] && { echo "[cruise] 缺 code-repo 参数——opencode 必须在代码仓开 session" >&2; exit 2; }
OUSHENG=${OUSHENG_BIN:-ousheng}; OC=${OC_BIN:-opencode}
EXTRA=${CRUISE_EXTRA:-}
run=0; idle=0; planfail=0
# idle 指数退避（档案 §35 P3，实栈 lhcp 早停实锤+PM BUG-123 配套）：
# idle→30min→1h→2h→4h→停。探测统一走 cruise plan（机制断言），不自判。
idle_wait() {
  case $idle in
    0|1) echo 600 ;;
    2) echo 1800 ;;
    3) echo 3600 ;;
    4) echo 7200 ;;
    *) echo 14400 ;;
  esac
}

inwin() { python3 -c "
import datetime
n=datetime.datetime.now().time()
lo,hi=[datetime.datetime.strptime(t,'%H:%M').time() for t in '$WIN'.split('-')]
print('1' if (lo<=n<=hi) if lo<hi else (n>=lo or n<=hi) else '')"; }

# 取 plan 输出（重试 3 次退避——一次网络抖动不该停掉整夜）
getplan() {
  local tries=0 out
  while [ $tries -lt 3 ]; do
    out=$("$OUSHENG" cruise plan --dir "$WS" --actor "$ACTOR" --wait "$WAIT" $EXTRA 2>/tmp/cruise-plan.err) && { echo "$out"; return 0; }
    tries=$((tries+1)); echo "[cruise] plan 失败($tries/3)：$(tail -1 /tmp/cruise-plan.err)" >&2
    [ -s /tmp/cruise-plan.err ] && cat /tmp/cruise-plan.err >> /tmp/cruise-loop.err
    sleep 60
  done
  return 1
}

while :; do
  run=$((run+1))
  [ $run -gt $MAXR ] && { echo "[cruise] 轮顶 $MAXR 达成，停"; break; }
  [ -z "$(inwin)" ] && { echo "[cruise] 时窗外 $WIN，停"; break; }

  PLAN=$(getplan) || { echo "[cruise] plan 连败 3 次，停（systemd 会拉起；手动模式人工查 /tmp/cruise-loop.err）"; break; }

  if echo "$PLAN" | grep -q "cruise: idle"; then
    idle=$((idle+1))
    W=$(idle_wait)
    if [ $idle -ge 5 ]; then
      echo "[cruise] idle×5（退避到 4h 仍无活），停——cron 下个时窗再启"
      break
    fi
    echo "[cruise] idle #$idle，退避 ${W}s 后再探"
    sleep "$W"
    continue
  fi
  idle=0

  # 会话代际（P0）：plan 给 attach id 则续同代（省上下文重构），否则新代。
  SESS=$(echo "$PLAN" | grep -oE 'attach: [A-Za-z0-9_-]+' | head -1 | cut -d' ' -f2)
  OCARGS=""
  [ -n "$SESS" ] && OCARGS="-s $SESS"
  # P5 模型路由：plan 输出含 bug/task/FEAT→强模型；notice/巡检→轻模型
  if echo "$PLAN" | grep -qE "BUG-|T-[0-9]|FEAT-|TASK-"; then
    [ -n "$OC_MODEL_CODE" ] && OCARGS="$OCARGS --model $OC_MODEL_CODE"
  else
    [ -n "$OC_MODEL_WAIT" ] && OCARGS="$OCARGS --model $OC_MODEL_WAIT"
  fi
  echo "[cruise] round $run → $OC ${SESS:+(续代 $SESS)}"

  ( cd "$REPO" && $OC run $OCARGS $OC_ARGS --format json --prompt "巡航执行（严格按 AGENTS.md 巡航协议节）：处理以下清单，一次一轮，触 human 门即挂起转下一项；收尾 ousheng handover save --system $SYSTEM --actor $ACTOR 后 ousheng sync。

$PLAN" ) > /tmp/cruise-round.out 2>>/tmp/cruise-loop.err || echo "[cruise] opencode 非零退出，继续下一轮" >&2

  # 驱动器补记本代 session id（正文是 agent 的事，指针是驱动器的事）
  NEWSID=$(grep -oE '"sessionID":"[^"]+"' /tmp/cruise-round.out | head -1 | cut -d'"' -f4)
  [ -n "$NEWSID" ] && "$OUSHENG" handover attach --dir "$WS" --system "$SYSTEM" --actor "$ACTOR" --session "$NEWSID" 2>/dev/null || true
done
