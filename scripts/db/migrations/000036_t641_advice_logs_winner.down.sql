-- T641 回滚：删 advice_logs
-- 新表无下游引用（没有任何外键指向 advice_id），索引随表一起消失，DROP 即可。
BEGIN;

DROP TABLE IF EXISTS advice_logs;

COMMIT;
