#!/usr/bin/env bash
# Claude Code Stop hook — shows OuSheng convergence state at session end.
# Install: cp this to .claude/hooks/session-stop.sh (or reference from settings.json)
set -euo pipefail

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
