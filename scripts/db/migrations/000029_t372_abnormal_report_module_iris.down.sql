-- T372 down：把两个角色的 modules 退回补键前那份 —— ROLE_ADMIN 15 项、ROLE_DOCTOR 6 项（均去掉 abnormal_report）
-- 退回的正是 000026 / 000028 的终态，逐字一致 ⇒ up/down 对称可由集成用例钉住
--   （services/user-service/internal/repo/t372_integration_test.go：读现值 → 跑 down → 15 / 6 → 再跑 up → 16 / 7）。
-- 只回滚 modules 一个键；scope 与 items 本迁移 up 就没动，down 也不动。ROLE_CS 全程不在本迁移内。
-- ⚠ seed.sql 的预置角色插入块现已是 16 / 7 项，但 seed 在 role_id 冲突时跳过 ⇒ 跑过 down 再重放 seed
--   也不会把 modules 前滚（既有行不插入），回滚结果稳定。
-- ⚠ 回滚后前端侧栏仍会显示「异常报告」（PAGE_MODULES 是 16 项）—— 页面准入以前端矩阵为准，
--   库里少这一键只影响 /admin/me/permissions 的返回值与运营自定义角色，不影响 admin 进页。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","patients","teams","devices","alerts","comm","orthosis","install","review","review_tpl","tech","doctor_acct","perm","config"]'::jsonb
       )
 WHERE role_id = 'ROLE_ADMIN';

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","alerts","orthosis","review","review_tpl"]'::jsonb
       )
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
