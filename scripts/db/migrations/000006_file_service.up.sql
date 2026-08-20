-- BraceSync file-service 文件元数据表（T022，golang-migrate up）
-- 对齐：架构 §3.3/§8 ADR-11（COS 预签名直传，仅记元数据）· 任务文档 T022 需求 2
-- 表 owner：file-service；枚举沿用仓库惯例 VARCHAR + CHECK，时间 timestamptz(UTC)

BEGIN;

CREATE TABLE files (
    file_id           VARCHAR(64)  PRIMARY KEY,                -- UUID format
    bucket            VARCHAR(128) NOT NULL,                   -- COS bucket name
    object_key        VARCHAR(512) NOT NULL,                   -- COS object path
    url               VARCHAR(1024) NOT NULL,                  -- Public/CDN URL after upload
    file_type         VARCHAR(32)  NOT NULL CHECK (file_type IN ('signature', 'install_photo', 'comm_photo', 'log_photo')),
    owner_type        VARCHAR(64)  NOT NULL,                   -- Entity type: InstallRecord, Patient, etc.
    owner_id          VARCHAR(64)  NOT NULL,                   -- Parent entity ID
    size              BIGINT       NOT NULL DEFAULT 0,         -- File size in bytes
    content_type      VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
    status            VARCHAR(16)  NOT NULL DEFAULT 'pending'  CHECK (status IN ('pending', 'uploaded', 'failed')),
    uploaded_at       TIMESTAMPTZ,                             -- Nullable, set when upload completes
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 幂等保障：file_id 主键天然防重复登记；同 owner 同 object_key 唯一（防同对象重复行）
ALTER TABLE files ADD CONSTRAINT uk_owner UNIQUE (owner_type, owner_id, object_key);

-- Indexes for common queries
CREATE INDEX idx_files_owner ON files(owner_type, owner_id);
CREATE INDEX idx_files_type ON files(file_type);
CREATE INDEX idx_files_status ON files(status) WHERE status = 'pending';

COMMIT;
