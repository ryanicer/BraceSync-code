// T337：GET /api/v1/teams 列表补齐 description / status 两列。
//
// 缺陷原貌：shared-types Team 一直声明 description / status，GET /api/v1/teams/:teamId
// （TeamDetailDTO）也一直带出这两列，唯独列表 TeamDTO 没有这两字段 ⇒ 列表接口结构上
// 就带不出来。与 T333（teams 缺 leader / createdAt）同族，方向相反的一侧此前无人扫到。
//
// 本文件守值层面那一列：两列真的出现在响应 JSON 里（按线上字段名断言，不是 Go 结构体字段）。
// 「详情有的键列表必须有」这条通用防线已在 T356 泛化成表驱动，见 list_detail_superset_t356_test.go。
package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

func t337TeamRows() []repo.TeamRow {
	return []repo.TeamRow{
		{TeamID: "TEAM01", Name: "一组", MemberCount: 2, PatientCount: 3,
			Leader: "D01", LeaderName: "医生甲", Description: "脊柱侧弯保守治疗组", Status: "active",
			CreatedAt: time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC)},
		// 描述为空串（库里 description 可空，handler 侧 COALESCE 成空串）：键必须在，值可以是空
		{TeamID: "TEAM02", Name: "二组", MemberCount: 1, PatientCount: 1, Status: "active",
			CreatedAt: time.Date(2026, 8, 4, 9, 8, 7, 0, time.UTC)},
	}
}

// t337TeamDetailRow 详情侧夹具。键集合的「详情 ⊆ 列表」断言已泛化成表驱动，
// 见 list_detail_superset_t356_test.go 的 teams 用例；本文件只守值层面这两列。
func t337TeamDetailRow() *repo.TeamDetailRow {
	return &repo.TeamDetailRow{
		TeamID: "TEAM01", Name: "一组", Leader: "D01", LeaderName: "医生甲",
		MemberCount: 2, PatientCount: 3, Description: "脊柱侧弯保守治疗组", Status: "active",
		CreatedAt: time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC),
	}
}

func TestListTeams_DescriptionStatusInWire(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teams = t337TeamRows()

	w, resp := e.do(http.MethodGet, "/api/v1/teams", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var list []map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	require.Len(t, list, 2)

	assert.Equal(t, "脊柱侧弯保守治疗组", list[0]["description"], "第一行 description 应为库里的描述")
	assert.Equal(t, "active", list[0]["status"])
	for i, row := range list {
		_, hasDesc := row["description"]
		_, hasStatus := row["status"]
		assert.True(t, hasDesc, "第 %d 行响应里必须有 description 键（契约 Team 声明）", i)
		assert.True(t, hasStatus, "第 %d 行响应里必须有 status 键（契约 Team 声明）", i)
	}
	assert.Equal(t, "", list[1]["description"], "无描述回空串而不是丢键")
}
