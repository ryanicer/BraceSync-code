-- BraceSync T130 复查记录表 + files 表 file_type 枚举扩展（golang-migrate up）
-- 对齐：合同患者端「复查管理」· PRD §8（字段待 Boss 确认，见自报）
-- 表 owner：user-service；files 表 owner：file-service（仅扩展 CHECK 约束）

BEGIN;

-- 复查记录表：医生在运营后台创建，患者端查询与下载报告
CREATE TABLE review_records (
    review_id         VARCHAR(32)  PRIMARY KEY,
    patient_id        VARCHAR(32)  NOT NULL REFERENCES patients(patient_id),
    review_date       DATE         NOT NULL,
    review_type       VARCHAR(16)  CHECK (review_type IN ('initial','follow-up')),
    findings          TEXT,
    next_review_date  DATE,
    doctor_id         VARCHAR(32)  REFERENCES doctors(doctor_id),
    report_file_id    VARCHAR(64),
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_review_records_patient ON review_records(patient_id, review_date DESC);

-- files 表 file_type 枚举扩展：新增 review_report（复查报告，T130）
ALTER TABLE files DROP CONSTRAINT files_file_type_check;
ALTER TABLE files ADD CONSTRAINT files_file_type_check
    CHECK (file_type IN ('signature','install_photo','comm_photo','log_photo','review_report'));

COMMIT;
