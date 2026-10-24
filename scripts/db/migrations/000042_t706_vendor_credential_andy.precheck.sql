BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T706 precheck（🔴 纯只读，零写）：厂商凭据迁移「前拍」
-- 用法：迁移 000042 执行**前**整段跑一次，读数贴卡；执行后跑 .postcheck.sql 对拍。
-- ══════════════════════════════════════════════════════════════════════════

-- ① 三张对象必须还不存在（存在 ⇒ 本迁移曾跑过/跑了一半，先人工核对再决定是否重跑）
SELECT 'vendors' AS object, to_regclass('public.vendors') IS NOT NULL AS present
UNION ALL SELECT 'vendor_api_keys', to_regclass('public.vendor_api_keys') IS NOT NULL
UNION ALL SELECT 'vendors_id_seq', to_regclass('public.vendors_id_seq') IS NOT NULL
UNION ALL SELECT 'devices.vendor_id',
       EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema = 'public' AND table_name = 'devices' AND column_name = 'vendor_id');

-- ② 快照表不得已存在（有=上次执行留下的，重跑时 CTAS 会被跳过，先核对行数）
SELECT to_regclass('public.devices_t706_bak') IS NOT NULL AS bak_present;

-- ③ 七枚限额键必须都不在（在 ⇒ 改数值前先读现值，别被 DO NOTHING 静默留在旧值）
SELECT config_key, config_value
  FROM sys_configs
 WHERE config_key IN ('vendor_key_qps_limit', 'vendor_key_daily_calls', 'vendor_batch_max_devices',
                      'vendor_secret_fetch_daily', 'vendor_key_ttl_days',
                      'vendor_key_rotation_overlap_hours', 'vendor_key_max_active')
 ORDER BY config_key;

-- ④ 当前迁移水位（本迁移应把 max(version) 从 000041 推到 000042）
SELECT count(*) AS applied_rows, max(version) AS max_version FROM schema_migrations;

-- ⑤ devices 现状（加列前基线；本迁移不改数据，后拍行数与 status 分布必须逐值不动）
SELECT count(*) AS devices_rows,
       count(*) FILTER (WHERE status = 'unbound')  AS unbound,
       count(*) FILTER (WHERE status = 'online')   AS online,
       count(*) FILTER (WHERE status = 'offline')  AS offline,
       count(*) FILTER (WHERE status = 'abnormal') AS abnormal
  FROM devices;

COMMIT;
