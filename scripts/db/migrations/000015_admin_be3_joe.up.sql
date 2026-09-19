-- T256 后端第3批（Joe）：8.2 佩戴感受两档 + 12.5 系统参数三项
-- 对齐：docs/tasks/joe/T256-admin后端第3批-prompt.md
-- owner：feeling_logs / sys_configs 均为 user-service

BEGIN;

-- ── 8.2 佩戴感受两档（设计稿 矫形日志.html:107-110：贴合 / 不适）──────────────
-- PRD §7A.7 / §8.2 原为 1–5 星（可半星）连续值，K6 收口为设计稿两档口径。
ALTER TABLE feeling_logs ADD COLUMN IF NOT EXISTS comfort_level VARCHAR(8)
    CHECK (comfort_level IN ('fitted', 'discomfort'));

COMMENT ON COLUMN feeling_logs.comfort_level IS
    'T256 8.2 佩戴感受两档：fitted=贴合 / discomfort=不适。NULL=未评（与 comfort_score 同为可空）';
COMMENT ON COLUMN feeling_logs.comfort_score IS
    'T256 8.2 起为历史星级口径（PRD §8.2），仅供审计与波3 前端过渡展示；写入口径以 comfort_level 为准';

-- 存量数据口径：以 3.0 星为界（PRD 1–5 星的中位），≥3.0 判「贴合」，<3.0 判「不适」。
-- 阈值口径已登记 docs/tasks/_pm-notes/待Boss裁决.md（改数据语义需 Boss 确认）。
UPDATE feeling_logs
   SET comfort_level = CASE WHEN comfort_score >= 3.0 THEN 'fitted' ELSE 'discomfort' END
 WHERE comfort_level IS NULL
   AND comfort_score IS NOT NULL;

-- ── 12.5 系统参数三项（设计稿 系统配置.html:88-90）────────────────────────
-- 采集间隔单位收口为「秒」（K9）；alert-service / data-service 运行时仍消费
-- collect_interval_minutes ⇒ 新增秒键，由分钟键换算回填，两键由 settings 写入端点保持一致。
INSERT INTO sys_configs (config_key, config_value, description)
SELECT 'collect_interval_seconds',
       (config_value::INT * 60)::TEXT,
       '数据采集间隔（秒，T256 12.5 设计稿口径）；由存量 collect_interval_minutes 换算回填'
  FROM sys_configs
 WHERE config_key = 'collect_interval_minutes'
ON CONFLICT (config_key) DO NOTHING;

-- 两项净新增（PRD §7D.12 未列 ⇒ 需回写；本卡只出字段与读写端点，保留期清理任务与建员上限
-- 属后续卡，本迁移不启用任何行为变更）。
INSERT INTO sys_configs (config_key, config_value, description) VALUES
  ('data_retention_days', '365',   '数据保留天数（T256 12.5，设计稿默认 365）'),
  ('max_patients',        '10000', '最大患者数（T256 12.5，设计稿默认 10000）')
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
