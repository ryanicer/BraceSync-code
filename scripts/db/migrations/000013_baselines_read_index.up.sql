-- BraceSync T173 baselines 读取索引（golang-migrate up）
-- 目的：data-service 读侧校准（calibration.Store.GetLatestBaseline）按
--   SELECT baseline_id, offset_values FROM baselines WHERE device_id = $1
--   ORDER BY baseline_id DESC LIMIT 1
-- 取设备当前权威基线（规矩 A：单次校准、禁复校，最新 baseline_id 即当前版）。
-- baselines 原先零索引（000001 全仓唯一无显式索引的邻居表），实时路径将走 seq scan。
--
-- ⚠️ 应用本迁移前预检（照 000012 风格，异常即停手）：
--   SELECT count(*) FROM baselines;  -- 表体量评估；当前量级（种子/早期安装）预期极小
--
-- 跨服务只读注意：baselines owner = device-service（写），data-service 仅只读（D3 裁决）。
-- schema 变更已获 Boss 签字（T173-decision D7，2026-09-13）。
BEGIN;

CREATE INDEX idx_baselines_device ON baselines(device_id);

COMMIT;
