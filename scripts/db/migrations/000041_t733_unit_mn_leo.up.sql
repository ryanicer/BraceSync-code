BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T733 方案A：压力量纲改为「原始 mN 入库 + 展示层唯一换算」的存量迁移
-- 依据卡：TAPD 1133005753001000520（Boss 2026-10-22 拍板方案A · P80 · PM 定案四口径）
--
-- 🔴 执行面：**只跑 staging**。prod 是否跑，取决于 prod 是否部署过 T719（口径④）——
--    未部署过 = 零动作，本文件不得进 prod。核实方法与结论落在卡面评论。
-- 🔴 纪律：本文件假定由 Andy 在目标库单发执行（staging 唯一部署人）。执行前必须先整段跑
--    000041_t733_unit_mn_leo.precheck.sql 取「前拍」，执行后跑 .postcheck.sql 取「后拍」。
--
-- 幂等保护：**结构**与**数据**分两段，各自的幂等方式不同。
--   A 段（列改名 + NUMERIC 加宽）每次执行都必须保证到位，用 information_schema 存在性判据
--      自幂等——代码已按 wearing_threshold_mn / mN 量程写库，新库或半成品库都必须补齐结构。
--   B 段（×1000 / ÷1000 换算）只在「threshold_pressure_high 仍是 N 量纲（< 1000）」时执行一次。
--      跑过之后 config 是 5000 ⇒ 再跑整段跳过（不会二次 ×1000 / 二次 ÷1000）。
--      新库/空库走这里跳过是对的：B 段无数据可换，且缺键不代表旧数据未迁移，宁可用值域判据。
--   因此**必须先跑前拍**：前拍若显示已是 mN，说明本迁移已生效，直接停手回报。
--
-- 做什么（对应 PM 定案四条）：
--   ① sys_configs 六枚压力量纲阈值 ×1000（搬到 mN 存储空间）；baselines.offset_values ×1000
--   ②③ 存储语义变了，历史行必须同量纲：pressure_records 污染帧（>5000）÷1000、
--        daily_wear_stats 聚合列 ÷1000 + 印章列改名 wearing_threshold_mn（×1000）、
--        alerts 两类的 threshold_value/actual_value ×1000（读侧出口按 mN 展示）
--   ④ 快照先行：六张 *_t733_bak 表在任何写操作之前建立
--
-- Redis 不在本文件内（SQL 改不了缓存）：迁移后由 Andy 清 rt:frame:* 与 stat:today:*，
--   或等 TTL 自然过期；不清的症状 = 实时页峰值差 1000 倍（缓存里还是旧量纲）。
-- ══════════════════════════════════════════════════════════════════════════

-- ── ④ 快照先行（四纪律：改前备份，随时可回退）──────────────────────────────
-- 🔴 快照表的 patient_id 一律改名 patient_id_snap：user-service 的 T467 门禁
--    （TestITDeletePatient_RefTableListMatchesDatabase_T467）把「库内所有带 patient_id 列的
--    顶层表」钉成删除级联名单，快照表混进去会凭空扩删面（且删患者会连回退证据一起删掉）。
--    改名 = 快照数据一字不动，且不参与患者删除语义；清理由 Andy 在封存后执行。
CREATE TABLE IF NOT EXISTS sys_configs_t733_bak        AS SELECT * FROM sys_configs;
CREATE TABLE IF NOT EXISTS baselines_t733_bak          AS SELECT * FROM baselines;
CREATE TABLE IF NOT EXISTS daily_wear_stats_t733_bak   AS SELECT * FROM daily_wear_stats;
CREATE TABLE IF NOT EXISTS alerts_t733_bak             AS SELECT * FROM alerts;
CREATE TABLE IF NOT EXISTS alert_point_rules_t733_bak  AS SELECT * FROM alert_point_rules;
-- 帧表可能很大：只备要动的那批（>5000 的污染帧）
CREATE TABLE IF NOT EXISTS pressure_records_t733_bak AS
SELECT * FROM pressure_records
WHERE greatest(p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
               p11,p12,p13,p14,p15,p16,p17,p18,p19,p20) > 5000;
-- 改名带存在性守卫：重跑时 CTAS 被 IF NOT EXISTS 跳过、列已是 _snap（无守卫则 42703 整事务回滚）
DO $t733_snap$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY['daily_wear_stats_t733_bak','alerts_t733_bak','pressure_records_t733_bak']
    LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                    WHERE table_schema = 'public' AND table_name = tbl
                      AND column_name = 'patient_id') THEN
            EXECUTE format('ALTER TABLE %I RENAME COLUMN patient_id TO patient_id_snap', tbl);
        END IF;
    END LOOP;
END
$t733_snap$;

-- ── A 段：结构（无条件执行，存在性自幂等）──────────────────────────────────
-- 代码侧已按新列名/新量纲读写，结构必须先于数据就位：
--   · 印章列改名（Go model.DailyWearStats.WearingThresholdMn 直接引用该列名）
--   · alert_point_rules.upper_n / lower_n 加宽：原 NUMERIC(6,2) 上限 9999.99，
--     而 mN 空间的逐点上限是 20000.00（=20 N 满量程）⇒ 不加宽则 B 段 ×1000 与新写入双双
--     撞 numeric field overflow（22003）。精度不变，只扩整数位。
DO $t733_schema$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'daily_wear_stats' AND column_name = 'wearing_threshold_n')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'daily_wear_stats' AND column_name = 'wearing_threshold_mn') THEN
        ALTER TABLE daily_wear_stats RENAME COLUMN wearing_threshold_n TO wearing_threshold_mn;
        RAISE NOTICE 'T733 A 段：daily_wear_stats.wearing_threshold_n 已改名 wearing_threshold_mn';
    ELSE
        RAISE NOTICE 'T733 A 段：印章列名已就位（或旧列不存在），跳过改名';
    END IF;

    ALTER TABLE alert_point_rules ALTER COLUMN upper_n TYPE NUMERIC(9,2);
    ALTER TABLE alert_point_rules ALTER COLUMN lower_n TYPE NUMERIC(9,2);

    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_name = 'daily_wear_stats' AND column_name = 'wearing_threshold_mn') THEN
        COMMENT ON COLUMN daily_wear_stats.wearing_threshold_mn IS
          '本次聚合实际生效的佩戴帧判定阈值（单位 mN，T733 方案A 存储空间）；对外 DTO 字段仍为 wearingThresholdN（N）';
    END IF;
