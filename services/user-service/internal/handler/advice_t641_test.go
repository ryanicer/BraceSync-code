// Package handler — T641 医护建议（advice_logs 五枚端点）
//
// 本文件守五件事，逐条对应设计稿 docs/tasks/peter/T637-附-医生建议功能设计稿.md 的裁定：
//  1. R7 甲「作者谓词在 SQL 里」：handler 不做「先查一行再比作者」，所以越权时写替身照样被调到、
//     但返回值空 ⇒ 403；反过来越权「写」在归属判定阶段必须零次库写（第 2 条）。
//  2. 归属判定排在库写之前（口径同 T373 D2）：跨团队发送 createAdviceCalls 必须为 0。
//  3. 身份不信客户端：author_doctor_id 只来自 DoctorIDByAdmin(X-User-Id)，body 里自报的作者字段一律无效。
//  4. R2 甲长度上限 500 走 rune：500 枚汉字（1500 字节）必须放行 —— 这条是「字节口径」的反证，
//     抄 handler 里 len() 那处先例的话会被砍成约 166 个汉字。
//  5. §八 隐私断言：患者端两枚新读口的出参字段名集合里不得出现姓名/账号/手机号族字段。
//     按字段名断言而非按值（值是 seed 数据、字段名才是契约），且必须配同尺正对照：
//     staff 面的感受日志 DTO 带 patientName，尺子在那一面要能响，否则「患者面 0 命中」是空转。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/user-service/internal/model"
	"github.com/bracesync/bracesync/services/user-service/internal/repo"
)

const (
	t641OwnPatient   = "PAT-OWN"   // TEAM01：医护本人团队
	t641OtherPatient = "PAT-OTHER" // TEAM02：他团队
	t641DoctorAcct   = "ADM-D001"  // X-User-Id（admins.admin_id）
	t641DoctorID     = "D0001"     // doctors.doctor_id（由 DoctorIDByAdmin 解析出，不信客户端）
	t641OtherDocID   = "D0002"     // 同团队另一位医护，用于 editable 反证
	t641PatientHdr   = "patient"   // 患者角色头（非 staff，走 self-scope）
)

func t641Env(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, false, false)
	t350DoctorInTeam(e, t350OwnTeam)
	base := samplePatient()
	mk := func(pid, team string) repo.PatientRow {
		row := base
		row.PatientID = pid
		t2 := team
		row.TeamID = &t2
		return row
	}
	e.store.patients = []repo.PatientRow{mk(t641OwnPatient, t350OwnTeam), mk(t641OtherPatient, t350OtherTeam)}
	e.store.doctorFound = true
	e.store.doctorID = t641DoctorID
	return e
}

func t641Row(id int64, author, content string) repo.AdviceRow {
	title := "李医师"
	return repo.AdviceRow{
		AdviceID:       id,
		PatientID:      t641OwnPatient,
		AuthorDoctorID: author,
		Content:        content,
		CreatedAt:      time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC),
		AuthorTitle:    &title,
	}
}

// ── 1. 发送：身份来源 + 零库写 + 长度口径 ─────────────────────────

func TestT641_Create_AuthorIdentityNotFromClient(t *testing.T) {
	e := t641Env(t)
	e.store.createdAdvice = &repo.AdviceRow{
		AdviceID: 77, PatientID: t641OwnPatient, AuthorDoctorID: t641DoctorID, Content: "继续佩戴 22 小时",
		CreatedAt:   time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC),
		AuthorTitle: strPtr("李医师"),
	}
	// body 里自报一枚作者：契约上不存在这个字段，落库必须用解析结果
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": "继续佩戴 22 小时", "authorDoctorId": "EVIL"},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, t641DoctorID, e.store.lastAdviceDoctorID, "作者只能来自 X-User-Id 解析，不接受 body 自报")
	assert.Equal(t, t641OwnPatient, e.store.lastAdvicePatientID)
	assert.Equal(t, "继续佩戴 22 小时", e.store.lastAdviceContent)

	var saved model.AdviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &saved))
	assert.Equal(t, "77", saved.AdviceID)
	assert.Equal(t, "李医师", saved.Title)
	assert.True(t, saved.Editable, "作者本人读到自己那一行应可编辑")
}

