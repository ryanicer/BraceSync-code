-- T368 预置登录角色 ROLE_DOCTOR 的 modules 补齐：4 项 → 6 项（补 review / review_tpl）
-- Boss 2026-09-24 09:3x 裁定 (a)「补库」：把「复查报告 / 复查模板管理」两页正式授权给医护，
--   使库与前端准入矩阵同源。PM 09:35/09:36 两条卡内评论转达，派发单
--   docs/tasks/iris/T368-medical-page-perm-sync-prompt.md。
-- 现象（Ella T345 验收报告唯一不通过项 D-A，现网实测）：医护 doctor_li 侧栏 6 项，
--   同一时刻 GET /api/v1/admin/me/permissions 只回 4 项；差集两页 /review-records、
--   /review-templates 深链可进、数据照常取回。
-- 根因位：同一角色的页面准入有两套来源同屏并存 ——
--   前端硬编码矩阵 apps/admin-web/src/router/permissions.ts 的 ROLE_PAGE_MATRIX（doctor 6 条，
--   T135 时有意给医护，注释「医生可下载空白模板」）与库 roles.permissions_json.modules（4 项）。
--   admin / cs 两侧恰好相等，所以这个分叉一直没被暴露。
-- 裁定取向：**以库为准对齐** ⇒ 补库而不是收回前端（PM 建议 (a) 的理由：复查报告本身就是医生产出的，
--   属医护本职；改 (b) 是需求收缩，不该由实现侧代裁）。
-- 词表：review / review_tpl 两键取自 apps/admin-web/src/router/permissions.ts 的 PAGE_MODULES
--   （模块短键 ↔ 路由路径的唯一映射，T345 已入库），与 000026 给 ROLE_ADMIN 补的三键同一套词，
--   本迁移不新造键名。顺序按 PAGE_MODULES 序 ⇒ 与前端 doctor 可见页换算结果逐元素相等（钉在
--   apps/admin-web/test/permissions.spec.ts 的同源用例里）。
-- 写法：jsonb_set 只替换 modules 一个键，scope 与 items（若已被运营细化过）原值保留 —— 与 000026 同款。
-- 覆盖两条建库路径：只跑迁移的库（000017 幂等补播 4 项）与跑过 seed.sql 的库（dev / staging / prod，
--   本迁移后 seed 也是 6 项）都命中 1 行、终态一致。⚠ seed 是 ON CONFLICT DO NOTHING，
--   对已建库的既有行不生效 ⇒ 线上补键只能靠本迁移（先例：000025 / 000021 / 000022 / 000026）。
-- 编号：000027 已被在飞的 code #204（T366 日聚合来源可辨）占用，本迁移取 000028，
--   不回头改任何已入 main 的迁移（000017 播的仍是 4 项，由本迁移正向纠正）。
-- 范围红线：**只动 ROLE_DOCTOR 的 modules**。ROLE_ADMIN（000026 已补到 15）/ ROLE_CS（1 项）一字不动；
--   scope 仍是 team（数据范围不变，补的是「能不能进这两页」不是「能看到哪些团队的数据」）；
--   网关鉴权模型不动（T368 派发单 §三 边界）；子权限目录 permissions_t257.go 保持 9 组 23 项不扩
--   （T345 裁定：review / review_tpl 只有页面级权限，设计稿未画组内勾选项）。
-- ⚠ 若运营在权限页自行改过该角色的模块集，本迁移会把它覆盖成裁定的 6 项终态（要保留请先备份该行）。
-- owner：roles 表属 user-service（与 000016 / 000017 / 000025 / 000026 同域）。

BEGIN;

UPDATE roles
   SET permissions_json = jsonb_set(
         permissions_json,
         '{modules}',
         '["dashboard","realtime","alerts","orthosis","review","review_tpl"]'::jsonb
       )
 WHERE role_id = 'ROLE_DOCTOR';

COMMIT;
