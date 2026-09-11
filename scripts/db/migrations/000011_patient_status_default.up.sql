-- BraceSync T151(2b) 患者 status 默认值收口（golang-migrate up）
-- 现象：patients.status DEFAULT 'pending' 而 pending 非业务态（登录三入口硬校验 active）。
-- 裁定(PM 2026-09-11 18:05)：仅改默认值为 'active'，不动存量，保留 CHECK('active','pending')。
-- DDL 元数据级、秒级、不重写表；存量行无任何变更。
BEGIN;

ALTER TABLE patients ALTER COLUMN status SET DEFAULT 'active';

COMMENT ON COLUMN patients.status IS 'pending 非业务态(2b 从生产路径排除)；仅 active 可登录；CHECK(''active'',''pending'') 保留以兼容历史值';
COMMENT ON COLUMN patients.device_id IS '[DEPRECATED] 权威性已废弃(T151 方案1)：当前绑定以 devices.patient_id 为准，历史见 device_bindings；本列不再被读取';

COMMIT;