func TestT641_Create_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	e := t641Env(t)
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t641OtherPatient+"/advice",
		map[string]string{"content": "越界发送"}, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.createAdviceCalls, "跨团队必须零次 INSERT，判据不能只回一个 403")
}

func TestT641_Create_NoDoctorRowDenied(t *testing.T) {
	e := t641Env(t)
	e.store.doctorFound = false // 客服 / 运营管理员令牌：admins 有行、doctors 无行
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": "x"}, t350Hdr("ROLE_CS", "ADM-CS01"))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, 0, e.store.createAdviceCalls)
}

func TestT641_Create_PatientTokenDenied(t *testing.T) {
	e := t641Env(t)
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": "患者给自己发一条"}, t350Hdr(t641PatientHdr, t641OwnPatient))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.createAdviceCalls, "self-scope 放行的是本人档案域，不含建议写域")
}

func TestT641_Create_RuneCapNotByteCap(t *testing.T) {
	e := t641Env(t)
	e.store.createdAdvice = &repo.AdviceRow{AdviceID: 78, PatientID: t641OwnPatient, AuthorDoctorID: t641DoctorID}

	// 500 枚汉字 = 1500 字节：rune 口径放行，字节口径（len()）会砍成 400 —— 这条是口径的反证
	w, resp := e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": strings.Repeat("方", adviceContentMaxRunes)},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Equal(t, adviceContentMaxRunes, len([]rune(e.store.lastAdviceContent)))

	// 反证段：把计数清零，后面三枚「不触库」断言量的才是本段而不是上一段
	e.store.createAdviceCalls = 0
	w, resp = e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": strings.Repeat("方", adviceContentMaxRunes+1)},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusBadRequest, w.Code, resp.Message)
	assert.Equal(t, model.CodeInvalidParam, resp.Code)

	// 空正文（含纯空白）→ 400
	w, _ = e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": "   "}, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, e.store.createAdviceCalls, "校验失败不得触库")
}

func TestT641_Create_StoreError500(t *testing.T) {
	e := t641Env(t)
	e.store.createAdviceErr = errors.New("db")
	w, _ := e.do(http.MethodPost, "/api/v1/patients/"+t641OwnPatient+"/advice",
		map[string]string{"content": "x"}, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── 2. 建议流：两枚 token 同一路径 + editable + 隐私 ───────────────

func TestT641_List_ScopeAndEditable(t *testing.T) {
	e := t641Env(t)
	e.store.advices = []repo.AdviceRow{t641Row(2, t641OtherDocID, "他人那条"), t641Row(1, t641DoctorID, "我写的那条")}

	// 医护本人：自己那行 editable=true，他人那行 false
	w, resp := e.do(http.MethodGet, "/api/v1/patients/"+t641OwnPatient+"/advice", nil,
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var asDoctor []model.AdviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &asDoctor))
	require.Len(t, asDoctor, 2)
	assert.Equal(t, "1", asDoctor[1].AdviceID)
	assert.True(t, asDoctor[1].Editable)
	assert.False(t, asDoctor[0].Editable)

	// 患者本人：同一条路径可读，且 editable 恒 false（R7 甲：编辑权只在作者医护）
	w, resp = e.do(http.MethodGet, "/api/v1/patients/"+t641OwnPatient+"/advice", nil,
		t350Hdr(t641PatientHdr, t641OwnPatient))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var asPatient []model.AdviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &asPatient))
	require.Len(t, asPatient, 2)
	for _, a := range asPatient {
		assert.False(t, a.Editable, "患者端不许出现编辑位")
	}

	// 患者读他人：403，且零次建议流读（判定在触库前）
	e.store.adviceListCalls = 0
	w, _ = e.do(http.MethodGet, "/api/v1/patients/"+t641OtherPatient+"/advice", nil,
		t350Hdr(t641PatientHdr, t641OwnPatient))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.adviceListCalls)

	// 医护跨团队读：403 + 零次读
	w, _ = e.do(http.MethodGet, "/api/v1/patients/"+t641OtherPatient+"/advice", nil,
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusForbidden, w.Code)

	e.store.advicesErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/patients/"+t641OwnPatient+"/advice", nil,
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// t641Keys 把响应 data（数组或单对象）里的字段名收成串列表。
func t641Keys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		names := make([]string, 0)
		for _, o := range arr {
			for k := range o {
				names = append(names, k)
			}
		}
		return names
	}
	var one map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &one))
	names := make([]string, 0, len(one))
	for k := range one {
		names = append(names, k)
	}
	return names
}

