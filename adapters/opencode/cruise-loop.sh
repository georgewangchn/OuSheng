#!/usr/bin/env bash
# cruise-loop.sh —— 巡航驱动器 v3（档案 §34 P0 代际 / §42 三修 / §46 并回 v5）
# 用法: ./scripts/cruise-loop.sh <kanban工作区> <actor> <system> <代码仓> [轮顶=20] [wait秒=600] [时窗=22:00-07:00] [截止='YYYY-MM-DD HH:MM']
# 环境旋钮: OUSHENG_BIN / OC_BIN / OC_ARGS（全局透传）
#   OC_MODEL_WAIT  = 值守/轻活模型（如 deepseek/deepseek-flash）——默认不传=全局默认
#   OC_MODEL_CODE  = 编码/强推理模型（如 zai/glm-5.3）——默认不传=全局默认
#     （兼容 v5 命名：OC_DEV_MODEL/OC_FLASH_MODEL 作 fallback）
#   CONCURRENCY_MAX = 同机并发上限（默认 2，§42 三修③信号量）
#   CRUISE_EXTRA   = plan 透传（如 --allow-new）
# 2026-11-14 §46 并回 v5（pm 实栈迭代）：吸收两件——①flock 座位单实例（BUG-179 根治：
#   同座位同 repo 只许 1 实例，防双开竞写/双 agent 同卡）②截止参数（window 外的硬截止，
#   人到点自停）。canonical 原有退避/信号量/轮头/横幅/attach 全部保留。
# 挂载: 手动 / systemd（见 scripts/ousheng-cruise@.service 模板）；启动=人（档案 §45）。
set -uo pipefail
WS=$1; ACTOR=$2; SYSTEM=$3; REPO=$4; MAXR=${5:-20}; WAIT=${6:-600}; WIN=${7:-22:00-07:00}; DEADLINE=${8:-}
[ -z "$REPO" ] && { echo "[cruise] 缺 code-repo 参数——opencode 必须在代码仓开 session" >&2; exit 2; }
OUSHENG=${OUSHENG_BIN:-ousheng}; OC=${OC_BIN:-opencode}
EXTRA=${CRUISE_EXTRA:-}
OC_ARGS=${OC_ARGS:-}
OC_MODEL_CODE=${OC_MODEL_CODE:-${OC_DEV_MODEL:-}}
OC_MODEL_WAIT=${OC_MODEL_WAIT:-${OC_FLASH_MODEL:-}}
run=0; idle=0; planfail=0; SAME=0; GAP_EXTRA=0

# --- BUG-179 根治（§46 并回）：座位级 flock 单实例 ---
# 同座位同 repo 只许 1 实例；锁由 flock 父持有，fd 不传给子（父死锁不死）。
# flock 不可用（无 util-linux）则降级跳过——单机单座形态仍安全。
if command -v flock >/dev/null 2>&1; then
  SEAT_LOCK="/tmp/opencode/cruise-seat-$ACTOR.lock"
  exec flock -n -o "$SEAT_LOCK" "$0" "$@"
fi

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

# 启动横幅（§37 复盘 #1）：pm 重排曾绕过 systemd（nohup 形态）导致驱动形态
# 不可辨识、attach 不可观测——横幅进 journal/nohup.out，任何启动形态可验。
echo "[cruise] === driver 启动 | seat=$ACTOR/$SYSTEM | ws=$WS | repo=$REPO | win=$WIN | 轮顶=$MAXR 间隔=$WAIT | 截止=${DEADLINE:-无} | 并发上限=$MAXCON | pid=$$ | $($OUSHENG version 2>/dev/null | head -1) ==="
# /tmp 隔离（评审 🔴 修复）：同机多座位并行时共享 /tmp 会互读他座位的
# round 输出——NEWSID 错配 = 把 B 的 session attach 到 A 的快照（静默）。
TAG="$ACTOR.$$"
PLANERR=/tmp/cruise-plan.$TAG.err
ROUNDOUT=/tmp/cruise-round.$TAG.out
LOOPERR=/tmp/cruise-loop.$ACTOR.err
# idle 指数退避（档案 §35 P3）：idle→30min→1h→2h→4h→停。探测走 cruise plan（机制断言）。
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

# 截止（§46 并回 v5）：窗外硬截止，人到点自停。
deadline_passed() {
  [ -z "$DEADLINE" ] && return 1
  python3 -c "
import datetime
d=datetime.datetime.strptime('$DEADLINE','%Y-%m-%d %H:%M')
print('1' if datetime.datetime.now()>d else '')"
}

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
  [ "$(deadline_passed)" = "1" ] && { echo "[cruise] 截止 $DEADLINE 已过，停"; break; }

  PLAN=$(getplan) || { echo "[cruise] plan 连败 3 次，停（systemd 会拉起；手动模式人工查 ${LOOPERR}）"; break; }

  # §42 修④：维持轮退避——plan 文本与上轮逐字相同 = 维持空转，间隔指数拉长（封顶 2h）。
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

  # L0 轮头（档案 §37）：round 号 + 活单 ID 进 --title 与 prompt 首行。
  # 参数面契约（opencode v2.2.5 实测）：消息是位置参数，无 --prompt flag。
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
