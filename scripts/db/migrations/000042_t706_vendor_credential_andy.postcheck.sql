BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T706 postcheck（🔴 纯只读，零写）：厂商凭据迁移「后拍」，与 .precheck.sql 逐条对拍
-- 对拍口径：每条写完期望值，不符即没跑对，不放行。
-- ══════════════════════════════════════════════════════════════════════════

-- ① 三张对象必须都在
SELECT 'vendors' AS object, to_regclass('public.vendors') IS NOT NULL AS present
UNION ALL SELECT 'vendor_api_keys', to_regclass('public.vendor_api_keys') IS NOT NULL
UNION ALL SELECT 'vendors_id_seq', to_regclass('public.vendors_id_seq') IS NOT NULL
UNION ALL SELECT 'devices.vendor_id',
       EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema = 'public' AND table_name = 'devices' AND column_name = 'vendor_id');

-- ② 列清单与约束：期望 devices.vendor_id 可空、带外键；vendor_api_keys 两个 CHECK 与一个唯一约束在位
SELECT table_name, column_name, data_type, is_nullable, character_maximum_length
  FROM information_schema.columns
 WHERE table_schema = 'public' AND table_name IN ('vendors', 'vendor_api_keys')
 ORDER BY table_name, ordinal_position;

SELECT conrelid::regclass AS tbl, conname, contype
  FROM pg_constraint
 WHERE conrelid IN ('vendors'::regclass, 'vendor_api_keys'::regclass, 'devices'::regclass)
   AND (conname LIKE 'vendors%' OR conname LIKE 'vendor_api_keys%' OR conname LIKE 'devices_vendor_id%')
 ORDER BY 1, 2;

-- ③ 七枚限额键必须齐且值 = 设计默认（90 天 / 24h / 3 把 / 200 台 / 10 QPS / 5 万 / 2000 次）
SELECT config_key, config_value
  FROM sys_configs
 WHERE config_key IN ('vendor_key_qps_limit', 'vendor_key_daily_calls', 'vendor_batch_max_devices',
                      'vendor_secret_fetch_daily', 'vendor_key_ttl_days',
                      'vendor_key_rotation_overlap_hours', 'vendor_key_max_active')
 ORDER BY config_key;

-- ④ 迁移水位期望 max_version = 000042
SELECT count(*) AS applied_rows, max(version) AS max_version FROM schema_migrations;

-- ⑤ 快照表行数必须 = 加列前 devices 行数（后拍 ⑤ 与前拍 ⑤ 的 devices_rows 相等）
SELECT count(*) AS devices_rows, count(*) FILTER (WHERE vendor_id IS NOT NULL) AS vendor_bound_rows
  FROM devices;
SELECT count(*) AS bak_rows FROM devices_t706_bak;

COMMIT;
