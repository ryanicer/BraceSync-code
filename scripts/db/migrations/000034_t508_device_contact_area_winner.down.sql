-- T508 回滚：删 devices.contact_area_cm2
-- 该列只有展示层换算消费（快照透出），无索引、无外键、无被其它列引用，DROP 即可。
BEGIN;

ALTER TABLE devices DROP COLUMN IF EXISTS contact_area_cm2;

COMMIT;
