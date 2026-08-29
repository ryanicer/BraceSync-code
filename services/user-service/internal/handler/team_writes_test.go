// Package handler T059 团队管理写功能 KNOWN_RED 测试
//
// 覆盖 6 个新端点（设计源：docs/design/admin/团队管理.html）：
//
//	POST   /api/v1/teams                            创建团队（name 唯一 + leader 校验）
//	PUT    /api/v1/teams/:teamId                    编辑团队（团队存在 + name 查重排除自身）
//	DELETE /api/v1/teams/:teamId                    删除团队（被引用拒绝：reject-if-referenced）
//	POST   /api/v1/teams/:teamId/members            添加成员（doctor/tech.team_id 置本 team）
//	PUT    /api/v1/teams/:teamId/members/:memberId  编辑成员（更新 doctor.title）
//	DELETE /api/v1/teams/:teamId/members/:memberId  移除成员（幂等：team_id 置 NULL）
//
// 预期红态：当前 handler.go 中 6 个 stub handler 统一返回 500 CodeInternal，
// 不调用 store、不做参数校验、不映射业务错误。本文件断言"实现后的契约行为"
// （200/400/404/409 + DTO 字段 + 入参透传），因此在 stub 阶段全部 FAIL，
// 属 TDD 预期红态。
//
// 删除约束策略：reject-if-referenced（Ella 推荐，待 Boss 评审）。
// 若 Boss 改选软删除（status=deleted），实现方调整 store 行为 + 移除 409 用例改 200。
//
// 实现方转绿清单（详见 handler.go T059 段注释）：
//   - 6 stub handler 替换为真实校验 + store 调用 + 错误映射
//   - pg.go 6 个 stub 方法填充真实 SQL
//   - gateway proxy_admin.go userServiceRoutes 追加 6 条写路由
package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// ─────────────────────────────────────────────────────────────
// 测试样本装配
// ─────────────────────────────────────────────────────────────

// sampleTeamDetail 装配 TeamDetailRow 样本（实现方 store 返回行）
func sampleTeamDetail() repo.TeamDetailRow {
	return repo.TeamDetailRow{
		TeamID:       "TEAM26001",
		Name:         "骨科一组",
		Leader:       "D0001",
		LeaderName:   "张主任",
		MemberCount:  0,
		PatientCount: 0,
		Description:  "骨科诊疗组",
		Status:       "active",
		CreatedAt:    time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
	}
}

// sampleTeamMember 装配 TeamMemberRow 样本（实现方 store 返回行）
func sampleTeamMember() repo.TeamMemberRow {
	return repo.TeamMemberRow{
		MemberID:     "D0002",
		MemberType:   "doctor",
		Name:         "李医生",
		Role:         "主治医师",
		Title:        "骨科",
		PatientCount: 32,
		JoinTime:     time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
		Status:       "enabled",
	}
}

// ptrToTeamDetail 取 TeamDetailRow 指针
func ptrToTeamDetail(r repo.TeamDetailRow) *repo.TeamDetailRow { return &r }

// ptrToTeamMember 取 TeamMemberRow 指针
func ptrToTeamMember(r repo.TeamMemberRow) *repo.TeamMemberRow { return &r }

// ─────────────────────────────────────────────────────────────
// 创建团队 POST /api/v1/teams
// ─────────────────────────────────────────────────────────────

