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
OC_ARGS=${OC_ARGS:-}
run=0; idle=0; planfail=0; SAME=0; GAP_EXTRA=0

# §42 三修③：并发上限（BUG-132 定案机制化）——跨座位 mkdir 原子信号量。
# CONCURRENCY_MAX（默认 2）：同机同时跑 opencode 轮的座位数上限；轮前取槽轮后释放，
# 座位间可交错。槽含 owner pid：宿主死透则偷槽（防 -9 残留死锁）。
MAXCON=${CONCURRENCY_MAX:-2}
SEAT_SLOT=""
acquire_slot() {
  local i pid
  while :; do
    for i in $(seq 1 "$MAXCON"); do
      if mkdir "/tmp/cruise-sem.$i" 2>/dev/null; then SEAT_SLOT=$i; echo $$ > "/tmp/cruise-sem.$i/pid"; return 0; fi
      pid=$(cat "/tmp/cruise-sem.$i/pid" 2>/dev/null)
      if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
        rm -rf "/tmp/cruise-sem.$i" 2>/dev/null  # 宿主已死——偷槽
      fi
    done
    sleep 15
  done
}
release_slot() { [ -n "$SEAT_SLOT" ] && rm -rf "/tmp/cruise-sem.$SEAT_SLOT" 2>/dev/null; SEAT_SLOT=""; }
trap release_slot EXIT

# 启动横幅（§37 复盘 #1，2026-11-13）：pm 重排曾绕过 systemd（nohup 形态）导致
# 驱动形态不可辨识、attach 不可观测——横幅进 journal/nohup.out，任何启动形态可验。
echo "[cruise] === driver 启动 | seat=$ACTOR/$SYSTEM | ws=$WS | repo=$REPO | win=$WIN | 轮顶=$MAXR 间隔=$WAIT | 并发上限=$MAXCON | pid=$$ | $($OUSHENG version 2>/dev/null | head -1) ==="
# /tmp 隔离（评审 🔴 修复）：同机多座位并行时共享 /tmp 会互读他座位的
# round 输出——NEWSID 错配 = 把 B 的 session attach 到 A 的快照（静默）。
# 命名 = 座位 + PID；跨轮日志按座位稳定（人工查 /tmp/cruise-loop.<actor>.err）。
TAG="$ACTOR.$$"
PLANERR=/tmp/cruise-plan.$TAG.err
ROUNDOUT=/tmp/cruise-round.$TAG.out
LOOPERR=/tmp/cruise-loop.$ACTOR.err
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
ok = (lo<=n<=hi) if lo<hi else (n>=lo or n<=hi)
print('1' if ok else '')"; }

# 取 plan 输出（重试 3 次退避——一次网络抖动不该停掉整夜）
getplan() {
  local tries=0 out
  while [ $tries -lt 3 ]; do
    out=$("$OUSHENG" cruise plan --dir "$WS" --actor "$ACTOR" --wait "$WAIT" $EXTRA 2>"$PLANERR") && { echo "$out"; return 0; }
    tries=$((tries+1)); echo "[cruise] plan 失败($tries/3)：$(tail -1 "$PLANERR")" >&2
    [ -s "$PLANERR" ] && cat "$PLANERR" >> "$LOOPERR"
    sleep 60
  done
  return 1
}

