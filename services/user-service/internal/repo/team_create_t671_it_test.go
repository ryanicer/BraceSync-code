//go:build integration
// +build integration

// T671 —— 新建团队负责人自动入团（真实 PG15，testcontainers）
//
// 成员明细唯一口径是 doctors.team_id（ListDoctorsByTeam）。旧 CreateTeam 只写 teams.leader，
// 负责人 team_id 不落 ⇒ 建团后成员面板看不到负责人（Boss 亲测）。本文件锁同事务修复后的行为：
// 建团成功负责人即在成员明细内；建团失败（重名/负责人不存在）不留下半行数据。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t671SeedTeamlessDoctor 插一枚不属任何团队的 enabled 医生，返回 doctor_id。
func t671SeedTeamlessDoctor(t *testing.T, suffix string) string {
	t.Helper()
	doctorID := "DOC-T671-" + suffix
	_, err := itStore.pool.Exec(context.Background(),
		`INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id, status)
		 VALUES ($1, 'T671负责人', '主治医师', '骨科', NULL, $2, 'enabled')
		 ON CONFLICT (doctor_id) DO NOTHING`,
		doctorID, itAdmin)
	require.NoError(t, err)
	return doctorID
}

func TestIT_T671_CreateTeam_LeaderJoinsMembersAtomically(t *testing.T) {
	ctx := context.Background()
	doctorID := t671SeedTeamlessDoctor(t, "JOIN")
	teamName := "T671入团验证组-JOIN"
	// 同名残留先清（用例可重复执行）
	_, _ = itStore.pool.Exec(ctx, `DELETE FROM teams WHERE name = $1`, teamName)

	created, err := itStore.CreateTeam(ctx, TeamInput{
		Name: teamName, Leader: doctorID, Description: "T671 IT", TeamType: "medical",
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	// ① 成员明细立即可见负责人（读 doctors.team_id 的那条腿）
	members, err := itStore.ListDoctorsByTeam(ctx, created.TeamID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, doctorID, members[0].DoctorID)

	// ② doctors 行本体 team_id 已落
	var teamID *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT team_id FROM doctors WHERE doctor_id = $1`, doctorID).Scan(&teamID))
	require.NotNil(t, teamID)
	assert.Equal(t, created.TeamID, *teamID)

	// ③ 详情实时成员数（teamMemberCountExpr 数 enabled 医生/技师）含负责人
	detail, err := itStore.GetTeam(ctx, created.TeamID)
	require.NoError(t, err)
	assert.Equal(t, 1, detail.MemberCount, "建团后实时成员数应为 1（负责人）")
	assert.Equal(t, doctorID, detail.Leader)
}

func TestIT_T671_CreateTeam_DuplicateNameRollsBackLeaderAttach(t *testing.T) {
	ctx := context.Background()
	doctorID := t671SeedTeamlessDoctor(t, "DUP")
	teamName := "T671入团验证组-DUP"
	_, _ = itStore.pool.Exec(ctx, `DELETE FROM teams WHERE name = $1`, teamName)

	// 第一次建团成功（前置）
	first, err := itStore.CreateTeam(ctx, TeamInput{Name: teamName, Leader: doctorID, TeamType: "medical"})
	require.NoError(t, err)

	// 第二名无团医生尝试用同名建团：必须整笔失败，且他的 team_id 保持 NULL（不被半提交带走）
	doctorID2 := t671SeedTeamlessDoctor(t, "DUP2")
	_, err = itStore.CreateTeam(ctx, TeamInput{Name: teamName, Leader: doctorID2, TeamType: "medical"})
	assert.ErrorIs(t, err, ErrTeamNameExists)

	var teamID *string
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT team_id FROM doctors WHERE doctor_id = $1`, doctorID2).Scan(&teamID))
	assert.Nil(t, teamID, "重名回滚后第二位负责人不该被挂到任何团队")

	// 原团成员不变
	members, err := itStore.ListDoctorsByTeam(ctx, first.TeamID)
	require.NoError(t, err)
	assert.Len(t, members, 1)
}

func TestIT_T671_CreateTeam_UnknownLeaderRejected(t *testing.T) {
	ctx := context.Background()
	teamName := "T671入团验证组-GHOST"
	_, _ = itStore.pool.Exec(ctx, `DELETE FROM teams WHERE name = $1`, teamName)

	_, err := itStore.CreateTeam(ctx, TeamInput{
		Name: teamName, Leader: "DOC-T671-NOT-EXIST", TeamType: "medical",
	})
	assert.ErrorIs(t, err, ErrLeaderNotFound)

	// 团队行不得留下
	var count int
	require.NoError(t, itStore.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM teams WHERE name = $1`, teamName).Scan(&count))
	assert.Equal(t, 0, count)
}
