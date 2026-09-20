-- T257 admin 后端第3批（Winner）：2.6 告警类型对齐设计稿四类 + 2.7 告警处理三态
-- 对齐：docs/tasks/winner/T257-admin后端第3批-prompt.md · 契约 docs/api/api-contracts.ts（docs PR #150 已合并）
-- 裁定：Boss/PM 2026-09-20 —— Q1=方案A（严格照设计稿四类；「设备离线」≡ 既有 wear_interrupt，同一件事）
--                     Q2=(c) 子权限仅前端呈现（本迁移不涉及）
--                     Q3=压力三档合成两键、不加第三键（本迁移不涉及）
-- owner：alerts / alert_notify_rules 均为 alert-service 写、msg-service 读规则（shared DB，同 000001）

BEGIN;

-- ── 2.6 告警类型枚举：4 值 → 5 值（alerts.type / alert_notify_rules.type）──────────────────
-- 设计稿 docs/design/admin/告警管理.html:246-249 四类 = 压力偏高 / 设备离线 / 佩戴时长不足 / 传感器标定异常。
-- 与实现比对后净变化两处：
--   ① 新增 wear_duration_short（佩戴时长不足）——当日累计佩戴 < wear_target_hours；
--   ② pressure_fluctuation（压力波动）自本迁移起 **停止产生**（引擎不再评估该规则），
--      但字面量**保留在 CHECK 内**：000001:191-193 起即有历史行（seed.sql:164-166 亦有示例），
--      把它从 CHECK 里摘掉会让既有行的读取/筛选直接违反约束，等于自毁历史数据可读性。
-- 🔴 alert_notify_rules.type 同时是该表主键（000001:324-326），放宽 CHECK 后必须补一行规则：
--    msg-service 路由按类型查规则，**查不到规则即静默不发送**（notify.go:124-129 accepted=false），
--    故新类型缺规则行 = 告警进库但没人收到通知。渠道/对象口径同 wear_interrupt（患者+医生，微信优先降级短信）。
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_type_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_type_check CHECK (
  type IN ('pressure_high', 'pressure_fluctuation', 'wear_interrupt', 'sensor_drift', 'wear_duration_short')
);

ALTER TABLE alert_notify_rules DROP CONSTRAINT IF EXISTS alert_notify_rules_type_check;
ALTER TABLE alert_notify_rules ADD CONSTRAINT alert_notify_rules_type_check CHECK (
  type IN ('pressure_high', 'pressure_fluctuation', 'wear_interrupt', 'sensor_drift', 'wear_duration_short')
);

-- 幂等补播：CI 集成测试 harness 只跑 migrations、不跑 seed.sql ⇒ 规则行必须由迁移自带，否则真库断言必缺行。
INSERT INTO alert_notify_rules (type, channels, notify_targets) VALUES
  ('wear_duration_short', ARRAY['wechat', 'sms']::varchar[], ARRAY['patient', 'doctor']::varchar[])
ON CONFLICT (type) DO NOTHING;

-- ── 2.7 处理态两态 → 三态（待处理 / 处理中 / 已处理）──────────────────────────────────────
-- 设计稿 docs/design/admin/告警管理.html:239 + 筛选下拉「全部状态/待处理/处理中/已处理」。
-- 🔴 resolved_status 不并入三态：它是「设备是否自愈」的正交轴（扫描器在设备恢复上报时自动置 resolved，
--    scanner.go recoverDevice），合并会丢掉「无人工处理但已恢复」那一格。
-- 存量行零影响：新列可空，老行 process_status 仍是 pending/processed，CHECK 只放宽不收紧。
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_process_status_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_process_status_check CHECK (
  process_status IN ('pending', 'processing', 'processed')
);

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS in_progress_at TIMESTAMPTZ;

COMMENT ON COLUMN alerts.in_progress_at IS
  'T257 2.7 进入「处理中」的时刻；NULL = 从未进入过处理中（含迁移前的全部历史行）⇒ 前端耗时列显示「—」，不得按 0 计';

-- 「处理中」也需要在待办列表里被筛出来 ⇒ 原索引只覆盖 pending（000001:214），扩到 pending + processing。
DROP INDEX IF EXISTS idx_alerts_process;
CREATE INDEX IF NOT EXISTS idx_alerts_process ON alerts (process_status)
  WHERE process_status IN ('pending', 'processing');

COMMIT;
