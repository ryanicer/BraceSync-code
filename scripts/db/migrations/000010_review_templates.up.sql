-- BraceSync T135 复查报告模板表（golang-migrate up）
-- 合同：运营后台子系统「复查报告模板管理」（软件购销合同功能清单 L53）
-- PRD §7A.11.y · R1-c 读法 A（系统不据模板自动生成报告，仅承载模板文件管理）
-- DB schema 变更：已按红线④上报告知 PM/Boss，up/down 成对、可回滚。
-- 表 owner：user-service。文件本体复用 file-service review_report 预签名通道，
-- 以 owner_type=ReviewTemplate 区分，【不扩展】files.file_type 枚举。

BEGIN;

-- 复查报告模板（按版本一行：同 name 归同 template_group_id，version 组内递增，
-- 新版本 active、同组其余 retired【非删除】，已填填写报告(review_records)不受影响）
CREATE TABLE review_templates (
    template_id       VARCHAR(32)  PRIMARY KEY,
    template_group_id VARCHAR(32)  NOT NULL,
    name              VARCHAR(128) NOT NULL,
    version           INT          NOT NULL,
    file_id           VARCHAR(64),
    status            VARCHAR(16)  NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active','retired')),
    uploaded_by       VARCHAR(64)  NOT NULL,  -- 登录凭证 X-User-Id（前端不可传，防伪造）
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX idx_review_templates_group ON review_templates(template_group_id, version DESC);
CREATE INDEX idx_review_templates_name ON review_templates(name);

COMMIT;