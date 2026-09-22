-- T314 医护账号登录账号发号器：DB 序列 doctor_username_seq
-- 现象：PRD §7D.10（3）要求「服务端生成登录账号 doc+5 位序号」，但全仓没有任何序号发号设施
--   （现有 ID 全是随机 hex：newTechID/newPatientID/newRoleID），doctors/admins 也没有序列。
-- 根因：设计稿 医生账号.html 里的 nextAcct() 用 doctors.length + 1 占位 ⇒ 前端本地数组长度
--   既不受并发保护也不与库里已占用的 username 对账；admins.username 有 UNIQUE（000001:30），
--   多实例/连点两次创建必然撞 23505 或产生重号。PRD/T316 已明令实现侧不得照抄。
-- 修法：发号交给 DB 序列（nextval 自带行锁，跨事务/跨实例唯一），repo 层在创建事务内取值，
--   拼成 doc%05d；仍保留 23505 有限重试，兜住「序号被历史人工账号占掉」的窗口。
-- START 起点：不从 1 硬起，先扫库里已存在的 ^doc[0-9]{5}$ 用户名取最大值 +1
--   ⇒ seed 链（ops_admin/doctor_li/cs_wang，均非 doc 格式）与人工造过的号都不会撞。
-- owner：admins.username（user-service 写）；down 只 drop 序列，不动任何 admins 行。

BEGIN;

DO $$
DECLARE
  base BIGINT;
BEGIN
  SELECT COALESCE(MAX(substring(username FROM 4 FOR 5)::BIGINT), 0)
    INTO base
    FROM admins
   WHERE username ~ '^doc[0-9]{5}$';

  EXECUTE format('CREATE SEQUENCE IF NOT EXISTS doctor_username_seq START %s', base + 1);
END $$;

COMMENT ON SEQUENCE doctor_username_seq IS
  'T314 医护登录账号序号：nextval 后拼成 doc%05d 写入 admins.username（PRD §7D.10（3），禁 count+1）';

COMMIT;
