-- T508 热力图双单位（N / kPa）后端数据：devices 增「压力点有效受压面积」列
--
-- 口径依据（PRD §7A.2.1 已裁约束 1/5 与 §7D.5）：
--   面积在同一设备上所有采集点相同，配置入口在运营后台「设备管理」⇒ 列挂在 devices，不是全局键；
--   默认值 0.64 cm² 属「可配置项的默认值」，由后台表单预填，不落库、不作读侧兜底。
--
-- 刻意不写 DEFAULT：NULL 就是「未配置」，是 §五.3 那条 fail-closed（kPa 档显示 --）的唯一可辨态。
--    一旦列带 DEFAULT 0.64，「未配置」在库里就不存在了，非法面积兜底那一格只能靠 0 造出来，
--    而 §五.3 同时禁止「用常量默认值在前端补位」——列带默认值等于把这条禁令从后端开始破。
--    注册通路（RegisterDevice 的 INSERT 列清单）也不写该列 ⇒ 新注册设备同样是未配置态。
--
-- 类型取 DOUBLE PRECISION，与同族展示口径列 daily_wear_stats.wearing_threshold_n（迁移 000027 用 REAL）
-- 有意不同：本列存的是**后台录入、并原样回显**的配置值。REAL（float4）没有 0.64 的精确表示，
-- 而读路把它扫进 Go 的 float64（同族先例：data-service model.go:488 的 *float64 扫 REAL 列），
-- 拓宽即现形为 0.6399999856948853 ⇒ 设备管理页 JSON 回显会带一串尾数。
-- ⚠️ psql 自己打印 0.64::real::text 仍显示 0.64（float4 的最短往返表示），失真只在拓宽侧可见，
--    所以选型证据是 pgx 那两条读数，不是 psql 的那一行。
-- DOUBLE PRECISION 保持十进制短形式往返，且与 REAL 一样是 PG 原生浮点，pgx 侧直扫 *float64，
-- 避开 NUMERIC 的回读形状问题。
BEGIN;

ALTER TABLE devices ADD COLUMN IF NOT EXISTS contact_area_cm2 DOUBLE PRECISION;

COMMENT ON COLUMN devices.contact_area_cm2 IS
  '压力点有效受压面积（cm2，T508 / PRD §7A.2.1）。NULL=未配置⇒kPa 档 fail-closed 显示 --；'
  '默认值 0.64 属后台配置项默认值，不落库、不作读侧兜底。写侧唯一通路 = PUT /api/v1/devices/:deviceId/contact-area。'
  '只用于展示层换算（kPa = N / area × 10），不参与落库判定、告警阈值与聚合（判档恒为 N）。';

COMMIT;