// t641Screen 隐私筛：精确名 + phone 前缀族（出参里出现任何一枚即泄漏）。
// 🔴 键一律小写、比较前也归一：驼峰名拿去比驼峰键会「筛子空转」（首跑的正对照就是这么红的）。
func t641Screen(names []string) []string {
	banned := map[string]bool{
		"name": true, "doctorname": true, "teamname": true, "patientname": true,
		"username": true, "realname": true, "nickname": true, "phonehash": true,
	}
	hits := make([]string, 0)
	for _, n := range names {
		lower := strings.ToLower(n)
		if banned[lower] || strings.HasPrefix(lower, "phone") {
			hits = append(hits, n)
		}
	}
	return hits
}

func TestT641_Privacy_FieldNamesAndPositiveControl(t *testing.T) {
	e := t641Env(t)
	e.store.advices = []repo.AdviceRow{t641Row(1, t641DoctorID, "每天佩戴 22 小时")}
	e.store.careTeamRows = []repo.CareTeamRow{{MemberType: "doctor", Title: strPtr("李医师")}, {MemberType: "technician"}}

	// 正对照：同一把尺扫 staff 面的感受日志 DTO —— 那一面带 patientName，尺子必须响。
	// 没有这一格，下面两枚「0 命中」可能只是筛子根本没电。
	score := 4.0
	e.store.feelings = []repo.FeelingLogRow{{LogID: 5, PatientID: t641OwnPatient,
		LogDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ComfortScore: &score, PatientName: "患者小明"}}
	w, resp := e.do(http.MethodGet, "/api/v1/patients/"+t641OwnPatient+"/feeling-logs", nil,
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.NotEmpty(t, t641Screen(t641Keys(t, resp.Data)), "正对照失效：这把隐私筛在带姓名的面上都不响，下面的断言不作数")

	// 患者端建议流
	w, resp = e.do(http.MethodGet, "/api/v1/patients/"+t641OwnPatient+"/advice", nil,
		t350Hdr(t641PatientHdr, t641OwnPatient))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	assert.Empty(t, t641Screen(t641Keys(t, resp.Data)))

	// 患者端团队行
	w, resp = e.do(http.MethodGet, "/api/v1/patient/care-team", nil,
		t350Hdr(t641PatientHdr, t641OwnPatient))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	keys := t641Keys(t, resp.Data)
	require.NotEmpty(t, keys, "团队面若整页为空，字段名断言也是空转")
	assert.Empty(t, t641Screen(keys))
	for _, k := range keys {
		assert.Contains(t, []string{"memberType", "title"}, k, "患者端团队行只允许这两列")
	}
}

// ── 3. 编辑 / 删除：作者谓词在 SQL 里，两格合一 403 ────────────────

func TestT641_Update_AuthorPredicate(t *testing.T) {
	e := t641Env(t)
	path := "/api/v1/advice/1"

	// 库回 nil（「行不存在」与「非本人所写」合一）⇒ 403，且必须真的把 doctorID 作为谓词传下去
	e.store.updatedAdvice = nil
	w, resp := e.do(http.MethodPut, path, map[string]string{"content": "改成另一句"},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 1, e.store.updateAdviceCalls, "作者谓词在 SQL 的 WHERE 里，handler 不许先查一行再比")
	assert.Equal(t, t641DoctorID, e.store.lastAdviceDoctorID)
	assert.Equal(t, int64(1), e.store.lastAdviceID)

	// 命中作者 ⇒ 200 + 回读整行
	updated := t641Row(1, t641DoctorID, "改成另一句")
	updated.UpdatedAt = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	e.store.updatedAdvice = &updated
	w, resp = e.do(http.MethodPut, path, map[string]string{"content": "改成另一句"},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var dto model.AdviceDTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.Equal(t, "改成另一句", dto.Content)
	assert.True(t, dto.Editable)

	// 非法 id / 超长正文：不得触库
	e.store.updateAdviceCalls = 0
	w, _ = e.do(http.MethodPut, "/api/v1/advice/abc", map[string]string{"content": "x"},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.do(http.MethodPut, path, map[string]string{"content": strings.Repeat("方", adviceContentMaxRunes+1)},
		t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, e.store.updateAdviceCalls)

	// 非医护身份令牌：403 且零次 UPDATE
	e.store.doctorFound = false
	e.store.updateAdviceCalls = 0
	w, _ = e.do(http.MethodPut, path, map[string]string{"content": "x"}, t350Hdr("ROLE_CS", "ADM-CS01"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.updateAdviceCalls)
}

func TestT641_Delete_HardDeleteAuthorOnly(t *testing.T) {
	e := t641Env(t)
	path := "/api/v1/advice/9"

	e.store.deleteAdviceOK = false
	w, resp := e.do(http.MethodDelete, path, nil, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusForbidden, w.Code, resp.Message)
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.Equal(t, 1, e.store.deleteAdviceCalls)
	assert.Equal(t, t641DoctorID, e.store.lastAdviceDoctorID)

	e.store.deleteAdviceOK = true
	w, _ = e.do(http.MethodDelete, path, nil, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusOK, w.Code)

	e.store.deleteAdviceErr = errors.New("db")
	w, _ = e.do(http.MethodDelete, path, nil, t350Hdr(t350DoctorHDR, t641DoctorAcct))
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// 患者令牌：连身份解析都不该走到（assertAdminOrSelf 之外的写域收口在网关，服务层再兜一层）
	e.store.deleteAdviceCalls = 0
	w, _ = e.do(http.MethodDelete, path, nil, t350Hdr(t641PatientHdr, t641OwnPatient))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.deleteAdviceCalls)
}

// ── 4. 团队行：self-scope + 回落链 + 技师无职称列 ──────────────────

func TestT641_CareTeam_SelfScopeAndFallback(t *testing.T) {
	e := t641Env(t)
	dept := "康复科"

	// 身份头缺失：fail-closed 403，且零次团队读
	w, _ := e.do(http.MethodGet, "/api/v1/patient/care-team", nil, map[string]string{"X-Role": t641PatientHdr})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, e.store.careTeamCalls)

	e.store.careTeamRows = []repo.CareTeamRow{
		{MemberType: "doctor", Title: strPtr("李医师"), Department: &dept},
		{MemberType: "doctor", Title: strPtr("  "), Department: &dept}, // 空职称 → 回落科室
		{MemberType: "doctor"},     // 两列都空 → 回落固定词
		{MemberType: "technician"}, // technicians 无 title 列
	}
	w, resp := e.do(http.MethodGet, "/api/v1/patient/care-team", nil,
		t350Hdr(t641PatientHdr, t641OwnPatient))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var team []model.CareTeamMemberDTO
	require.NoError(t, json.Unmarshal(resp.Data, &team))
	require.Len(t, team, 4)
	assert.Equal(t, "李医师", team[0].Title)
	assert.Equal(t, dept, team[1].Title, "空职称必须回落 department，不留白更不回填姓名")
	assert.Equal(t, adviceTeamLabel, team[2].Title)
	assert.Equal(t, adviceTechLabel, team[3].Title)
	assert.Equal(t, t641OwnPatient, e.store.lastAdvicePatientID, "患者 ID 只能取 X-User-Id")

	e.store.careTeamErr = errors.New("db")
	w, _ = e.do(http.MethodGet, "/api/v1/patient/care-team", nil, t350Hdr(t641PatientHdr, t641OwnPatient))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
