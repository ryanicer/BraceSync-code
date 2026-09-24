//go:build integration
// +build integration

// T350 判据③ 后半（PM 第 8 轮打回项）：真库里 GetPatientInTeam 的四种「不可见」必须同结果。
//
// 打回前的现网事实：医护读跨团队患者回 403、读不存在的患者号回 404 ⇒ 状态码本身成了
// 患者号存在性 oracle。修法是把团队谓词并进详情读的那一条 SQL，让「无此行 / 在他团队 /
// 未分配团队 / 调用者无团队」在 repo 层就一律 (nil, nil)，由 handler 统一回 403。
//
// 单测只能证明 handler 分支写对了，证明不了「NULL team_id 的患者不被等值比较放行」
// 与「join 投影加谓词后仍取到团队名」这两条 SQL 语义，故本文件跑真库（testcontainers PG15）。
//
// 运行：make test-integration（需 Docker；本机无 Docker 时由 CI 跑，按用例名核日志）
package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const t350ForeignTeam = "TEAM-USR-IT-OTHER"

func TestITT350GetPatientInTeamUnifiesDeniedAndMissing(t *testing.T) {
	ctx := context.Background()

	// 1. 本团队命中：走的是与 GetPatient 同一套投影，团队名仍来自 teams join
	own, err := itStore.GetPatientInTeam(ctx, "P-USR-IT-1", itTeam)
	require.NoError(t, err)
	require.NotNil(t, own, "本团队患者必须读得到，否则收口过头")
	assert.Equal(t, "P-USR-IT-1", own.PatientID)
	require.NotNil(t, own.TeamName)
	assert.Equal(t, "集成团队", *own.TeamName)

	// 2~5. 四种不可见同形：既不返回行，也不报错（报错会变成 500，那就又可分了）
	type denyCase struct {
		name      string
		patientID string
		teamID    string
	}
	for _, tc := range []denyCase{
		{"跨团队（档案真实存在）", "P-USR-IT-1", t350ForeignTeam},
		{"未分配团队（patients.team_id 为 NULL）", "P-USR-IT-2", itTeam},
		{"档案不存在（存在性探测）", "P-USR-NOT-EXIST", itTeam},
		{"调用者无团队归属（teamID 空串）", "P-USR-IT-1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, denyErr := itStore.GetPatientInTeam(ctx, tc.patientID, tc.teamID)
			require.NoError(t, denyErr, "不可见必须是「无行」而不是报错，否则 handler 会回 500 又可分")
			assert.Nil(t, row)
		})
	}

	// 6. 与不带谓词的读法对照：repo 层两者对「不存在」同回 (nil, nil)，
	//    差别只在受限身份多了「跨团队」与「NULL 团队」两格 ⇒ 403/404 的分歧纯粹是 handler 映射。
	missing, err := itStore.GetPatient(ctx, "P-USR-NOT-EXIST")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
