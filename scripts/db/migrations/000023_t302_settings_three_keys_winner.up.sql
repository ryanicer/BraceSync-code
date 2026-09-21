-- T302 F1 后端半边：把 PRD §7D.12 的三项配置补成「有键、有读写口、有校验」
-- 现象：三项在 PRD 里有语义，但 GET/PUT /api/v1/admin/settings 都不接管 ⇒ 运营无从配置：
--   1) 空载校准偏差上限 threshold_calibration_offset —— seed.sql:354 已有行（000019 ÷10 到 0.05），
--      但**没有任何迁移播它**：只跑 migrations 不跑 seed 的库（CI 集成库）里该行不存在；
--      代码侧只有 packages/constants 的硬编码 0.05 被技师端安装页读，配置值与展示值两处不同源。
--   2) 微信模板消息 ID notify_wechat_template_id —— 全仓无键（msg-service 两个 sender 是
--      无模板配置时的占位实现），属净新增。
--   3) 短信模板 ID notify_sms_template_id —— 同上。
-- 修法：本迁移只建行 + 给默认值；读写与校验在 user-service handler 侧（同卡交付）。
--       两个模板 ID 播**空串**= 未配置（保持现行为：sender 不带模板直发），不猜厂商模板号。
-- 与 seed.sql 同一份值：CI 集成测只跑 migrations 不跑 seed，纯 seed 建库也要有这三行（T281/T257 同法）。
-- owner：sys_configs（user-service 读写）

BEGIN;

INSERT INTO sys_configs (config_key, config_value, description) VALUES
  -- 描述与 seed.sql:354 逐字一致：两条链都是 ON CONFLICT DO NOTHING，谁先跑谁定 description，
  -- 文案分叉会让运维在两类库里看到两套说法。
  ('threshold_calibration_offset', '0.05', '空载校准偏差上限（N，T203 ÷10）'),
  ('notify_wechat_template_id',    '',     '告警微信模板消息 ID（PRD §7D.12 通知模板配置；空=未配置，直发不带模板）'),
  ('notify_sms_template_id',       '',     '告警短信模板 ID（PRD §7D.12 通知模板配置；空=未配置）')
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
