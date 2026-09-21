-- T302 修 feeling_logs.comfort_level 列宽：VARCHAR(8) → VARCHAR(16)
-- 现象：接口层 feeling 收 fitted|discomfort 两值（handler.go:1432 校验、pg.go:775 按列比对），
--       但 000015:9 建的列是 VARCHAR(8)，而 'discomfort' 本身 10 字符
--       ⇒ PG 真库写「不适」报 value too long for type character varying(8)，
--         矫形日志页「佩戴感受」列与「不适」筛选对既有数据恒空。
-- 根因：列宽按 'fitted'（6 字符）估的，没按两档里最长的那个估；
--       000015 自带的回填 UPDATE（ELSE 'discomfort'）同样必炸，
--       当时跑在空表上（无 comfort_score<3.0 行）才没暴露。
-- 修法：只加宽、不改列型、不动 CHECK 语义、不删改历史数据。
--       16 = 两档最长值 10 字符留余量；同表 discomfort_areas / notes 等枚举列同量级。
--       CHECK 无需重建（fitted / discomfort 均 ≤16，PG 在 ALTER TYPE 时原地保留约束）。
-- 回填补做：000015 那条 UPDATE 在存量库里要么整事务回滚、要么只写进了 fitted 一档，
--       comfort_score<3.0 的行至今仍是 NULL ⇒ 列宽修好后按同一口径（≥3.0 判 fitted）补跑一次，
--       幂等（WHERE comfort_level IS NULL），且必须能在非空表上跑通（见交付自报的真库原始输出）。
-- owner：feeling_logs（user-service 读写；后台矫形日志页 / 患者端佩戴感受）
-- 不改历史迁移：000015 冻住，列宽收口只由本迁移负责。

BEGIN;

ALTER TABLE feeling_logs ALTER COLUMN comfort_level TYPE VARCHAR(16);

COMMENT ON COLUMN feeling_logs.comfort_level IS
    'T256 8.2 佩戴感受两档：fitted=贴合 / discomfort=不适。NULL=未评（与 comfort_score 同为可空）。'
    'T302 起列宽 16（原 VARCHAR(8) 装不下 10 字符的 discomfort）';

UPDATE feeling_logs
   SET comfort_level = CASE WHEN comfort_score >= 3.0 THEN 'fitted' ELSE 'discomfort' END
 WHERE comfort_level IS NULL
   AND comfort_score IS NOT NULL;

COMMIT;
