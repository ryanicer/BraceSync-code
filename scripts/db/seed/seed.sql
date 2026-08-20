-- BraceSync 测试/开发种子数据（单测 / 集成 / E2E / 本地开发共用）
-- 依赖：已执行 000001_init_schema.up.sql
-- 说明：phone_enc 用占位 bytea（真实环境由服务 AES-GCM 加密写入）；phone_hash 用示例 SHA-256
-- 幂等：可重复执行（ON CONFLICT DO NOTHING）

-- ===== 预置角色（PRD §7D.11）=====
INSERT INTO roles (role_id, name, description, permissions_json) VALUES
  ('ROLE_ADMIN', '运营管理员', '全量数据，无团队隔离',
    '{"scope":"all","modules":["dashboard","realtime","patients","teams","devices","alerts","comm","orthosis","install","tech","perm","config"]}'),
  ('ROLE_DOCTOR', '医生', '仅本团队患者数据',
    '{"scope":"team","modules":["dashboard","realtime","alerts","orthosis"]}'),
  ('ROLE_CS', '客服', '仅患者沟通模块，全量患者',
    '{"scope":"all_patients","modules":["comm"]}')
ON CONFLICT (role_id) DO NOTHING;

-- ===== 团队 =====
INSERT INTO teams (team_id, name, member_count, patient_count) VALUES
  ('TEAM01', '脊柱矫形一组', 2, 2)
ON CONFLICT (team_id) DO NOTHING;

-- ===== 运营账号（测试环境统一密码 admin123）=====
-- password_hash 为 admin123 的真实 bcrypt（cost=10，T030 登录链路落地时生成替换占位）
INSERT INTO admins (admin_id, username, name, password_hash, role_id) VALUES
  ('A0001', 'ops_admin', '运营小张', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_ADMIN'),
  ('A0002', 'doctor_li', '医生李医师', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_DOCTOR'),
  ('A0003', 'cs_wang', '客服小王', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_CS')
ON CONFLICT (admin_id) DO NOTHING;

-- ===== 医生 =====
INSERT INTO doctors (doctor_id, name, title, department, team_id, phone_enc, phone_hash, admin_id) VALUES
  ('D0001', '李医师', '主任医师', '骨科', 'TEAM01', '\x00'::bytea,
   'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90', 'A0002')
ON CONFLICT (doctor_id) DO NOTHING;

-- ===== 技师（T037：统一初始密码 Password1!）=====
INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id, install_count, auth_status, password_hash) VALUES
  ('T0001', '技师老陈', '\x00'::bytea,
   'b1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90', 'TEAM01', 5, 'authorized',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe')
ON CONFLICT (tech_id) DO NOTHING;

-- ===== 患者（T037：统一初始密码 Password1!）=====
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, diagnosis, cobb_angle,
                      device_id, team_id, primary_doctor_id, status, password_hash) VALUES
  ('P20260001', '患者小明', '\x00'::bytea,
   'c1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90',
   'male', 14, '胸椎右侧凸 28°', 28.00, 'PRS-ML05-RC-20260701001', 'TEAM01', 'D0001', 'active',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('P20260002', '患者小红', '\x00'::bytea,
   'd1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90',
   'female', 12, '腰椎左侧凸 22°', 22.00, NULL, 'TEAM01', 'D0001', 'pending',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe')
ON CONFLICT (patient_id) DO NOTHING;

-- ===== 设备 =====
INSERT INTO devices (device_id, model, firmware_version, device_secret_enc, patient_id,
                    wifi_ssid, bind_time, status, last_report_at) VALUES
  ('PRS-ML05-RC-20260701001', 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, 'P20260001',
   'ClinicWiFi', now() - INTERVAL '10 days', 'online', now() - INTERVAL '20 minutes'),
  ('PRS-ML05-RC-20260701002', 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, NULL,
   NULL, NULL, 'unbound', NULL)
ON CONFLICT (device_id) DO NOTHING;

-- ===== 压力采集样本（落入 202607 分区）=====
INSERT INTO pressure_records (device_id, patient_id, ts,
  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,p11,p12,p13,p14,p15,p16,p17,p18,p19,p20, upload_time) VALUES
  ('PRS-ML05-RC-20260701001', 'P20260001', '2026-07-26 09:00:00+08',
   12.3,20.1,18.5,15.0,22.4,30.2,28.1,19.9,25.5,21.0,
   17.8,24.3,26.6,20.0,23.1,18.2,29.9,27.4,16.5,22.2, '2026-07-26 09:00:05+08'),
  ('PRS-ML05-RC-20260701001', 'P20260001', '2026-07-26 09:30:00+08',
   13.1,21.0,47.2,15.5,22.9,31.0,28.8,20.2,25.9,21.4,
   18.1,24.9,27.0,20.5,23.5,18.6,30.3,27.9,16.9,22.6, '2026-07-26 09:30:04+08')
ON CONFLICT (device_id, ts) DO NOTHING;

-- ===== 日聚合样本 =====
INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure,
                              max_point, frame_count, abnormal_count) VALUES
  ('P20260001', '2026-07-26', 1200, 22.4, 47.2, 'P03', 40, 1)
ON CONFLICT (patient_id, stat_date) DO NOTHING;

-- ===== 告警样本（压力偏高，P03 超 45N 阈值）=====
INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point, threshold_value, actual_value,
                    ts, read_status, process_status, resolved_status) VALUES
  ('P20260001', 'PRS-ML05-RC-20260701001', 'pressure_high',
   '采集点 P03 压力 47.2N 超阈值', 'P03', 45.0, 47.2,
   '2026-07-26 09:30:00+08', 'unread', 'pending', 'active')
ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING;

-- ===== 安装记录 + 基线（循环引用，三步法：先建 install_records → 建 baselines → 回填 baseline_id）=====
-- install_id/baseline_id 由 IDENTITY 生成，全新测试库从 1 开始；重跑时前序 ON CONFLICT 已防重
INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time, baseline_id,
                             notes, signature_url, wifi_status)
SELECT 'PRS-ML05-RC-20260701001', 'P20260001', 'T0001', '2026-07-16 10:00:00+08', NULL,
       '安装顺利，各点偏差在阈值内', 'cos://signatures/P20260001-20260716.png', 'connected'
WHERE NOT EXISTS (SELECT 1 FROM install_records WHERE install_id = 1);

INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id)
SELECT 1, 'PRS-ML05-RC-20260701001',
       ARRAY[0.1,0.2,0.1,0.0,0.3,0.2,0.1,0.0,0.2,0.1,0.1,0.2,0.3,0.1,0.2,0.0,0.1,0.2,0.1,0.0]::real[], 'T0001'
