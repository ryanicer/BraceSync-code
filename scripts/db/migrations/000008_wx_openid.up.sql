-- T069：患者端微信登录支持
-- 1) 允许微信-only 用户无手机号注册（openid 作为唯一登录凭据即可）
ALTER TABLE patients ALTER COLUMN phone_enc DROP NOT NULL;
ALTER TABLE patients ALTER COLUMN phone_hash DROP NOT NULL;

-- 2) 微信 openid 列（患者端小程序用户唯一标识，VARCHAR(64) 覆盖 openid/unionid 上限）
ALTER TABLE patients ADD COLUMN wx_openid VARCHAR(64);

-- 3) 部分唯一索引：只对非 NULL wx_openid 唯一（允许多个 NULL 行，兼容 phone-only 老用户）
CREATE UNIQUE INDEX idx_patients_wx_openid ON patients(wx_openid) WHERE wx_openid IS NOT NULL;
