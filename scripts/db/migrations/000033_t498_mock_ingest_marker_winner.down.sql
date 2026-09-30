-- T498 回滚：删掉来源印章两列（连带各自的 CHECK 约束随列一起消失）
--
-- 顺序与 up 相反；DROP COLUMN 在分区父表上会传播到全部子分区。
-- 🔴 本回滚会丢掉「哪些行是注入来的」这一信息本身，不会删任何数据行：
--    mock 注入产生的帧与告警在列被删后仍在库里，只是再也分不出来源。
--    若要在回滚前清掉注入数据，按列值先删（交件单里那条判据 4 的处置口径）：
--    DELETE FROM alerts           WHERE ingest_source = 'mock';
--    DELETE FROM pressure_records WHERE ingest_source = 'mock';
--    这两句属共享库写动作，须 PM/Boss 授权后由运维执行，不在本迁移内自动做。

BEGIN;

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_ingest_source_check;
ALTER TABLE alerts DROP COLUMN IF EXISTS ingest_source;

ALTER TABLE pressure_records DROP CONSTRAINT IF EXISTS pressure_records_ingest_source_check;
ALTER TABLE pressure_records DROP COLUMN IF EXISTS ingest_source;

COMMIT;
