-- BraceSync T151(2b) 患者 status 默认值回滚（golang-migrate down）
BEGIN;

ALTER TABLE patients ALTER COLUMN status SET DEFAULT 'pending';

COMMIT;