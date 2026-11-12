#!/usr/bin/env bash
# board-unify.sh —— 板归一迁移脚本（2026-11-12，档案 §38，pm 拍板「一个看板」）
# 事故驱动一次性工具（§5 纪律）：A 机板与本地板 11-05 17:24 分叉（13cc1ef2），
# 七天独立发号已撞 ID（T-099/100/101 双侧不同内容）。归一 = 以一侧为基准，
# 另一侧撞号卡重编号后快照重放，env 随迁打标。
#
# 用法: ./board-unify.sh <基准板目录> <合入板目录> <合入侧env标>
#   基准板 = 保留原 ID 与全部历史的一侧（A 机板：2810 commits 主体）
#   合入板 = 撞号卡重编号 + 现状快照重放的一侧（本地板：68 commits）
#   env 标 = 合入卡打的 env 值（如 local；基准侧后续批量打 aliyun）
#
# 干跑: DRY=1 ./board-unify.sh ... （只出盘点报告不写）
set -euo pipefail
BASE=$1; MERGE=$2; ENVTAG=$3
[ -d "$BASE/.ousheng" ] && [ -d "$MERGE/.ousheng" ] || { echo "usage: board-unify.sh <base> <merge> <env-tag>"; exit 2; }
OUS=${OUSHENG_BIN:-ousheng}
DRY=${DRY:-0}

ids() { ls "$1/.ousheng/work" 2>/dev/null | sed 's/\.yaml$//' | sort; }

# ① 盘点：撞号 = 双侧同名 ID（内容必然不同——独立发号）
comm -12 <(ids "$BASE") <(ids "$MERGE") > /tmp/board-unify.collision
BASE_ONLY=$(comm -23 <(ids "$BASE") <(ids "$MERGE") | wc -l | tr -d ' ')
MERGE_ONLY=$(comm -13 <(ids "$BASE") <(ids "$MERGE") | wc -l | tr -d ' ')
echo "== 盘点：撞号 $(wc -l < /tmp/board-unify.collision | tr -d ' ') 张 | 仅基准 $BASE_ONLY | 仅合入 $MERGE_ONLY"
cat /tmp/board-unify.collision | sed 's/^/  撞号: /'
[ "$DRY" = 1 ] && { echo "DRY=1 止于盘点"; exit 0; }

# ② 合入侧：非撞号卡原 ID 快照重放（yaml 现状 + env 打标 + 迁移说明）
#    撞号卡重编号（原ID→新ID 段，映射表留档）；related/depends 引用同步改写
MAXN=$(ids "$BASE" | grep -oE '[0-9]+$' | sort -n | tail -1 | awk '{print $1+1000}')
next_id() { # 前缀保留，序号从 MAXN 递增（T-099 → T-2xxx 段，永不撞基准侧活跃段）
  echo "$1" | awk -v n="$MAXN" -F- '{n++; printf "%s-%04d\n", $1, n}' > /tmp/board-unify.nxt
  MAXN=$((MAXN+1)); cat /tmp/board-unify.nxt
}
mkdir -p "$MERGE/.ousheng/migration"
: > "$MERGE/.ousheng/migration/id-map.tsv"
while read -r mid; do
  [ -z "$mid" ] && continue
  NEW=$(next_id "$mid")
  echo -e "$mid\t$NEW" >> "$MERGE/.ousheng/migration/id-map.tsv"
  echo "  重编号: $mid → $NEW"
done < /tmp/board-unify.collision

echo "== 重放合入侧卡片到基准板（env=$ENVTAG）"
for f in "$MERGE/.ousheng/work"/*.yaml; do
  [ -e "$f" ] || continue
  ID=$(basename "$f" .yaml)
  NEWID=$(awk -F'\t' -v id="$ID" '$1==id{print $2}' "$MERGE/.ousheng/migration/id-map.tsv")
  TARGET=${NEWID:-$ID}
  cp "$f" "/tmp/board-unify.card.yaml"
  # 引用改写（related_to/depends_on/evidence locator 中的旧 ID）
  while IFS=$'\t' read -r old new; do
    sed -i '' -e "s/$old/$new/g" /tmp/board-unify.card.yaml 2>/dev/null || sed -i "s/$old/$new/g" /tmp/board-unify.card.yaml
  done < "$MERGE/.ousheng/migration/id-map.tsv"
  # env 打标（frontmatter env 字段；已显式 env 者保留）
  if ! grep -q '^env:' /tmp/board-unify.card.yaml; then
    sed -i '' -e 's/^system:/env: '"$ENVTAG"'\nsystem:/' /tmp/board-unify.card.yaml 2>/dev/null || sed -i 's/^system:/env: '"$ENVTAG"'\nsystem:/' /tmp/board-unify.card.yaml
  fi
  cp "/tmp/board-unify.card.yaml" "$BASE/.ousheng/work/$TARGET.yaml"
  echo "  重放: $ID → $TARGET (env=$ENVTAG)"
done

# ③ 注册表合并（actors/systems/roles：同名同义取并集，冲突人工清单）
echo "== 注册表并集（冲突留人工）"
for reg in actors systems.yaml roles.yaml assignments.yaml; do
  if [ -f "$MERGE/.ousheng/$reg" ] && [ ! -f "$BASE/.ousheng/$reg" ]; then
    cp "$MERGE/.ousheng/$reg" "$BASE/.ousheng/$reg"; echo "  整体迁入: $reg"
  fi
done
echo "== 完成。后续（人工）：①cd $BASE && converge 零 BLOCKER 验证 ②git add -A && git commit -m 'board: unify merge ($ENVTAG side)' ③force push 换干：git push -f <gitee> main（先在 gitee 侧把旧 main 打 board/local-archive 分支）④合入板仓降级归档（不再写）"
