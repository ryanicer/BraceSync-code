// Package handler T378 单测：文件域读侧/写侧归属判定（越权必拒 + 零写 + 本团队反证）。
//
// 反证面刻意覆盖三条真实业务通道，防止「收窄把正常功能关掉」：
//   - /review-records 上传患者复查报告（owner_type=patient）
//   - /alerts 流程页上传告警附件（owner_type=alert，经 alerts→patients 二级反查）
//   - /review-templates 上传模板文件（owner_type=ReviewTemplate，不是患者数据）
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

const (
	t378Doc       = "ADM-T378-DOC"  // 有团队医护
	t378DocNoTeam = "ADM-T378-ORPH" // 无团队医护：必须落恒假收窄
	t378TeamOwn   = "TEAM-T378-A"
	t378TeamOther = "TEAM-T378-B"
	t378PatOwn    = "P-T378-OWN"
	t378PatOther  = "P-T378-OTHER"
)

// t378Store 最小现场：医护在 A 团队；两患者/两告警/四文件按团队与 owner_type 交叉摆放。
func t378Store() *memStore {
	s := newMemStore()
	s.doctorTeam[t378Doc] = t378TeamOwn
	s.patientTeam[t378PatOwn] = t378TeamOwn
	s.patientTeam[t378PatOther] = t378TeamOther
	s.alertTeam["AL-OWN"] = t378TeamOwn
	s.alertTeam["AL-OTHER"] = t378TeamOther

	seedFile(s, "F-OWN", "patient", t378PatOwn, model.FileStatusUploaded)
	seedFile(s, "F-OTHER", "patient", t378PatOther, model.FileStatusUploaded)
	seedFile(s, "F-ALERT-OTHER", "alert", "AL-OTHER", model.FileStatusUploaded)
	seedFile(s, "F-TPL", "ReviewTemplate", "ADM-ROOT", model.FileStatusUploaded)
	return s
}

func t378Srv(t *testing.T, s *memStore) *httptest.Server {
	t.Helper()
	return setupTestServer(s)
}

// queryTotal 取 /files/query 响应里的 total
func queryTotal(t *testing.T, body map[string]interface{}) float64 {
	t.Helper()
	data, ok := body["data"].(map[string]interface{})
	require.True(t, ok, "响应缺 data：%v", body)
	return data["total"].(float64)
}

func TestT378_Query_DoctorScopedToOwnTeam(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query", t378Doc, "ROLE_DOCTOR")
	require.Equal(t, http.StatusOK, code, body["message"])
	// 反证：本团队患者文件 + 非患者模板可见；他团队患者/告警文件不可见
	assert.Equal(t, float64(2), queryTotal(t, body))
	assert.True(t, s.teamQuery.TeamScoped, "列表必须带上推导出的团队范围")
	assert.Equal(t, t378TeamOwn, s.teamQuery.TeamID)
	assert.Equal(t, "", s.teamQuery.OwnerID, "团队收窄不靠前端自报 owner 参数")
}

func TestT378_Query_DoctorWithoutTeamFailsClosed(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query", t378DocNoTeam, "ROLE_DOCTOR")
	require.Equal(t, http.StatusOK, code, body["message"])
	assert.True(t, s.teamQuery.TeamScoped, "无团队医护也必须带收窄标记，不能退化成不过滤")
	assert.Equal(t, "", s.teamQuery.TeamID)
	assert.Equal(t, float64(1), queryTotal(t, body), "只剩非患者材料（模板）")
}

func TestT378_Query_OpsRolesUnscoped(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	for _, role := range []string{"ROLE_ADMIN", "ROLE_CS", "admin", "technician"} {
		code, body := doGET(t, srv.URL+"/api/v1/files/query", "OPS-1", role)
		require.Equal(t, http.StatusOK, code, role+": "+body["message"].(string))
		assert.False(t, s.teamQuery.TeamScoped, role+" 不应被团队收窄")
		assert.Equal(t, float64(4), queryTotal(t, body), role+" 应看到全量")
	}
	assert.Zero(t, s.scopeCalls, "不受限角色不得触发归属探测")
}

func TestT378_FileDetail_CrossTeamDenied(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/F-OTHER", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])
	// 1 次身份→团队推导 + 1 次文件归属探测，之后再无取文件动作
	assert.Equal(t, 2, s.scopeCalls)
}

func TestT378_FileDetail_SameTeamAndTemplateSucceed(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/F-OWN", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusOK, code, body["message"].(string))

	// 模板不是患者数据：无团队医护也要能预览（/review-templates 页回归）
	code, body = doGET(t, srv.URL+"/api/v1/files/F-TPL", t378DocNoTeam, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusOK, code, body["message"].(string))
}

func TestT378_FileDetail_AlertOwnedCrossTeamDenied(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, _ := doGET(t, srv.URL+"/api/v1/files/F-ALERT-OTHER", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusForbidden, code, "告警附件须经 alerts→patients 反查团队")
}

func TestT378_FileDetail_CrossTeamAndMissingAreIndistinguishable(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	codeA, bodyA := doGET(t, srv.URL+"/api/v1/files/F-OTHER", t378Doc, "ROLE_DOCTOR")
	codeB, bodyB := doGET(t, srv.URL+"/api/v1/files/F-NOPE", t378Doc, "ROLE_DOCTOR")
	require.Equal(t, http.StatusForbidden, codeA, bodyA["message"])
	require.Equal(t, http.StatusForbidden, codeB, bodyB["message"])
	assert.Equal(t, bodyA["code"], bodyB["code"])
	assert.Equal(t,
		t378Shape(bodyA["message"].(string), "F-OTHER"),
		t378Shape(bodyB["message"].(string), "F-NOPE"),
		"跨团队与查无此文件必须同形，否则 fileID 存在性可辨")
}

