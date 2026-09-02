-- T069 回滚
-- ⚠️ 注意：SET NOT NULL 前需保证所有 patients 行 phone_enc / phone_hash 非 NULL
-- （即需先删除或补全 wx-only 新患者的 phone 字段，否则 ALTER 会失败）
DROP INDEX idx_patients_wx_openid;
ALTER TABLE patients DROP COLUMN wx_openid;
ALTER TABLE patients ALTER COLUMN phone_enc SET NOT NULL;
ALTER TABLE patients ALTER COLUMN phone_hash SET NOT NULL;
