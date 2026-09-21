-- T281 threshold_pressure_low 量纲倒挂修复（= T203 ÷10 的漏改键）
-- 现象（PM 2026-09-21 12:55 staging 直查）：high=5 / low=10 ⇒ 两键倒挂。
-- 根因：000016:31 播的 '10' 是 ÷10 之前的旧量纲（其 description 自述「设计稿示意值 10」），
--       而 000019 的 ÷10 只列了 5 个键、没带 low ⇒ 只有 high 被改到 5。
--       user-service validateSettings 要求 pressureHigh > 现库 low（handler.go:1661）
--       ⇒ PUT /api/v1/admin/settings 恒 400，系统配置页保存不了。新库同样会踩（seed 里根本没这个键）。
-- 修法：按 T203 同口径 ÷10，10 → 1；1 < 5 恢复不变量。
--       000016 每个库都会真实执行（deploy-staging 的旧库探测只覆盖 000001–000005），行必在 ⇒ 用 UPDATE。
-- 不改历史迁移：000016 / 000019 冻住，值收口只由本迁移负责；seed.sql 同步补该键，口径与本迁移一致。
-- owner：sys_configs（user-service 读写；系统配置页与告警页 Tab2 共用此键）

BEGIN;

UPDATE sys_configs
   SET config_value = '1',
       description  = '统一压力下限(N，T203 ÷10 口径；原 000016 播的是 ÷10 前示意值 10，T281 补齐漏改)'
 WHERE config_key = 'threshold_pressure_low';

COMMIT;
