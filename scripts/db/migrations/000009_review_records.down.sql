-- BraceSync T130 复查记录表回滚（golang-migrate down）

BEGIN;

DROP INDEX IF EXISTS idx_review_records_patient;
DROP TABLE IF EXISTS review_records CASCADE;

-- 还原 files 表 file_type CHECK 约束（移除 review_report）
ALTER TABLE files DROP CONSTRAINT IF EXISTS files_file_type_check;
ALTER TABLE files ADD CONSTRAINT files_file_type_check
    CHECK (file_type IN ('signature','install_photo','comm_photo','log_photo'));

COMMIT;
