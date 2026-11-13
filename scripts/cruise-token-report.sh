#!/usr/bin/env bash
# cruise-token-report.sh —— 巡航 token 晨检（§37 复盘 #3 数据先行，2026-11-13）
# journal × opencode.db join：每座位 轮数/session 数/token 三线——per-round 曲线定减面。
# 用法: ./cruise-token-report.sh [小时数=11]   （在巡航机本机跑，需 root 读 db）
set -euo pipefail
H=${1:-11}
DB=/root/.local/share/opencode/opencode.db
python3 - "$H" "$DB" <<'EOF'
import sqlite3, subprocess, datetime, sys
hours, db = sys.argv[1], sys.argv[2]
now = datetime.datetime.now()
lo_ms = int((now - datetime.timedelta(hours=int(hours))).timestamp()*1000)
c = sqlite3.connect(f'file:{db}?mode=ro', uri=True)
print(f'== 巡航 token 晨检（近 {hours}h）==')
print(f'{"座位":<8}{"session":>8}{"轮(journal)":>12}{"压缩比":>8}{"input(M)":>10}{"cache(M)":>10}{"out(K)":>8}')
for d, label in [('/data/tide/data','data'),('/data/tide/lhcp','lhcp'),('/data/tide/mlake','mlake'),('/data/tide/ui','ui'),('/data/tide/pm','pm')]:
    row = c.execute('''select count(*), coalesce(sum(tokens_input),0), coalesce(sum(tokens_cache_read),0), coalesce(sum(tokens_output),0)
        from session_v2 where directory=? and time_created>=? and time_archived is null''',(d,lo_ms)).fetchone()
    n, ti, tc, to = row
    try:
        rounds = int(subprocess.run(['bash','-c',
            f"journalctl -u ousheng-cruise@{label} --since '-{hours} hours' --no-pager 2>/dev/null | grep -cE 'round [0-9]+ →' || true"],
            capture_output=True, text=True).stdout.strip() or 0)
    except Exception:
        rounds = 0
    comp = f'{rounds/max(n,1):.1f}x' if n else '—'
    print(f'{label:<8}{n:>8}{rounds:>12}{comp:>8}{ti/1e6:>10.1f}{tc/1e6:>10.1f}{to/1e3:>8.0f}')
print()
print('判读：压缩比≈轮/session（>2=续代生效）；单座位 cache/轮 = cache(M)/轮，data 型重上下文 >1.5M/轮 需减面')
EOF
