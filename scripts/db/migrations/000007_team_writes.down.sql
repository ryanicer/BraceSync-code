-- T059: 回滚 teams 表扩展列（leader/description/status）

BEGIN;

ALTER TABLE teams DROP COLUMN IF EXISTS leader;
ALTER TABLE teams DROP COLUMN IF EXISTS description;
ALTER TABLE teams DROP COLUMN IF EXISTS status;

COMMIT;
