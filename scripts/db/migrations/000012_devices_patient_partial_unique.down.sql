-- BraceSync T151(方案C) devices.patient_id 部分唯一索引回滚（golang-migrate down）
BEGIN;

DROP INDEX IF EXISTS uk_devices_active_patient;

COMMIT;