-- T345 down：把 ROLE_ADMIN 的 modules 退回补键前的 12 项（去掉 review / review_tpl / doctor_acct）
-- 只回滚 modules 一个键；scope 与 items 本迁移 up 就没动，down 也不动。
-- ⚠ seed.sql 的预置角色插入块现已是 15 项，但 seed 在 role_id 冲突时跳过 ⇒ 跑过 down 再重放 seed
--   也不会把 modules 前滚回 15（既有行不插入），回滚结果稳定。
-- ⚠ 若运营在权限页自行改过该角色的模块集，本 down 会一并覆盖成补键前的 12 项（要保留请先备份该行）。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","patients","teams","devices","alerts","comm",
           "orthosis","install","tech","perm","config"]'::jsonb
       )
 WHERE role_id = 'ROLE_ADMIN';

COMMIT;
