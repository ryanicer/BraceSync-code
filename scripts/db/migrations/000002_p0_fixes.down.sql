-- BraceSync P0 问题修复迁移回滚（golang-migrate down）

BEGIN;

-- P0-4：回滚 birth_date 列
ALTER TABLE patients DROP COLUMN IF EXISTS birth_date;

-- P0-3：回滚 UNIQUE 约束
ALTER TABLE install_records DROP CONSTRAINT IF EXISTS uk_install_baseline;

-- P0-2：回滚 device_bindings 表
DROP TABLE IF EXISTS device_bindings;

COMMIT;
