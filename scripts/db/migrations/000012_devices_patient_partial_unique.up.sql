-- BraceSync T151(方案C) devices.patient_id 部分唯一索引（golang-migrate up）
-- 目的：保证「一个患者至多一台当前设备」，user-service 的 patientSelect LEFT JOIN devices
-- 依赖此约束不扇出（否则列表会出现重复行）。
--
-- ⚠️ 应用本迁移前必须先执行冲突预检（PM 已实测当前 staging 0 冲突）：
--   SELECT patient_id FROM devices WHERE patient_id IS NOT NULL
--     GROUP BY patient_id HAVING COUNT(*) > 1;
--   → 若返回 >0 行：停手，出数据核对报告，禁止强建（绝不静默改用查询侧消歧）。
--
-- 跨服务只读注意：devices owner = device-service，user-service 仅只读关联，不写。
BEGIN;

CREATE UNIQUE INDEX uk_devices_active_patient ON devices(patient_id) WHERE patient_id IS NOT NULL;

COMMIT;