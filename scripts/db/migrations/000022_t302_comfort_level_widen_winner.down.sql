-- T302 down：列宽回到 VARCHAR(8) + 注释还原为 000015 原文
-- ⚠ 本迁移**会**在真有「不适」数据的库上报错退出（22001 value too long），这是刻意的：
--    宁可回滚失败、让人来处理数据，也不静默截断成 'discomfor' 把脏值写进枚举列。
--    若确需回滚：先人工把 discomfort 行清成 NULL 或改判 fitted，再跑 down（属改数据语义，需 Boss 确认）。
-- ⚠ 回填不撤销：up 里的 UPDATE 只把「NULL 且有 comfort_score」的行补成两档值，
--    回滚后无法区分哪些是本次补的 ⇒ down 不碰数据，只回列宽与注释。

BEGIN;

ALTER TABLE feeling_logs ALTER COLUMN comfort_level TYPE VARCHAR(8);

COMMENT ON COLUMN feeling_logs.comfort_level IS
    'T256 8.2 佩戴感受两档：fitted=贴合 / discomfort=不适。NULL=未评（与 comfort_score 同为可空）';

COMMIT;
