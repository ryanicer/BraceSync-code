-- T653 告警类型 ↔ 流程模板绑定：新表 flow_type_bindings（PRD V3.44 §8.2 提案行落地）
--
-- 口径依据（T653 卡 + PRD V3.44 §7D.6 Tab4 R8，Boss 2026-10-09 全裁按甲，裁定评论 1133005753001004790）：
--   显式绑定、无隐式兜底 —— 类型无绑定行则不自动创建流程实例，告警照常产生与通知（通知链与流程链解耦）；
--   一告警一实例幂等（user-service 建实例既有 409 语义不变）；绑定变更不追溯在途实例（实例自持 template_id）。
--
-- 字段先例坐标：
--   alert_type  ← 000001 alerts.type VARCHAR(24)；本表用 VARCHAR(32) 与 PRD §8.2 提案行一致。
--                 值域 = 现行四类（000018 已扩 wear_duration_short）：
--                   pressure_high        压力偏高
--                   wear_interrupt       设备离线（000018 注：「设备离线」≡ wear_interrupt，同一件事）
--                   sensor_drift         传感器标定异常
--                   wear_duration_short  佩戴时长不足
--                 pressure_fluctuation 已砍（000018 保留 CHECK 仅为历史行可读），不入本词表。
--   template_id ← 000020 flow_template.template_id VARCHAR(32) PK，直接 FK。
--   updated_by  ← 000001 alert_notify_rules.updated_by VARCHAR(32) 同形；不建账号外键（口径同 flow_template.creator）。
--   updated_at  ← 000020 flow_template.updated_at 同形，应用层 upsert 时刷新。
--
-- 不选「flow_template 加 bound_type 列」方案（PRD §8.2 明示）：绑定是「类型→模板」多对一映射，
-- 塞模板行会污染模板库语义。
BEGIN;

CREATE TABLE IF NOT EXISTS flow_type_bindings (
  alert_type   VARCHAR(32)  PRIMARY KEY
               CHECK (alert_type IN ('pressure_high',
                                     'wear_interrupt',
                                     'sensor_drift',
                                     'wear_duration_short')),
  template_id  VARCHAR(32)  NOT NULL REFERENCES flow_template(template_id),
  updated_by   VARCHAR(32)  NOT NULL,
  updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE flow_type_bindings IS
  'T653 告警类型到流程模板的显式绑定（PRD V3.44 §8.2，R8 甲）：一行一类型，无行=不自动建实例（告警照常通知）；'
  '保存写审计 action=data_modify target=flow_binding/<alert_type>；绑定变更不追溯在途实例。';

COMMIT;
