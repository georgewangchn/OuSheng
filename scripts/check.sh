#!/usr/bin/env bash
# check.sh —— 产品仓全检的单一入口（收尾/发版前跑一次；档案 §57）。
#
# 内容：gofmt（仅改动/新增的 .go——历史文件本就不齐，不翻旧账）+ go vet +
#       go test ./...（含裁决账本与吞错锁）+ S7 四剧本。
# 日常每次改动仍只跑 go test ./... && go vet ./...（AGENTS.md 验证命令）；
# 触 state 面的推送由 .githooks/pre-push 兜底（git config core.hooksPath .githooks）。
# 剧本日志落 /tmp/ousheng-check-<剧本>.log，失败不删现场。
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1
fail=0

echo "== gofmt（改动/新增的 .go——只拦新引入的不齐，历史欠账不翻）"
for f in $(git ls-files -m -o --exclude-standard -- '*.go'); do
  [ -z "$(gofmt -l "$f")" ] && continue            # 现在就齐
  if git cat-file -e "HEAD:$f" 2>/dev/null && [ -n "$(git show "HEAD:$f" | gofmt -l)" ]; then
    continue                                        # HEAD 就不齐——旧账，不在本改动的责任面
  fi
  echo "gofmt 未过（新引入）: $f"
  fail=1
done

echo "== go vet ./..."
go vet ./... || fail=1

echo "== go test ./..."
go test ./... || fail=1

echo "== S7 四剧本"
for s in scripts/dogfood-s7.sh scripts/dogfood-s7-chaos.sh scripts/dogfood-s7-chaos2.sh scripts/dogfood-s7-design.sh; do
  log="/tmp/ousheng-check-$(basename "$s").log"
  if bash "$s" >"$log" 2>&1; then
    echo "ok   $s"
  else
    echo "FAIL ${s}（日志 ${log}）"
    fail=1
  fi
done

if [ "$fail" = "0" ]; then echo "== check 全绿"; else echo "== check 有红"; fi
exit "$fail"
