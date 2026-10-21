-- T715 回滚：按快照表逐行还原 contact_area_cm2 的原值，再撤快照表。
-- 与「简单 UPDATE 回 0.64→NULL」相比，这里按 device_id 精确还原，
-- 因此 down 之后被人工配过值的行不会被误清空（只还原 000040 真正动过的那批行）。
-- 注：COMMENT 不回滚——注释是口径文本，不承载行为，留着不影响读写路径。
BEGIN;

UPDATE devices d
SET contact_area_cm2 = b.contact_area_cm2
FROM devices_contact_area_bak_t715 b
WHERE d.device_id = b.device_id;

DROP TABLE IF EXISTS devices_contact_area_bak_t715;

COMMIT;
