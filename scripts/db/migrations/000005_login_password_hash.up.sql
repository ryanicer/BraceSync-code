-- T037: 技师/患者密码登录——password_hash 列添加
-- 对齐：docs/
-- 用途：technicians + patients 表各加 password_hash VARCHAR(60)（bcrypt），支撑手机号+密码登录

BEGIN;

ALTER TABLE technicians ADD COLUMN IF NOT EXISTS password_hash VARCHAR(60);
ALTER TABLE patients    ADD COLUMN IF NOT EXISTS password_hash VARCHAR(60);

COMMENT ON COLUMN technicians.password_hash IS 'bcrypt 密码哈希（T037 技师端登录）';
COMMENT ON COLUMN patients.password_hash    IS 'bcrypt 密码哈希（T037 患者端登录）';

COMMIT;
