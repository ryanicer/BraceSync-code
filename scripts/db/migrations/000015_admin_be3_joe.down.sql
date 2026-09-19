-- T256 后端第3批（Joe）回滚
BEGIN;

ALTER TABLE feeling_logs DROP COLUMN IF EXISTS comfort_level;

DELETE FROM sys_configs
 WHERE config_key IN ('collect_interval_seconds', 'data_retention_days', 'max_patients');

COMMIT;
