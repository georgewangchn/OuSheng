#!/usr/bin/env bash
# deploy-fleet.sh —— 维护者工具：tide 实栈舰队（224/225/226 + 本机）二进制部署 + 验证。
#
# 范围裁决：实栈验证场专用运维层（产品仓 scripts/，与 dogfood 剧本同层）；
# 不属于「为已锁机制再造脚本」禁区（那是验证层，这是部署层）。
#
# 用法：
#   scripts/deploy-fleet.sh            # 测试 → 建 4 二进制 → 部署 3 机 + 本机 → 验证
#   scripts/deploy-fleet.sh --seats    # 另跑五座位 adapter install（plugin/协议段变更后）
#
# 铁律内建：
#   - 工作树脏 = 拒绝部署（版本戳会撒谎：标 HEAD 却含未提交代码）。FORCE=1 可强行。
#   - 远端一律 tmp+mv 原子替换（mcp 进程 text-file-busy）。
#   - 座位身份逐位显式传 --actor（省略回落 me：本机会打回 pm、226 双座位会串）。
#   - 任一验证失败退出 1（fail-closed，不留半部署状态不报）。
#   - 水位非 0/0 = WARN 不扣退出码（漂移合法存在——收口是 plugin/sync 的活）。
set -euo pipefail

MACHINES=(224 225 226)                 # 192.168.1.x
BIN_DIR_LOCAL="$HOME/.local/bin"       # 本机安装位
LINUX_BIN="/usr/local/bin"
SEAT_LOCAL="$HOME/Documents/siicode/tide/project_datax"
REFRESH_SEATS=0
[[ "${1:-}" == "--seats" ]] && REFRESH_SEATS=1

seats_of() { case $1 in
  224) echo "/data/tide/Datax" ;;
  225) echo "/data/mlake" ;;
  226) echo "/data/ui /data/test" ;;
esac }
ws_of() { case $1 in
  224) echo "/data/tide/kanban" ;;
  225|226) echo "/data/kanban" ;;
esac }

cd "$(dirname "$0")/.."

echo "== 0. 预检 =="
if [[ -n "$(git status --short)" && "${FORCE:-0}" != "1" ]]; then
  echo "FAIL: 工作树脏——版本戳会撒谎。提交后再来，或 FORCE=1 强行：" >&2
  git status --short >&2
  exit 1
fi
C=$(git rev-parse --short HEAD); D=$(date +%F)
echo "部署 $C ($D)"

echo "== 1. 测试 + vet =="
if ! go vet ./... || ! go test ./... > /tmp/deploy-fleet-test.log 2>&1; then
  tail -20 /tmp/deploy-fleet-test.log; echo "FAIL: 测试未过"; exit 1
fi
echo "ok"

echo "== 2. 建 4 二进制（ldflags 版本戳） =="
L="-X main.buildCommit=$C -X main.buildDate=$D"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$L" -o /tmp/ousheng-linux ./cmd/ousheng
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$L" -o /tmp/ousheng-mcp-linux ./cmd/mcp
go build -ldflags "$L" -o "$BIN_DIR_LOCAL/ousheng" ./cmd/ousheng
go build -ldflags "$L" -o "$BIN_DIR_LOCAL/ousheng-mcp" ./cmd/mcp
BASE_OS=$(md5 -q /tmp/ousheng-linux); BASE_MCP=$(md5 -q /tmp/ousheng-mcp-linux)
echo "基准 md5: ousheng=$BASE_OS ousheng-mcp=$BASE_MCP"

echo "== 3. 部署三台 Linux（tmp+mv 原子） =="
for m in "${MACHINES[@]}"; do
  scp -q /tmp/ousheng-linux root@192.168.1.$m:$LINUX_BIN/ousheng.new
  scp -q /tmp/ousheng-mcp-linux root@192.168.1.$m:$LINUX_BIN/ousheng-mcp.new
  ssh -o BatchMode=yes root@192.168.1.$m "mv $LINUX_BIN/ousheng.new $LINUX_BIN/ousheng && mv $LINUX_BIN/ousheng-mcp.new $LINUX_BIN/ousheng-mcp && chmod +x $LINUX_BIN/ousheng $LINUX_BIN/ousheng-mcp"
  echo "  $m 已替换"
