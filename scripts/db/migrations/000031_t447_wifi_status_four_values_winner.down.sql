-- T447 down：wifi_status 值域收回 000001 建表时的 2 值（connected / unconfigured）
-- 与 up 对称：先删 4 值约束，再按同名（install_records_wifi_status_check）建回 2 值约束。
--   名字一致不是巧合 —— 000001 里那条是列内联未命名 CHECK，PG 自动命名即为该串；
--   本地 PG14 实测 pg_constraint 只有这一条约束，up/down 往返后 conname+convalidated 与建库初态一致。
-- ⚠ 存量带新值时本迁移会**整事务失败**（23514），不是静默截断：
--   ADD CONSTRAINT 要对全表校验，库里只要有一行 wifi_status IN ('failed','skipped')，
--   这条 ADD 就报错回滚，约束保持 4 值不变。这是刻意的 ——
--   宁可回滚失败，也不要「回滚成功但脏值留在库里」，那样下次 up 又会因同样的脏值报错，
--   而中间这段时间读侧会把库里那个值原样投影出来（repo/query.go:152），前端拿到两档词表外的取值。
--   真要回滚，先按派发单口径把那两值清洗回 unconfigured（属数据变更，须先报 PM，本迁移不代做）。
-- 顺带清掉约束注释：注释属于 4 值口径的说明，留着会在回滚后误导下一个读 pg_description 的人。

BEGIN;

ALTER TABLE install_records DROP CONSTRAINT install_records_wifi_status_check;

ALTER TABLE install_records ADD CONSTRAINT install_records_wifi_status_check CHECK (
    wifi_status IN ('connected', 'unconfigured')
);

COMMENT ON CONSTRAINT install_records_wifi_status_check ON install_records IS NULL;

COMMIT;
