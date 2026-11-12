#!/usr/bin/env bash
# board-unify.sh —— 板归一迁移 v3（2026-11-12，档案 §38，pm 拍板：一个看板、单序列发号）
# 号段分区提议已撤回（pm 2026-11-12）：单板单发号器结构性地永不撞号；
# 离线双写由 sync diverged 检测兜底（档案 §16 已锁）。号段自动化判据 =
# 真实离线双写事故实证，记档再议。
#
# 分诊（v2 教训：撞号 245 ≠ 真撞号 245——分叉前共同遗产同 ID 同内容）：
#   遗产（同ID同内容）→ 不动
#   真撞号（同ID双版本）→ 基准版（aliyun）保留原 ID；合入版（local）重编号
#     入新段（基准侧全局最大序号 +1 递增）+ env: local 打标
#   仅基准 → 不动（原 ID 原段）
#   仅合入 → 原样入板 + env: local 打标
#
# 用法: board-unify.sh <基准板=aliyun侧> <合入板=local侧>
# DRY=1 只出分诊报告不写。
set -euo pipefail
BASE=$1; MERGE=$2
[ -d "$BASE/.ousheng" ] && [ -d "$MERGE/.ousheng" ] || { echo "usage: board-unify.sh <base=aliyun> <merge=local>"; exit 2; }
DRY=${DRY:-0}
B="$BASE/.ousheng"; M="$MERGE/.ousheng"

ids() { ls "$1/work" 2>/dev/null | sed 's/\.yaml$//' | sort; }
addenv() { local f=$1 e=$2; if ! grep -q '^env:' "$f"; then awk -v e="$e" '/^id:/{print; print "env: " e; next} {print}' "$f" > "$f.tmp" && mv "$f.tmp" "$f"; fi; }

# ① 分诊
HERIT=""; CLASH=""; BASEONLY=""; MERGEONLY=""
for id in $(comm -12 <(ids "$B") <(ids "$M")); do
  if diff -q "$B/work/$id.yaml" "$M/work/$id.yaml" >/dev/null 2>&1; then HERIT="$HERIT $id"; else CLASH="$CLASH $id"; fi
done
BASEONLY=$(comm -23 <(ids "$B") <(ids "$M"))
MERGEONLY=$(comm -13 <(ids "$B") <(ids "$M"))
NH=$(echo $HERIT | wc -w | tr -d ' '); NC=$(echo $CLASH | wc -w | tr -d ' ')
NB=$(echo "$BASEONLY" | wc -w | tr -d ' '); NM=$(echo "$MERGEONLY" | wc -w | tr -d ' ')
echo "== 分诊：遗产 $NH（不动）| 真撞号 $NC（合入侧重编号）| 仅基准 $NB（不动）| 仅合入 $NM（原样入）"
echo "真撞号: $CLASH"
echo "仅合入: $(echo $MERGEONLY | tr '\n' ' ')"
[ "$DRY" = 1 ] && { echo "DRY=1 止于分诊"; exit 0; }

mkdir -p "$B/migration"
: > "$B/migration/id-map.tsv"

# ② 新段基点 = 基准侧全局最大序号 +1
MAXN=$(ids "$B" | grep -oE '[0-9]+$' | sort -n | tail -1)
NEWID() { MAXN=$((MAXN+1)); echo "$1" | awk -v n="$MAXN" -F- '{printf "%s-%03d\n", $1, n}'; }

# ③ 真撞号：合入版重编号入板（引用改写限本卡内部——入板卡引用同批重编号 ID 才改）
# 先建全量 map（MAXN 行内递增——命令替换子壳会丢计数，首跑实锤 23 张全映射 166）
for id in $CLASH; do
  MAXN=$((MAXN+1))
  echo -e "$id\t$(echo "$id" | awk -v n="$MAXN" -F- '{printf "%s-%03d\n", $1, n}')" >> "$B/migration/id-map.tsv"
done
for id in $CLASH; do
  NEW=$(awk -F'\t' -v i="$id" '$1==i{print $2}' "$B/migration/id-map.tsv")
  cp "$M/work/$id.yaml" "$B/work/$NEW.yaml"
  while IFS=$'\t' read -r old new; do sed -i "s/\b$old\b/$new/g" "$B/work/$NEW.yaml"; done < "$B/migration/id-map.tsv"
  addenv "$B/work/$NEW.yaml" local
  echo "  真撞号入板: $id → $NEW (env=local；基准版保留原 ID)"
done

# ④ 仅合入：原样入板
for id in $MERGEONLY; do
  cp "$M/work/$id.yaml" "$B/work/$id.yaml"
  addenv "$B/work/$id.yaml" local
  echo "  仅合入入板: $id (env=local)"
done

# ⑤ 注册表以基准为准（已验证：actors 8/8 同名、systems 相同、assignments 基准为超集——零合并）
echo "== 完成。后续：①cd $BASE && ousheng converge --dir . 零 BLOCKER ②git add -A && git commit -m 'board: unify + local 侧重编号入板（档案 §38）' ③force push 换干 gitee main（旧 main 已保 board/local-archive）④发号=单序列 max+1（无段规则，pm 2026-11-12 撤号段提议）"
