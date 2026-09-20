-- T203 压力阈值统一下调 ÷10（Boss 2026-09-14 明令，并入 T257 Winner 执行）
-- 背景：seed.sql 的 ON CONFLICT DO NOTHING 使存量库从未真正应用新值；
--       本迁移用显式 UPDATE 确保 staging/生产已部署实例同步生效。
-- 不动键：threshold_wear_interrupt_minutes(60) / wear_target_hours(22) / collect_interval_minutes(30)
--         / collect_interval_seconds(1800) / threshold_pressure_fluctuation_pct(30)
-- owner: sys_configs（user-service / alert-service / data-service 共享读）

BEGIN;

UPDATE sys_configs SET value = '5'    WHERE key = 'threshold_pressure_high';       -- 45 → 5
UPDATE sys_configs SET value = '6'    WHERE key = 'heatmap_max_n';                -- 60 → 6
UPDATE sys_configs SET value = '0.3'  WHERE key = 'threshold_sensor_drift';       -- 2.8 → 0.3
UPDATE sys_configs SET value = '0.05' WHERE key = 'threshold_calibration_offset';  -- 0.5 → 0.05
UPDATE sys_configs SET value = '0.05' WHERE key = 'wearing_pressure_threshold';   -- 0.5 → 0.05

COMMIT;