while :; do
  run=$((run+1))
  [ $run -gt $MAXR ] && { echo "[cruise] 轮顶 $MAXR 达成，停"; break; }
  [ -z "$(inwin)" ] && { echo "[cruise] 时窗外 ${WIN}，停"; break; }

  PLAN=$(getplan) || { echo "[cruise] plan 连败 3 次，停（systemd 会拉起；手动模式人工查 ${LOOPERR}）"; break; }

  # §42 修④：维持轮退避（§37 遗留判据激活——lhcp 纯记账轮 48% 触线三夜）。
  # plan 文本与上轮逐字相同 = 没新活没新反应 = 维持空转——间隔（WAIT）指数拉长（封顶 2h），
  # 有变化即复位。治「每 10 分钟全量轮做同一件零 delta 事」。
  PH="/tmp/cruise-planhash.$TAG"
  PH_NOW=$(printf '%s' "$PLAN" | cksum | cut -d' ' -f1)
  PH_OLD=$(cat "$PH" 2>/dev/null || echo "")
  if [ -n "$PH_OLD" ] && [ "$PH_NOW" = "$PH_OLD" ]; then
    SAME=$((SAME+1))
    if [ "$SAME" -ge 2 ]; then
      GAP_EXTRA=$((WAIT * (1 << (SAME-2))))
      [ "$GAP_EXTRA" -gt 7200 ] && GAP_EXTRA=7200
      echo "[cruise] 维持轮退避 #${SAME}：plan 与前轮同 → 本轮后加睡 ${GAP_EXTRA}s"
    fi
  else
    SAME=0; GAP_EXTRA=0
  fi
  printf '%s' "$PH_NOW" > "$PH"

  if echo "$PLAN" | grep -q "cruise: idle"; then
    idle=$((idle+1))
    W=$(idle_wait)
    if [ $idle -ge 5 ]; then
      echo "[cruise] idle×5（退避到 4h 仍无活），停——cron 下个时窗再启"
      break
    fi
    echo "[cruise] idle #${idle}，退避 ${W}s 后再探"
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
    [ -n "${OC_MODEL_CODE:-}" ] && OCARGS="$OCARGS --model $OC_MODEL_CODE"
  else
    [ -n "${OC_MODEL_WAIT:-}" ] && OCARGS="$OCARGS --model $OC_MODEL_WAIT"
  fi
  echo "[cruise] round $run → $OC ${SESS:+(续代 $SESS)}"

  # 并发槽（§42 三修③）：opencode 轮是资源大头——取槽才跑，跑完即释。
  acquire_slot

  # L0 轮头（2026-11-12 档案 §37）：round 号 + 活单 ID 进 --title 与 prompt 首行
  # ——session 列表直接可辨（A 机复盘：233 轮标题全同不可辨）。
  # 参数面契约（opencode v2.2.5 实测 2026-11-12 首夜自旋事故）：消息是位置参数，
  # 无 --prompt flag——错 flag = usage dump 秒退 = 20 轮自旋烧完轮顶。
  IDS=$(echo "$PLAN" | grep -oE '(BUG|FEAT|TASK|T|REQ|N|TEST)-[0-9]+' | sort -u | head -6 | tr '\n' ' ')
  ( cd "$REPO" && $OC run $OCARGS $OC_ARGS --format json --title "巡航 r${run} ${IDS:-无} @${ACTOR}" "巡航 round $run | 活单: ${IDS:-无} | 座位 $ACTOR/$SYSTEM
巡航执行（严格按 AGENTS.md 巡航协议节）：处理以下清单，一次一轮，触 human 门即挂起转下一项；收尾 ousheng handover save --system ${SYSTEM} --actor ${ACTOR}（首行=一句话人话摘要，pm 可读）后 ousheng sync。

$PLAN" ) > "$ROUNDOUT" 2>>"$LOOPERR" || echo "[cruise] opencode 非零退出，继续下一轮" >&2

  # 驱动器补记本代 session id（正文是 agent 的事，指针是驱动器的事）
  NEWSID=$(grep -oE '"sessionID":"[^"]+"' "$ROUNDOUT" | head -1 | cut -d'"' -f4)
  [ -n "$NEWSID" ] && "$OUSHENG" handover attach --dir "$WS" --system "$SYSTEM" --actor "$ACTOR" --session "$NEWSID" 2>/dev/null || true
  release_slot

  # §42 修④：维持轮退避落点——轮后加睡（plan 零变化=维持空转）。
  if [ "$GAP_EXTRA" -gt 0 ]; then
    echo "[cruise] 退避加睡 ${GAP_EXTRA}s（维持轮 #${SAME}）"
    sleep "$GAP_EXTRA"
  fi
done