// TestCreateTeam_KNOWN_RED 创建团队端点契约
//
// 预期：成功 200 + TeamDetailDTO（含 leaderName）；缺 name 400；leader 不存在 400；
// name 重复 409；name 超 50 400；description 超 200 400。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestCreateTeam_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回新建团队 DTO（含 leaderName）+ 入参透传到 store
	t.Run("success_200_with_TeamDetailDTO", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + TeamDetailDTO（含 leaderName）")
		e.store.createdTeam = ptrToTeamDetail(sampleTeamDetail())
		e.store.createTeamErr = nil
		body := model.CreateTeamRequestDTO{
			Name:        "骨科一组",
			Leader:      "D0001",
			Description: "骨科诊疗组",
		}
		w, resp := e.do(http.MethodPost, "/api/v1/teams", body, nil)
		assert.Equal(t, http.StatusOK, w.Code, "成功应返回 200")
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.TeamDetailDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "TEAM26001", dto.TeamID)
		assert.Equal(t, "骨科一组", dto.Name)
		assert.Equal(t, "D0001", dto.Leader)
		assert.Equal(t, "张主任", dto.LeaderName, "响应应含 leaderName（join doctors.name）")
		assert.Equal(t, "骨科诊疗组", dto.Description)
		assert.Equal(t, "active", dto.Status)
		assert.NotEmpty(t, dto.CreatedAt, "响应应含 createdAt RFC3339")
		// 入参透传断言（实现方需将 DTO 字段映射到 TeamInput）
		assert.Equal(t, "骨科一组", e.store.lastCreateTeamIn.Name)
		assert.Equal(t, "D0001", e.store.lastCreateTeamIn.Leader)
		assert.Equal(t, "骨科诊疗组", e.store.lastCreateTeamIn.Description)
	})

	// 缺 name → 400 CodeInvalidParam
	t.Run("missing_name_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（name 必填）")
		e.store.createdTeam = nil
		e.store.createTeamErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{Leader: "D0001"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// name 纯空白 → 400 CodeInvalidParam
	t.Run("blank_name_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（name 须非空白）")
		e.store.createdTeam = nil
		e.store.createTeamErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{Name: "   ", Leader: "D0001"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// name 超 50 字符 → 400 CodeInvalidParam
	t.Run("name_too_long_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（name 超 50 字符）")
		e.store.createdTeam = nil
		e.store.createTeamErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{
				Name:   strings.Repeat("团", 51), // 51 字符超过 50 上限
				Leader: "D0001",
			}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// 缺 leader → 400 CodeInvalidParam
	t.Run("missing_leader_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（leader 必填）")
		e.store.createdTeam = nil
		e.store.createTeamErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{Name: "新团队"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// leader 不存在（store 返回 ErrLeaderNotFound）→ 400 CodeInvalidParam
	t.Run("leader_not_found_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（leader doctorId 不存在）")
		e.store.createdTeam = nil
		e.store.createTeamErr = repo.ErrLeaderNotFound
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{Name: "新团队", Leader: "D-NOTEXIST"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// description 超 200 字符 → 400 CodeInvalidParam
	t.Run("description_too_long_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（description 超 200 字符）")
		e.store.createdTeam = nil
		e.store.createTeamErr = nil
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{
				Name:        "新团队",
				Leader:      "D0001",
				Description: strings.Repeat("x", 201), // 201 字符超过 200 上限
			}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// name 已存在（store 返回 ErrTeamNameExists）→ 409 CodeConflict
	t.Run("duplicate_name_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 409 CodeConflict（team name 已存在）")
		e.store.createdTeam = nil
		e.store.createTeamErr = repo.ErrTeamNameExists
		w, resp := e.do(http.MethodPost, "/api/v1/teams",
			model.CreateTeamRequestDTO{Name: "骨科一组", Leader: "D0001"}, nil)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, model.CodeConflict, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 编辑团队 PUT /api/v1/teams/:teamId
// ─────────────────────────────────────────────────────────────

// TestUpdateTeam_KNOWN_RED 编辑团队端点契约
//
// 预期：成功 200 + TeamDetailDTO；团队不存在 404；缺 name 400；
// leader 不存在 400；name 被其他团队占用 409。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestUpdateTeam_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回更新后的团队
	t.Run("success_200_with_updated_team", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + TeamDetailDTO（name 已更名）")
		updated := sampleTeamDetail()
		updated.Name = "骨科一组（更名）"
		updated.Leader = "D0002"
		updated.LeaderName = "李主任"
		e.store.updatedTeam = ptrToTeamDetail(updated)
		e.store.updateTeamErr = nil
		body := model.UpdateTeamRequestDTO{
			Name:        "骨科一组（更名）",
			Leader:      "D0002",
			Description: "更新描述",
		}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001", body, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.TeamDetailDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "TEAM26001", dto.TeamID)
		assert.Equal(t, "骨科一组（更名）", dto.Name)
		assert.Equal(t, "D0002", dto.Leader)
		assert.Equal(t, "李主任", dto.LeaderName)
		// 入参透传
		assert.Equal(t, "TEAM26001", e.store.lastUpdateTeamID)
		assert.Equal(t, "骨科一组（更名）", e.store.lastUpdateTeamIn.Name)
		assert.Equal(t, "D0002", e.store.lastUpdateTeamIn.Leader)
	})

	// 团队不存在 → 404 CodeNotFound
	t.Run("team_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（team 不存在）")
		e.store.updatedTeam = nil
		e.store.updateTeamErr = repo.ErrTeamNotFound
		body := model.UpdateTeamRequestDTO{Name: "新名", Leader: "D0001"}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM-NOPE", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// 缺 name → 400 CodeInvalidParam
	t.Run("missing_name_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（name 必填）")
		e.store.updatedTeam = nil
		e.store.updateTeamErr = nil
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001",
			model.UpdateTeamRequestDTO{Leader: "D0001"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// leader 不存在 → 400 CodeInvalidParam
	t.Run("leader_not_found_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（leader 不存在）")
		e.store.updatedTeam = nil
		e.store.updateTeamErr = repo.ErrLeaderNotFound
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001",
			model.UpdateTeamRequestDTO{Name: "新名", Leader: "D-NOPE"}, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// name 被其他团队占用（store 返回 ErrTeamNameExists，查重排除自身）→ 409 CodeConflict
	t.Run("duplicate_name_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 409 CodeConflict（name 被其他团队占用）")
		e.store.updatedTeam = nil
		e.store.updateTeamErr = repo.ErrTeamNameExists
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001",
			model.UpdateTeamRequestDTO{Name: "已被占用的团队名", Leader: "D0001"}, nil)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, model.CodeConflict, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 删除团队 DELETE /api/v1/teams/:teamId
// ─────────────────────────────────────────────────────────────

// TestDeleteTeam_KNOWN_RED 删除团队端点契约
//
// 预期：成功 200 data:null；团队不存在 404；团队被引用 409（文案含计数）。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
//
// 删除约束策略：reject-if-referenced（Ella 推荐，待 Boss 评审）。
func TestDeleteTeam_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回 200 data:null
	t.Run("success_200_data_null", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 data:null")
		e.store.deleteTeamErr = nil
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001", nil, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		// 入参透传
		assert.Equal(t, "TEAM26001", e.store.lastDeleteTeamID)
		// data 应为 null（ok(c, nil) → data: null）
		assert.Equal(t, "null", string(resp.Data))
	})

	// 团队不存在 → 404 CodeNotFound
	t.Run("team_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（team 不存在）")
		e.store.deleteTeamErr = repo.ErrTeamNotFound
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM-NOPE", nil, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// 团队被引用（patients + members 命中）→ 409 CodeConflict（文案含计数）
	// 删除约束策略 A：reject-if-referenced（Ella 推荐，待 Boss 评审）
	t.Run("team_in_use_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 409 CodeConflict（团队被引用：N 患者，M 成员）")
		e.store.deleteTeamErr = &repo.ErrTeamInUse{PatientCount: 5, MemberCount: 3}
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001", nil, nil)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, model.CodeConflict, resp.Code)
		// 文案应含引用计数（"5 patients" 与 "3 members"）
		assert.Contains(t, resp.Message, "5 patients", "409 文案应含患者引用计数")
		assert.Contains(t, resp.Message, "3 members", "409 文案应含成员引用计数")
	})
}

