-- T314 down：撤销发号序列
-- ⚠ 只 DROP 序列，不回滚任何 admins/doctors 行 ⇒ 已用 doc%05d 建出的账号留在库里；
--   回到本迁移前重新 up 时，START 会按这些存量账号的最大序号继续，不会重号。
--   若代码侧回滚不彻底（仍 nextval 该序列）会报 42P01，属预期：序列与写入代码同进退。

BEGIN;

DROP SEQUENCE IF EXISTS doctor_username_seq;

COMMIT;
