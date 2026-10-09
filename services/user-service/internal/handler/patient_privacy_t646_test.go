// Package handler — T646 患者端载荷去医护姓名（依 T637 设计稿 §八）
//
// 设计稿那条断言的原文口径是「按字段名集合断言，比按值断言稳」：值是 seed 数据可以恰好为空，
// 字段名是契约。所以本文件量的都是**载荷面的键集合**，不是某个值。
//
// 覆盖四格：
//  1. GET /api/v1/patient/profile 的键集合 == 患者侧名册 + dailyWearTargetHours，且不含 teamName / doctorName；
//  2. PUT /api/v1/patients/:patientId（T226 自助写）的写响应回填 == 患者侧名册本身
//     —— 这一枚是最容易漏的：它原先直接回 AdminPatientDTO，「改自己的资料」顺手把医护姓名发回患者端；
//  3. 在场正对照（同一行夹具、同一把尺）：后台详情与列表两面照旧带着这两枚姓名
//     —— 没有这一格，前两格的「不含」可能只是尺子扫了空面或夹具根本没货；
//  4. 结构面对账：AdminPatientDTO 的键集合 == PatientSelfDTO 的键集合 + 那两枚姓名键，一枚不多一枚不少，
//     这样「往后台 DTO 上加字段」会在患者侧用例上显形，而不是静默地少发或多发。
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

// t646StaffNameKeys 本卡要收口的两枚键（设计稿 §八 点名的医护姓名）。
var t646StaffNameKeys = []string{"doctorName", "teamName"}

// t646SelfKeys 患者侧载荷的名册（model.PatientSelfDTO 的 json tag，逐枚现抄；顺序按结构体声明）。
// 写死成字面量而不是从结构体反射，是为了让「键集合变了」这件事在测试文件里就先显形一次：
// 反射只会跟着结构体一起改，而这张名册改动时必须有人手工对一次设计稿。
var t646SelfKeys = []string{
	"patientId",
	"name",
	"gender",
	"age",
	"diagnosis",
	"cobbAngle",
	"deviceId",
	"teamId",
	"doctorId",
	"phone",
	"status",
	"createdAt",
	"updatedAt",
	"heightCm",
	"weightKg",
	"emergencyContactName",
	"emergencyContactPhone",
	"emergencyContactRelation",
}

func t646Sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// t646WireKeys 取响应体 data 的 JSON 键集合（排序后返回，便于整集合判等）。
func t646WireKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m), "data 必须是扁平 JSON 对象")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// t646AssertNoStaffNames 先钉「不在场」，再把整枚键集合打进日志（报 0 要同屏打被扫的面）。
func t646AssertNoStaffNames(t *testing.T, face string, keys []string) {
	t.Helper()
	for _, k := range t646StaffNameKeys {
		assert.NotContains(t, keys, k, "%s 的载荷面不得出现 %s", face, k)
	}
	t.Logf("[T646][%s] 键集合共 %d 枚：%v", face, len(keys), keys)
}

// selfProfileRaw 走一次本人档案读，回 data 的原始字节（键集合尺吃的是这一枚面）。
func selfProfileRaw(t *testing.T, e *testEnv, patientID string) json.RawMessage {
	t.Helper()
	w, resp := e.do(http.MethodGet, profilePath, nil, selfHdr(patientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code)
	return resp.Data
}

func TestT646_SelfProfileWireHasNoStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	require.NotNil(t, row.TeamName, "夹具必须带着团队名（否则「载荷面没有」是空面假绿）")
	require.NotNil(t, row.DoctorName, "夹具必须带着医生名（同上）")

	// 读路 = 患者侧名册 + T576 那枚佩戴目标（本卡只剥两枚姓名键，不碰它）
	want := t646Sorted(append(append([]string{}, t646SelfKeys...), wearTargetJSONKey))
	keys := t646WireKeys(t, selfProfileRaw(t, e, row.PatientID))
	t646AssertNoStaffNames(t, "GET /patient/profile", keys)
	assert.Equal(t, want, keys, "患者侧档案的键集合 = 名册 + dailyWearTargetHours，多一枚少一枚都要显式对账")
}

func TestT646_SelfProfileWriteReplyHasNoStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	// 假库默认不回写行对象（既有 T226 用例只看入参）；开启后「写 → 回读」才真是刚写的那一行，
	// 否则下面那枚正对照断言吃到的是改前快照。
	e.store.profileApplies = true

	w, resp := e.do(http.MethodPut, t226ProfilePath, map[string]any{"name": "改名测试"}, selfHdr(row.PatientID, "patient"))
	require.Equal(t, http.StatusOK, w.Code, "自助写应当成功（状态面不是本卡引入的）")
	require.Contains(t, string(resp.Data), "改名测试", "正对照：写响应确实回读了刚写的那一行")

	keys := t646WireKeys(t, resp.Data)
	t646AssertNoStaffNames(t, "PUT /patients/:patientId 写响应", keys)
	assert.Equal(t, t646Sorted(t646SelfKeys), keys, "写响应就是患者侧名册，不许与读路分叉")
	assert.NotContains(t, string(resp.Data), wearTargetJSONKey,
		"写响应不携带系统配置值（回的是 PatientSelfDTO 而非 PatientProfileDTO）")
}

// TestT646_AdminFacesStillCarryStaffNames 在场正对照：后台两面照旧带姓名。
// 作用是把前两格的结论钉在「载荷装配收口」上：同一把尺、同一行夹具，后台出得了、患者侧出不了。
func TestT646_AdminFacesStillCarryStaffNames(t *testing.T) {
	e := newEnv(t, true, true)
	row := seedProfilePatient(e)
	e.store.patients = []repo.PatientRow{row}
	e.store.patientTotal = 1

	dw, detail := e.do(http.MethodGet, "/api/v1/admin/patients/"+row.PatientID, nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, dw.Code)
	dKeys := t646WireKeys(t, detail.Data)
	for _, k := range t646StaffNameKeys {
		assert.Contains(t, dKeys, k, "后台详情面必须照旧带 %s（消费方 = admin-web 患者页与异常报告页）", k)
	}

	lw, list := e.do(http.MethodGet, "/api/v1/admin/patients", nil, t248AdminHdr())
	require.Equal(t, http.StatusOK, lw.Code)
	var page struct {
		List []map[string]json.RawMessage `json:"list"`
	}
	require.NoError(t, json.Unmarshal(list.Data, &page))
	require.Len(t, page.List, 1, "正对照：列表面确实出了这一行")
	for _, k := range t646StaffNameKeys {
		_, ok := page.List[0][k]
		assert.True(t, ok, "后台列表面必须照旧带 %s", k)
	}
}

// TestT646_SelfDTOKeySetIsAdminMinusStaffNames 结构面对账（判据 4）。
func TestT646_SelfDTOKeySetIsAdminMinusStaffNames(t *testing.T) {
	adminKeys := t646Sorted(reflectJSONKeysOf(model.AdminPatientDTO{}))
	selfKeys := t646Sorted(reflectJSONKeysOf(model.PatientSelfDTO{}))

	assert.Equal(t, t646Sorted(t646SelfKeys), selfKeys, "结构体的 json tag 与载荷名册分叉了（改 DTO 要同步改名册）")

	want := make([]string, 0, len(adminKeys))
	for _, k := range adminKeys {
		if k == "doctorName" || k == "teamName" {
			continue
		}
		want = append(want, k)
	}
	assert.Equal(t, want, selfKeys,
		"患者侧载荷必须恰好等于后台载荷减去两枚姓名键（多出来的每一枚都得按隐私面重审）")
}
