-- T302 down：撤销 000023 播的三行
-- ⚠ threshold_calibration_offset 这一行在「migrations + seed」两条链里都播（seed.sql:354），
--    本 down 删除后 seed 库也不再持有它 ⇒ 回到 000023 之前的状态（幂等，不比现状更差）；
--    读侧不受影响：user-service getSettings 缺行按 defaultCalibrationOffsetN=0.05 兜底。
-- ⚠ 两个模板 ID 若已被运营配过值，回滚会连值一起删（配置项本身被撤销，非改数据语义）。
--    如只想回滚代码不想丢配置：先备份三行再跑 down。

BEGIN;

DELETE FROM sys_configs
 WHERE config_key IN ('threshold_calibration_offset', 'notify_wechat_template_id', 'notify_sms_template_id');

COMMIT;
