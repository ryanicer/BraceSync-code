// Package handler T396 判据：presign 的 owner_type 枚举收口。
//
// 三条行为线（同一 role/file_type/owner_id 现场，只让 owner_type 变动）：
//  1. 集合外取值 → 400 且库内零写（不再是穿过校验后由迁移 000030 的 CHECK 拒成 500）；
//  2. 集合内四值 → 一律放行，不打断现网三条上传链路（复查报告 / 告警附件 / 模板）；
//  3. 非 staff 的 owner_type 仍被服务端强制覆盖（T261 身份单一来源），本校验排在覆盖之后，
//     故患者端不会因请求体里的垃圾 owner 值新增 400 面。
package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/file-service/internal/model"
)

// t396PresignBody 只变 owner_type 的请求体（owner_id 用他团队患者：非医护角色不受团队判定影响，
// 医护角色另有专门用例，见 TestT396_Presign_DoctorUnknownOwnerType_400BeforeScopeProbe）
func t396PresignBody(ownerType string) string {
	return fmt.Sprintf(`{"file_type":"comm_photo","owner_type":%q,"owner_id":"P-T378-OWN","content_type":"image/png"}`, ownerType)
}

func t396StoredFile(t *testing.T, s *memStore, body map[string]interface{}) *model.FileMetadata {
	t.Helper()
	data, ok := body["data"].(map[string]interface{})
	require.True(t, ok, "响应缺 data：%v", body)
	fm, err := s.GetFileByFileID(context.Background(), data["file_id"].(string))
	require.NoError(t, err)
	return fm
}

func TestT396_Presign_OwnerTypeOutsideEnum_400WithZeroWrites(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	// Patient / review 是 T391 取证里 staging 真存过的大小写脏值；其余是「任意未知串」样本
	for _, ot := range []string{"Patient", "PATIENT", "review", "installRecord", "InstallRecord", "unknown", "patient ", " patient"} {
		code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "ADM-ROOT", "ROLE_ADMIN", t396PresignBody(ot))
		require.Equal(t, http.StatusBadRequest, code, "owner_type=%q 竟被放行", ot)
		assert.Equal(t, float64(ErrorCodeInvalidRequest), body["code"])
		assert.Equal(t, "unsupported owner_type", body["message"], "owner_type=%q", ot)
	}
	assert.Zero(t, s.writes, "400 必须排在签发之前：签发即落 files pending 行")
	assert.Zero(t, s.scopeCalls, "枚举不合格的请求不该再触达团队判定")
}

func TestT396_Presign_MissingOwnerType_Still400(t *testing.T) {
	srv := t378Srv(t, t378Store())
	defer srv.Close()

	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "ADM-ROOT", "ROLE_ADMIN",
		`{"file_type":"comm_photo","owner_id":"P-T378-OWN","content_type":"image/png"}`)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, float64(ErrorCodeInvalidRequest), body["code"])
}

func TestT396_Presign_EnumValues_AllPassAndStoredVerbatim(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	for _, ot := range []string{
		model.OwnerTypePatient,
		model.OwnerTypeAlert,
		model.OwnerTypeReviewTemplate,
		model.OwnerTypeInstallRecord,
	} {
		before := s.writes
		code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", "ADM-ROOT", "ROLE_ADMIN", t396PresignBody(ot))
		require.Equal(t, http.StatusOK, code, "枚举内取值 %q 被拦 = 打断上传链路：%v", ot, body["message"])
		assert.Equal(t, before+1, s.writes)
		assert.Equal(t, ot, t396StoredFile(t, s, body).OwnerType, "落库值必须与请求逐字相等（大小写敏感）")
	}
}

func TestT396_Presign_NonStaffOwnerTypeStillForced(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	// 患者端请求体塞脏 owner_type：按 T261 该字段本就忽略，不该因 T396 变成 400
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378PatOther, "patient",
		t396PresignBody("Patient"))
	require.Equal(t, http.StatusOK, code, body["message"])

	fm := t396StoredFile(t, s, body)
	assert.Equal(t, model.OwnerTypePatient, fm.OwnerType, "非 staff 仍由服务端强制成小写 patient")
	assert.Equal(t, t378PatOther, fm.OwnerID, "owner_id 仍强制为本人")
	assert.Zero(t, s.scopeCalls, "patient 角色不该触达团队判定")
}

func TestT396_Presign_DoctorUnknownOwnerType_400BeforeScopeProbe(t *testing.T) {
	s := t378Store()
	srv := t378Srv(t, s)
	defer srv.Close()

	// 收口前：医护用任意未知 owner_type 就能绕开团队判定（default 分支恒真）并落库。
	// 收口后：这类请求在枚举校验处即 400，既不落库也不触库判定。
	code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		t396PresignBody("Patient"))
	require.Equal(t, http.StatusBadRequest, code, "不该是 403（越权）而该是 400（参数非法）：%v", body["message"])
	assert.Equal(t, "unsupported owner_type", body["message"])
	assert.Zero(t, s.writes)
	assert.Zero(t, s.scopeCalls)

	// 反证：同角色同现场，本团队患者走枚举内取值仍正常放行（新门禁没顺手把正常链路带红）
	code, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/files/presign", t378Doc, "ROLE_DOCTOR",
		t396PresignBody(model.OwnerTypePatient))
	require.Equal(t, http.StatusOK, code, body["message"])
	assert.Equal(t, 1, s.writes)
}
