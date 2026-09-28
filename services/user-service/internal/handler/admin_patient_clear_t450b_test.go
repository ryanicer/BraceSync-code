// Package handler — T450-②b 乙案实现侧测试：admin「编辑患者」的显式置 NULL 入参形态
//
// 背景（Alice 第 70 轮登记的那条差异）：五个指针键只能表达「不给 / 给个值」两态 ——
// JSON null 解码后与键缺席同形，所以接口根本没有「恢复为空」的入参形态，
// 管理端唯一置空通道只能写空串。乙案（PM 卡 T450 评论 2266，2026-09-28 17:41 拍）
// 用白名单数组键 clearFields 显式声明，同列既给值又被列为清空判 400。
//
// 判据（对应我 §2.3 交 Alice 的四条）：
//  1. 四个可空列（gender/age/diagnosis/cobbAngle）各能单独置空，且落库侧收到的只有列名；
//  2. 撞列 / 重复 / 表外字段 / name 一律 400，且拒绝腿零留痕、零触库；
//  3. 审计快照把置空记成 null（不是空串），与「没提交该字段压根不进快照」区分开；
//  4. 患者自助通道（T226）没有 clearFields 键 ⇒ DisallowUnknownFields 先拒，不受本次改动波及。
package handler

import (
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

// TestT450B_ClearFieldOrderLocksRepoWhitelist 错误文案里的可接受集合按 clearFieldOrder 拼，
// 真正的接受判定读 repo.PatientProfileClearColumns —— 两张表一旦漂移，文案就会写成
// 「按它填照样被拒」的假话。这里逐字锁死，增删一列必须同时改两处。
func TestT450B_ClearFieldOrderLocksRepoWhitelist(t *testing.T) {
	keys := make([]string, 0, len(repo.PatientProfileClearColumns))
	for k := range repo.PatientProfileClearColumns {
		keys = append(keys, k)
	}
	got := append([]string(nil), clearFieldOrder...)
	sort.Strings(got)
	sort.Strings(keys)
	assert.Equal(t, keys, got, "clearFieldOrder 必须与 repo.PatientProfileClearColumns 键集合同集")
	assert.Len(t, repo.PatientProfileClearColumns, 4, "可空列只有 4 个（patients 里仅 name 是 NOT NULL）")

	// 列名侧同样锁定：请求体字段名 → patients 列名的映射写错，置空会落到别的列上
	assert.Equal(t, map[string]string{
		"gender":    "gender",
		"age":       "age",
		"diagnosis": "diagnosis",
		"cobbAngle": "cobb_angle",
	}, repo.PatientProfileClearColumns)
}

func TestT450B_BuildAdminPatientEdit_ClearFieldsTable(t *testing.T) {
	gender := "male"
	age := 14
	diag := "胸椎右侧凸"
	cobb := 28.0

	t.Run("四列各自单独置空⇒只给列名，值指针全 nil", func(t *testing.T) {
		for _, tc := range []struct{ field, col string }{
			{"gender", "gender"},
			{"age", "age"},
			{"diagnosis", "diagnosis"},
			{"cobbAngle", "cobb_angle"},
		} {
			in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{
				ClearFields: []string{tc.field},
			})
			require.Nil(t, appErr, "%s 单独置空应是有效编辑", tc.field)
			assert.Equal(t, []string{tc.col}, in.ClearColumns)
			assert.Nil(t, in.Name)
			assert.Nil(t, in.Gender)
			assert.Nil(t, in.Age)
			assert.Nil(t, in.Diagnosis)
			assert.Nil(t, in.CobbAngle)
		}
	})

	t.Run("多个字段一次置空⇒按请求序给列名", func(t *testing.T) {
		in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{
			ClearFields: []string{"cobbAngle", "diagnosis"},
		})
		require.Nil(t, appErr)
		assert.Equal(t, []string{"cobb_angle", "diagnosis"}, in.ClearColumns)
	})

	t.Run("给值与置空可以同请求，只要不撞同一列", func(t *testing.T) {
		newName := "患者小明改"
		in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{
			Name:        &newName,
			Age:         &age,
			ClearFields: []string{"gender", "cobbAngle"},
		})
		require.Nil(t, appErr)
		require.NotNil(t, in.Name)
		assert.Equal(t, "患者小明改", *in.Name)
		require.NotNil(t, in.Age)
		assert.Equal(t, 14, *in.Age)
		assert.Equal(t, []string{"gender", "cobb_angle"}, in.ClearColumns)
	})

	// 拒绝腿：表外字段（含 name —— patients.name NOT NULL，压根不在可空列集合内）、
	// 重复项、撞列。三道判定各自独立，不靠同一条兜。
	t.Run("表外字段名⇒400 且文案给全可接受集合", func(t *testing.T) {
		for _, key := range []string{"name", "phone", "heightCm", "status", "deviceId", "", "  ", "cobb_angle"} {
			in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{ClearFields: []string{key}})
			require.NotNil(t, appErr, "%q 不在白名单必须 400", key)
			assert.Nil(t, in)
			assert.Contains(t, appErr.Message, "gender、age、diagnosis、cobbAngle", "%q 的文案要列全可接受集合", key)
		}
	})

	t.Run("重复项⇒400", func(t *testing.T) {
		in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{
			ClearFields: []string{"age", "age"},
		})
		require.NotNil(t, appErr)
		assert.Nil(t, in)
		assert.Contains(t, appErr.Message, "duplicated field: age")
	})

	t.Run("同列既给值又被列为清空⇒400 不留隐式优先级", func(t *testing.T) {
		cases := []struct {
			name  string
			req   adminPatientEditRequest
			field string
		}{
			{"gender", adminPatientEditRequest{Gender: &gender, ClearFields: []string{"gender"}}, "gender"},
			{"age", adminPatientEditRequest{Age: &age, ClearFields: []string{"age"}}, "age"},
			{"diagnosis", adminPatientEditRequest{Diagnosis: &diag, ClearFields: []string{"diagnosis"}}, "diagnosis"},
			{"cobbAngle", adminPatientEditRequest{CobbAngle: &cobb, ClearFields: []string{"cobbAngle"}}, "cobbAngle"},
		}
		for _, tc := range cases {
			in, appErr := buildAdminPatientEdit(&tc.req)
			require.NotNil(t, appErr, "%s 撞列必须 400", tc.name)
			assert.Nil(t, in)
			assert.Contains(t, appErr.Message, "both assigned and listed in clearFields")
		}
	})

	t.Run("空数组与空串项都不算有效编辑⇒400", func(t *testing.T) {
		in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{ClearFields: []string{}})
		require.NotNil(t, appErr, "clearFields:[] 无可写字段，仍走「空编辑」判定")
		assert.Nil(t, in)
		assert.Contains(t, appErr.Message, "no updatable fields")
	})

	t.Run("值域校验排在撞列之前⇒坏值不会先被洗成 NULL", func(t *testing.T) {
		bad := 200
		in, appErr := buildAdminPatientEdit(&adminPatientEditRequest{
			Age:         &bad, // 越界
			ClearFields: []string{"age"},
		})
		require.NotNil(t, appErr)
		assert.Nil(t, in)
		assert.Contains(t, appErr.Message, "age must be between 0 and 150",
			"先拒越界值，不能让它落到置空支")
	})
}

