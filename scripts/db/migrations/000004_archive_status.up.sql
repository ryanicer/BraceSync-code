-- T021: archive_status 冷归档状态追踪表
-- 对齐：docs/ §4.5 冷热分层三步走
-- 用途：记录 pressure_records 分区归档进度（导出→校验→清理），确保幂等续跑

BEGIN;

CREATE TABLE archive_status (
  partition_name  VARCHAR(32) PRIMARY KEY,   -- pressure_records_YYYYMM
  period_year     INT         NOT NULL,
  period_month    INT         NOT NULL,
  status          VARCHAR(16) NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','exported','verified','cleaned','failed')),
  row_count       BIGINT,                    -- 导出行数（exported 阶段写入）
  checksum        VARCHAR(64),               -- SHA-256 校验和（exported 阶段写入）
  export_path     VARCHAR(512),              -- 导出文件路径
  error_message   TEXT,                      -- 失败原因（failed 阶段写入）
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE archive_status IS '冷归档三步走状态追踪（T021）：pending→exported→verified→cleaned，failed 可幂等续跑';

COMMIT;
