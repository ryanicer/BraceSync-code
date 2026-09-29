-- T487 down：撤掉 admins 的手机号两列与部分唯一索引
--
-- 顺序与 up 对称：先 DROP INDEX（索引依赖列），再 DROP COLUMN。两条都带 IF EXISTS，
-- 因为 down 也可能作用在「up 从未执行过」的库上（本地往返验证、或回滚到 000031 之前的分支库）。
--
-- 数据面：DROP COLUMN 会把 up 之后录入的后台账号手机号一起删掉 —— 这是回滚的应有语义，
-- 但要写明后果：那些医护此后只能回到用户名+密码登录（用户名从未变过，不会出现「谁都登不进」）。
-- 不做「先把手机号搬回 doctors 再删列」的保数据回滚：doctors 那两列本来就一直在写、
-- 是本卡的读侧展示源（handler.go toDoctorDTO 的 PhoneMasked）， admins 侧只是登录副本，删了不丢档案。

BEGIN;

DROP INDEX IF EXISTS uk_admins_phone_hash;

ALTER TABLE admins DROP COLUMN IF EXISTS phone_hash;
ALTER TABLE admins DROP COLUMN IF EXISTS phone_enc;

COMMIT;
