-- ══════════════════════════════════════════════════════════════════════════
-- T706 回退（down）：撤掉厂商凭据面
--
-- 🔴 数据面：vendor_api_keys / vendors 是**台账**，down 即整表删除。只在
--    「本迁移刚跑完、尚未签发任何 key」的窗口直接跑本文件。
--    已产生厂商数据时：先 `\copy vendors TO ...` / `\copy vendor_api_keys TO ...`
--    导出留档，再执行；库侧没有第二份台账可还原。
-- 🔴 devices.vendor_id 的既有值随列一并丢失（回滚后所有设备归属回到 NULL）。
--    devices_t706_bak 只备了加列前的行级字段（device_id/model/status/created_at），
--    不像 000041 那样能回灌数据 —— 因为本迁移从未改过 devices 的数据值。
-- 🔴 顺序：先摘 devices 外键列，再删 key 表，最后删厂商表（外键依赖方向）。
-- ══════════════════════════════════════════════════════════════════════════

BEGIN;

DROP INDEX IF EXISTS idx_devices_vendor_id;
ALTER TABLE devices DROP COLUMN IF EXISTS vendor_id;

DROP INDEX IF EXISTS idx_vendor_api_keys_vendor;
DROP INDEX IF EXISTS idx_vendor_api_keys_prefix;
DROP TABLE IF EXISTS vendor_api_keys;
DROP TABLE IF EXISTS vendors;
DROP SEQUENCE IF EXISTS vendors_id_seq;

-- sys_configs 七枚键：本迁移的净新增行。不删是更安全的选择（代码回退后这七行不会被读），
-- 但为「回退后表结构与迁移前逐字一致」的口径，这里删掉本迁移自己的键。
DELETE FROM sys_configs
 WHERE config_key IN ('vendor_key_qps_limit', 'vendor_key_daily_calls', 'vendor_batch_max_devices',
                      'vendor_secret_fetch_daily', 'vendor_key_ttl_days',
                      'vendor_key_rotation_overlap_hours', 'vendor_key_max_active');

-- devices_t706_bak 保留（不 DROP）：回退后仍需对拍，确认无需二次回退再由人工清理。

COMMIT;
