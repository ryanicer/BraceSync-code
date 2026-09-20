-- T203 回滚：恢复 ÷10 前的旧量纲值
BEGIN;

UPDATE sys_configs SET config_value = '45'  WHERE config_key = 'threshold_pressure_high';
UPDATE sys_configs SET config_value = '60'  WHERE config_key = 'heatmap_max_n';
UPDATE sys_configs SET config_value = '2.8' WHERE config_key = 'threshold_sensor_drift';
UPDATE sys_configs SET config_value = '0.5' WHERE config_key = 'threshold_calibration_offset';
UPDATE sys_configs SET config_value = '0.5' WHERE config_key = 'wearing_pressure_threshold';

COMMIT;
