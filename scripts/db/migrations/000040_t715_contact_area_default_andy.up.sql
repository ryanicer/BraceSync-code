-- T715 注册即非空：devices.contact_area_cm2 存量 NULL 一次性补 Boss 标称 0.64 cm²
-- 依据：TAPD 1133005753001000501（Boss 令）。2026-10-17 真机数据链实证——该列为 NULL 时患者端
--   整片提示「未配置面积，暂无法换算」（kPa 换算分母缺失），当期靠人工 PUT 单台救火
--   （PRS-ML05-RC-20260701001 手工置 0.64 后恢复），但注册流程无默认 ⇒ 每台新设备都复发。
--   注册面默认落在代码（model.DefaultContactArea + RegisterDevice 的 INSERT 列清单），
--   本迁移负责存量那一半。
--
-- 为什么选「一次性 UPDATE」，不选 DB DEFAULT / 读侧惰性补位：
--   ① 列带 DEFAULT 0.64 会把「没人配过」这个可辨态从库里抹掉，且默认值同时存在于 DB 与代码两处，
--      必然漂移；
--   ② 读侧用常量补位被 PRD §7A.2.1 五.3 明令禁止，也会让 fail-closed 那一档永远无法被验证；
--   ③ 本期单型号 PRS-ML05-RC（单点 0.8cm×0.8cm），「同型号各设备同值」是 PRD §7D.5 约束 2
--      （Boss 2026-09-30 08:58 裁定 d），补 0.64 与人工配值同果；配错仍可经
--      PUT /api/v1/devices/:deviceId/contact-area 改，该路由是整值覆盖、无回退 NULL 通路。
--
-- 不加 NOT NULL：夹具直建/灾备重建仍可能出现 NULL，判据「读侧不得用常量补位」需要库里保留该形态。
-- 四纪律·快照：补值前把这批行的原值整体落 devices_contact_area_bak_t715，down 按行精确还原。
BEGIN;

CREATE TABLE IF NOT EXISTS devices_contact_area_bak_t715 (
    device_id        VARCHAR(64) PRIMARY KEY,
    contact_area_cm2 DOUBLE PRECISION,
    backed_up_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 只快照本次要动的行；ON CONFLICT DO NOTHING 让重跑仍保留最早那份原值
INSERT INTO devices_contact_area_bak_t715 (device_id, contact_area_cm2)
SELECT device_id, contact_area_cm2
FROM devices
WHERE contact_area_cm2 IS NULL
ON CONFLICT (device_id) DO NOTHING;

UPDATE devices
SET contact_area_cm2 = 0.64
WHERE contact_area_cm2 IS NULL;

COMMENT ON COLUMN devices.contact_area_cm2 IS
  '压力点有效受压面积（cm2，T508 建列 / T715 注册即带默认 / PRD §7A.2.1）。'
  'T715 起注册通路写入 Boss 标称 0.64（0.8cm×0.8cm 单点），存量 NULL 由迁移 000040 补齐；'
  '库里仍为 NULL = 该行走过非注册通路或被绕开建数据，kPa 档据此 fail-closed 显示 --，读侧不得用常量补位。'
  '写侧唯一人工通路 = PUT /api/v1/devices/:deviceId/contact-area（整值覆盖，无回退 NULL）。'
  '只用于展示层换算（kPa = N / area × 10），不参与落库判定、告警阈值与聚合（判档恒为 N）。';

COMMIT;
