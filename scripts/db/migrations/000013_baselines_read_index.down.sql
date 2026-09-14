-- BraceSync T173 baselines 读取索引回滚（golang-migrate down）
BEGIN;

DROP INDEX IF EXISTS idx_baselines_device;

COMMIT;