WHERE EXISTS (SELECT 1 FROM install_records WHERE install_id = 1)
  AND NOT EXISTS (SELECT 1 FROM baselines WHERE install_id = 1);

UPDATE install_records SET baseline_id = 1 WHERE install_id = 1 AND baseline_id IS NULL;

-- ===== 感受日志 / 反馈 / 矫形方案 / 偏好 / 报告样本 =====
INSERT INTO feeling_logs (patient_id, log_date, comfort_score, discomfort_areas, notes) VALUES
  ('P20260001', '2026-07-26', 4.0, ARRAY['thoracic']::varchar[], '胸段稍紧，可接受')
ON CONFLICT (patient_id, log_date) DO NOTHING;

INSERT INTO feedbacks (patient_id, type, content, status) VALUES
  ('P20260001', '佩戴咨询', '支具晚上佩戴时有点压痛，正常吗？', 'pending')
ON CONFLICT DO NOTHING;

INSERT INTO orthosis_plans (patient_id, doctor_id, content, version) VALUES
  ('P20260001', 'D0001', '维持当前矫形力，2 周后复查压力趋势', 'v1.0')
ON CONFLICT DO NOTHING;

INSERT INTO patient_preferences (patient_id, reminder_enabled, reminder_time, subscription_auth_status) VALUES
  ('P20260001', true, '20:00', 'authorized')
ON CONFLICT (patient_id) DO NOTHING;

INSERT INTO health_reports (patient_id, report_type, period_start, period_end,
                            wear_compliance_rate, avg_pressure, trend_judgment, suggestion) VALUES
  ('P20260001', 'weekly', '2026-07-20', '2026-07-26', 85.00, 22.4, 'flat',
   '维持当前矫形力，2 周后复查压力趋势')
ON CONFLICT (patient_id, report_type, period_start) DO NOTHING;

-- ===== 监护人同意记录样本（隐私合规 §3）=====
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260001', 'privacy_policy', 'v1.0', '小明爸爸', 'father', '203.0.113.10'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260001' AND consent_type='privacy_policy');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260001', 'sensitive_data', 'v1.0', '小明爸爸', 'father', '203.0.113.10'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260001' AND consent_type='sensitive_data');

-- ===== 全局配置默认值（PRD §7D.12）=====
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('collect_interval_minutes', '30', '采集间隔（分钟）'),
  ('wear_target_hours', '22', '每日佩戴目标时长（小时）'),
  ('threshold_pressure_high', '45', '压力偏高阈值（N）'),
  ('threshold_pressure_fluctuation_pct', '30', '压力波动幅度阈值（%）'),
  ('threshold_wear_interrupt_minutes', '60', '佩戴中断判定时间（分钟，须≥2×采集间隔）'),
  ('threshold_sensor_drift', '2.8', '传感器漂移告警阈值（N）'),
  ('threshold_calibration_offset', '0.5', '空载校准偏差上限（N）'),
  ('wearing_pressure_threshold', '0.5', 'wearing 佩戴判定压力阈值（N）'),
  ('device_config_version', '1', '设备配置版本（设备侧上报后比对，不一致则应用新配置）'),
  ('wifi_presets', '[{"ssid":"ClinicWiFi"}]', 'WiFi 预置列表（JSON 数组，技师端拉取辅助配网）')
ON CONFLICT (config_key) DO NOTHING;

-- ===== 告警通知规则默认值（PRD §7D.6）=====
INSERT INTO alert_notify_rules (type, channels, notify_targets) VALUES
  ('pressure_high',         ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('pressure_fluctuation',  ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('wear_interrupt',        ARRAY['wechat','sms']::varchar[], ARRAY['patient','doctor']::varchar[]),
  ('sensor_drift',          ARRAY['wechat']::varchar[],       ARRAY['tech','ops']::varchar[])
ON CONFLICT (type) DO NOTHING;
