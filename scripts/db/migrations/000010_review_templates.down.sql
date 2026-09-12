-- BraceSync T135 复查报告模板表回滚（golang-migrate down）
BEGIN;

DROP TABLE IF EXISTS review_templates;

COMMIT;