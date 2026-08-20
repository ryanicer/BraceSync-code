-- BraceSync file-service 文件元数据表回滚（T022，golang-migrate down）

BEGIN;

DROP INDEX IF EXISTS idx_files_owner;
DROP INDEX IF EXISTS idx_files_type;
DROP INDEX IF EXISTS idx_files_status;
DROP TABLE IF EXISTS files CASCADE;

COMMIT;
