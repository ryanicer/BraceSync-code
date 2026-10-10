#!/usr/bin/env bash
# T627 方案乙 · 存量技师归属刷数（把挂在医护团队上的技师搬到维护班组）
#
# 用法：
#   bash scripts/db/backfill/t627_team_type_backfill.sh                     # 默认 dry-run，纯只读
#   MAP=映射.csv DB_URL=... T627_CONFIRM_APPLY=yes \
#     bash scripts/db/backfill/t627_team_type_backfill.sh --apply           # 执行
#
# 映射.csv：两列「tech_id,target_team_id」，首行表头会被跳过；每行是「这个技师搬到哪个维护班组」。
#   为什么必须由人给这张表：技师搬进哪一个班组是运营归属决定，不是数据推导——脚本能把「不该出现的状态」
#   归零，但替谁选班组不在本卡授权范围内。没有这张表本脚本一律不动库。
#
# 两条红线（卡面第六节）：生产零写、staging seed 只读。
#   所以 --apply 除了 MAP 还必须有 T627_CONFIRM_APPLY=yes，两个条件缺一个就退 2；
#   dry-run 不写任何东西，可以在任何库上跑（含现网只读）。
set -euo pipefail

DB_URL="${DB_URL:-postgres://bracesync:bracesync@localhost:5432/bracesync?sslmode=disable}"
MODE="${1:-dry-run}"
MAP="${MAP:-}"

q() { psql "$DB_URL" -v ON_ERROR_STOP=1 -At -c "$1"; }

# 存量面：技师按「当前所属团队的类型」分布。team_type 为 unknown = 团队行不在场（脏归属），
# 为 NULL = 该技师没有团队（合法，本脚本不动它）。这三形分开打，不合成一个「有问题 N 条」。
print_counts() {
  echo "== 技师归属分布（按所属团队 team_type）=="
  q "SELECT COALESCE(tm.team_type, '(无团队)') AS side, COUNT(*)
       FROM technicians t
       LEFT JOIN teams tm ON tm.team_id = t.team_id
       GROUP BY 1 ORDER BY 1"
  echo "-- 其中挂在医护团队上的（本脚本要搬的那一撮）--"
  q "SELECT COUNT(*) FROM technicians t
       JOIN teams tm ON tm.team_id = t.team_id WHERE tm.team_type = 'medical'"
  echo "-- 团队行不在场的脏归属（外键理论上挡住，出现即报，不当 0 处理）--"
  q "SELECT COUNT(*) FROM technicians t
       LEFT JOIN teams tm ON tm.team_id = t.team_id
       WHERE t.team_id IS NOT NULL AND t.team_id <> '' AND tm.team_id IS NULL"
  echo "== 现有维护班组 =="
  q "SELECT team_id, name FROM teams WHERE team_type = 'maintenance' ORDER BY team_id"
}

if [ "$MODE" != "--apply" ]; then
  echo "[dry-run] 只读，不写库。DB_URL 的库名=$(q "SELECT current_database()")"
  print_counts
  echo "[dry-run] 结束。执行需 MAP=<csv> 与 T627_CONFIRM_APPLY=yes，且按卡面第六节须 Boss 批准的单独窗口。"
  exit 0
fi

if [ -z "$MAP" ] || [ ! -f "$MAP" ]; then
  echo "拒执行：缺 MAP（映射.csv）或文件不存在。没有逐行归属表，本脚本不猜。" >&2
  exit 2
fi
if [ "${T627_CONFIRM_APPLY:-}" != "yes" ]; then
  echo "拒执行：--apply 必须同时带 T627_CONFIRM_APPLY=yes（生产零写 / 现网刷数须单独批准窗口）。" >&2
  exit 2
fi

# 把映射读进临时表：整段在一个事务里，任一校验不过就 ROLLBACK，不留半张页。
psql "$DB_URL" -v ON_ERROR_STOP=1 -q <<SQL
BEGIN;
CREATE TEMP TABLE t627_map (tech_id text primary key, target_team_id text not null) ON COMMIT DROP;
\copy t627_map(tech_id,target_team_id) FROM '$MAP' WITH (FORMAT csv, HEADER true)

-- 1. 映射里的技师必须存在（写错 ID 不能静默跳过）
DO \$\$
DECLARE n int;
BEGIN
  SELECT COUNT(*) INTO n FROM t627_map m LEFT JOIN technicians t ON t.tech_id = m.tech_id
    WHERE t.tech_id IS NULL;
  IF n > 0 THEN RAISE EXCEPTION 'T627: 映射里有 % 行的 tech_id 在库中不存在', n; END IF;
END \$\$;

-- 2. 目标班组必须存在且是 maintenance（把技师搬回医护团队就是本卡要消灭的那个状态）
DO \$\$
DECLARE n int;
BEGIN
  SELECT COUNT(*) INTO n FROM t627_map m LEFT JOIN teams tm ON tm.team_id = m.target_team_id
    WHERE tm.team_id IS NULL OR tm.team_type <> 'maintenance';
  IF n > 0 THEN RAISE EXCEPTION 'T627: 映射里有 % 行的目标不是维护班组（不存在或非 maintenance）', n; END IF;
END \$\$;

-- 3. 反向闭合：所有挂在医护团队上的技师都必须在映射里，否则搬完仍留存量、读数继续混着。
--    这一条是 fail-closed 的落点——宁可拒执行，也不做「搬一半、剩下不说」。
DO \$\$
DECLARE n int;
BEGIN
  SELECT COUNT(*) INTO n FROM technicians t
    JOIN teams tm ON tm.team_id = t.team_id
    WHERE tm.team_type = 'medical' AND t.tech_id NOT IN (SELECT tech_id FROM t627_map);
  IF n > 0 THEN RAISE EXCEPTION 'T627: 还有 % 名技师挂在医护团队且映射表里没有它', n; END IF;
END \$\$;

UPDATE technicians t
  SET team_id = m.target_team_id
  FROM t627_map m
  WHERE t.tech_id = m.tech_id;
COMMIT;
SQL

echo "[apply] 写后复点（应与 dry-run 同一条尺，medical 那一行必须消失）："
print_counts
