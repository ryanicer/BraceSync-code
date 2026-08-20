-- BraceSync 初始 schema（golang-migrate up）
-- 对齐：docs/ V1.0
-- 说明：单库 shared database，枚举用 VARCHAR + CHECK，时间 timestamptz(UTC)
-- 迁移顺序处理 baselines <-> install_records 循环引用（先建表后补 FK）

BEGIN;

-- ============ 4.1 身份域 ============

CREATE TABLE teams (
  team_id        VARCHAR(32)  PRIMARY KEY,
  name           VARCHAR(128) NOT NULL,
  member_count   INT          NOT NULL DEFAULT 0,
  patient_count  INT          NOT NULL DEFAULT 0,
  created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE roles (
  role_id          VARCHAR(32)  PRIMARY KEY,
  name             VARCHAR(64)  NOT NULL,
  description      VARCHAR(255),
  permissions_json JSONB        NOT NULL DEFAULT '{}',
  status           VARCHAR(16)  NOT NULL DEFAULT 'enabled'
                     CHECK (status IN ('enabled','disabled')),
  created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE admins (
  admin_id       VARCHAR(32)  PRIMARY KEY,
  username       VARCHAR(64)  NOT NULL UNIQUE,       -- 登录用户名（区别于 admin_id 内部 ID 与 name 显示名）
  name           VARCHAR(64)  NOT NULL,
  password_hash  VARCHAR(128) NOT NULL,
  role_id        VARCHAR(32)  NOT NULL REFERENCES roles(role_id),
  status         VARCHAR(16)  NOT NULL DEFAULT 'enabled'
                   CHECK (status IN ('enabled','disabled')),
  created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE doctors (
  doctor_id    VARCHAR(32)  PRIMARY KEY,
  name         VARCHAR(64)  NOT NULL,
  title        VARCHAR(64),
  department   VARCHAR(128),
  team_id      VARCHAR(32)  REFERENCES teams(team_id),
  phone_enc    BYTEA,
  phone_hash   CHAR(64),
  admin_id     VARCHAR(32)  REFERENCES admins(admin_id),
  status       VARCHAR(16)  NOT NULL DEFAULT 'enabled'
                 CHECK (status IN ('enabled','disabled')),
  created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE technicians (
  tech_id       VARCHAR(32)  PRIMARY KEY,
  name          VARCHAR(64)  NOT NULL,
  phone_enc     BYTEA        NOT NULL,
  phone_hash    CHAR(64)     NOT NULL,
  team_id       VARCHAR(32)  REFERENCES teams(team_id),
  install_count INT          NOT NULL DEFAULT 0,
  status        VARCHAR(16)  NOT NULL DEFAULT 'enabled'
                  CHECK (status IN ('enabled','disabled')),
  auth_status   VARCHAR(16)  NOT NULL DEFAULT 'authorized'
                  CHECK (auth_status IN ('authorized','unauthorized')),
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uk_technicians_phone_hash ON technicians(phone_hash);

CREATE TABLE patients (
  patient_id        VARCHAR(32)  PRIMARY KEY,
  name              VARCHAR(64)  NOT NULL,
  phone_enc         BYTEA        NOT NULL,
  phone_hash        CHAR(64)     NOT NULL,
  gender            VARCHAR(8)   CHECK (gender IN ('male','female')),
  age               INT          CHECK (age BETWEEN 0 AND 150),
  diagnosis         VARCHAR(255),
  cobb_angle        NUMERIC(5,2),
  device_id         VARCHAR(48),
  team_id           VARCHAR(32)  REFERENCES teams(team_id),
  primary_doctor_id VARCHAR(32)  REFERENCES doctors(doctor_id),
  status            VARCHAR(16)  NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('active','pending')),
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()  -- 换绑/换团队/换医生等归属变更行级时间戳（应用层刷新）
);
CREATE UNIQUE INDEX idx_patients_phone_hash ON patients(phone_hash);
CREATE INDEX idx_patients_team ON patients(team_id);
CREATE INDEX idx_patients_doctor ON patients(primary_doctor_id);

-- ============ 4.2 设备域 ============

CREATE TABLE devices (
  device_id          VARCHAR(48)  PRIMARY KEY,
  model              VARCHAR(32)  NOT NULL DEFAULT 'PRS-ML05-RC',
  firmware_version   VARCHAR(32),
  device_secret_enc  BYTEA        NOT NULL,
  secret_version     INT          NOT NULL DEFAULT 1,
  patient_id         VARCHAR(32)  REFERENCES patients(patient_id),
  wifi_ssid          VARCHAR(128),
  bind_time          TIMESTAMPTZ,
  status             VARCHAR(16)  NOT NULL DEFAULT 'unbound'
                       CHECK (status IN ('online','offline','abnormal','unbound')),
  last_report_at     TIMESTAMPTZ,
  created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()  -- status/firmware/绑定变更行级时间戳（应用层刷新）
);
CREATE INDEX idx_devices_patient ON devices(patient_id);
CREATE INDEX idx_devices_status ON devices(status);

-- 循环引用：先不带 install_id 的 FK
CREATE TABLE baselines (
  baseline_id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  install_id     BIGINT       NOT NULL,
  device_id      VARCHAR(48)  NOT NULL REFERENCES devices(device_id),
  offset_values  REAL[]       NOT NULL,
  calibrator_id  VARCHAR(32)  NOT NULL REFERENCES technicians(tech_id),
  created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
  CONSTRAINT chk_baselines_offset_len CHECK (array_length(offset_values,1) = 20)
);

CREATE TABLE install_records (
  install_id     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  device_id      VARCHAR(48)  NOT NULL REFERENCES devices(device_id),
  patient_id     VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  tech_id        VARCHAR(32)  NOT NULL REFERENCES technicians(tech_id),
  calibrate_time TIMESTAMPTZ  NOT NULL,
  baseline_id    BIGINT       REFERENCES baselines(baseline_id),
  notes          TEXT,
  signature_url  VARCHAR(512),
  wifi_status    VARCHAR(16)  NOT NULL DEFAULT 'unconfigured'
                   CHECK (wifi_status IN ('connected','unconfigured')),
  created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_install_device ON install_records(device_id);
CREATE INDEX idx_install_tech ON install_records(tech_id);
CREATE INDEX idx_install_patient ON install_records(patient_id);

-- 补 baselines.install_id 的 FK（此时 install_records 已存在）
ALTER TABLE baselines
  ADD CONSTRAINT fk_baselines_install
  FOREIGN KEY (install_id) REFERENCES install_records(install_id);

-- ============ 4.3 时序与聚合域 ============

CREATE TABLE pressure_records (
  record_id     BIGINT GENERATED ALWAYS AS IDENTITY,
  device_id     VARCHAR(48)  NOT NULL,
  patient_id    VARCHAR(32)  NOT NULL,
  ts            TIMESTAMPTZ  NOT NULL,
  p01 REAL NOT NULL, p02 REAL NOT NULL, p03 REAL NOT NULL, p04 REAL NOT NULL,
  p05 REAL NOT NULL, p06 REAL NOT NULL, p07 REAL NOT NULL, p08 REAL NOT NULL,
  p09 REAL NOT NULL, p10 REAL NOT NULL, p11 REAL NOT NULL, p12 REAL NOT NULL,
  p13 REAL NOT NULL, p14 REAL NOT NULL, p15 REAL NOT NULL, p16 REAL NOT NULL,
  p17 REAL NOT NULL, p18 REAL NOT NULL, p19 REAL NOT NULL, p20 REAL NOT NULL,
  max_pressure  REAL GENERATED ALWAYS AS (
                  greatest(p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
                           p11,p12,p13,p14,p15,p16,p17,p18,p19,p20)) STORED,
  upload_time   TIMESTAMPTZ  NOT NULL DEFAULT now(),
  PRIMARY KEY (record_id, ts),
  UNIQUE (device_id, ts)
) PARTITION BY RANGE (ts);

CREATE INDEX idx_pr_patient_ts ON pressure_records (patient_id, ts DESC);

-- 初始分区：预建当前及未来 2 个月（此后由 data-service cron 每月 25 日预建）
CREATE TABLE pressure_records_202607 PARTITION OF pressure_records
  FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE pressure_records_202608 PARTITION OF pressure_records
  FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE pressure_records_202609 PARTITION OF pressure_records
  FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

CREATE TABLE daily_wear_stats (
  patient_id      VARCHAR(32) NOT NULL,
  stat_date       DATE        NOT NULL,
  wear_minutes    INT         NOT NULL DEFAULT 0,
  avg_pressure    REAL        NOT NULL DEFAULT 0,
  max_pressure    REAL        NOT NULL DEFAULT 0,
  max_point       VARCHAR(4),
  frame_count     INT         NOT NULL DEFAULT 0,
  abnormal_count  INT         NOT NULL DEFAULT 0,
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (patient_id, stat_date)
);

-- ============ 4.4 告警与业务域 ============

CREATE TABLE alerts (
  alert_id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id      VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  device_id       VARCHAR(48)  NOT NULL REFERENCES devices(device_id),
  type            VARCHAR(24)  NOT NULL
                    CHECK (type IN ('pressure_high','pressure_fluctuation',
                                    'wear_interrupt','sensor_drift')),
  detail          VARCHAR(255),
  sensor_point    VARCHAR(4),
  threshold_value REAL,
  actual_value    REAL,
  ts              TIMESTAMPTZ  NOT NULL,
  read_status     VARCHAR(8)   NOT NULL DEFAULT 'unread'
                    CHECK (read_status IN ('read','unread')),
  process_status  VARCHAR(12)  NOT NULL DEFAULT 'pending'
                    CHECK (process_status IN ('pending','processed')),
  resolved_status VARCHAR(12)  NOT NULL DEFAULT 'active'
                    CHECK (resolved_status IN ('active','resolved')),
  resolved_at     TIMESTAMPTZ,
  processed_by    VARCHAR(32),
  processed_at    TIMESTAMPTZ,
  process_note    VARCHAR(512),
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
  -- 自然唯一键：同一患者/设备/类型/时刻的告警唯一；DB 层保底 alert-service 应用层去重窗口
  CONSTRAINT uk_alerts_natural UNIQUE (patient_id, device_id, type, ts)
);
CREATE INDEX idx_alerts_patient_ts ON alerts (patient_id, ts DESC);
CREATE INDEX idx_alerts_process ON alerts (process_status) WHERE process_status = 'pending';
CREATE INDEX idx_alerts_type ON alerts (type);

CREATE TABLE orthosis_plans (
  plan_id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id   VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  doctor_id    VARCHAR(32)  NOT NULL REFERENCES doctors(doctor_id),
  content      TEXT         NOT NULL,
  version      VARCHAR(16)  NOT NULL,
  created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_plans_patient ON orthosis_plans (patient_id, created_at DESC);

CREATE TABLE feeling_logs (
  log_id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id       VARCHAR(32) NOT NULL REFERENCES patients(patient_id),
  log_date         DATE        NOT NULL,
  comfort_score    NUMERIC(2,1) CHECK (comfort_score BETWEEN 0.5 AND 5),  -- 1–5 星可半星（PRD §7A.7），最低 0.5
  discomfort_areas VARCHAR(16)[],
  notes            VARCHAR(200),
  reply_content    VARCHAR(200),
  reply_time       TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uk_feeling_patient_date UNIQUE (patient_id, log_date)  -- 每患者每日一条（PRD §7A.7），兼防重写
);
CREATE INDEX idx_feeling_patient_date ON feeling_logs (patient_id, log_date DESC);

CREATE TABLE feedbacks (
  feedback_id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id    VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  type          VARCHAR(32),
  content       VARCHAR(500) NOT NULL,
  submit_time   TIMESTAMPTZ  NOT NULL DEFAULT now(),
  handler       VARCHAR(32),
  reply_content VARCHAR(500),
  reply_time    TIMESTAMPTZ,
  status        VARCHAR(12)  NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','replied','resolved'))
);
CREATE INDEX idx_feedbacks_patient ON feedbacks (patient_id, submit_time DESC);
CREATE INDEX idx_feedbacks_status ON feedbacks (status) WHERE status <> 'resolved';

CREATE TABLE health_reports (
  report_id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id           VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  report_type          VARCHAR(8)   NOT NULL CHECK (report_type IN ('weekly','monthly')),
  period_start         DATE         NOT NULL,
  period_end           DATE         NOT NULL,
  wear_compliance_rate NUMERIC(5,2),
  avg_pressure         REAL,
  trend_judgment       VARCHAR(8)   CHECK (trend_judgment IN ('up','flat','down')),
  suggestion           TEXT,
  generate_time        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  UNIQUE (patient_id, report_type, period_start)
);

CREATE TABLE patient_preferences (
  pref_id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id               VARCHAR(32) NOT NULL UNIQUE REFERENCES patients(patient_id),
  reminder_enabled         BOOLEAN     NOT NULL DEFAULT false,
  reminder_time            TIME,
  subscription_auth_status VARCHAR(12) NOT NULL DEFAULT 'closed'
                             CHECK (subscription_auth_status IN ('authorized','rejected','closed')),
  subscription_quota       SMALLINT    NOT NULL DEFAULT 3,
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 监护人同意记录（PIPL 未成年人硬性要求，隐私合规 §3）：与偏好分离，支持版本追踪与撤回
CREATE TABLE consents (
  consent_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id          VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
  consent_type        VARCHAR(32)  NOT NULL
                        CHECK (consent_type IN ('privacy_policy','sensitive_data','third_party_share')),
  policy_version      VARCHAR(16)  NOT NULL,    -- 同意的隐私政策版本号（更新需重新征得）
  consenter_name      VARCHAR(64),              -- 监护人姓名
  consenter_relation  VARCHAR(32),              -- father/mother/legal_guardian
  granted_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
  withdrawn_at        TIMESTAMPTZ,              -- 撤回时间（NULL=有效）
  withdrawn_reason    VARCHAR(255),
  ip                  VARCHAR(45),              -- 同意/撤回时 IP（审计）
  created_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_consents_patient ON consents (patient_id, consent_type, granted_at DESC);
CREATE INDEX idx_consents_active ON consents (patient_id) WHERE withdrawn_at IS NULL;

-- ============ 4.5 审计与配置域 ============

CREATE TABLE audit_logs (
  log_id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  operator_id   VARCHAR(32),
  operator_role VARCHAR(32),
  action        VARCHAR(64)  NOT NULL,
  target_type   VARCHAR(32),
  target_id     VARCHAR(64),
  detail        JSONB,
  ip            VARCHAR(45),
  ts            TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_operator_ts ON audit_logs (operator_id, ts DESC);
CREATE INDEX idx_audit_target ON audit_logs (target_type, target_id);

CREATE TABLE sys_configs (
  config_key   VARCHAR(64)  PRIMARY KEY,
  config_value VARCHAR(255) NOT NULL,
  description  VARCHAR(255),
  updated_by   VARCHAR(32),
  updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 告警通知规则（每告警类型的通知渠道与对象，PRD §7D.6）；结构化多维度，不宜入 sys_configs KV
CREATE TABLE alert_notify_rules (
  type           VARCHAR(24)  PRIMARY KEY
                   CHECK (type IN ('pressure_high','pressure_fluctuation','wear_interrupt','sensor_drift')),
  channels       VARCHAR(8)[]  NOT NULL,   -- wechat / sms
  notify_targets VARCHAR(8)[]  NOT NULL,   -- patient / doctor / tech / ops
  updated_by     VARCHAR(32),
  updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMIT;
