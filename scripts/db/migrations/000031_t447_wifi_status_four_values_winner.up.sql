-- T447：install_records.wifi_status 枚举 2 值扩到 4 值（Boss 2026-09-28 11:0x 裁定②甲）
-- 现象：000001:129-130 建列时 CHECK 只放行 connected / unconfigured，
--       而实际语义有四档 —— 配网成功 / 配网失败（云端不可达）/ 从未配网 / 技师主动跳过。
--       「连接失败」这一档在库里无值可落，所以稿面 records.html 状态列里那一档永远显示不出来（T446 §四 C 套）。
--       该稿面行号不写：Peter 的 T449（docs PR 667）正在删 tech/*.html 的数据源 chip，行号会漂移。
-- 根因：写侧只有一条通路会把该列写成非默认值 —— repo.go SetWifiSSID（BLE 配网成功回写 'connected'）。
--       前端 PUT /install-records/:id 的请求体里虽然一直带 wifiStatus 键
--       （install/index.vue:430），但 installMetaRequest 从未声明该字段，ShouldBindJSON 默认忽略未知字段
--       ⇒ 该键静默丢弃，跳过 / 失败两态从未进过 SQL（T446 §一 C-3b、§四 关键量）。
-- 存量映射：本迁移不回填任何行。库里现有可能取值就是 CHECK 允许的那两值，
--       而新出现的两值（failed / skipped）在旧约束下不可能存在于任何历史行，
--       且上面那条「前端发而被丢弃」的事实说明跳过配网的历史记录也没有被误写成 connected 的脏行
--       —— TC-2 那个缺陷只污染了内存 store（stores/install.ts 的 wifiStatus），从未落库。
--       故「映射」是空集，不需要 UPDATE；staging seed 三行安装记录取值为 connected（seed.sql:204,221,236），
--       其余走列默认 unconfigured，均落在新集合内。
-- 列宽：VARCHAR(16) 不动 —— 四值最长 'unconfigured' 12 字符，容得下。
-- 应用层同步收口（model.ValidWifiStatus + service 写侧校验 + shared-types 联合类型）在同一提交里做，
--       目的是让未知取值回 400，而不是穿过校验后由本约束在 UPDATE 时报 23514、被 handler 兜成 500。
--       跨层一致性由 services/device-service/internal/model/wifi_status_t447_test.go 比对字面值钉住（改一侧不改另一侧 = 用例判红）。
-- 不改的两侧：列表读侧零改动（repo/query.go:152 已投影该列，四值天然回显）；
--       前端文案常量 apps/tech-miniapp/src/utils/installStatus.ts 的 WIFI_STATUS_LABEL 仍两档，
--       按 PM seq356 口径由 Iris 在 T443 拿本卡词形表同步，不归本卡。

BEGIN;

-- 不带 IF EXISTS：若约束名漂移（例如历史库手工建过别名），这里要显式失败而不是
-- 静默跳过 DROP、再 ADD 出一条与之相交的第二条 CHECK —— 两约束同时生效会把值域收回 2 值却报告迁移成功。
ALTER TABLE install_records DROP CONSTRAINT install_records_wifi_status_check;

ALTER TABLE install_records ADD CONSTRAINT install_records_wifi_status_check CHECK (
    wifi_status IN (
        'connected',    -- 配网成功。唯一历史写侧：repo.go SetWifiSSID（POST /devices/:id/wifi）
        'unconfigured', -- 从未配网。列默认值（000001:129）
        'failed',       -- 已配上 WiFi 但云端不可达（BLE B512 错误码 -1..-4，PRD §7C.6 错误态表）。T447 新增
        'skipped'       -- 技师主动跳过配网（PRD §7C.4 跳过配网按钮）。T447 新增
    )
);

COMMENT ON CONSTRAINT install_records_wifi_status_check ON install_records IS
    'T447：wifi_status 枚举 2 值扩 4 值（Boss 裁定②甲）。值集与 model.ValidWifiStatus、'
    'packages/shared-types 的 wifiStatus 联合类型同一集合，由 model/wifi_status_t447_test.go 钉住。'
    '本约束不回填存量：新两值在旧 CHECK 下不可能已存在于库里。';

COMMIT;
