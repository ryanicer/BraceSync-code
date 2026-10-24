BEGIN;

-- ══════════════════════════════════════════════════════════════════════════
-- T706 厂商凭据体系（设计稿 V1.0 §8.1，Boss 2026-10-23 23:2x 批准）
-- 依据卡：TAPD 1133005753001000492
--
-- 做什么：
--   ① vendors 新表（厂商主体，VND000001 平台发号）
--   ② vendor_api_keys 新表（只存 HMAC 摘要 + 定位前缀/尾 4 位，明文一次性）
--   ③ devices.vendor_id 新列（nullable：运营手工注册的设备为 NULL，零回填）
--   ④ sys_configs 七枚限额键（改数不改码）
--   ⑤ 四纪律快照：devices_t706_bak（加列前的行级取证；本迁移不改任何 devices 数据）
--
-- 幂等：三张 DDL 全部带存在性守卫，重跑安全（000041 同形）。
-- 回滚：down 可回退到加列前（vendor 台账随表删除，故 down 只用于「本迁移刚跑完且
--      尚未产生厂商数据」的窗口；已有厂商数据时回滚=丢台账，需人工导出后再动）。
-- ══════════════════════════════════════════════════════════════════════════

-- ── ⑤ 快照先行（只取加列前与归属相关的行级字段；不带 patient_id 列，避开 T467 删除面门禁）
CREATE TABLE IF NOT EXISTS devices_t706_bak AS
SELECT device_id, model, COALESCE(firmware_version, '') AS firmware_version,
       status, created_at
  FROM devices;

-- ── ① 厂商主体
CREATE TABLE IF NOT EXISTS vendors (
    vendor_id   VARCHAR(32) PRIMARY KEY,              -- VND + 6 位数字（VND000001）
    name        VARCHAR(128) NOT NULL,                -- 合同主体名
    contact_enc BYTEA,                                -- 联系人手机密文（AES-GCM，同 phone_enc 口径）
    contact_hash CHAR(64),                            -- 联系人手机 HMAC 查询摘要
    models      VARCHAR(255) NOT NULL,                -- 授权型号清单，逗号分（PRS-ML05-RC）
    status      VARCHAR(16) NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'suspended', 'revoked')),
    note        TEXT,
    created_by  VARCHAR(32),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 发号序列：VND000001 起，nextval 天然防并发重号（禁厂商自取，禁应用侧 MAX()+1）
CREATE SEQUENCE IF NOT EXISTS vendors_id_seq START WITH 1 INCREMENT BY 1;

-- ── ② API key（明文不落库：key_hash = HMAC-SHA256(pepper, 全串)）
CREATE TABLE IF NOT EXISTS vendor_api_keys (
    key_id      VARCHAR(32) PRIMARY KEY,              -- VK + 8 位 hex
    vendor_id   VARCHAR(32) NOT NULL REFERENCES vendors(vendor_id),
    key_hash    CHAR(64) NOT NULL,                    -- 64 位 hex
    key_prefix  VARCHAR(48) NOT NULL,                 -- bsk_VND000001_（定位用，非秘密）
    key_tail    CHAR(4)  NOT NULL,                    -- 展示尾 4 位
    status      VARCHAR(16) NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'suspended', 'revoked', 'expired')),
    issued_by   VARCHAR(32) NOT NULL,
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    revoke_reason VARCHAR(255),
    last_used_at TIMESTAMPTZ,
    UNIQUE (key_prefix, key_tail)
);

-- 鉴权按前缀取候选行（同厂商 ≤3 把有效 + 历史吊销行），这是每次请求的唯一定位路径
CREATE INDEX IF NOT EXISTS idx_vendor_api_keys_prefix ON vendor_api_keys (key_prefix);
-- 管理页「本厂商 key 列表 / 有效把数」与吊销
CREATE INDEX IF NOT EXISTS idx_vendor_api_keys_vendor ON vendor_api_keys (vendor_id, status);

-- ── ③ devices.vendor_id（nullable，零回填：运营/手工注册设备保持 NULL）
ALTER TABLE devices ADD COLUMN IF NOT EXISTS vendor_id VARCHAR(32) REFERENCES vendors(vendor_id);
CREATE INDEX IF NOT EXISTS idx_devices_vendor_id ON devices (vendor_id) WHERE vendor_id IS NOT NULL;

-- ── ④ 限额与 TTL 键（§5.1 七枚；ON CONFLICT DO NOTHING 与既有覆盖不打架）
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('vendor_key_qps_limit',            '10',    '厂商 API key 每秒请求上限（超过 429 vendor_rate_limited）'),
  ('vendor_key_daily_calls',          '50000', '厂商 API key 每自然日调用上限（超过 429）'),
  ('vendor_batch_max_devices',        '200',   '厂商批量注册单请求设备数上限（超过整请求 400 拒绝）'),
  ('vendor_secret_fetch_daily',       '2000',  '单厂商每日补取 secret 次数上限（超过 429，且落风控审计）'),
  ('vendor_key_ttl_days',             '90',    '厂商 API key 默认有效期（天）'),
  ('vendor_key_rotation_overlap_hours','24',   '轮换重叠期（小时）：新 key 即刻可用，旧 key 重叠期内仍可验'),
  ('vendor_key_max_active',           '3',     '同厂商同时有效的 key 上限（第 4 把签发 409 vendor_key_limit）')
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
