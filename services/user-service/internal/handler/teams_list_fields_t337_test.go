// T337：GET /api/v1/teams 列表补齐 description / status 两列。
//
// 缺陷原貌：shared-types Team 一直声明 description / status，GET /api/v1/teams/:teamId
// （TeamDetailDTO）也一直带出这两列，唯独列表 TeamDTO 没有这两字段 ⇒ 列表接口结构上
// 就带不出来。与 T333（teams 缺 leader / createdAt）同族，方向相反的一侧此前无人扫到。
//
// 本文件守两件事：① 两列真的出现在响应 JSON 里（按线上字段名断言，不是 Go 结构体字段）；
// ② 列表与详情的字段集合不再漂移（详情有的键列表必须有），这是这条缺陷的通用防线。
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

// wireKeys 把响应 data 解成 map，拿线上真实字段名
func wireKeys(t *testing.T, v interface{}) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

// TestListTeams_FieldsSupersetOfDetail 列表字段集合必须覆盖详情字段集合。
// 详情（TeamDetailDTO）是这条资源的字段基准；列表少任何一个键都是 T337 这类缺陷。
func TestListTeams_FieldsSupersetOfDetail(t *testing.T) {
	e := newEnv(t, true, true)
	e.store.teams = t337TeamRows()
	e.store.gotTeam = &repo.TeamDetailRow{
		TeamID: "TEAM01", Name: "一组", Leader: "D01", LeaderName: "医生甲",
		MemberCount: 2, PatientCount: 3, Description: "脊柱侧弯保守治疗组", Status: "active",
		CreatedAt: time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC),
	}

	_, listResp := e.do(http.MethodGet, "/api/v1/teams", nil, nil)
	var rows []map[string]interface{}
	require.NoError(t, json.Unmarshal(listResp.Data, &rows))
	require.NotEmpty(t, rows)

	_, detailResp := e.do(http.MethodGet, "/api/v1/teams/TEAM01", nil, nil)
	detailKeys := wireKeys(t, detailResp.Data)

	listKeys := map[string]bool{}
	for k := range rows[0] {
		listKeys[k] = true
	}
	for _, k := range detailKeys {
		assert.True(t, listKeys[k], "详情返回了 %s 但列表没有 —— 列表接口漏回填字段（T337 同类）", k)
	}
}
