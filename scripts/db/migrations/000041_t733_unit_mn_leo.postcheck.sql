BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T733 postcheck（🔴 纯只读，零写）：量纲迁移「后拍」，与 .precheck.sql 逐条同形对拍
-- 用法：迁移 000041 执行**后**由 Andy 在目标库整段跑一次，读数贴回卡面评论（验收判据 5）。
-- 对拍口径：每条都写明「期望值」，不符就是没跑对，不要放行。
-- ══════════════════════════════════════════════════════════════════════════

-- ① 六枚压力量纲阈值期望（mN 存储空间）：
--   threshold_pressure_high=5000  threshold_pressure_low=1000  heatmap_max_n=6000
--   wearing_pressure_threshold=50  threshold_sensor_drift=300  threshold_calibration_offset=50
--   任一仍是小数（5 / 0.05 / 0.3 …）⇒ 000041 未生效
SELECT config_key, config_value, updated_at
FROM sys_configs
WHERE config_key IN ('threshold_pressure_high', 'threshold_pressure_low', 'heatmap_max_n',
                     'wearing_pressure_threshold', 'threshold_sensor_drift', 'threshold_calibration_offset')
ORDER BY config_key;

-- ①b 非压力量纲配置必须与「前拍 ①b」逐键等值（000041 不该碰它们；有一行变了就是写错了面）
SELECT config_key, config_value
FROM sys_configs
WHERE config_key NOT IN ('threshold_pressure_high', 'threshold_pressure_low', 'heatmap_max_n',
                         'wearing_pressure_threshold', 'threshold_sensor_drift', 'threshold_calibration_offset')
ORDER BY config_key;

-- ② 基线腿：期望 max_abs_offset 全部 ≥ 1000 量级带（N 时代的 0.3xx 不该再出现）
SELECT count(*)                                   AS baselines_rows,
       min(offset_values[1])                      AS min_o1,
       max(offset_values[1])                      AS max_o1,
       count(*) FILTER (WHERE max_abs_offset < 1) AS still_newton_space_rows
FROM (
  SELECT baseline_id, offset_values,
         (SELECT max(abs(v)) FROM unnest(offset_values) AS v) AS max_abs_offset
  FROM baselines
) t;

-- ③ 帧值分桶：期望 **零行落在 z（≤50）** 之外的错标带；桶 b 应为空或仅剩真实高施力
SELECT CASE
         WHEN frame_max >  20000 THEN 'c 大于20000mN（物理不可能）'
         WHEN frame_max >   5000 THEN 'b 5000-20000mN（>5000 已被 000041 修正，此处应为空）'
         WHEN frame_max >     50 THEN 'a 50-5000mN（mN 语义正常带）'
         ELSE                         'z 不大于50（N 语义遗留，000041 未处理，只列清单）'
       END                                        AS bucket,
       count(*)                                   AS rows_cnt,
       min(ts)                                    AS ts_from,
       max(ts)                                    AS ts_to,
       round(max(frame_max)::numeric, 3)          AS max_value
FROM (
  SELECT record_id, ts,
         greatest(p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
                  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20) AS frame_max
  FROM pressure_records
) t
GROUP BY 1 ORDER BY 1;

-- ④ 快照表必须四张都在且行数可核对（改前取证 = 回退依据）
SELECT 'sys_configs'      AS tbl, count(*) FROM sys_configs_t733_bak
UNION ALL SELECT 'baselines',           count(*) FROM baselines_t733_bak
UNION ALL SELECT 'pressure_records',    count(*) FROM pressure_records_t733_bak
UNION ALL SELECT 'daily_wear_stats',    count(*) FROM daily_wear_stats_t733_bak
UNION ALL SELECT 'alerts',              count(*) FROM alerts_t733_bak;

-- ⑤ 日聚合行：列名必须已是 wearing_threshold_mn（改名的直接证据），且峰值回到 mN 量程
SELECT count(*)                                                    AS stat_rows,
       count(*) FILTER (WHERE wearing_threshold_mn IS NOT NULL)    AS stamped_rows,
       round(max(max_pressure)::numeric, 3)                        AS max_pressure_high_water
FROM daily_wear_stats;

-- ⑥ 告警行压力量纲两列期望已 ×1000（与帧同存储空间；读侧出口 ÷1000 展示）
SELECT type, count(*) AS rows_cnt,
       round(min(actual_value)::numeric, 3)   AS min_actual,
       round(max(actual_value)::numeric, 3)   AS max_actual
FROM alerts
WHERE type IN ('pressure_high', 'pressure_low', 'sensor_drift')
GROUP BY type ORDER BY type;

COMMIT;
