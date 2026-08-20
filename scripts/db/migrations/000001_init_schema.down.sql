-- BraceSync 初始 schema 回滚（golang-migrate down）
-- 按依赖反序 DROP；分区表 DROP 级联清除子分区

BEGIN;

DROP TABLE IF EXISTS sys_configs;
DROP TABLE IF EXISTS alert_notify_rules;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS patient_preferences;
DROP TABLE IF EXISTS consents;
DROP TABLE IF EXISTS health_reports;
DROP TABLE IF EXISTS feedbacks;
DROP TABLE IF EXISTS feeling_logs;
DROP TABLE IF EXISTS orthosis_plans;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS daily_wear_stats;
DROP TABLE IF EXISTS pressure_records;  -- 级联删除所有月分区

-- 先断循环引用 FK，再按序删表
ALTER TABLE IF EXISTS baselines DROP CONSTRAINT IF EXISTS fk_baselines_install;
DROP TABLE IF EXISTS install_records;
DROP TABLE IF EXISTS baselines;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS patients;
DROP TABLE IF EXISTS technicians;
DROP TABLE IF EXISTS doctors;
DROP TABLE IF EXISTS admins;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS teams;

COMMIT;
