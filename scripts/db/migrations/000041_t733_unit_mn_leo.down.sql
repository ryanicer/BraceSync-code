-- ══════════════════════════════════════════════════════════════════════════
-- T733 方案A 回退（down）：把量纲搬回迁移前状态
-- 依据：up 里的四张 *_t733_bak 快照表 = 唯一可信回退源（快照先行）。
-- 纪律：只在「up 刚跑完且代码尚未发布」的窗口用；代码已按 mN 上线后再回退 = 数据与代码两层错量纲，
--       必须先回退服务版本再跑本文件（顺序写反比不回退更糟）。
-- ══════════════════════════════════════════════════════════════════════════

BEGIN;

-- ① 阈值配置与基线偏移：从快照整表回灌（只回被本迁移动过的键/列，其余键 up 未改，无需动）
UPDATE sys_configs s
   SET config_value = b.config_value,
       updated_at   = b.updated_at
  FROM sys_configs_t733_bak b
 WHERE s.config_key = b.config_key
   AND s.config_key IN ('threshold_pressure_high', 'threshold_pressure_low',
                        'heatmap_max_n', 'wearing_pressure_threshold',
                        'threshold_sensor_drift', 'threshold_calibration_offset');

UPDATE baselines bl
   SET offset_values = b.offset_values
  FROM baselines_t733_bak b
 WHERE bl.baseline_id = b.baseline_id;

-- ②③ 帧 / 日聚合 / 告警：这三处 up 里有条件过滤（>5000、列改名），逐行反算不如按主键回灌快照
UPDATE pressure_records pr
   SET p01 = b.p01, p02 = b.p02, p03 = b.p03, p04 = b.p04, p05 = b.p05,
       p06 = b.p06, p07 = b.p07, p08 = b.p08, p09 = b.p09, p10 = b.p10,
       p11 = b.p11, p12 = b.p12, p13 = b.p13, p14 = b.p14, p15 = b.p15,
       p16 = b.p16, p17 = b.p17, p18 = b.p18, p19 = b.p19, p20 = b.p20
  FROM pressure_records_t733_bak b
 WHERE pr.record_id = b.record_id;

UPDATE alert_point_rules pr
   SET upper_n = b.upper_n,
       lower_n = b.lower_n
  FROM alert_point_rules_t733_bak b
 WHERE pr.point_id = b.point_id;

UPDATE alerts a
   SET threshold_value = b.threshold_value,
       actual_value    = b.actual_value
  FROM alerts_t733_bak b
 WHERE a.alert_id = b.alert_id;

-- 印章列：先改回旧列名，再从快照整列回灌
--   （快照表 daily_wear_stats_t733_bak 建于 up 的 A 段改名之前，列名仍是 wearing_threshold_n，
--    所以必须「先改名、后回灌」；反算 ÷1000 只作兜底，快照在时以快照为准）
DO $t733_down_schema$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'daily_wear_stats' AND column_name = 'wearing_threshold_mn') THEN
        ALTER TABLE daily_wear_stats RENAME COLUMN wearing_threshold_mn TO wearing_threshold_n;
    END IF;
END
$t733_down_schema$;

UPDATE daily_wear_stats d
   SET max_pressure = b.max_pressure,
       avg_pressure = b.avg_pressure,
       wearing_threshold_n = b.wearing_threshold_n
  FROM daily_wear_stats_t733_bak b
 WHERE d.patient_id = b.patient_id_snap     -- 快照表的该列已改名（避开 T467 删除面门禁）
   AND d.stat_date  = b.stat_date;

-- alert_point_rules 列宽回退（up 的 A 段把 NUMERIC(6,2) 加宽到 (9,2)）：
-- 只缩列宽不改语义；若回退后仍有 >9999.99 的残留值会报错，属预期——说明数据未回灌干净，停手回报。
ALTER TABLE alert_point_rules ALTER COLUMN upper_n TYPE NUMERIC(6,2);
ALTER TABLE alert_point_rules ALTER COLUMN lower_n TYPE NUMERIC(6,2);

COMMENT ON COLUMN daily_wear_stats.wearing_threshold_n IS
  '本次聚合实际生效的佩戴帧判定阈值（单位 N，迁移 000027 原口径）';

COMMIT;

-- 快照表保留（不 DROP）：回退后仍需对拍；确认无需二次回退再由 Andy 清理。
