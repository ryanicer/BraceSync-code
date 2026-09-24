-- T366 down：丢「聚合印章」两列
-- 本迁移 up 不动数据（只加可空列），故 down 也无需回滚任何行：丢列即完全对称。
-- ⚠ 丢列会连带丢掉「哪些行是聚合任务写的」这一层信息（印章只存在这两列里）。
--    回滚后读侧 provenance 全部退化为按 frame_count 与明细比对的两值判定
--    （rollup 档不再可能命中）—— 这是回滚的既定后果，不是缺陷。

BEGIN;

ALTER TABLE daily_wear_stats DROP COLUMN IF EXISTS wearing_threshold_n;
ALTER TABLE daily_wear_stats DROP COLUMN IF EXISTS aggregated_at;

COMMIT;
