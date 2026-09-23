-- T343 预置登录角色 ROLE_DOCTOR 的显示名「医生」→「医护」（Boss 2026-09-22 14:21 裁定）
-- 现象：设计稿与 PRD 侧已全站收口（docs PRD V3.21「外溢收口状态订正」），但 staging 权限页
--   角色列表第三行、顶栏角色徽标仍显示旧名 —— 角色名是从库里 roles.name 读出来的，改文档收不了口。
-- 根因位：seed.sql:10 与 migration 000017:30 各把这行播成 '医生'。
--   🔴 000017 已入 main，按它自己第 7 行立的规矩「已入 main 的迁移不回头改」⇒ 本迁移正向纠正
--   （与 000022 纠正 000015、000021 纠正 000019 量纲 同一先例）。
--   ⚠ 只改 seed.sql 不够：seed 是 ON CONFLICT (role_id) DO NOTHING，对已建库的既有行不生效，
--     现网 / staging 上的旧名只有走 UPDATE 迁移才改得动。
-- 范围红线：**只动 name 这一列的中文显示值**。role_id 字面量 'ROLE_DOCTOR'、
--   permissions_json（scope=team + 四个 modules）、status 一字不动 —— 裁定只改称谓，改到键即越界。
-- 覆盖两条路径：空库（000017 刚补播该行）与已跑过 seed 的库（dev / staging / prod）都命中 1 行，
--   终态一致。
-- owner：roles 表属 user-service（与 000016 / 000017 同域）。

BEGIN;

UPDATE roles
   SET name = '医护'
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
