-- 000003_msg_service.up.sql — T017 msg-service 通知域表
-- 对齐：T017 任务文档（补 migration：notification_records + 本地重试队列）
--       api-contracts.ts 消息域 · database-design.md §6（golang-migrate 命名）
--
-- 写归属（架构 §4.2 一期偏离声明，见 api-contracts.ts updateWearReminder/updateNotifyRule 注释）：
--   notification_records / notification_retry_queue / quota_grants 写归 msg-service；
--   patient_preferences（owner: user-service）仅 reminder_*/subscription_quota/updated_at 字段由 msg-service 写；
--   alert_notify_rules（owner: alert-service）仅经 msg-service 后台接口写（写入路径唯一）。

-- 通知发送记录（audit/可追溯，验收 4）
CREATE TABLE notification_records (
  record_id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id   VARCHAR(32) NOT NULL REFERENCES patients(patient_id),
  alert_id     VARCHAR(32),                     -- 关联告警 ID（契约 alertId: string；佩戴提醒为 NULL）
  alert_type   VARCHAR(24),                     -- alerts.type 同域（提醒类为 NULL）
  kind         VARCHAR(16)  NOT NULL DEFAULT 'alert'
               CHECK (kind IN ('alert', 'wear_reminder')),
  channel      VARCHAR(8)   NOT NULL CHECK (channel IN ('wechat', 'sms')),
  status       VARCHAR(12)  NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'sent', 'failed', 'degraded')),
  content      VARCHAR(255) NOT NULL DEFAULT '',
  retry_count  INT          NOT NULL DEFAULT 0,
  sent_at      TIMESTAMPTZ,
  created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_notification_records_patient ON notification_records (patient_id, created_at DESC);
CREATE INDEX idx_notification_records_status ON notification_records (status);
-- 佩戴提醒当日去重（scheduler 按业务时区 Asia/Shanghai 切日查询）
CREATE INDEX idx_notification_records_kind ON notification_records (kind, created_at);

-- 本地重试队列（失败不丢通知，对齐 T010 降级队列模式；DB 版可跨重启恢复）
CREATE TABLE notification_retry_queue (
  queue_id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  record_id     BIGINT      NOT NULL REFERENCES notification_records(record_id),
  retry_count   INT         NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  status        VARCHAR(8)  NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'done', 'failed')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_retry_queue_pending ON notification_retry_queue (next_retry_at) WHERE status = 'pending';

-- 订阅额度授予台账（grantSubscriptionQuota 幂等：同 Idempotency-Key 不重复增额）
CREATE TABLE quota_grants (
  grant_id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  patient_id     VARCHAR(32) NOT NULL REFERENCES patients(patient_id),
  idempotency_key VARCHAR(64) NOT NULL,
  increment      SMALLINT    NOT NULL DEFAULT 1,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (patient_id, idempotency_key)
);
