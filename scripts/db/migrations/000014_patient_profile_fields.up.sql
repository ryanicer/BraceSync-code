-- BraceSync T226 患者自助资料写接口（golang-migrate up）
-- patients 补设计稿（docs/design/patient/profile.html 编辑表单）字段：身高/体重/紧急联系人×3，均 nullable。
-- DDL 元数据级、秒级、不重写表；存量行无任何变更。
-- 🔴 手机号不在此列：由微信登录授权写入，患者不可自助改、无任何填号入口（PM 2026-09-16 裁定）；
--    PATCH /api/v1/patients/:patientId 白名单不含 phone，请求携带即 400。
BEGIN;

ALTER TABLE patients
  ADD COLUMN IF NOT EXISTS height_cm                   NUMERIC(5,1),
  ADD COLUMN IF NOT EXISTS weight_kg                   NUMERIC(5,1),
  ADD COLUMN IF NOT EXISTS emergency_contact_name      VARCHAR(64),
  ADD COLUMN IF NOT EXISTS emergency_contact_phone     VARCHAR(32),
  ADD COLUMN IF NOT EXISTS emergency_contact_relation  VARCHAR(32);

COMMENT ON COLUMN patients.height_cm                  IS 'T226 身高(cm)，患者自助维护（应用校验 30–250，设计稿口径）';
COMMENT ON COLUMN patients.weight_kg                  IS 'T226 体重(kg)，患者自助维护（应用校验 2–300，设计稿口径）';
COMMENT ON COLUMN patients.emergency_contact_name     IS 'T226 紧急联系人姓名，患者自助维护';
COMMENT ON COLUMN patients.emergency_contact_phone    IS 'T226 紧急联系人电话，患者自助维护；非登录手机号，不参与鉴权';
COMMENT ON COLUMN patients.emergency_contact_relation IS 'T226 紧急联系人与本人关系，患者自助维护';

COMMIT;
