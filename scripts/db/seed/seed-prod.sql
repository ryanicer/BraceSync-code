-- seed-prod.sql —— BraceSync 生产首发最小 seed（T696 手册 V1.1.1 §9 口径件）
-- 🔴 纪律（红线同手册）：
--   1. 本文件只含字典与 1 个管理员账号占位参数，禁一切测试家族数据（teams/doctors/patients/设备/告警等一律不进）；
--   2. 禁任何真实口令/hash 入库——admins 行经 psql -v 运行时注入，明文与 hash 都不落本文件；
--   3. 执行属生产写：🔴 需 Boss 授权 + 双人复核后方可在 prod 跑；staging 不需要本件（staging 用全量 seed.sql）。
-- 执行方式（prod 宿主机，§2.0 首发闸门 b，迁移 000001→000038 之后、冒烟 S2 之前）：
--   PW 只走现场环境变量；hash 用 python3 bcrypt(cost 10) 现算（宿主无库则线下算好）：
--   HASH=$(python3 -c 'import bcrypt,sys; print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(10)).decode())' "$PROD_ADMIN_PW")
--   docker compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
--     -v admin_id='<Boss 定>' -v username='<Boss 定，禁照抄 staging 三件套>' -v name='<Boss 定>' \
--     -v password_hash="$HASH" -f seed-prod.sql
--   注：password_hash 值含 $ 字符，宿主机传参务必单引号包 $HASH 展开结果或经文件读入，防 shell 二次展开。
-- 幂等：全部 ON CONFLICT DO NOTHING，重放安全；回出口=按主键 DELETE（需单独授权），不动 schema_migrations。

-- ===== 1) 角色字典（3 行；结构照 seed.sql 原样，纯字典非凭据） =====
INSERT INTO roles (role_id, name, description, permissions_json) VALUES
  ('ROLE_ADMIN', '运营管理员', '全量数据，无团队隔离',
    '{"scope":"all","modules":["dashboard","realtime","patients","abnormal_report","teams","devices","alerts","comm","orthosis","install","review","review_tpl","tech","doctor_acct","perm","config"]}'),
  ('ROLE_DOCTOR', '医护', '仅本团队患者数据',
    '{"scope":"team","modules":["dashboard","realtime","abnormal_report","alerts","orthosis","review","review_tpl"]}'),
  ('ROLE_CS', '客服', '仅患者沟通模块，全量患者',
    '{"scope":"all_patients","modules":["comm"]}')
ON CONFLICT (role_id) DO NOTHING;

-- ===== 2) 管理员账号（1 行；四值全运行时注入，文件零凭据） =====
INSERT INTO admins (admin_id, username, name, password_hash, role_id)
VALUES (:'admin_id', :'username', :'name', :'password_hash', 'ROLE_ADMIN')
ON CONFLICT (admin_id) DO NOTHING;

-- ===== 3) 系统参数字典（17 行；值=产品默认口径，与迁移 000016–000023 同源） =====
-- 🔴 T173 阈值口径（Boss 2026-09-14 明令）：压力量纲阈值均为「可配置参数」，配置驱动、不硬编码；
-- 现值基于已作废的 N×100 量纲估算，仅作占位、待按 mN/÷1000 量级重定（重定输入：Boss+小顾真实物理范围）。
-- 远期后台运营可调（alert-service config.Manager.Update 已预留写入口），调参不改代码。
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('collect_interval_minutes', '30', '采集间隔（分钟）'),
  ('wear_target_hours', '22', '每日佩戴目标时长（小时）'),
  ('threshold_pressure_high', '5', '压力偏高阈值（N，T203 ÷10）'),
  ('threshold_pressure_low', '1', '统一压力下限（N，T203 ÷10；须小于 threshold_pressure_high）'),
  ('threshold_pressure_fluctuation_pct', '30', '压力波动幅度阈值（%）'),
  ('threshold_wear_interrupt_minutes', '60', '佩戴中断判定时间（分钟，须≥2×采集间隔）'),
  ('threshold_sensor_drift', '0.3', '传感器漂移告警阈值（N，T203 ÷10）'),
  ('threshold_calibration_offset', '0.05', '空载校准偏差上限（N，T203 ÷10）'),
  ('wearing_pressure_threshold', '0.05', 'wearing 佩戴判定压力阈值（N，T203 ÷10）'),
  ('heatmap_max_n', '6', '热力图色阶上界（N，T203 ÷10）'),
  ('device_config_version', '1', '设备配置版本（设备侧上报后比对，不一致则应用新配置）'),
  ('collect_interval_seconds', '1800', '数据采集间隔（秒，运营后台按设计稿秒口径读写；=collect_interval_minutes×60）'),
  ('data_retention_days', '365', '数据保留天数（T256 12.5；清理任务未启用）'),
  ('max_patients', '10000', '最大患者数（T256 12.5；建员上限未启用）'),
  ('wifi_presets', '[{"ssid":"ClinicWiFi"},{"ssid":"HomeWiFi"}]', 'WiFi 预置列表（JSON 数组，技师端拉取辅助配网）'),
  ('notify_wechat_template_id', '', '告警微信模板消息 ID（PRD §7D.12 通知模板配置；空=未配置，直发不带模板）'),
  ('notify_sms_template_id', '', '告警短信模板 ID（PRD §7D.12 通知模板配置；空=未配置）')
ON CONFLICT (config_key) DO NOTHING;

-- ===== 4) 告警通知规则（5 行；与迁移 000018 同一份值） =====
INSERT INTO alert_notify_rules (type, channels, notify_targets) VALUES
  ('pressure_high',         ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('pressure_fluctuation',  ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('wear_interrupt',        ARRAY['wechat','sms']::varchar[], ARRAY['patient','doctor']::varchar[]),
  ('sensor_drift',          ARRAY['wechat']::varchar[],       ARRAY['tech','ops']::varchar[]),
  ('wear_duration_short',   ARRAY['wechat','sms']::varchar[], ARRAY['patient','doctor']::varchar[])
ON CONFLICT (type) DO NOTHING;

-- ===== 5) 回读自检（=冒烟 S13 判据；输出即留痕，随交件原文落板） =====
SELECT count(*) AS roles_expect_3            FROM roles;
SELECT count(*) AS admins_expect_1           FROM admins;
SELECT count(*) AS sys_configs_expect_17     FROM sys_configs;
SELECT count(*) AS alert_rules_expect_5      FROM alert_notify_rules;
SELECT (SELECT count(*) FROM patients) AS patients_expect_0,
       (SELECT count(*) FROM teams)    AS teams_expect_0,
       (SELECT count(*) FROM doctors)  AS doctors_expect_0;
