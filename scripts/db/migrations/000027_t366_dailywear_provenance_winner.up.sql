-- T366 日聚合可解释性（Winner）：给 daily_wear_stats 补「聚合印章」两列
-- 现象：同一张表里既放着聚合任务每日写的行，也放着 seed.sql:144-154 手工造的示例行，
--       读接口 GET /api/v1/patients/:patientId/daily-wear 返回的行**形状完全一致**
--       （model.go DailyWearDayDTO 只有统计值，没有任何来源信息）⇒ 验收侧无法判断
--       手上这一行是「算出来的」还是「设计出来的」，也无法独立复算。
-- 根因：写侧从来没有把「这行是谁写的、按什么阈值算的」落成数据。
--       旧 rollup 口径（帧数 × 配置采集间隔）甚至能从 frame_count 反推出 wear_minutes，
--       T352 改成时间跨度口径后，这一条隐式指纹也消失了 ⇒ 只能显式盖章。
-- 修法：两列可空「印章」，只由 data-service 聚合任务（RunDailyRollup / 补传重算）的 UPSERT 写：
--         aggregated_at        —— 本次聚合发生的时刻（UTC）
--         wearing_threshold_n  —— 本次聚合实际生效的佩戴帧判定阈值（单位 N，来自
--                                 sys_configs.wearing_pressure_threshold；读不到时回退 model 默认，
--                                 两者都记真实生效值，供复算用同一阈值而不是猜）
-- 🔴 零数据回填：本迁移**不** UPDATE 任何存量行。理由——
--       ① 存量行是谁写的无法事后证明（能反推旧口径的只有聚合任务写的行，且随 T352 失效）；
--         给它们补盖章 = 造出「看起来可信」的假证据，与本卡目的相反。
--       ② staging seed 数据只读（共享环境红线），seed 行必须保持原样、由读侧判为未佐证。
--       可空即语义：NULL = 无印章 = 该行不是本服务聚合任务写的（示例行 / 迁移前的历史行）。
--       读侧据此派生三值 provenance（rollup / corroborated / unsupported），
--       判据与测试见 services/data-service/internal/service/record.go。
-- owner：daily_wear_stats（架构 §4.2 写归 data-service；msg-service / alert-service 只读）
-- 既有读方全部写显式列清单（repo/dashboard_repo.go、msg-service repo.go:597、
-- alert-service wear.go:31），加列不影响它们；无 CHECK / 无索引变更。

BEGIN;

ALTER TABLE daily_wear_stats ADD COLUMN IF NOT EXISTS aggregated_at TIMESTAMPTZ;
ALTER TABLE daily_wear_stats ADD COLUMN IF NOT EXISTS wearing_threshold_n REAL;

COMMENT ON COLUMN daily_wear_stats.aggregated_at IS
    'T366 聚合印章：本行由 data-service 聚合任务写入的时刻（UTC）。'
    'NULL = 无印章 ⇒ 非聚合任务所写（seed 示例行 / 印章上线前的历史行），读侧不得当作聚合口径证据';
COMMENT ON COLUMN daily_wear_stats.wearing_threshold_n IS
    'T366 聚合印章：本次聚合实际生效的佩戴帧判定阈值（N，sys_configs.wearing_pressure_threshold 或代码兜底）。'
    '与 aggregated_at 同生同灭；NULL = 无印章。日均压力/佩戴分钟都只对这一阈值成立，复算必须用它';

COMMIT;
