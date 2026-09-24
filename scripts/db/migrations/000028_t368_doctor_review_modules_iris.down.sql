-- T368 down：把 ROLE_DOCTOR 的 modules 退回补键前的 4 项（去掉 review / review_tpl）
-- 退回的正是 000017 与旧 seed 播的那一份，逐字一致 ⇒ up/down 对称可由集成用例钉住
--   （services/user-service/internal/repo/t368_integration_test.go：读现值 → 跑 down → 4 项 → 再跑 up → 6 项）。
-- 只回滚 modules 一个键；scope 与 items 本迁移 up 就没动，down 也不动。
-- ⚠ seed.sql 的预置角色插入块现已是 6 项，但 seed 在 role_id 冲突时跳过 ⇒ 跑过 down 再重放 seed
--   也不会把 modules 前滚回 6（既有行不插入），回滚结果稳定。
-- ⚠ 回滚后 T368 的越权读取面重新出现（侧栏 6 项 vs 库 4 项），属预期：那是回滚本裁定，不是回滚一个修复。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","alerts","orthosis"]'::jsonb
       )
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
