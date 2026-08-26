-- BraceSync 测试/开发种子数据（单测 / 集成 / E2E / 本地开发共用）
-- 依赖：已执行 000001_init_schema.up.sql ~ 000006_file_service.up.sql
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
ON CONFLICT (role_id) DO 
NOTHING;

-- ===== 团队（3+，覆盖团队管理页）=====
INSERT INTO teams (team_id, name, member_count, patient_count) VALUES
  ('TEAM01', '脊柱矫形一组', 3, 3),
  ('TEAM02', '脊柱矫形二组', 2, 2),
  ('TEAM03', '康复理疗三组', 2, 1)
ON CONFLICT (team_id) DO NOTHING;

-- ===== 运营账号（测试环境统一密码 admin123）=====
INSERT INTO admins (admin_id, username, name, password_hash, role_id) VALUES
  ('A0001', 'ops_admin', '运营小张', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_ADMIN'),
  ('A0002', 'doctor_li', '医生李医师', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_DOCTOR'),
  ('A0003', 'cs_wang', '客服小王', '$2a$10$HpFYM9TY7cv8ABe.UZDx/OWi/HpdFcPSBf4rbvyJtlgLBmSo2/Snm', 'ROLE_CS')
ON CONFLICT (admin_id) DO NOTHING;

-- ===== 医生（3+，覆盖医生管理页）=====
INSERT INTO doctors (doctor_id, name, title, department, team_id, phone_enc, phone_hash, admin_id) VALUES
  ('D0001', '李医师', '主任医师', '骨科', 'TEAM01', '\x00'::bytea,
   'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90', 'A0002'),
  ('D0002', '王医师', '副主任医师', '骨科', 'TEAM02', '\x00'::bytea,
   'a2b3c4d5e6f70819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f91', NULL),
  ('D0003', '赵医师', '主治医师', '康复科', 'TEAM03', '\x00'::bytea,
   'a3b4c5d6e7f80819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f92', NULL)
ON CONFLICT (doctor_id) DO NOTHING;

-- ===== 技师（3+，覆盖技师管理页，含启停状态）=====
INSERT INTO technicians (tech_id, name, phone_enc, phone_hash, team_id, install_count, auth_status, password_hash) VALUES
  ('T0001', '技师老陈', '\x00'::bytea,
   'b1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90', 'TEAM01', 5, 'authorized',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('T0002', '技师小刘', '\x00'::bytea,
   'b2b3c4d5e6f70819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f91', 'TEAM02', 3, 'authorized',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('T0003', '技师小周', '\x00'::bytea,
   'b3b4c5d6e7f80819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f92', 'TEAM03', 1, 'suspended',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe')
ON CONFLICT (tech_id) DO NOTHING;

-- ===== 患者（5+，覆盖患者管理页，含团队/医生 join 字段）=====
INSERT INTO patients (patient_id, name, phone_enc, phone_hash, gender, age, birth_date, diagnosis, cobb_angle,
                      device_id, team_id, primary_doctor_id, status, password_hash) VALUES
  ('P20260001', '患者小明', '\x00'::bytea,
   'c1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90',
   'male', 14, '2012-03-15', '胸椎右侧凸 28°', 28.00, 'PRS-ML05-RC-20260701001', 'TEAM01', 'D0001', 'active',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('P20260002', '患者小红', '\x00'::bytea,
   'd1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90',
   'female', 12, '2014-06-20', '腰椎左侧凸 22°', 22.00, NULL, 'TEAM01', 'D0001', 'pending',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('P20260003', '患者小杰', '\x00'::bytea,
   'c2b3c4d5e6f70819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f91',
   'male', 13, '2013-09-10', '胸椎左侧凸 35°', 35.00, 'PRS-ML05-RC-20260701003', 'TEAM02', 'D0002', 'active',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('P20260004', '患者小琳', '\x00'::bytea,
   'c3b4c5d6e7f80819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f92',
   'female', 15, '2011-04-25', '双弯型 40°', 40.00, 'PRS-ML05-RC-20260701004', 'TEAM02', 'D0002', 'active',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe'),
  ('P20260005', '患者小宇', '\x00'::bytea,
   'c4b5c6d7e8f90819293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f93',
   'male', 10, '2016-01-08', '胸椎右侧凸 18°', 18.00, NULL, 'TEAM03', 'D0003', 'pending',
   '$2a$10$1CSYb.nghdJ77L1BKVefheXct/R3K5js8SBqYaC.2XFPpk4CtRjAe')
ON CONFLICT (patient_id) DO NOTHING;

-- ===== 设备（5+，覆盖设备管理页，含患者姓名 join）=====
INSERT INTO devices (device_id, model, firmware_version, device_secret_enc, patient_id,
                    wifi_ssid, bind_time, status, last_report_at) VALUES
  ('PRS-ML05-RC-20260701001', 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, 'P20260001',
   'ClinicWiFi', now() - INTERVAL '10 days', 'online', now() - INTERVAL '20 minutes'),
  ('PRS-ML05-RC-20260701002', 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, NULL,
   NULL, NULL, 'unbound', NULL),
  ('PRS-ML05-RC-20260701003', 'PRS-ML05-RC', 'v1.3.0', '\x00'::bytea, 'P20260003',
   'ClinicWiFi', now() - INTERVAL '8 days', 'online', now() - INTERVAL '35 minutes'),
  ('PRS-ML05-RC-20260701004', 'PRS-ML05-RC', 'v1.3.0', '\x00'::bytea, 'P20260004',
   'HomeWiFi', now() - INTERVAL '5 days', 'offline', now() - INTERVAL '3 hours'),
  ('PRS-ML05-RC-20260701005', 'PRS-ML05-RC', 'v1.2.0', '\x00'::bytea, NULL,
   NULL, NULL, 'unbound', NULL)
ON CONFLICT (device_id) DO NOTHING;

INSERT INTO device_bindings (device_id, patient_id, bind_at, unbind_at, reason, operator_id)
SELECT 'PRS-ML05-RC-20260701001', 'P20260001', '2026-07-16 10:00:00+08', NULL, 'install', 'T0001'
WHERE NOT EXISTS (
  SELECT 1 FROM device_bindings WHERE device_id = 'PRS-ML05-RC-20260701001' AND unbind_at IS NULL
);

INSERT INTO device_bindings (device_id, patient_id, bind_at, unbind_at, reason, operator_id)
SELECT 'PRS-ML05-RC-20260701003', 'P20260003', '2026-07-18 14:00:00+08', NULL, 'install', 'T0002'
WHERE NOT EXISTS (
  SELECT 1 FROM device_bindings WHERE device_id = 'PRS-ML05-RC-20260701003' AND unbind_at IS NULL
);

INSERT INTO device_bindings (device_id, patient_id, bind_at, unbind_at, reason, operator_id)
SELECT 'PRS-ML05-RC-20260701004', 'P20260004', '2026-07-21 09:30:00+08', NULL, 'install', 'T0002'
WHERE NOT EXISTS (
  SELECT 1 FROM device_bindings WHERE device_id = 'PRS-ML05-RC-20260701004' AND unbind_at IS NULL
);
ON CONFLICT DO NOTHING;

-- ===== 压力采集样本（落入 202607/202608 分区，多患者多时段）=====
INSERT INTO pressure_records (device_id, patient_id, ts,
  p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,p11,p12,p13,p14,p15,p16,p17,p18,p19,p20, upload_time) VALUES
  ('PRS-ML05-RC-20260701001', 'P20260001', '2026-07-26 09:00:00+08',
   12.3,20.1,18.5,15.0,22.4,30.2,28.1,19.9,25.5,21.0,
   17.8,24.3,26.6,20.0,23.1,18.2,29.9,27.4,16.5,22.2, '2026-07-26 09:00:05+08'),
  ('PRS-ML05-RC-20260701001', 'P20260001', '2026-07-26 09:30:00+08',
   13.1,21.0,47.2,15.5,22.9,31.0,28.8,20.2,25.9,21.4,
   18.1,24.9,27.0,20.5,23.5,18.6,30.3,27.9,16.9,22.6, '2026-07-26 09:30:04+08'),
  ('PRS-ML05-RC-20260701001', 'P20260001', '2026-08-20 08:00:00+08',
   11.5,19.8,17.2,14.5,21.8,29.5,27.3,19.2,24.8,20.5,
   17.0,23.8,25.9,19.5,22.6,17.8,29.2,26.8,16.0,21.8, '2026-08-20 08:00:03+08'),
  ('PRS-ML05-RC-20260701003', 'P20260003', '2026-08-20 10:00:00+08',
   14.0,22.5,20.1,16.3,24.0,31.5,29.5,21.0,26.8,22.3,
   19.0,25.5,28.0,21.3,24.5,19.5,31.0,28.8,17.5,23.5, '2026-08-20 10:00:04+08'),
  ('PRS-ML05-RC-20260701003', 'P20260003', '2026-08-20 10:30:00+08',
   14.5,23.0,46.8,16.8,24.5,32.0,30.0,21.5,27.2,22.8,
   19.5,26.0,28.5,21.8,25.0,20.0,31.5,29.2,18.0,24.0, '2026-08-20 10:30:03+08'),
  ('PRS-ML05-RC-20260701004', 'P20260004', '2026-08-21 14:00:00+08',
   10.2,18.0,16.5,13.0,19.5,27.0,25.0,17.5,22.0,18.5,
   15.5,21.5,23.8,18.0,20.8,16.0,26.5,24.5,14.8,19.8, '2026-08-21 14:00:05+08')
ON CONFLICT (device_id, ts) DO NOTHING;

-- ===== 日聚合样本（多患者多日，覆盖 Dashboard 佩戴趋势）=====
INSERT INTO daily_wear_stats (patient_id, stat_date, wear_minutes, avg_pressure, max_pressure,
                              max_point, frame_count, abnormal_count) VALUES
  ('P20260001', '2026-07-26', 1200, 22.4, 47.2, 'P03', 40, 1),
  ('P20260001', '2026-08-20', 1320, 21.8, 29.2, 'P19', 44, 0),
  ('P20260001', '2026-08-21', 1260, 22.0, 28.5, 'P19', 42, 0),
  ('P20260001', '2026-08-22', 1180, 21.5, 27.8, 'P07', 39, 0),
  ('P20260003', '2026-08-20', 1080, 24.5, 46.8, 'P03', 36, 1),
  ('P20260003', '2026-08-21', 1150, 23.8, 32.0, 'P06', 38, 0),
  ('P20260003', '2026-08-22', 1020, 24.0, 31.5, 'P06', 34, 0),
  ('P20260004', '2026-08-21', 960, 20.2, 26.5, 'P19', 32, 0),
  ('P20260004', '2026-08-22', 900, 19.8, 25.0, 'P15', 30, 0)
ON CONFLICT (patient_id, stat_date) DO NOTHING;

-- ===== 告警样本（3+ 各类型，待处理/已处理，覆盖告警管理页）=====
INSERT INTO alerts (patient_id, device_id, type, detail, sensor_point, threshold_value, actual_value,
                    ts, read_status, process_status, resolved_status) VALUES
  ('P20260001', 'PRS-ML05-RC-20260701001', 'pressure_high',
   '采集点 P03 压力 47.2N 超阈值', 'P03', 45.0, 47.2,
   '2026-07-26 09:30:00+08', 'unread', 'pending', 'active'),
  ('P20260003', 'PRS-ML05-RC-20260701003', 'pressure_high',
   '采集点 P03 压力 46.8N 超阈值', 'P03', 45.0, 46.8,
   '2026-08-20 10:30:00+08', 'read', 'pending', 'active'),
  ('P20260001', 'PRS-ML05-RC-20260701001', 'wear_interrupt',
   '佩戴中断超过 60 分钟', NULL, 60.0, 90.0,
   '2026-08-22 14:00:00+08', 'read', 'processed', 'resolved'),
  ('P20260004', 'PRS-ML05-RC-20260701004', 'sensor_drift',
   '传感器漂移 2.9N 超阈值', 'P07', 2.8, 2.9,
   '2026-08-21 18:00:00+08', 'unread', 'pending', 'active'),
  ('P20260003', 'PRS-ML05-RC-20260701003', 'pressure_fluctuation',
   '压力波动幅度 35% 超阈值', 'P06', 30.0, 35.0,
   '2026-08-22 09:00:00+08', 'read', 'processed', 'resolved')
ON CONFLICT (patient_id, device_id, type, ts) DO NOTHING;

-- ===== 安装记录 + 基线（3+ 安装记录，覆盖安装记录页）=====
-- install_id/baseline_id 由 IDENTITY 生成，全新测试库从 1 开始；重跑时前序 ON CONFLICT 已防重

-- 安装记录 1（保留原有）
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

-- 安装记录 2
INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time, baseline_id,
                             notes, signature_url, wifi_status)
SELECT 'PRS-ML05-RC-20260701003', 'P20260003', 'T0002', '2026-07-18 14:00:00+08', NULL,
       '安装正常，P03 点需关注', 'cos://signatures/P20260003-20260718.png', 'connected'
WHERE NOT EXISTS (SELECT 1 FROM install_records WHERE install_id = 2);

INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id)
SELECT 2, 'PRS-ML05-RC-20260701003',
       ARRAY[0.0,0.1,0.2,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1]::real[], 'T0002'
WHERE EXISTS (SELECT 1 FROM install_records WHERE install_id = 2)
  AND NOT EXISTS (SELECT 1 FROM baselines WHERE install_id = 2);

UPDATE install_records SET baseline_id = 2 WHERE install_id = 2 AND baseline_id IS NULL;

-- 安装记录 3
INSERT INTO install_records (device_id, patient_id, tech_id, calibrate_time, baseline_id,
                             notes, signature_url, wifi_status)
SELECT 'PRS-ML05-RC-20260701004', 'P20260004', 'T0002', '2026-07-21 09:30:00+08', NULL,
       '安装顺利，WiFi 配网正常', 'cos://signatures/P20260004-20260721.png', 'connected'
WHERE NOT EXISTS (SELECT 1 FROM install_records WHERE install_id = 3);

INSERT INTO baselines (install_id, device_id, offset_values, calibrator_id)
SELECT 3, 'PRS-ML05-RC-20260701004',
       ARRAY[0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2,0.1,0.0,0.1,0.2]::real[], 'T0002'
WHERE EXISTS (SELECT 1 FROM install_records WHERE install_id = 3)
  AND NOT EXISTS (SELECT 1 FROM baselines WHERE install_id = 3);

UPDATE install_records SET baseline_id = 3 WHERE install_id = 3 AND baseline_id IS NULL;

-- ===== 感受日志（2+ 患者各 1+，覆盖矫形日志页）=====
INSERT INTO feeling_logs (patient_id, log_date, comfort_score, discomfort_areas, notes) VALUES
  ('P20260001', '2026-07-26', 4.0, ARRAY['thoracic']::varchar[], '胸段稍紧，可接受'),
  ('P20260001', '2026-08-20', 3.5, ARRAY['thoracic','lumbar']::varchar[], '胸段和腰段均有压迫感'),
  ('P20260003', '2026-08-20', 4.5, ARRAY['thoracic']::varchar[], '整体舒适，轻微紧绷'),
  ('P20260003', '2026-08-22', 3.0, ARRAY['lumbar']::varchar[], '腰段压痛较明显'),
  ('P20260004', '2026-08-21', 4.0, ARRAY[]::varchar[], '佩戴舒适，无不适')
ON CONFLICT (patient_id, log_date) DO NOTHING;

-- ===== 患者反馈（3+，含回复，覆盖患者沟通页）=====
INSERT INTO feedbacks (patient_id, type, content, status) VALUES
  ('P20260001', '佩戴咨询', '支具晚上佩戴时有点压痛，正常吗？', 'pending'),
  ('P20260003', '设备问题', '设备指示灯一直闪红灯，是什么情况？', 'resolved'),
  ('P20260004', '佩戴咨询', '佩戴支具后皮肤有些过敏，需要处理吗？', 'resolved'),
  ('P20260001', '功能建议', '希望 App 能增加佩戴提醒自定义铃声功能', 'pending')
ON CONFLICT DO NOTHING;

-- ===== 矫形方案（2+ 患者各 1+，覆盖矫形日志页）=====
INSERT INTO orthosis_plans (patient_id, doctor_id, content, version) VALUES
  ('P20260001', 'D0001', '维持当前矫形力，2 周后复查压力趋势', 'v1.0'),
  ('P20260003', 'D0002', '适当增加 P03 点矫形力，4 周后复查 Cobb 角', 'v1.0'),
  ('P20260004', 'D0002', '双弯型需关注胸椎和腰椎协同矫形，维持当前方案', 'v1.0'),
  ('P20260001', 'D0001', '根据 8 月数据微调，P03 点压力偏高需降低矫形力', 'v1.1')
ON CONFLICT DO NOTHING;

-- ===== 患者偏好（多患者）=====
INSERT INTO patient_preferences (patient_id, reminder_enabled, reminder_time, subscription_auth_status) VALUES
  ('P20260001', true, '20:00', 'authorized'),
  ('P20260003', true, '19:30', 'authorized'),
  ('P20260004', false, '21:00', 'authorized'),
  ('P20260005', true, '20:00', 'pending')
ON CONFLICT (patient_id) DO NOTHING;

-- ===== 健康报告（2+ 患者各 1+，覆盖矫形日志页）=====
INSERT INTO health_reports (patient_id, report_type, period_start, period_end,
                            wear_compliance_rate, avg_pressure, trend_judgment, suggestion) VALUES
  ('P20260001', 'weekly', '2026-07-20', '2026-07-26', 85.00, 22.4, 'flat',
   '维持当前矫形力，2 周后复查压力趋势'),
  ('P20260001', 'weekly', '2026-08-18', '2026-08-24', 88.00, 21.8, 'down',
   '压力趋势下降，矫形效果良好，继续维持'),
  ('P20260003', 'weekly', '2026-08-18', '2026-08-24', 82.00, 24.0, 'up',
   'P03 点压力偏高，建议调整矫形方案'),
  ('P20260004', 'weekly', '2026-08-18', '2026-08-24', 75.00, 20.0, 'flat',
   '佩戴时长偏低，建议加强佩戴依从性指导'),
  ('P20260001', 'monthly', '2026-07-26', '2026-08-25', 86.00, 22.1, 'flat',
   '月度佩戴率稳定，矫形力维持良好')
ON CONFLICT (patient_id, report_type, period_start) DO NOTHING;

-- ===== 监护人同意记录样本（隐私合规 §3）=====
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260001', 'privacy_policy', 'v1.0', '小明爸爸', 'father', '203.0.113.10'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260001' AND consent_type='privacy_policy');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260001', 'sensitive_data', 'v1.0', '小明爸爸', 'father', '203.0.113.10'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260001' AND consent_type='sensitive_data');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260003', 'privacy_policy', 'v1.0', '小杰妈妈', 'mother', '203.0.113.11'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260003' AND consent_type='privacy_policy');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260003', 'sensitive_data', 'v1.0', '小杰妈妈', 'mother', '203.0.113.11'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260003' AND consent_type='sensitive_data');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260004', 'privacy_policy', 'v1.0', '小琳爸爸', 'father', '203.0.113.12'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260004' AND consent_type='privacy_policy');
INSERT INTO consents (patient_id, consent_type, policy_version, consenter_name, consenter_relation, ip)
SELECT 'P20260004', 'sensitive_data', 'v1.0', '小琳爸爸', 'father', '203.0.113.12'
WHERE NOT EXISTS (SELECT 1 FROM consents WHERE patient_id='P20260004' AND consent_type='sensitive_data');

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
  ('wifi_presets', '[{"ssid":"ClinicWiFi"},{"ssid":"HomeWiFi"}]', 'WiFi 预置列表（JSON 数组，技师端拉取辅助配网）')
ON CONFLICT (config_key) DO NOTHING;

-- ===== 告警通知规则默认值（PRD §7D.6）=====
INSERT INTO alert_notify_rules (type, channels, notify_targets) VALUES
  ('pressure_high',         ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('pressure_fluctuation',  ARRAY['wechat']::varchar[],       ARRAY['doctor']::varchar[]),
  ('wear_interrupt',        ARRAY['wechat','sms']::varchar[], ARRAY['patient','doctor']::varchar[]),
  ('sensor_drift',          ARRAY['wechat']::varchar[],       ARRAY['tech','ops']::varchar[])
ON CONFLICT (type) DO NOTHING;
