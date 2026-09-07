#!/usr/bin/env bash
# Claude Code SessionStart hook — OuSheng v0.3 查看时机协议 · 时机①（每日/session 启动）。
# 动作：git pull + 索引刷新 + 项目概览 +（可选）我的上下文。
# 安装：从 settings.json 引用，或拷贝到 .claude/hooks/session-start.sh
# 环境变量：
#   OUSHENG_DIR    workspace 根目录（含 .ousheng/，默认 .）
#   OUSHENG_ACTOR  当前 agent 的 actor id（设置后额外输出 get_my_context）
set -euo pipefail

WS_DIR="${OUSHENG_DIR:-.}"
OUSHENG_BIN="${OUSHENG_BIN:-ousheng}"

# v0.3 工作区优先
if [ -d "$WS_DIR/.ousheng" ]; then
  if command -v "$OUSHENG_BIN" &>/dev/null; then
    echo "=== OuSheng Engineering Context (session start) ==="
    if [ -n "${OUSHENG_ACTOR:-}" ]; then
      "$OUSHENG_BIN" sync --actor "$OUSHENG_ACTOR" --dir "$WS_DIR" 2>/dev/null || \
        "$OUSHENG_BIN" sync --dir "$WS_DIR" 2>/dev/null || echo "(ousheng sync failed)"
    else
      "$OUSHENG_BIN" sync --dir "$WS_DIR" 2>/dev/null || echo "(ousheng sync failed)"
    fi
    echo "=== End OuSheng Context ==="
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

echo "=== OuSheng Board State ==="
"$BOARD_BIN" read --dir "$BOARD_DIR" 2>/dev/null || echo "(board read failed)"
echo "=== End Board State ==="
