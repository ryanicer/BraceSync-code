-- T498 告警数据源受控开关（Winner）：给「帧」和「由这条帧触发的告警」盖上来源印章
--
-- 卡面背景：无硬件期 Boss 验收要求触发数据用 mock，但必须做好开关随时切真实数据。
--           现状核查（本卡开工实测）：全仓零受控开关（git grep ALLOW_MOCK / mock_inject / APP_ENV
--           三个键在本分支基线 origin/main 5685f3e 命中 0 行），mock 只能直接打真实上报端点
--           POST /api/v1/device/records 灌假帧 ⇒ 假数据与真数据在库里长得一模一样，分不开。
-- 根因：写侧没把「这一行是谁写进来的」落成数据。与 T366 是同一个病根的另一处表现
--       （T366 治的是 daily_wear_stats 的「算出来的 vs 设计出来的」，本迁移治 pressure_records
--        与 alerts 的「设备真帧 vs 受控注入」）。
-- 修法：两列同名 ingest_source，可空，值域 real / mock：
--         real —— pressure_records：由本服务真实上报链路（POST /api/v1/device/records 与其批量补传）写入；
--                  alerts：由一条真实上报帧触发的告警（内联评估与 alert:pending 补偿两路都带来源）；
--         mock —— 由 T498 新增的受控注入端点 POST /internal/mock-frame 写入 / 触发；
--         NULL —— 本迁移上线前的存量行（含 seed 手工示例行），
--                 以及 alerts 里扫描器（佩戴中断 / 佩戴时长不足）派生的告警 —— 这两类触发条件读的是
--                 Redis dev:lastseen 与 daily_wear_stats 聚合行，没有逐帧来源可证，故刻意不盖章。
--                 🔴 读侧不得把 NULL 读成「真实数据」（本卡交件单里把这一格列为待裁）。
-- 🔴 零数据回填、零列默认值：刻意不写 DEFAULT 'real'。
--       ① 存量行里混着 scripts/db/seed.sql 造的手工示例帧，给它们默认成 real 等于把假证据洗白成
--         「设备真帧」，与本卡目的相反（同 T366 交件里那条「给存量补盖章 = 造出看起来可信的假证据」）；
--       ② 无默认值后，新写入方必须显式表态，漏写会因 CHECK 允许 NULL 而静默成「未知来源」，
--         可被用例与集成测检出（见 services/data-service/internal/repo/mock_t498_integration_test.go
--         与 alert-service 侧的来源断言），而不是被一个错误的默认值掩盖。
--       写侧三处显式表态点：data-service repo/records.go 的单帧与批量 INSERT、
--       data-service repo/mock_t498.go 的注入事务、alert-service repo/repo.go 的 CreateAlert。
-- CHECK 只约束值域、不放默认值，且允许 NULL；pressure_records 是分区表，
--       在父表上加列与 CHECK 由 PG11+ 传播到全部现存与后续预建分区（分区由
--       service/partition.go 以 PARTITION OF 父表方式预建，自动继承本列，无需改模板）。
-- owner：pressure_records 归 data-service（架构 §4.2）、alerts 归 alert-service；
--       audit_logs 归 user-service（本卡不动它的词表，理由见交件单：沿用 T448「不加新动作词」先例）。
-- 既有读方全部走显式列清单（records.go 查询、repo/dashboard_repo.go、msg-service、
-- alert-service wear.go、admin-web 经接口取字段），加列不影响它们；无索引变更。

BEGIN;

ALTER TABLE pressure_records ADD COLUMN IF NOT EXISTS ingest_source VARCHAR(8);
ALTER TABLE pressure_records ADD CONSTRAINT pressure_records_ingest_source_check
    CHECK (ingest_source IS NULL OR ingest_source IN ('real', 'mock'));

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ingest_source VARCHAR(8);
ALTER TABLE alerts ADD CONSTRAINT alerts_ingest_source_check
    CHECK (ingest_source IS NULL OR ingest_source IN ('real', 'mock'));

COMMENT ON COLUMN pressure_records.ingest_source IS
    'T498 来源印章：real = 真实上报链路写入；mock = 受控注入端点 POST /internal/mock-frame 写入；'
    'NULL = 本迁移上线前的存量行（含 seed 手工示例帧），来源未逐行取证，不得当作设备真帧证据';
COMMENT ON COLUMN alerts.ingest_source IS
    'T498 来源印章：由哪一类帧触发的告警。real = 真实上报帧触发（内联评估与补偿队列两路都带来源）；'
    'mock = 受控注入帧触发；NULL = 存量告警与扫描器派生告警（佩戴中断/时长不足读聚合结果与设备状态，'
    '无逐帧来源可证）⇒ NULL 不等于真实数据';

COMMIT;
