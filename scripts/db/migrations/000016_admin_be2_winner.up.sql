-- T252 admin 后端第2批（Winner）：2.2 逐采集点告警规则 + 11.4 预置 5 角色
-- 对齐：docs/tasks/winner/T252-admin后端第2批-prompt.md · 契约 docs/api/api-contracts.ts（docs PR #146）
-- owner：alert_point_rules / sys_configs / roles 均为 user-service（配置与账号同域，与 /admin/settings 同写通道）

BEGIN;

-- ── 2.2 逐采集点阈值（合同:51「压力采集点阈值参数配置」+:52「异常告警规则配置」明文 P0）──────
-- 设计稿 docs/design/admin/告警管理.html:255-296 Tab2：4×5 网格（20 点）逐点独立上下限 + 勾选监控 + 统一上下限。
-- 🔴 表**稀疏存放**：只有被显式保存过的点才有行；未落库的点由服务层回默认（对前端恒呈现 P01–P20 共 20 条）。
--    这样「恢复默认」= DELETE 全表，不需要再播 20 条种子行。
-- point_id 口径与后端既有编号一致（data-service model.PointID / alert-service sensorPointName ⇒ P01–P20 零填充两位）。
CREATE TABLE IF NOT EXISTS alert_point_rules (
  point_id   VARCHAR(4)   PRIMARY KEY CHECK (point_id ~ '^P(0[1-9]|1[0-9]|20)$'),
  monitored  BOOLEAN      NOT NULL DEFAULT true,
  upper_n    NUMERIC(6,2) CHECK (upper_n IS NULL OR upper_n > 0),
  lower_n    NUMERIC(6,2) CHECK (lower_n IS NULL OR lower_n >= 0),
  updated_by VARCHAR(32),
  updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE alert_point_rules IS
  'T252 2.2 逐采集点告警阈值（告警页 Tab2）；upper_n/lower_n 为 NULL = 跟随 sys_configs 统一上下限';
COMMENT ON COLUMN alert_point_rules.monitored IS
  'false = 设计稿网格未勾选 ⇒ alert-service 引擎跳过该点（不参与压力偏高判定）';

-- 统一上下限与四条全局告警规则是标量 ⇒ 入 sys_configs KV（与本表同一次保存提交）。
-- 🔴 统一压力上限复用既有键 threshold_pressure_high（§7D.12 的「压力偏高阈值」）——
--    设计稿 Tab2 的「统一压力上限」与系统配置页那项是同一个参数，不建第二份以免双写漂移；
-- 🔴 「佩戴时长下限」同理复用 wear_target_hours（系统配置页 dailyWearTargetHours）。
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('threshold_pressure_low',    '10', '统一压力下限(N)（T252 2.2 告警页 Tab2，设计稿示意值 10）'),
  ('device_offline_minutes',    '30', '全局告警规则：设备离线阈值(分钟)（T252 2.2，设计稿 :289）'),
  ('continuous_wear_max_hours', '23', '全局告警规则：连续佩戴上限(小时)（T252 2.2，设计稿 :291）'),
  ('report_timeout_minutes',     '5', '全局告警规则：数据上报超时(分钟)（T252 2.2，设计稿 :292）')
ON CONFLICT (config_key) DO NOTHING;

-- ── 11.4 预置 5 角色（设计稿 docs/design/admin/权限控制.html:100-104；合同:49）────────────
-- Boss 2026-09-19 四层关系裁定：PRD 与设计稿冲突听设计稿 ⇒ 预置角色 5 个。
-- 🔴 原 3 个登录角色（ROLE_ADMIN/ROLE_DOCTOR/ROLE_CS，seed.sql）**保留**：`admins.role_id` 有外键引用
--    （000001:33；doctors/technicians 表无 role_id 列，角色只挂在运营账号上），
--    且 gateway RBAC 与登录签发链路按这三条字面量匹配，删除即破坏现有契约（派发单§三.5）。
--    两套并存的收敛（旧角色改名对齐、账号迁移、新角色如何进网关 RBAC）已登记待 PM/Boss 裁定。
-- permissions_json 形状与 seed 预置角色一致（scope ∈ all|team|all_patients；modules 为 12 页面模块键）。
INSERT INTO roles (role_id, name, description, permissions_json) VALUES
  ('ROLE_SUPER_ADMIN', '超级管理员', '系统全部权限',
   '{"scope":"all","modules":["dashboard","realtime","patients","teams","devices","alerts","comm","orthosis","install","tech","perm","config"]}'),
  ('ROLE_CHIEF_DOCTOR', '主任医师', '患者管理+数据分析+团队管理',
   '{"scope":"all","modules":["dashboard","realtime","patients","teams","alerts","orthosis","install"]}'),
  ('ROLE_ATTENDING_DOCTOR', '主治医师', '患者数据+告警处理+沟通',
   '{"scope":"team","modules":["dashboard","realtime","patients","alerts","comm","orthosis"]}'),
  ('ROLE_REHAB_THERAPIST', '康复师', '患者数据查看+矫形日志+沟通',
   '{"scope":"team","modules":["realtime","patients","alerts","comm","orthosis"]}'),
  ('ROLE_NURSE', '护士', '患者列表查看+基本沟通',
   '{"scope":"team","modules":["patients","comm"]}')
ON CONFLICT (role_id) DO NOTHING;

-- ── 12.3 操作日志读路径索引（audit_logs 表 000001:301 已建，但只索引了
--    (operator_id, ts) 与 (target_type, target_id)；GET /admin/audit-logs 默认按 ts 倒序翻页，
--    缺单列 ts 索引会走全表排序）
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_logs (ts DESC);

COMMIT;
