-- seed-prod.sql —— BraceSync 生产首发最小 seed（T696 手册 V1.2 §9 口径件）
-- 🔴 纪律（红线同手册）：
--   1. 本文件只含字典、1 个管理员账号占位参数与**一对引导组织行（T703 案1）**；除引导组织外禁一切测试家族数据（patients/设备/告警等一律不进）；
--   2. 禁任何真实口令/hash 入库——admins 行经 psql -v 运行时注入，明文与 hash 都不落本文件；
--   3. 执行属生产写：🔴 需 Boss 授权 + 双人复核后方可在 prod 跑；staging 不需要本件（staging 用全量 seed.sql）。
--      例外（T703）：本件的**空库自举实证**在 staging 的独立探针库（迁移全链 + 本文件）上跑，
--      不写 staging 业务库 bracesync——探针库用完即删，见手册 §9.3。
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

-- ===== 3) 系统参数字典（本件 17 行；值=产品默认口径，与迁移 000016–000023 同源） =====
-- 🔴 V1.2/⑥ 终值判据=**20**：本件按 §9.2 在迁移 000001→000038 之后执行，迁移链已插 9 键
--   （000015: data_retention_days/max_patients；000016: threshold_pressure_low/device_offline_minutes/
--    continuous_wear_max_hours/report_timeout_minutes；000023: threshold_calibration_offset/
--    notify_wechat_template_id/notify_sms_template_id），与 seed 17 键重叠 6 → ON CONFLICT 跳过，
--   净增 11，终值 9+11=**20**（迁移独有 3 键：device_offline_minutes/continuous_wear_max_hours/report_timeout_minutes）。
-- 🔴 T173 阈值口径（Boss 2026-09-14 明令）：压力量纲阈值均为「可配置参数」，配置驱动、不硬编码；
-- 现值基于已作废的 N×100 量纲估算，仅作占位、待按 mN/÷1000 量级重定（重定输入：Boss+小顾真实物理范围）。
-- 远期后台运营可调（alert-service config.Manager.Update 已预留写入口），调参不改代码。
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('collect_interval_minutes', '30', '采集间隔（分钟）'),
  ('wear_target_hours', '22', '每日佩戴目标时长（小时）'),
  ('threshold_pressure_high', '5000', '压力偏高阈值（mN，T733 方案A 存储空间；键名沿用历史不改，见卡面注）'),
  ('threshold_pressure_low', '1000', '统一压力下限（mN，T733 存储空间；须小于 threshold_pressure_high；键名沿用历史）'),
  ('threshold_pressure_fluctuation_pct', '30', '压力波动幅度阈值（%）'),
  ('threshold_wear_interrupt_minutes', '60', '佩戴中断判定时间（分钟，须≥2×采集间隔）'),
  ('threshold_sensor_drift', '300', '传感器漂移告警阈值（mN，T733 方案A 存储空间；键名沿用历史不改，见卡面注）'),
  ('threshold_calibration_offset', '50', '空载校准偏差上限（mN，T733 方案A 存储空间；键名沿用历史不改，见卡面注）'),
  ('wearing_pressure_threshold', '50', 'wearing 佩戴判定压力阈值（mN，T733 方案A 存储空间；键名沿用历史不改，见卡面注）'),
  ('heatmap_max_n', '6000', '热力图色阶上界（mN，T733 方案A 存储空间；键名沿用历史不改，见卡面注）'),
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