// ── HTTP 腿：admin PUT ──

// TestT450B_AdminPut_ClearFieldsReachesRepoAndAuditsNull 端到端一条放行腿：
// 请求体 → handler → repo 入参（列名）→ 审计快照记 null（不是空串）。
func TestT450B_AdminPut_ClearFieldsReachesRepoAndAuditsNull(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient() // gender=male / age=14 / diagnosis=胸椎右侧凸 / cobbAngle=28.0
	e.store.patient = &p
	e.store.profileApplies = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"clearFields": []string{"diagnosis", "cobbAngle"}}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)

	// 到 repo 的入参只有列名（字段名→列名映射在 handler 收口，pg.go 不再解析请求体字符串）
	assert.Equal(t, []string{"diagnosis", "cobb_angle"}, t226LastUpdate.ClearColumns)
	assert.Nil(t, t226LastUpdate.Diagnosis, "置空腿不得同时带值指针")
	assert.Nil(t, t226LastUpdate.CobbAngle)

	require.Len(t, e.store.auditRows, 1)
	row := e.store.auditRows[0]
	assert.Equal(t, []string{"diagnosis", "cobbAngle"}, row.Detail["changed"])
	before := t450MapDetail(t, row, "before")
	after := t450MapDetail(t, row, "after")
	assert.Equal(t, "胸椎右侧凸", before["diagnosis"])
	assert.Nil(t, after["diagnosis"], "置空要记 null，不能写成空串")
	assert.Equal(t, 28.0, before["cobbAngle"])
	assert.Nil(t, after["cobbAngle"])
	// 序列化面同锁：审计页/导出取的是 JSON，NULL 必须是 null 而不是空串
	blob := t450RowBlob(t, row)
	assert.Contains(t, blob, `"diagnosis":null`, "改后快照序列化成 JSON 后仍要是 null")
	assert.NotContains(t, blob, `"diagnosis":""`, "不得用空串冒充 NULL")

	// 响应体回读：DTO 两列都应是 null（读侧指针序列化后为 JSON null）
	assert.Nil(t, e.store.patient.Diagnosis)
	assert.Nil(t, e.store.patient.CobbAngle)
}