// ─────────────────────────────────────────────────────────────
// 添加成员 POST /api/v1/teams/:teamId/members
// ─────────────────────────────────────────────────────────────

// TestAddTeamMember_KNOWN_RED 添加成员端点契约
//
// 预期：成功 200 + TeamMemberDTO；团队不存在 404；member 不存在 404；
// member 已在本团队 409；memberType 非枚举 400；memberId 空 400。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestAddTeamMember_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回 TeamMemberDTO（含脱敏手机号）+ 入参透传
	t.Run("success_200_with_TeamMemberDTO", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + TeamMemberDTO（含 phoneMasked）")
		e.store.addedMember = ptrToTeamMember(sampleTeamMember())
		e.store.addMemberErr = nil
		body := model.AddMemberRequestDTO{
			MemberType: "doctor",
			MemberID:   "D0002",
			Role:       "主治医师",
		}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.TeamMemberDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "D0002", dto.MemberID)
		assert.Equal(t, "doctor", dto.MemberType)
		assert.Equal(t, "李医生", dto.Name)
		assert.Equal(t, "主治医师", dto.Role)
		assert.Equal(t, 32, dto.PatientCount)
		assert.NotEmpty(t, dto.JoinTime, "响应应含 joinTime RFC3339")
		// 入参透传
		assert.Equal(t, "TEAM26001", e.store.lastAddTeamID)
		assert.Equal(t, "doctor", e.store.lastAddMemberIn.MemberType)
		assert.Equal(t, "D0002", e.store.lastAddMemberIn.MemberID)
		assert.Equal(t, "主治医师", e.store.lastAddMemberIn.Role)
	})

	// 团队不存在 → 404 CodeNotFound
	t.Run("team_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（team 不存在）")
		e.store.addedMember = nil
		e.store.addMemberErr = repo.ErrTeamNotFound
		body := model.AddMemberRequestDTO{MemberType: "doctor", MemberID: "D0002"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM-NOPE/members", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// member 不存在（store 返回 ErrMemberNotFound）→ 404 CodeNotFound
	t.Run("member_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（member 不存在）")
		e.store.addedMember = nil
		e.store.addMemberErr = repo.ErrMemberNotFound
		body := model.AddMemberRequestDTO{MemberType: "doctor", MemberID: "D-NOPE"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// member 已在本团队（store 返回 ErrMemberInTeam）→ 409 CodeConflict
	t.Run("member_already_in_team_409", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 409 CodeConflict（member 已属本团队）")
		e.store.addedMember = nil
		e.store.addMemberErr = repo.ErrMemberInTeam
		body := model.AddMemberRequestDTO{MemberType: "doctor", MemberID: "D0002"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, model.CodeConflict, resp.Code)
	})

	// memberType 非枚举值 → 400 CodeInvalidParam
	t.Run("invalid_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 须 doctor|technician）")
		e.store.addedMember = nil
		e.store.addMemberErr = nil
		body := model.AddMemberRequestDTO{MemberType: "nurse", MemberID: "X0001"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// memberId 空 → 400 CodeInvalidParam
	t.Run("missing_memberId_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberId 必填）")
		e.store.addedMember = nil
		e.store.addMemberErr = nil
		body := model.AddMemberRequestDTO{MemberType: "doctor"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// memberType 缺失 → 400 CodeInvalidParam
	t.Run("missing_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 必填）")
		e.store.addedMember = nil
		e.store.addMemberErr = nil
		body := model.AddMemberRequestDTO{MemberID: "D0002"}
		w, resp := e.do(http.MethodPost, "/api/v1/teams/TEAM26001/members", body, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 编辑成员 PUT /api/v1/teams/:teamId/members/:memberId
// ─────────────────────────────────────────────────────────────

// TestUpdateTeamMember_KNOWN_RED 编辑成员端点契约
//
// 预期：成功 200 + TeamMemberDTO；团队不存在 404；member 不存在或不属本团队 404；
// memberType 非枚举 400。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
func TestUpdateTeamMember_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回更新后的成员
	t.Run("success_200_with_updated_member", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 + TeamMemberDTO（role 已更新为主任医师）")
		updated := sampleTeamMember()
		updated.Role = "主任医师"
		e.store.updatedMember = ptrToTeamMember(updated)
		e.store.updateMemberErr = nil
		body := model.UpdateMemberRequestDTO{
			MemberType: "doctor",
			Role:       "主任医师",
		}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001/members/D0002", body, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		var dto model.TeamMemberDTO
		require.NoError(t, json.Unmarshal(resp.Data, &dto))
		assert.Equal(t, "D0002", dto.MemberID)
		assert.Equal(t, "主任医师", dto.Role, "role 应更新为主任医师")
		// 入参透传
		assert.Equal(t, "TEAM26001", e.store.lastUpdateMTeamID)
		assert.Equal(t, "D0002", e.store.lastUpdateMID)
		assert.Equal(t, "doctor", e.store.lastUpdateMemberIn.MemberType)
		assert.Equal(t, "主任医师", e.store.lastUpdateMemberIn.Role)
	})

	// 团队不存在 → 404 CodeNotFound
	t.Run("team_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（team 不存在）")
		e.store.updatedMember = nil
		e.store.updateMemberErr = repo.ErrTeamNotFound
		body := model.UpdateMemberRequestDTO{MemberType: "doctor", Role: "主任医师"}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM-NOPE/members/D0002", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// member 不存在或不属本团队 → 404 CodeNotFound
	t.Run("member_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（member 不存在或不属本团队）")
		e.store.updatedMember = nil
		e.store.updateMemberErr = repo.ErrMemberNotFound
		body := model.UpdateMemberRequestDTO{MemberType: "doctor", Role: "主任医师"}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001/members/D-NOPE", body, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// memberType 非枚举值 → 400 CodeInvalidParam
	t.Run("invalid_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 须 doctor|technician）")
		e.store.updatedMember = nil
		e.store.updateMemberErr = nil
		body := model.UpdateMemberRequestDTO{MemberType: "nurse", Role: "护士长"}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001/members/D0002", body, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// 缺 memberType → 400 CodeInvalidParam
	t.Run("missing_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 必填）")
		e.store.updatedMember = nil
		e.store.updateMemberErr = nil
		body := model.UpdateMemberRequestDTO{Role: "主任医师"}
		w, resp := e.do(http.MethodPut, "/api/v1/teams/TEAM26001/members/D0002", body, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})
}

// ─────────────────────────────────────────────────────────────
// 移除成员 DELETE /api/v1/teams/:teamId/members/:memberId?memberType=doctor
// ─────────────────────────────────────────────────────────────

// TestRemoveTeamMember_KNOWN_RED 移除成员端点契约
//
// 预期：成功 200 data:null（幂等）；团队不存在 404；memberType 缺失 400；
// memberType 非枚举 400。
// 当前 stub 统一返回 500 → 全部断言 FAIL（预期红态）。
//
// 幂等语义：移除不属本团队的成员（或已移除）→ 200 no-op（不返回 404）。
func TestRemoveTeamMember_KNOWN_RED(t *testing.T) {
	e := newEnv(t, true, true)

	// 成功：返回 200 data:null
	t.Run("success_200_data_null", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 data:null")
		e.store.removeMemberErr = nil
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001/members/D0002?memberType=doctor", nil, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, model.CodeOK, resp.Code)
		// 入参透传
		assert.Equal(t, "TEAM26001", e.store.lastRemoveTeamID)
		assert.Equal(t, "D0002", e.store.lastRemoveMID)
		assert.Equal(t, "doctor", e.store.lastRemoveMType)
		// data 应为 null
		assert.Equal(t, "null", string(resp.Data))
	})

	// 幂等：移除不属本团队的成员 → 200 no-op（不返回 404）
	t.Run("idempotent_remove_not_in_team_200", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 200 no-op（移除幂等：member 不在本团队也返回 200）")
		// store 不返回 ErrMemberNotFound（移除幂等：已 NULL no-op）
		e.store.removeMemberErr = nil
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001/members/D-ALREADY-REMOVED?memberType=doctor", nil, nil)
		assert.Equal(t, http.StatusOK, w.Code, "移除幂等：member 不在本团队应返回 200，不返回 404")
		assert.Equal(t, model.CodeOK, resp.Code)
	})

	// 团队不存在 → 404 CodeNotFound
	t.Run("team_not_found_404", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 404 CodeNotFound（team 不存在）")
		e.store.removeMemberErr = repo.ErrTeamNotFound
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM-NOPE/members/D0002?memberType=doctor", nil, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, model.CodeNotFound, resp.Code)
	})

	// memberType 缺失 → 400 CodeInvalidParam
	t.Run("missing_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 必填）")
		e.store.removeMemberErr = nil
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001/members/D0002", nil, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})

	// memberType 非枚举值 → 400 CodeInvalidParam
	t.Run("invalid_memberType_400", func(t *testing.T) {
		t.Log("KNOWN_RED: stub handler 返回 500，预期 400 CodeInvalidParam（memberType 须 doctor|technician）")
		e.store.removeMemberErr = nil
		w, resp := e.do(http.MethodDelete, "/api/v1/teams/TEAM26001/members/D0002?memberType=nurse", nil, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, model.CodeInvalidParam, resp.Code)
	})
}
