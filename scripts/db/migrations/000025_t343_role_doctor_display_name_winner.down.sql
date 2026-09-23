-- T343 down：把 ROLE_DOCTOR 的显示名退回旧名「医生」
-- 只回滚 name 一列；role_id / permissions_json / status 本迁移 up 就没碰，down 也不碰。
-- ⚠ seed.sql:10 现已是新名 '医护'，但 seed 是 ON CONFLICT DO NOTHING ⇒ 跑过 down 再重放 seed
--   也不会把名字前滚回去（既有行不更新），回滚结果稳定。
-- ⚠ 若运营在库里自行改过该角色显示名，本 down 会一并覆盖成 '医生'（要保自定义值请先备份该行）。

BEGIN;

UPDATE roles
   SET name = '医生'
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
