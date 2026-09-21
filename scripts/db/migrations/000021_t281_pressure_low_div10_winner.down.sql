-- T281 回滚：threshold_pressure_low 恢复到 000016 播的旧量纲值与原描述
-- ⚠ 回滚后 high(5) < low(10) 的倒挂会重现 ⇒ PUT /admin/settings 重新恒 400，仅用于核对 up/down 对称。
BEGIN;

UPDATE sys_configs
   SET config_value = '10',
       description  = '统一压力下限(N)（T252 2.2 告警页 Tab2，设计稿示意值 10）'
 WHERE config_key = 'threshold_pressure_low';

COMMIT;
