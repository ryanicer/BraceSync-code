// Package handler — T646 裁定甲：患者侧载荷按值收口医护姓名（键在场、值恒 null）
//
// 在册裁定（weide-duty 2026-10-09 22:52 卡内评论 4865，依 Boss 22:3x「做」的拍板）：
//   - 范围 = teamName 与 doctorName 两枚都收（以设计稿 T637 八末句断言禁止集合为准）；
//   - 路径 = 甲（按值收口，键在、值恒 null）；乙（展平独立 DTO、键不在场）不进 10-22 关键链；
//   - PUT /api/v1/patients/{patientId} 自助改档的写响应同口径一并收；
//   - 后台四处可见列不受影响（admin 侧维持可读）。
//
// 本文件因此钉两件事，缺一不算收口：
//  1. 值面：患者侧两条载荷的这两枚键值恒为 JSON null，且正对照证明「库里确有姓名」
//     （否则 null 只是假库没数据的回声，断言就是空转）；
//  2. 键面：裁定甲的形状是「键在场」，所以键缺席同样算漂移 —— 与后台两张面逐键同集，
//     防止有人日后「顺手」删键却不同步契约对拍登记表。
package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t646TeamNameKey   = "teamName"
	t646DoctorNameKey = "doctorName"
)

// t646WireKeys 取一枚 data 的键名集合（排序后返回，便于整集判等）
func t646WireKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m), "data 必须是扁平对象")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// t646StaffNamesClosed 断言一枚患者侧载荷：两枚键在场且值为 JSON null，三枚标识照常非 null。
// face 只用于断言消息定位（哪一张面），不参与判据。
func t646StaffNamesClosed(t *testing.T, data []byte, face string, wantTeamID, wantDoctorID string) {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &m), face+"：data 必须是扁平对象")

	for _, key := range []string{t646TeamNameKey, t646DoctorNameKey} {
		raw, ok := m[key]
		assert.True(t, ok, "%s：%s 必须键在场（裁定甲的形状是「键在值 null」，键缺席＝漂移）", face, key)
		assert.Equal(t, "null", string(raw), "%s：%s 必须收口成 JSON null", face, key)
	}

	// 收的是姓名，不是关联能力：三枚标识照常下发
	var dto model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(data, &dto))
	require.NotNil(t, dto.TeamID, face)
	assert.Equal(t, wantTeamID, *dto.TeamID, face+"：teamId 照常")
	require.NotNil(t, dto.DoctorID, face)
	assert.Equal(t, wantDoctorID, *dto.DoctorID, face+"：doctorId 照常")
	require.NotNil(t, dto.DeviceID, face)
	assert.Equal(t, "PRS-001", *dto.DeviceID, face+"：deviceId 照常")
}

// TestT646_SelfProfileRead_ClosesStaffNames GET /api/v1/patient/profile 的读响应收口。
// 正对照打在「假库里这两列确有值」这一面：seed 行自带团队名与医生名，
// 所以响应里的 null 只可能来自装配腿的收口，不是数据缺失。
func TestT646_SelfProfileRead_ClosesStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	require.NotNil(t, row.TeamName, "正对照：seed 行确有团队名（null 否则是空库回声）")
	require.NotNil(t, row.DoctorName, "正对照：seed 行确有医生名")
	assert.Equal(t, "脊柱矫形一组", *row.TeamName)
	assert.Equal(t, "李医师", *row.DoctorName)

	w, resp := e.do(http.MethodGet, profilePath, nil, selfHdr(row.PatientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code)
	t646StaffNamesClosed(t, resp.Data, "GET /patient/profile", "TEAM01", "D0001")
}

// TestT646_SelfProfileWriteReply_ClosesStaffNames 自助改档的写响应（PUT /patients/:patientId）同口径收口。
// profileApplies 打开 ⇒ 写后回读的是新行快照，这样「写响应里出了姓名」不会被写前快照掩盖。
func TestT646_SelfProfileWriteReply_ClosesStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	e.store.profileApplies = true

	w, resp := e.do(http.MethodPut, t226ProfilePath, map[string]any{"name": "患者小明"}, selfHdr(row.PatientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code)
	t646StaffNamesClosed(t, resp.Data, "PUT /patients/:patientId 写响应", "TEAM01", "D0001")
}

// TestT646_AdminFacesStillCarryStaffNames 后台两张面不受影响（裁定四）。
// 这一格是上一格的对照臂：同一枚 seed 行、同一条投影，只有装配腿不同 ⇒
// 若有人把收口写进 toPatientDTO，这里立刻转红。
func TestT646_AdminFacesStillCarryStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	e.store.patients = []repo.PatientRow{row}
	e.store.patientTotal = 1

	dw, detail := e.do(http.MethodGet, "/api/v1/admin/patients/"+row.PatientID, nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, dw.Code)
	require.Contains(t, string(detail.Data), "P20260001", "正对照：后台详情面确实出了这个患者")
	var dd model.AdminPatientDTO
	require.NoError(t, json.Unmarshal(detail.Data, &dd))
	require.NotNil(t, dd.TeamName, "后台详情必须仍回团队名（裁定四：admin 侧维持可读）")
	assert.Equal(t, "脊柱矫形一组", *dd.TeamName)
	require.NotNil(t, dd.DoctorName, "后台详情必须仍回医生名")
	assert.Equal(t, "李医师", *dd.DoctorName)

	lw, list := e.do(http.MethodGet, "/api/v1/admin/patients", nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, lw.Code)
	require.Contains(t, string(list.Data), "P20260001", "正对照：后台列表面确实出了这一行")
	assert.Contains(t, string(list.Data), "脊柱矫形一组", "后台列表的姓名列是真消费点")
	assert.Contains(t, string(list.Data), "李医师", "后台列表的姓名列是真消费点")
}

// TestT646_PayloadKeySetsMatchAdmin 键面判据：患者侧读载荷的 wire 键集合 = 后台 DTO 的键集合 ∪ {dailyWearTargetHours}。
// 裁定甲只改这两枚键的**值**，不改形状 ⇒ 两枚键必须继续在患者侧 wire 面在场（缺席就是漂移），
// 也不许有人借本卡往患者侧多加键（加键同样是契约改动，得另走裁定）。
// 注：reflectJSONKeysOf 不展平匿名内嵌，所以结构面取 AdminPatientDTO 自己的键，wire 面另取 ——
// 两把尺不同名，判据写在 wire 面上。
func TestT646_PayloadKeySetsMatchAdmin(t *testing.T) {
	adminKeys := reflectJSONKeysOf(model.AdminPatientDTO{})
	sort.Strings(adminKeys)
	require.Contains(t, adminKeys, t646TeamNameKey, "结构面正对照：这枚键在 AdminPatientDTO 里本来就在场")
	require.Contains(t, adminKeys, t646DoctorNameKey, "结构面正对照：这枚键在 AdminPatientDTO 里本来就在场")

	want := append(append([]string{}, adminKeys...), wearTargetJSONKey)
	sort.Strings(want)

	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	_, resp := e.do(http.MethodGet, profilePath, nil, selfHdr(row.PatientID, "patient"))
	assert.Equal(t, want, t646WireKeys(t, resp.Data),
		"患者侧 wire 键集合必须逐键等于后台 DTO 加那一枚标量（既不缺席也不新增）")
}
