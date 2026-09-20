-- T203 回滚：恢复 ÷10 前的旧量纲值
BEGIN;

UPDATE sys_configs SET value = '45'  WHERE key = 'threshold_pressure_high';
UPDATE sys_configs SET value = '60'  WHERE key = 'heatmap_max_n';
UPDATE sys_configs SET value = '2.8' WHERE key = 'threshold_sensor_drift';
UPDATE sys_configs SET value = '0.5' WHERE key = 'threshold_calibration_offset';
UPDATE sys_configs SET value = '0.5' WHERE key = 'wearing_pressure_threshold';

COMMIT;