// TestT450B_AdminPut_AgeZeroIsAValueNotAClear 乙案立项理由之一：age=0 是合法业务值，
// 不能拿零值当清空的暗号。这一腿锁「给 0」与「置空」是两条不同的通道。
func TestT450B_AdminPut_AgeZeroIsAValueNotAClear(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p
	e.store.profileApplies = true

	w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"age": 0}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	require.NotNil(t, t226LastUpdate.Age)
	assert.Equal(t, 0, *t226LastUpdate.Age)
	assert.Empty(t, t226LastUpdate.ClearColumns, "给 0 不是清空")
	require.NotNil(t, e.store.patient.Age)
	assert.Equal(t, 0, *e.store.patient.Age, "库里应落 0 而非 NULL")

	t226ResetStoreSpies()
	w2, resp2 := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001",
		map[string]any{"clearFields": []string{"age"}}, t248AdminHdr())
	require.Equal(t, http.StatusOK, w2.Code, resp2.Message)
	assert.Equal(t, []string{"age"}, t226LastUpdate.ClearColumns)
	assert.Nil(t, t226LastUpdate.Age)
	assert.Nil(t, e.store.patient.Age)
}

func TestT450B_AdminPut_RejectLegsTouchNothing(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"name 不可置空", map[string]any{"clearFields": []string{"name"}}},
		{"表外字段", map[string]any{"clearFields": []string{"teamId"}}},
		{"重复项", map[string]any{"clearFields": []string{"age", "age"}}},
		{"撞列", map[string]any{"age": 15, "clearFields": []string{"age"}}},
		{"空数组", map[string]any{"clearFields": []string{}}},
		{"非数组", map[string]any{"clearFields": "age"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t226ResetStoreSpies()
			e := newEnv(t, true, true)
			p := samplePatient()
			e.store.patient = &p
			e.store.profileApplies = true

			w, resp := e.do(http.MethodPut, "/api/v1/admin/patients/P20260001", tc.payload, t248AdminHdr())
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s 必须 400（响应体 %s）", tc.name, resp.Message)
			assert.Empty(t, t226LastUpdatePatient, "%s 被拒时不得触达数据层", tc.name)
			assert.Empty(t, e.store.auditRows, "%s 被拒时不得留痕", tc.name)
			assert.NotNil(t, e.store.patient.Diagnosis, "被拒的写不得改动库内现值")
		})
	}
}

// TestT450B_SelfChannel_ClearFieldsIsNotAWhitelistKey T226 患者自助通道零波及：
// 请求体白名单没有 clearFields ⇒ DisallowUnknownFields 先拒，置空形态只此一条 admin 通道。
func TestT450B_SelfChannel_ClearFieldsIsNotAWhitelistKey(t *testing.T) {
	t226ResetStoreSpies()
	e := newEnv(t, true, true)
	p := samplePatient()
	e.store.patient = &p

	for _, payload := range []map[string]any{
		{"clearFields": []string{"diagnosis"}},
		{"name": "患者小明改", "clearFields": []string{"age"}},
	} {
		w, resp := e.do(http.MethodPut, t226ProfilePath, payload, selfHdr("P20260001", "patient"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "自助通道不认 clearFields（响应体 %s）", resp.Message)
		assert.Empty(t, t226LastUpdatePatient, "拒绝腿不得触达数据层")
		assert.Empty(t, t226LastUpdate.ClearColumns)
	}

	// 反证：同一条自助请求去掉 clearFields 照常 200 ⇒ 上一轮的 400 确实是这个键引起的
	t226ResetStoreSpies()
	w, resp := e.do(http.MethodPut, t226ProfilePath, map[string]any{"name": "患者小明改"}, selfHdr("P20260001", "patient"))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, t226LastUpdate.ClearColumns)
}
