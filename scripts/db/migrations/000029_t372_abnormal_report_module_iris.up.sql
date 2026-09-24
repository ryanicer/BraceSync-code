-- T372 预置登录角色的 modules 补第 16 键 abnormal_report：ROLE_ADMIN 15→16、ROLE_DOCTOR 6→7
-- Boss 2026-09-24 裁定 (a)「异常报告按设计稿拆独立页」（派发单 docs/tasks/iris/T372-abnormal-report-page-prompt.md），
--   原「T345 挂起项 / PRD §7D.11 登记 6.2」就此收口：设计稿侧栏第 4 项 🧾 异常报告 得到独立路由
--   /abnormal-report 与模块短键 abnormal_report。
-- 为什么两个角色一起改：PRD §7D.11 矩阵第 4 行「异常报告」= admin ✅ / doctor ✅（仅本团队患者）/ cs —，
--   前端 ROLE_PAGE_MATRIX 已按该行放开两角色；本迁移把库对齐到同一集合（与 T368 的裁定取向一致：以矩阵为准补库，
--   不收前端）。数据范围不变 —— 医护「仅本团队」这条由后端团队过滤负责，且该过滤在当前所有 admin 域端点上
--   仍未实现（设计稿 异常报告.html T300 实现补记自陈的既有待裁项，另卡归口），本迁移不预装。
-- 词表：abnormal_report 取自 apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES（模块短键 ↔ 路由路径
--   的唯一映射），顺序 = 侧栏位置（patients 之后、teams 之前；doctor 为 realtime 之后、alerts 之前），
--   与前端两角色可见页换算结果逐元素相等（钉在 apps/admin-web/test/permissions.spec.ts 的同源用例里）。
-- 写法：jsonb_set 只替换 modules 一个键，scope 与 items（若已被运营细化过）原值保留 —— 与 000026 / 000028 同款。
-- 覆盖两条建库路径：跑过 seed.sql 的库（dev / staging / prod，seed 已是 16 / 7 项）与只跑迁移的库
--   （000017 幂等补播 + 000026 + 000028 的终态）都由本迁移命中 1 行、终态一致。⚠ seed 是
--   ON CONFLICT DO NOTHING，对已建库的既有行不生效 ⇒ 线上补键只能靠本迁移（先例 000025 / 000021 / 000026 / 000028）。
-- 编号：000027 被 code #204 占用、000028 为 T368，本迁移取 000029，不回头改任何已入 main 的迁移
--   （000026 / 000028 写的仍是它们当时的终态，由本迁移正向纠正）。
-- 范围红线：**只动这两个角色的 modules**。ROLE_CS（1 项 comm）一字不动；两个角色的 scope 不变；
--   网关鉴权模型不动；子权限目录 permissions_t257.go 保持 9 组 23 项不扩（T368 已裁：异常报告只有页面级权限，
--   设计稿未画组内勾选项）。
-- ⚠ 若运营在权限页自行改过这两个角色的模块集，本迁移会把它覆盖成裁定的 16 / 7 项终态（要保留请先备份该行）。
-- owner：roles 表属 user-service（与 000016 / 000017 / 000025 / 000026 / 000028 同域）。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","patients","abnormal_report","teams","devices","alerts","comm","orthosis","install","review","review_tpl","tech","doctor_acct","perm","config"]'::jsonb
       )
 WHERE role_id = 'ROLE_ADMIN';

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","abnormal_report","alerts","orthosis","review","review_tpl"]'::jsonb
       )
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