END
$t733_schema$;

-- ── B 段：数据换算（一次性，量纲判据幂等）────────────────────────────────
DO $t733$
DECLARE
    still_n BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM sys_configs
        WHERE config_key = 'threshold_pressure_high'
          AND config_value ~ '^[0-9.]+$'
          AND config_value::numeric < 1000          -- N 量纲 ⇒ 本迁移未跑
    ) INTO still_n;

    IF NOT still_n THEN
        RAISE NOTICE 'T733 B 段：threshold_pressure_high 已是 mN 量纲（或键缺失），整段跳过——不重复换算';
        RETURN;
    END IF;

    -- ① 阈值配置 ×1000（六枚压力量纲键；其余键一律不动）
    --    ::text 必带：config_value 是 VARCHAR，numeric 直接赋值无自动转换
    UPDATE sys_configs
       SET config_value = trim_scale(config_value::numeric * 1000)::text,
           updated_at   = NOW()
     WHERE config_key IN ('threshold_pressure_high', 'threshold_pressure_low',
                          'heatmap_max_n', 'wearing_pressure_threshold',
                          'threshold_sensor_drift', 'threshold_calibration_offset')
       AND config_value ~ '^[0-9.]+$';

    -- ① 基线偏移 ×1000（T733 口径①：与 mN 帧直接相减必须同量纲）
    --     baselines 无 updated_at 列（000001:110-118 只有 created_at），本句不写时间戳
    UPDATE baselines
       SET offset_values = (
             SELECT array_agg(v * 1000 ORDER BY ord)
               FROM unnest(offset_values) WITH ORDINALITY AS t(v, ord)
           );

    -- ②③ 污染帧 ÷1000（PM 定案面：staging 里 >5000 的帧按错标处理）
    --     注意：物理量程上限 20000 mN（=20 N），5000–20000 这一段本身可能是真高施力，
    --     前拍 ③ 的桶 b 行数即本句影响面；跑前把该数贴回卡面，异常放大就停手报 PM。
    UPDATE pressure_records
       SET p01 = p01 / 1000, p02 = p02 / 1000, p03 = p03 / 1000, p04 = p04 / 1000,
           p05 = p05 / 1000, p06 = p06 / 1000, p07 = p07 / 1000, p08 = p08 / 1000,
           p09 = p09 / 1000, p10 = p10 / 1000, p11 = p11 / 1000, p12 = p12 / 1000,
           p13 = p13 / 1000, p14 = p14 / 1000, p15 = p15 / 1000, p16 = p16 / 1000,
           p17 = p17 / 1000, p18 = p18 / 1000, p19 = p19 / 1000, p20 = p20 / 1000
     WHERE greatest(p01,p02,p03,p04,p05,p06,p07,p08,p09,p10,
                    p11,p12,p13,p14,p15,p16,p17,p18,p19,p20) > 5000;

    -- ②③ 逐采集点阈值表 upper_n / lower_n ×1000（键名沿用历史，语义随存储层改 mN）：
    --     读端是 alert-service repo/config.go（与 mN 帧直接比较）、写端是 user-service UI 边界，
    --     这一列不搬，逐点判档会低 1000 倍 ⇒ 每条帧都超阈（等于把热力图满屏 critical 搬进告警表）。
    UPDATE alert_point_rules
       SET upper_n = upper_n * 1000,
           lower_n = lower_n * 1000
     WHERE upper_n IS NOT NULL OR lower_n IS NOT NULL;

    -- ②③ 告警行的两枚压力量纲值 ×1000：读侧出口 handler.displayValueN 按 mN 存储 ÷1000 展示，
    --     不搬这两列，历史告警会显示成 0.005 N（旧行是 N，新行是 mN，同列两种量纲）。
    UPDATE alerts
       SET threshold_value = threshold_value * 1000,
           actual_value    = actual_value * 1000
     WHERE type IN ('pressure_high', 'pressure_low', 'sensor_drift')
       AND threshold_value IS NOT NULL;

    -- ②③ 日聚合：印章列 ×1000（改名已由 A 段完成，本句只搬值）
    UPDATE daily_wear_stats
       SET wearing_threshold_mn = wearing_threshold_mn * 1000
     WHERE wearing_threshold_mn IS NOT NULL;

    -- ②③ 日聚合的峰值/均值列与帧表同批处理（>5000 判污染，÷1000）
    --     这些列由 RollupService 每日重算，本句只是让历史行与新帧量纲自洽；
    --     迁移后建议顺手复算最近 7 天（卡面判据 5 要求改后回读）。
    --     放在 B 段内而非块外：mN 空间里 5000–20000 是合法真高施力，块外重跑会二次 ÷1000。
    UPDATE daily_wear_stats
       SET max_pressure = max_pressure / 1000,
           avg_pressure = avg_pressure / 1000
     WHERE max_pressure > 5000;

    RAISE NOTICE 'T733 方案A 量纲迁移完成：阈值/基线 ×1000，污染帧 ÷1000，告警压力量纲 ×1000，印章列改名+×1000';
END
$t733$;

COMMIT;
