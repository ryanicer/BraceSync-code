-- BraceSync T226 患者自助资料写接口（golang-migrate down）
BEGIN;

ALTER TABLE patients
  DROP COLUMN IF EXISTS height_cm,
  DROP COLUMN IF EXISTS weight_kg,
  DROP COLUMN IF EXISTS emergency_contact_name,
  DROP COLUMN IF EXISTS emergency_contact_phone,
  DROP COLUMN IF EXISTS emergency_contact_relation;

COMMIT;
