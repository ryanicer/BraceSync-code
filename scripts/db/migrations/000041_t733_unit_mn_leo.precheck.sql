BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T733 precheck（🔴 纯只读，零写）：方案A 量纲迁移「前拍」读数，供四纪律对拍与决策
-- 依据卡：TAPD 1133005753001000520（Boss 2026-10-22 拍板方案A · P80）
-- 用法：迁移 000041 执行**前**由 Andy 在目标库整段跑一次，把每条读数贴回卡面评论；
--       迁移执行后跑 000041_t733_unit_mn_leo.postcheck.sql 取「后拍」，两拍同形才算完成。
-- 纪律：本文件不含 UPDATE/INSERT/DELETE/ALTER，任何环境（含 prod）可安全执行。
-- ══════════════════════════════════════════════════════════════════════════

-- ① 阈值现状：这六枚是「压力量纲」配置，000041 会 ×1000 搬到 mN 存储空间。
--    读法：值域若仍是 N（5 / 6 / 1 / 0.05 / 0.3 / 0.05）= 本迁移未跑；若是 5000/6000/… = 已跑。
SELECT config_key, config_value, updated_at
FROM sys_configs
WHERE config_key IN ('threshold_pressure_high', 'threshold_pressure_low', 'heatmap_max_n',
                     'wearing_pressure_threshold', 'threshold_sensor_drift', 'threshold_calibration_offset')
ORDER BY config_key;

-- ①b 非压力量纲的配置键必须**保持原值**（000041 不动它们）；此处只取证，便于后拍比对
SELECT config_key, config_value
FROM sys_configs
WHERE config_key NOT IN ('threshold_pressure_high', 'threshold_pressure_low', 'heatmap_max_n',
                         'wearing_pressure_threshold', 'threshold_sensor_drift', 'threshold_calibration_offset')
ORDER BY config_key;

-- ② 基线腿现状（T733 口径①：offset_values 现在存 N，须 ×1000 成 mN 才能与 mN 帧直接相减）
SELECT count(*)                                   AS baselines_rows,
       min(offset_values[1])                      AS min_o1,
       max(offset_values[1])                      AS max_o1,
       count(*) FILTER (WHERE max_abs_offset < 1) AS looks_like_newton_rows
FROM (
  SELECT baseline_id,
         offset_values,
         (SELECT max(abs(v)) FROM unnest(offset_values) AS v) AS max_abs_offset
  FROM baselines
) t;

-- ③ 帧值分桶（决定 000041 的「污染修正」面到底有多大；物理量程 0–20 N ⇒ 0–20000 mN）
SELECT CASE
         WHEN frame_max >  20000 THEN 'c 大于20000mN（物理不可能＝必然错标）'
         WHEN frame_max >   5000 THEN 'b 5000-20000mN（可能是真实高施力，也可能错标）'
         WHEN frame_max >     50 THEN 'a 50-5000mN（mN 语义正常带）'
         ELSE                         'z 不大于50（N 语义遗留＝T719 之前入口曾 ÷1000）'
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
GROUP BY 1
ORDER BY 1;

-- ④ T719 上线时刻取证（决定桶 z 是不是「入口曾 ÷1000」那一代遗留；000041 不动桶 z，只列清单）
--    T719 合并 main 的提交时刻见 git（本卡附），库里用 ingest_source + ts 分布近似。
SELECT ingest_source,
       min(ts) AS ts_from,
       max(ts) AS ts_to,
       count(*) AS rows_cnt,
       count(*) FILTER (WHERE frame_max <= 50) AS legacy_n_space_rows
FROM (
  SELECT ingest_source, ts,
         greatest(p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
                  p11,p12,p13,p14,p15,p16,p17,p18,p19,p20) AS frame_max
  FROM pressure_records
) t
GROUP BY 1
ORDER BY 1;

-- ⑤ 日聚合行现状（avg/max 与 wearing_threshold_n 列名；000041 改列名为 wearing_threshold_mn）
SELECT count(*) AS stat_rows,
       round(max(max_pressure)::numeric, 3) AS max_pressure_high_water,
       count(*) FILTER (WHERE wearing_threshold_n IS NOT NULL) AS stamped_rows
FROM daily_wear_stats;

-- ⑥ 是否已有 000041 的快照表（有＝本迁移曾跑过/跑了一半，必须先人工核对再决定是否重跑）
SELECT table_name
FROM information_schema.tables
WHERE table_name IN ('sys_configs_t733_bak', 'baselines_t733_bak', 'pressure_records_t733_bak',
                     'daily_wear_stats_t733_bak')
ORDER BY 1;

COMMIT;