-- ===== 5) 引导组织（T703 案1：1 支引导团队 + 1 名引导医生 + 其登录账号行）=====
-- 存在理由（T695 一手实证）：全新库 teams/doctors 双空时，建团队要 leader（校验 doctors 存在 → 400）、
--   建医生要 teamId（校验 teams FK → 400），互为前提 ⇒ admin-web/API 无法自举，必须有一笔带外种子把环打开。
-- 互相引用：teams.leader='TST_DOC_BOOT01'（软引用无 FK）↔ doctors.team_id='TST_TEAM_BOOT01'（FK 到 teams）。
-- 🔴 账号行必带（2026-10-16 实建发现）：只插 doctors 行 ⇒ username=None、无登录能力、reset 口令 409，
--   所以 doctors.admin_id 必须指向一条真实 admins 行；该账号口令 hash **复用第 2) 节的运行时注入值**
--   （与首发管理员同口令），首登后由业务立即改口令 —— 本文件仍零凭据。
-- 可清理：业务建出**首个真实团队 + 首个真实医生**（且其可当 leader）后，按 §5c 清理 SQL 删除这三行；
--   判据 = SELECT count(*) FROM teams WHERE team_id NOT IN ('TST_TEAM_BOOT01') >= 1
--        AND SELECT count(*) FROM doctors WHERE doctor_id NOT IN ('TST_DOC_BOOT01') >= 1。
INSERT INTO teams (team_id, name, leader, description, status, team_type)
VALUES ('TST_TEAM_BOOT01', 'TST_引导团队', 'TST_DOC_BOOT01',
        'T703 首库引导团队：破「建团队要 leader / 建医生要 teamId」死锁，业务起步后可清理（§5c）',
        'active', 'medical')
ON CONFLICT DO NOTHING;

INSERT INTO admins (admin_id, username, name, password_hash, role_id, status)
VALUES ('ADM_TST_BOOT01', 'TST_bootstrap', 'TST_引导医生账号', :'password_hash', 'ROLE_DOCTOR', 'enabled')
ON CONFLICT DO NOTHING;

INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id, status)
VALUES ('TST_DOC_BOOT01', 'TST_引导医生', '引导岗（可清理）', 'T703 首库引导', 'TST_TEAM_BOOT01', 'ADM_TST_BOOT01', 'enabled')
ON CONFLICT DO NOTHING;

-- §5c 清理 SQL（业务起步后单独执行，属生产写需 Boss 授权 + 双人复核；顺序 = 子→父，防 FK 挡道）：
--   DELETE FROM doctors WHERE doctor_id='TST_DOC_BOOT01' AND team_id='TST_TEAM_BOOT01';
--   DELETE FROM admins  WHERE admin_id ='ADM_TST_BOOT01' AND username='TST_bootstrap';
--   DELETE FROM teams   WHERE team_id  ='TST_TEAM_BOOT01' AND leader='TST_DOC_BOOT01';
--   前后各拍一次：SELECT count(*) FROM teams WHERE team_id='TST_TEAM_BOOT01';
--                 SELECT count(*) FROM doctors WHERE doctor_id='TST_DOC_BOOT01';
--                 SELECT count(*) FROM admins WHERE admin_id='ADM_TST_BOOT01';   -- 期望 3 → 0

-- ===== 6) 回读自检（=冒烟 S13 判据；输出即留痕，随交件原文落板） =====
SELECT count(*) AS roles_expect_3            FROM roles;
SELECT count(*) AS admins_expect_2           FROM admins;   -- 1 管理员 + 1 引导医生账号
SELECT count(*) AS sys_configs_expect_20     FROM sys_configs;  -- V1.2/⑥：迁移后跑本件终值=20（见第 3) 节推导）
SELECT count(*) AS alert_rules_expect_5      FROM alert_notify_rules;
SELECT (SELECT count(*) FROM patients) AS patients_expect_0,
       (SELECT count(*) FROM teams)    AS teams_expect_1,   -- T703 案1：引导团队（业务再增）
       (SELECT count(*) FROM doctors)  AS doctors_expect_1; -- T703 案1：引导医生（业务再增）
-- 引导组织双向可解析（死锁已破的判据：leader↔team_id 互指 + 账号行挂上 + 账号可登录角色齐）
SELECT (SELECT count(*)
          FROM teams t
          JOIN doctors d ON d.team_id = t.team_id AND d.doctor_id = t.leader
          JOIN admins a  ON a.admin_id = d.admin_id
          JOIN roles r   ON r.role_id = a.role_id
         WHERE t.team_id = 'TST_TEAM_BOOT01'
           AND d.doctor_id = 'TST_DOC_BOOT01'
           AND a.username = 'TST_bootstrap'
           AND r.role_id = 'ROLE_DOCTOR') AS bootstrap_pair_expect_1;

