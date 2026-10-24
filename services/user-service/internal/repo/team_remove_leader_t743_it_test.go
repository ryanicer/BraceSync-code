//go:build integration
// +build integration

// T743 判据 1~4：移除团队成员时，若被移除者就是 teams.leader，leader 必须同步置 NULL，
// 且与成员摘除同一事务；非 leader 成员与重复/不存在 memberId 不得动 leader；
// 团队本身仍可读（孤儿团队可见）。
//
// 单测证明不了「两条 UPDATE 要么都成要么都不成」，故本文件跑真库（testcontainers PG15）：
// 用临时触发器把第二笔 UPDATE 打挂，读数必须显示第一笔也回滚。
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t743SeedTeam 建一套独占夹具：团队 + 负责人（同时在团）+ 另一名成员。
func t743SeedTeam(t *testing.T, suffix string) (teamID, leaderID, memberID string) {
	t.Helper()
	ctx := context.Background()
	teamID = "TEAM-T743-" + suffix
	leaderID = "DOC-T743-LEADER-" + suffix
	memberID = "DOC-T743-MEMBER-" + suffix

	_, err := itStore.pool.Exec(ctx,
		`INSERT INTO teams (team_id, name, leader, status, team_type)
		 VALUES ($1, $2, $3, 'active', 'medical')`,
		teamID, "T743夹具团队-"+suffix, leaderID)
	require.NoError(t, err)
	_, err = itStore.pool.Exec(ctx,
		`INSERT INTO doctors (doctor_id, name, title, department, team_id, admin_id, status) VALUES
		   ($1, 'T743负责人', '主任医师', '骨科', $3, $4, 'enabled'),
		   ($2, 'T743成员', '主治医师', '骨科', $3, $4, 'enabled')`,
		leaderID, memberID, teamID, itAdmin)
	require.NoError(t, err)

	t.Cleanup(func() {
		c := context.Background()
		_, _ = itStore.pool.Exec(c, `DELETE FROM doctors WHERE doctor_id = ANY($1)`, []string{leaderID, memberID})
		_, _ = itStore.pool.Exec(c, `DELETE FROM teams WHERE team_id = $1`, teamID)
	})
	return teamID, leaderID, memberID
}

func t743LeaderOf(t *testing.T, teamID string) *string {
	t.Helper()
	var leader *string
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT leader FROM teams WHERE team_id = $1`, teamID).Scan(&leader))
	return leader
}

func t743TeamOfDoctor(t *testing.T, doctorID string) *string {
	t.Helper()
	var teamID *string
	require.NoError(t, itStore.pool.QueryRow(context.Background(),
		`SELECT team_id FROM doctors WHERE doctor_id = $1`, doctorID).Scan(&teamID))
	return teamID
}

// 判据 1 + 4：移除 leader → leader=NULL、成员摘除、团队仍可读且仍在列表里。
func TestIT_T743_RemoveLeaderClearsLeader(t *testing.T) {
	teamID, leaderID, _ := t743SeedTeam(t, "CLEAR")
	ctx := context.Background()

	require.NoError(t, itStore.RemoveTeamMember(ctx, teamID, leaderID, "doctor"))

	assert.Nil(t, t743TeamOfDoctor(t, leaderID), "负责人必须已从成员表摘除")
	assert.Nil(t, t743LeaderOf(t, teamID), "被移除者是 leader ⇒ teams.leader 必须置 NULL")

	// 孤儿团队仍可读（handler 会把空 leader 投影成 JSON null）
	detail, err := itStore.GetTeam(ctx, teamID)
	require.NoError(t, err, "leader 为空不得影响团队详情可读（孤儿团队必须可见）")
	assert.Empty(t, detail.Leader)

	list, err := itStore.ListTeams(ctx)
	require.NoError(t, err)
	found := false
	for _, r := range list {
		if r.TeamID == teamID {
			found = true
			assert.Empty(t, r.Leader, "列表投影同样回空 leader，不得过滤掉该行")
		}
	}
	assert.True(t, found, "孤儿团队必须仍出现在团队列表里")
}

// 判据 2：移除普通成员 → leader 不动。
func TestIT_T743_RemoveNonLeaderKeepsLeader(t *testing.T) {
	teamID, leaderID, memberID := t743SeedTeam(t, "KEEP")
	ctx := context.Background()

	require.NoError(t, itStore.RemoveTeamMember(ctx, teamID, memberID, "doctor"))

	assert.Nil(t, t743TeamOfDoctor(t, memberID))
	leader := t743LeaderOf(t, teamID)
	require.NotNil(t, leader, "移除非 leader 成员不得清空负责人")
	assert.Equal(t, leaderID, *leader)
	assert.Equal(t, teamID, *t743TeamOfDoctor(t, leaderID), "负责人成员身份不动")
}

// 判据 3：重复/不存在 memberId 幂等 200，leader 不被误清。
func TestIT_T743_IdempotentRemoveKeepsLeader(t *testing.T) {
	teamID, leaderID, _ := t743SeedTeam(t, "IDEMP")
	ctx := context.Background()

	// 不存在的 memberId：0 行 no-op
	require.NoError(t, itStore.RemoveTeamMember(ctx, teamID, "DOC-T743-NOT-EXIST", "doctor"))
	leader := t743LeaderOf(t, teamID)
	require.NotNil(t, leader)
	assert.Equal(t, leaderID, *leader, "不存在的 memberId 不得误清 leader")

	// 首次移除 leader 后重复移除：仍 no-op，leader 保持 NULL
	require.NoError(t, itStore.RemoveTeamMember(ctx, teamID, leaderID, "doctor"))
	require.NoError(t, itStore.RemoveTeamMember(ctx, teamID, leaderID, "doctor"))
	assert.Nil(t, t743LeaderOf(t, teamID))
}

// 判据 1「同事务」：把第二笔 UPDATE（清 leader）打挂，第一笔（摘成员）必须一起回滚。
//
// 构造的失败路径只在本事务内可见：teams 上的临时触发器对该夹具团队把 leader=NULL 的更新
// 直接 RAISE；RemoveTeamMember 必须返回错误，且成员与 leader 都保持调用前原值。
func TestIT_T743_LeaderClearAndMemberRemovalAreAtomic(t *testing.T) {
	teamID, leaderID, _ := t743SeedTeam(t, "ATOMIC")
	ctx := context.Background()

	_, err := itStore.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION t743_fail_leader_clear() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.team_id = '`+teamID+`' AND NEW.leader IS NULL THEN
				RAISE EXCEPTION 'T743 injected leader-clear failure';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER t743_fail_leader_clear BEFORE UPDATE ON teams
			FOR EACH ROW EXECUTE FUNCTION t743_fail_leader_clear();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = itStore.pool.Exec(c, `DROP TRIGGER IF EXISTS t743_fail_leader_clear ON teams`)
		_, _ = itStore.pool.Exec(c, `DROP FUNCTION IF EXISTS t743_fail_leader_clear()`)
	})

	err = itStore.RemoveTeamMember(ctx, teamID, leaderID, "doctor")
	require.Error(t, err, "触发器打挂清 leader 后，整个移除动作必须失败")

	attached := t743TeamOfDoctor(t, leaderID)
	require.NotNil(t, attached, "成员摘除必须随事务回滚，不能留下半截")
	assert.Equal(t, teamID, *attached)
	leader := t743LeaderOf(t, teamID)
	require.NotNil(t, leader, "leader 清空失败时保持原值")
	assert.Equal(t, leaderID, *leader)
}