done

echo "== 4. 验证 =="
FAIL=0
for m in "${MACHINES[@]}"; do
  ip=192.168.1.$m
  V=$(ssh -o BatchMode=yes root@$ip "ousheng version | tail -1 && md5sum $LINUX_BIN/ousheng $LINUX_BIN/ousheng-mcp")
  VER=$(echo "$V" | head -1)
  OS=$(echo "$V" | sed -n '2p' | cut -d' ' -f1); MCP=$(echo "$V" | sed -n '3p' | cut -d' ' -f1)
  ok="OK"; [[ "$VER" == *"commit $C"* ]] || { ok="FAIL(版本)"; FAIL=1; }
  [[ "$OS" == "$BASE_OS" && "$MCP" == "$BASE_MCP" ]] || { ok="FAIL(md5)"; FAIL=1; }
  echo "  $m [$ok] $VER"
  for s in $(seats_of $m); do
    if [[ $REFRESH_SEATS == 1 ]]; then
      A=$(ssh -o BatchMode=yes root@$ip "sed -n 's/.*\"actor\": *\"\([^\"]*\)\".*/\1/p' $s/.opencode/ousheng.json 2>/dev/null" || true)
      ARGS=""; [[ -n "$A" ]] && ARGS="--actor $A"
      ssh -o BatchMode=yes root@$ip "ousheng adapter install --dir $s $ARGS" > /dev/null
    fi
    DOK=$(ssh -o BatchMode=yes root@$ip "cd $s && ousheng doctor | tail -1")
    sok="OK"; [[ "$DOK" == *"全部通过"* ]] || { sok="FAIL"; FAIL=1; }
    echo "    座位 $s [$sok] $DOK"
  done
  ws=$(ws_of $m)
  WL=$(ssh -o BatchMode=yes root@$ip "git -C $ws fetch -q; git -C $ws rev-list --left-right --count origin/main...HEAD" | tr -s ' \t' '/')
  wok="OK"; [[ "$WL" == "0/0" ]] || wok="WARN"
  echo "    工作区 $ws 水位 $WL [$wok]"
done

# 本机
VER=$("$BIN_DIR_LOCAL/ousheng" version | tail -1)
lok="OK"; [[ "$VER" == *"commit $C"* ]] || { lok="FAIL(版本)"; FAIL=1; }
echo "  本机 [$lok] $VER"
if [[ $REFRESH_SEATS == 1 ]]; then
  A=$(sed -n 's/.*"actor": *"\([^"]*\)".*/\1/p' "$SEAT_LOCAL/.opencode/ousheng.json" 2>/dev/null || true)
  ARGS=""; [[ -n "$A" ]] && ARGS="--actor $A"
  (cd "$SEAT_LOCAL" && ousheng adapter install $ARGS > /dev/null)
fi
DOK=$(cd "$SEAT_LOCAL" && ousheng doctor | tail -1)
sok="OK"; [[ "$DOK" == *"全部通过"* ]] || { sok="FAIL"; FAIL=1; }
WL=$(git -C "$SEAT_LOCAL" fetch -q 2>/dev/null; git -C "$SEAT_LOCAL" rev-list --left-right --count origin/main...HEAD | tr -s ' \t' '/')
wok="OK"; [[ "$WL" == "0/0" ]] || wok="WARN"
echo "    座位 本机 [$sok] $DOK / 水位 $WL [$wok]"

echo "== 5. 总结 =="
if [[ $FAIL == 0 ]]; then
  echo "部署完成：$C 三机一致 + 本机 + 验证全过"
else
  echo "FAIL：存在失败项，见上"; exit 1
fi
