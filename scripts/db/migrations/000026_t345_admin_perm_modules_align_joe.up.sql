-- T345 预置角色 ROLE_ADMIN 的 modules 补齐：12 项 → 15 项（与页面全集对齐）
-- 现象：权限控制页的「功能模块 × 角色」矩阵里，运营管理员一行有三格勾不上 ——
--   复查报告 / 复查模板管理 / 医护账号 三个页面早已上线（路由与网关都在），
--   但 roles.permissions_json.modules 从来没登记过它们的模块键。
-- 根因位：seed.sql 的预置角色插入块与 migration 000017 的预置角色插入块都把 ROLE_ADMIN 播成 12 项；
--   000017 已入 main，按它自己第 7 行立的规矩「已入 main 的迁移不回头改」⇒ 本迁移正向纠正
--   （先例：000025 纠正 000017 的角色名、000021/000022 纠正量纲）。
--   ⚠ 只改 seed.sql 不够：seed 在 role_id 冲突时跳过，对已建库（dev / staging / prod）的既有行不生效。
-- 连带口径：000017 第 22 行自称「内容与 seed.sql 预置角色块逐字同源」——自本迁移起不再成立
--   （000017 那份是 12 项，seed 已是 15 项），终态由本迁移统一钉到 15 项。
-- 连带口径：000017 第 22 行自称「内容与 seed.sql 预置角色块 **逐字同源**」——本迁移之后该句不再成立
--   （seed 15 项 / 000017 那份副本 12 项，副本按规矩不回头改），两边终态由本迁移统一到 15 项。
--   另：seed.sql 因本卡加了 6 行说明注释，旧文本里的 seed.sql:NN 行号引用整体 +6。
-- 词表：新增 review / review_tpl / doctor_acct 三键，取自 apps/admin-web/src/router/permissions.ts
--   的 PAGE_MODULES（模块短键 ↔ 路由路径的唯一映射），顺序与页面顺序一致。
--   现有 12 键一字不改名 —— 改名会同时打到子权限目录（permissions_t257.go 的 Module）与库里的历史值。
-- 写法：jsonb_set 只替换 modules 一个键，scope 与 items（若已被细化过）原值保留。
-- 范围红线：**只动 ROLE_ADMIN 的 modules**。ROLE_DOCTOR / ROLE_CS 一字不动 ——
--   医护对复查两页的权限位、异常报告是否建页建键，均为 T345 挂起项，待 Boss 裁（PM 2026-09-23 23:02 围栏）。
-- 覆盖两条路径：空库（000017 刚补播该行）与已跑过 seed 的库（dev / staging / prod）都命中 1 行，终态一致。
-- owner：roles 表属 user-service（与 000016 / 000017 / 000025 同域）。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","patients","teams","devices","alerts","comm",
           "orthosis","install","review","review_tpl","tech","doctor_acct","perm","config"]'::jsonb
       )
 WHERE role_id = 'ROLE_ADMIN';

COMMIT;
