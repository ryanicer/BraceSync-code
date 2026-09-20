-- T262 角色表收口（Winner）：撤销 000016 误播的 5 个角色，预置登录角色回到 3 个
-- 依据：**Boss 2026-09-20 11:21 裁定** ——
--   · 登录角色只 3 个：运营管理员 ROLE_ADMIN / 医生 ROLE_DOCTOR / 客服 ROLE_CS；
--   · 主任医师 / 主治医师 / 康复师 / 护士 = **医护职称**，走 doctors.title（000001 已建该列），不是角色；
--   · 设计稿 权限控制.html:100-104 那 5 个名字里，「超级管理员」= ROLE_ADMIN（功能重复），其余 4 个 = 职称。
-- 000016_admin_be2_winner.up.sql:44-55 按设计稿把 5 个名字都播成了角色 ⇒ 库里 8 个，本迁移纠正为 3 个。
-- 🔴 **不改 000016**（已入 main 的迁移不回头改），用本迁移正向纠正。
-- owner：roles 表属 user-service（与 000016 同域，配置与账号同库）。

BEGIN;

-- ── 1) 删除 5 个误播角色（语句与 000016_admin_be2_winner.down.sql:13-15 同源）──────────
-- ⚠️ 外键：admins.role_id → roles.role_id（000001_init_schema.up.sql:33）。
--    若已有运营账号被分配到这 5 个角色，DELETE 会被外键挡住 ⇒ 按 000016 down.sql 既有口径：
--    **由 DBA 先把成员转移到 ROLE_ADMIN / ROLE_DOCTOR / ROLE_CS，再执行本迁移**。
--    预期无引用：这 5 个角色至今**无登录身份** —— gateway 的 RBAC 矩阵只认 3 个字面量
--    （services/gateway/cmd/server/rbac.go:42-44），登录签发链路也不产出它们。
DELETE FROM roles
 WHERE role_id IN ('ROLE_SUPER_ADMIN', 'ROLE_CHIEF_DOCTOR', 'ROLE_ATTENDING_DOCTOR',
                   'ROLE_REHAB_THERAPIST', 'ROLE_NURSE');

-- ── 2) 补齐 3 个预置登录角色（内容与 scripts/db/seed/seed.sql:7-15 **逐字同源**）────────
-- 为什么迁移里也播一遍：集成测试 harness 只跑 migrations、**不跑 seed.sql**
-- （services/user-service/internal/repo/repo_integration_test.go:50-51 与其 seedITData:99-101
--   只播 ROLE_IT），只 DELETE 的话 CI 里 roles 表剩 0 条，验收「roles 表最终 = 3 条」无法在 CI 断言。
-- ON CONFLICT DO NOTHING ⇒ 对已跑过 seed.sql 的环境（dev / staging / prod）幂等、不改既有行。
INSERT INTO roles (role_id, name, description, permissions_json) VALUES
  ('ROLE_ADMIN', '运营管理员', '全量数据，无团队隔离',
    '{"scope":"all","modules":["dashboard","realtime","patients","teams","devices","alerts","comm","orthosis","install","tech","perm","config"]}'),
  ('ROLE_DOCTOR', '医生', '仅本团队患者数据',
    '{"scope":"team","modules":["dashboard","realtime","alerts","orthosis"]}'),
  ('ROLE_CS', '客服', '仅患者沟通模块，全量患者',
    '{"scope":"all_patients","modules":["comm"]}')
ON CONFLICT (role_id) DO NOTHING;

COMMIT;
