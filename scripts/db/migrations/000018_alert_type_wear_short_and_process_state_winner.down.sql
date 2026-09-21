-- T257 admin 后端第3批（Winner）回滚
-- 🔴 顺序：先把「新状态/新类型」的存量行改回旧口径，再收窄 CHECK，否则 ALTER 会被既有行挡住。
BEGIN;

-- ── 2.7 回滚 ──
-- 「处理中」在旧两态里不存在，语义最近的回退目标是 pending（并清空 in_progress_at）。
UPDATE alerts
   SET process_status = 'pending', in_progress_at = NULL
 WHERE process_status = 'processing';

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_process_status_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_process_status_check CHECK (
  process_status IN ('pending', 'processed')
);

ALTER TABLE alerts DROP COLUMN IF EXISTS in_progress_at;

-- 恢复 000001:214 的原索引定义（只覆盖 pending）
DROP INDEX IF EXISTS idx_alerts_process;
CREATE INDEX idx_alerts_process ON alerts (process_status) WHERE process_status = 'pending';

-- ── 2.6 回滚 ──
-- wear_duration_short 是回滚前不可能被旧 CHECK 接受的新值 ⇒ 必须先删行。
DELETE FROM alerts WHERE type = 'wear_duration_short';
DELETE FROM alert_notify_rules WHERE type = 'wear_duration_short';

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_type_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_type_check CHECK (
  type IN ('pressure_high', 'pressure_fluctuation', 'wear_interrupt', 'sensor_drift')
);

ALTER TABLE alert_notify_rules DROP CONSTRAINT IF EXISTS alert_notify_rules_type_check;
ALTER TABLE alert_notify_rules ADD CONSTRAINT alert_notify_rules_type_check CHECK (
  type IN ('pressure_high', 'pressure_fluctuation', 'wear_interrupt', 'sensor_drift')
);

-- 注：不回滚 pressure_fluctuation 的产生逻辑（停产生是代码侧行为，非数据侧），
--     也不动 threshold_pressure_fluctuation_pct 配置键 —— 历史告警行仍依赖该字面量可读。

COMMIT;
