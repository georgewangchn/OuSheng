#!/usr/bin/env bash
# Claude Code SessionStart hook — injects OuSheng board state into session context.
# Install: cp this to .claude/hooks/session-start.sh (or reference from settings.json)
set -euo pipefail

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
