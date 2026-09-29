-- T487：admins 加手机号两列 + phone_hash 部分唯一索引，让医护账号能用手机号+密码登录后台
-- 派发单：docs/tasks/winner/T487-后台登录体验.md（Boss 2026-09-29 18:42 收窄口径）
--
-- 为什么登录侧非要在 admins 上存手机号（而不是复用 doctors.phone_hash）：
--   POST /api/v1/auth/login 整条链只碰 admins 一张表（handler.go:497 GetAdminByUsername →
--   bcrypt → admins.status → roles.scope），T314 建号注释把这个设计明确写在
--   repo/doctor_accounts_t314.go:8-11（「登录侧零改动」）。若在登录路径上 JOIN doctors，
--   就把「登录凭据」建在「档案表」上了：档案可以没有账号（seed D0002/D0003 的 admin_id 就是 NULL），
--   凭据却必须只在凭据表里可解析。故手机号作为**登录凭据**落 admins，doctors 那两列继续当档案。
--
-- 为什么可空、为什么不回填：
--   Boss 18:42 收窄口径第 1 条「管理员不开放手机号」（ops_admin 等 seed 写死账号通道不动），
--   派发单第 1 条明写「NULL 不限，存量账号=继续 username 登录」。回填会把 doctors 档案手机号
--   静默升格成登录凭据 —— 那是数据变更（可由他人用手机号重置/枚举到账号），属派发单红线
--   「staging seed 默认只读、改数据先报 PM」，本迁移不代做。
--
-- 为什么是「部分」唯一索引（WHERE phone_hash IS NOT NULL）：
--   全表 UNIQUE 在 PG 里不约束 NULL（NULL 彼此不算重复），但写成普通索引又完全没有唯一性。
--   部分唯一索引 = 有手机号的账号彼此唯一、没手机号的账号（全部管理员与存量行）不受约束。
--   同族先例：000012 uk_devices_active_patient（同一写法，注释里还留了建索引前的冲突预检要求）。
--
-- 建索引前的冲突预检（本迁移执行时必为 0 行，因为两列刚刚新建、全表 NULL）：
--   SELECT phone_hash FROM admins WHERE phone_hash IS NOT NULL
--     GROUP BY phone_hash HAVING COUNT(*) > 1;
--   ⇒ 新列恒 0 行；未来若有库在 up 之前被手工写过这两列并出冲突，CREATE UNIQUE INDEX 会
--     直接 23505 整事务失败（不是静默建成普通索引），宁可失败也不要无约束的重复号。
--
-- 列型与同族一致：phone_enc BYTEA（AES-256-GCM nonce||密文）、phone_hash CHAR(64)
--   （SHA-256 hex，见 user-service internal/phone/phone.go:131-135 与 technicians/patients 同名列）。

BEGIN;

ALTER TABLE admins ADD COLUMN IF NOT EXISTS phone_enc  BYTEA    NULL;
ALTER TABLE admins ADD COLUMN IF NOT EXISTS phone_hash CHAR(64) NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_admins_phone_hash
    ON admins (phone_hash)
    WHERE phone_hash IS NOT NULL;

COMMENT ON COLUMN admins.phone_enc IS
    'T487：后台账号登录手机号密文（AES-256-GCM，键 PHONE_ENC_KEY）。NULL = 该账号不开放手机号登录。'
    '仅医护账号写入，管理员账号按 Boss 18:42 口径恒 NULL。';

COMMENT ON COLUMN admins.phone_hash IS
    'T487：SHA-256(明文手机号) hex，登录双凭证第二支与查重键。口径同 technicians.phone_hash。';

COMMENT ON INDEX uk_admins_phone_hash IS
    'T487 部分唯一：仅约束非 NULL 手机号彼此唯一，NULL（管理员与存量账号）不受限。';

COMMIT;