// TestT378_FileDetail_OpsRoleKeepsLegacy404 不受限角色响应逐字不变：查无仍 404。
func TestT378_FileDetail_OpsRoleKeepsLegacy404(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/F-NOPE", "OPS-1", "ROLE_ADMIN")
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, float64(ErrorCodeFileNotFound), body["code"])
	assert.Zero(t, s.scopeCalls)
}

func TestT378_Download_CrossTeamDenied(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/F-OTHER/download", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusForbidden, code, body["message"].(string))
	assert.Equal(t, float64(ErrorCodeForbidden), body["code"])

	code, body = doGET(t, srv.URL+"/api/v1/files/F-OWN/download", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusOK, code, "本团队下载反证："+body["message"].(string))
}

// TestT378_UploadComplete_CrossTeamDeniedWithZeroWrites 越权确认上传：拒绝且库内零变更。
func TestT378_UploadComplete_CrossTeamDeniedWithZeroWrites(t *testing.T) {
	s := t378Store()
	seedFile(s, "F-OTHER-PENDING", "patient", t378PatOther, model.FileStatusPending)
	srv := t378Srv(t, s)
	defer srv.Close()

	before := len(s.files)
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/upload-complete", t378Doc, "ROLE_DOCTOR",
		`{"file_id":"F-OTHER-PENDING","size":2048,"public_url":"https://evil/x.pdf"}`)
	require.Equal(t, http.StatusForbidden, code, body["message"].(string))
	assert.Zero(t, s.writes, "拒绝路径不得触达 MarkUploaded")
	assert.Equal(t, before, len(s.files))
	fm, err := s.GetFileByFileID(context.Background(), "F-OTHER-PENDING")
	require.NoError(t, err)
	assert.Equal(t, model.FileStatusPending, fm.Status, "状态机语义不被越权请求改写")
	assert.Empty(t, fm.URL)
}

// TestT378_Presign_CrossTeamOwnerDeniedWithZeroWrites 越权签发：pending 行都不许落库。
func TestT378_Presign_CrossTeamOwnerDeniedWithZeroWrites(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	before := len(s.files)
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		`{"file_type":"comm_photo","owner_type":"patient","owner_id":"`+t378PatOther+`","content_type":"image/png"}`)
	require.Equal(t, http.StatusForbidden, code, body["message"].(string))
	assert.Zero(t, s.writes, "签发即登记 pending 行，拒绝必须排在签发之前")
	assert.Equal(t, before, len(s.files))

	// 告警附件同理：owner 指向他团队告警 → 拒
	code, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		`{"file_type":"comm_photo","owner_type":"alert","owner_id":"AL-OTHER","content_type":"image/png"}`)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Zero(t, s.writes)
}

func TestT378_Presign_SameTeamAndTemplateSucceed(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		`{"file_type":"comm_photo","owner_type":"patient","owner_id":"`+t378PatOwn+`","content_type":"image/png"}`)
	require.Equal(t, http.StatusOK, code, body["message"].(string))
	assert.Equal(t, 1, s.writes, "本团队照常落 pending 行")

	// 模板上传不是患者数据：连无团队医护都不该被挡住（/review-templates 回归门槛）
	code, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378DocNoTeam, "ROLE_DOCTOR",
		`{"file_type":"comm_photo","owner_type":"ReviewTemplate","owner_id":"ADM-T378-ORPH","content_type":"image/png"}`)
	require.Equal(t, http.StatusOK, code, body["message"].(string))
}

func TestT378_Presign_PatientRoleUnchanged(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378PatOther, "patient",
		`{"file_type":"comm_photo","owner_type":"patient","owner_id":"`+t378PatOwn+`","content_type":"image/png"}`)
	require.Equal(t, http.StatusOK, code, body["message"].(string))

	data, ok := body["data"].(map[string]interface{})
	require.True(t, ok, "响应缺 data：%v", body)
	fm, err := s.GetFileByFileID(context.Background(), data["file_id"].(string))
	require.NoError(t, err)
	assert.Equal(t, t378PatOther, fm.OwnerID, "患者侧 owner 仍被强制为本人，不看请求体")
	assert.Equal(t, "patient", fm.OwnerType)
	assert.Zero(t, s.scopeCalls, "patient 角色不该触达团队判定")
}

func TestT378_ScopeLookupErrorFailsClosed(t *testing.T) {
	s := t378Store()
	s.scopeErr = errors.New("doctors read failed")
	srv := t378Srv(t, s)
	defer srv.Close()

	code, body := doGET(t, srv.URL+"/api/v1/files/query", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusInternalServerError, code, body["message"].(string))
	assert.False(t, s.teamQuery.TeamScoped, "判定失败时不得带着未收窄的条件去查库")
}

func TestT378_ProbeErrorFailsClosed(t *testing.T) {
	s := t378Store()
	s.probeErr = errors.New("files read failed")
	srv := t378Srv(t, s)
	defer srv.Close()

	code, _ := doGET(t, srv.URL+"/api/v1/files/F-OWN", t378Doc, "ROLE_DOCTOR")
	assert.Equal(t, http.StatusInternalServerError, code, "探测失败一律拒，不退化成放行")

	code, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		`{"file_type":"comm_photo","owner_type":"patient","owner_id":"`+t378PatOwn+`","content_type":"image/png"}`)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Zero(t, s.writes)
}

// t378Shape 抹掉标识符，只留响应形态。
func t378Shape(msg, id string) string {
	return strings.Replace(msg, id, "<id>", 1)
}
