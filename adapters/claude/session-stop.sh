#!/usr/bin/env bash
# Claude Code Stop hook — OuSheng v0.3 查看时机协议 · 时机③（任务结束收尾检查）。
# 动作：收敛检查 + 收尾提醒（本 session 若改过 WorkItem/契约，先写入再看板）。
set -euo pipefail

WS_DIR="${OUSHENG_DIR:-.}"
OUSHENG_BIN="${OUSHENG_BIN:-ousheng}"

# v0.3 工作区优先
if [ -d "$WS_DIR/.ousheng" ]; then
  if command -v "$OUSHENG_BIN" &>/dev/null; then
    echo "=== OuSheng Convergence Check ==="
    RESULT=$("$OUSHENG_BIN" converge --dir "$WS_DIR" 2>/dev/null || true)
    if [ -n "$RESULT" ]; then
      echo "$RESULT"
      case "$RESULT" in
        CONVERGED)
          echo "All work items done. Clean state."
          ;;
        BLOCKED*)
          echo "WARNING: workspace is BLOCKED — explicit blocked item, dangling dependency, cycle, or missing evidence/ack. Human intervention needed."
          ;;
        IN_PROGRESS*)
          echo "REMINDER: workspace is IN_PROGRESS. If you changed work items this session, run 'ousheng work update' / 'ousheng progress report' / 'ousheng evidence add' before ending, then 'ousheng context me' to re-read the board."
          ;;
      esac
    fi
    echo "=== End Convergence Check ==="
    exit 0
  fi
fi

# v1 board 兼容回退
BOARD_DIR="${OUSHENG_BOARD_DIR:-.ousheng}"
BOARD_BIN="${OUSHENG_BOARD_BIN:-board}"

if [ ! -d "$BOARD_DIR" ]; then
  exit 0
fi

if ! command -v "$BOARD_BIN" &>/dev/null; then
  exit 0
fi

RESULT=$("$BOARD_BIN" converge --dir "$BOARD_DIR" 2>/dev/null || true)

if [ -z "$RESULT" ]; then
  exit 0
fi

STATUS=$(echo "$RESULT" | grep -o 'status: [A-Z_]*' | head -1 | cut -d' ' -f2)

echo "=== OuSheng Convergence Check ==="
echo "$RESULT"

case "$STATUS" in
  CONVERGED)
    echo "All contracts verified. Clean state."
    ;;
  STUCK)
    echo "WARNING: Board is STUCK. Dependency cycle or broken dependency. Human intervention needed."
    ;;
  IN_PROGRESS)
    echo "REMINDER: Board is IN_PROGRESS. If you changed contracts this session, run 'board write' before ending."
    ;;
esac
echo "=== End Convergence Check ==="
