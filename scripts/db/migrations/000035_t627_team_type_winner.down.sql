-- T627 方案乙回滚：删掉 teams.team_type 一列，回到「teams 无类型列」的原形。
--
-- 刻意只删列、不动任何行：本卡没有任何一行数据是被本迁移改过的
--   （存量团队的新列值由 DEFAULT 产生，技师归属的修正在门外的存量刷数脚本里，
--    那个脚本自带反向通道）。所以 down 面不需要数据回填。
--
-- ⚠ 代码侧必须同拍回滚，否则列一删这几处立刻报错：
--   pg.go 的 listTeamsSelect 与 teamDetailSelect 的 t.team_type 投影、techColumns 的
--   teams.team_type、TeamTypeOf 的单列读、CreateTeam 的 INSERT 列清单。
--   只 down 迁移不下代码 = 团队列表整页 500，不是「回到旧行为」。
--   反之 teamMemberCountExpr 不引用本列（成员数读数按现状写法，分侧口径归 T623 附页请裁点 3，
--   Boss/PM 未裁 ⇒ 本卡没动它），所以它不在必须同拍回滚的清单里。
BEGIN;

ALTER TABLE teams DROP COLUMN IF EXISTS team_type;

COMMIT;
