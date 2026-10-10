-- T627 方案乙（依 T623 设计载体 docs/tasks/peter/T623-附-医护与维护模型区分方案及PRD修订提案.md）：
-- 给 teams 加「团队类型」列，把「医护 vs 维护」这条口径落成库里的硬件。
--
-- 为什么是加列而不是取消归属（甲案）或另立实体（丙案）：
--   「技师被读成医护的一种」的根因是归属轴复用 —— doctors.team_id（000001:44）与
--   technicians.team_id（000001:58）指向同一张没有任何类型列的 teams（000001:10-16），
--   所以团队列表/统计卡把两类人加进同一个 member_count。不加这一列，任何「只出维护侧团队」
--   的过滤都只能靠团队名字猜，服务端也无从校验（现状 validateTechTeam 只校验存在性）。
--
-- NOT NULL + DEFAULT 'medical'：存量团队全部落 'medical'，这是有意的一次性判定 ——
--   PRD §7D.4 本来就把本节「团队」定性为医疗团队，seed 里那三颗（TEAM01-03）也是医护命名。
--   不做「按名字里有没有『维护』字样」之类的数据驱动归类：现网名册是运行期建的，
--   命名不带类型语义，猜错就是把医护侧刷成维护侧（不可逆的方向）。
--   维护侧班组由 seed/后台新建时显式带 team_type='maintenance' 产生，不靠回填。
--
-- 技师侧的归属修正不在本迁移里做：那是「存量刷数」动作，须先 dry-run 出计数、Boss 批准
--   再执行（T627 §四/§五.1），与 DDL 分开，回滚面才不互相绑住。
--
-- CHECK 只收两枚字面量，不留 'unknown' 态：类型列一旦允许第三值，
--   「技师只能挂 maintenance」这条判据就出现既不放行也不拒绝的缝（写侧校验只能二选一）。
BEGIN;

ALTER TABLE teams ADD COLUMN IF NOT EXISTS team_type VARCHAR(16) NOT NULL DEFAULT 'medical'
    CHECK (team_type IN ('medical','maintenance'));

COMMENT ON COLUMN teams.team_type IS
  '团队类型（T627 方案乙）：medical=医疗团队（承载患者数据团队隔离，成员为医护）；'
  'maintenance=维护班组（技师侧归属）。写侧判据在 user-service 的 validateTechTeamType：'
  '技师新建/改队只能是 maintenance。成员数读数（teamMemberCountExpr）本卡未分侧——分侧口径是'
  'T623 附页请裁点 3，未裁 ⇒ 现网刷数前医生与技师仍相加为 member_count，刷数后两读自动同值。'
  '建库前存量团队由 DEFAULT 归 medical。';

COMMIT;
