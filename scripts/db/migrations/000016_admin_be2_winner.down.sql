-- T252 admin 后端第2批（Winner）回滚
BEGIN;

DROP TABLE IF EXISTS alert_point_rules;
DROP INDEX IF EXISTS idx_audit_ts;

DELETE FROM sys_configs
 WHERE config_key IN ('threshold_pressure_low', 'device_offline_minutes',
                      'continuous_wear_max_hours', 'report_timeout_minutes');

-- 只删本批新播的 5 个设计稿预置角色；旧 3 个登录角色不在本迁移播的，也不应被回滚删除。
-- 若有账号已引用新角色（11.2 建角色后分配），DELETE 会被外键挡住 ⇒ 由 DBA 先转移成员。
DELETE FROM roles
 WHERE role_id IN ('ROLE_SUPER_ADMIN', 'ROLE_CHIEF_DOCTOR', 'ROLE_ATTENDING_DOCTOR',
                   'ROLE_REHAB_THERAPIST', 'ROLE_NURSE');

COMMIT;
