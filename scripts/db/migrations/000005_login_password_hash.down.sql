-- T037: 回滚 technicians/patients password_hash 列

BEGIN;

ALTER TABLE technicians DROP COLUMN IF EXISTS password_hash;
ALTER TABLE patients    DROP COLUMN IF EXISTS password_hash;

COMMIT;
