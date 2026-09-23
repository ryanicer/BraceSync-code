// T350：Dashboard 聚合查询的数据范围（PRD §7D.11「医护仅本团队患者」）。
//
// 团队由服务端从网关透传身份推导（X-User-Id = admins.admin_id → doctors.team_id），
// 不接受任何客户端入参（teamId 查询参数 / 地址栏切换一律不作为权限来源）。
package model

// TeamScope 聚合查询的患者范围。
//
// 三态（后两态必须可区分，否则「无团队医生」会退化成「全院」）：
//   - 不限：admin / 客服 / 技师（本卡只收紧 ROLE_DOCTOR，其余口径原样）
//   - 限某团队：医生的 doctors.team_id
//   - 限「无团队」：有医生身份但 team_id 为 NULL/空 ⇒ 空集
type TeamScope struct {
	Limited bool
	TeamID  string
}

// ScopeAll 全院口径（非医生角色的默认值）。
func ScopeAll() TeamScope { return TeamScope{} }

// ScopeTeam 限定单团队；teamID 传空串 = 该医生无团队 ⇒ 查询返回空集（fail-closed）。
func ScopeTeam(teamID string) TeamScope { return TeamScope{Limited: true, TeamID: teamID} }

// Scoped 是否按团队收口（repo 拼谓词用）。
func (s TeamScope) Scoped() bool { return s.Limited }

// TeamArg SQL 参数形态：无团队 ⇒ nil（NULL）。
// 谓词写成 team_id = $n::text，NULL 比较结果恒为「非真」，行集为空 ⇒ fail-closed。
func (s TeamScope) TeamArg() any {
	if s.TeamID == "" {
		return nil
	}
	return s.TeamID
}
