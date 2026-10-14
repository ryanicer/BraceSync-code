-- T679 预置登录角色的 modules 补第 17 键 todo：ROLE_ADMIN 16→17、ROLE_DOCTOR 7→8
-- 依据 = Boss 2026-10-11 裁定 A 案「医护待办功能全栈单卡」（派发单 docs/tasks/winner/T679-*.md），
--   落地 T665 三屏定稿（commit 5994aa53，docs PR #1830）：新增页面 /todos「✅ 我的待办」，
--   模块短键 todo，与前端 apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES 同序
--   插于 dashboard 之后（侧栏第 2 位）。
-- 为什么两个角色一起改：PRD §7D.16「我的待办」= admin ✅ / doctor ✅（仅本团队患者）——
--   医护是待办的**主要用户**，运营管理员需可查看全部团队待办；前端 ROLE_PAGE_MATRIX 已按该口径
--   放开两角色，本迁移把库对齐到同一集合（与 T368 / T372 的裁定取向一致：以矩阵为准补库，不收前端）。
-- 数据范围不变 —— 医护「仅本团队」这条由后端团队过滤（T350 团队范围）在 user-service handler 层收口，
--   本迁移只补「能不能进这一页」，不预装任何数据范围逻辑。
-- 词表：todo 取自 PAGE_MODULES（模块短键 ↔ 路由路径 /todos 的唯一映射），顺序 = 侧栏位置。
--   钉在 apps/admin-web/test/permissions.spec.ts 的同源用例里（那里镜像源文件比对）。
-- 写法：jsonb_set 只替换 modules 一个键，scope 与 items（若已被运营细化过）原值保留 —— 与 000026 / 000028 / 000029 同款。
-- 覆盖两条建库路径：跑过 seed.sql 的库（dev / staging / prod，seed 已是 17 / 8 项）与只跑迁移的库
--   （000017 幂等补播 + 000026 → 000028 → 000029 的终态 16 / 7）都由本迁移命中 1 行、终态一致。
--   ⚠ seed 是 ON CONFLICT DO NOTHING，对已建库的既有行不生效 ⇒ 线上补键只能靠本迁移
--   （先例 000025 / 000021 / 000026 / 000028 / 000029）。
-- 编号：最新已入 main 的迁移为 000037，本迁移取 000038，不回头改任何已入 main 的迁移
--   （000026 / 000028 / 000029 写的仍是它们当时的终态，由本迁移正向纠正）。
-- 范围红线：**只动这两个角色的 modules**。ROLE_CS（1 项 comm）一字不动；两个角色的 scope 不变；
--   网关鉴权模型不动；子权限目录 permissions_t257.go 保持 9 组 23 项不扩（T679 已裁：待办只有页面级权限，
--   设计稿未画组内勾选项）。
-- ⚠ 若运营在权限页自行改过这两个角色的模块集，本迁移会把它覆盖成裁定的 17 / 8 项终态（要保留请先备份该行）。
-- owner：roles 表属 user-service（与 000016 / 000017 / 000025 / 000026 / 000028 / 000029 同域）。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","todo","realtime","patients","abnormal_report","teams","devices","alerts","comm","orthosis","install","review","review_tpl","tech","doctor_acct","perm","config"]'::jsonb
       )
 WHERE role_id = 'ROLE_ADMIN';

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","todo","realtime","abnormal_report","alerts","orthosis","review","review_tpl"]'::jsonb
       )
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
