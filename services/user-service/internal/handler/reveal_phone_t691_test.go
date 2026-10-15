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

// T691 r134（方案2a）后端判别面：
//  1. listFeedbacks 按角色裁剪 phoneMasked——ROLE_ADMIN 装配、其余角色字段整体缺席（隐私不依赖前端）。
//  2. reveal 端点 admin 出明文、cs 403（handler 层 requireAdminRole 兜底）。
//  3. 每次 reveal 落一行 sensitive_read 审计，detail 只记脱敏值，明文绝不入审计行。
func TestT691PhoneMaskedTrimAndReveal(t *testing.T) {
	e := newEnv(t, true, true)
	enc, err := e.h.phone.Encrypt("13866886688")
	require.NoError(t, err)

	e.store.patient = &repo.PatientRow{PatientID: "PT-001", Name: "林小雨", PhoneEnc: enc}
	e.store.feedbacks = []repo.FeedbackRow{{
		FeedbackID: 1, PatientID: "PT-001", Type: strPtr("佩戴不适"), Content: "左肩压痛",
		SubmitTime: time.Now(), Status: "pending", PhoneEnc: enc,
	}}

	type item struct {
		PatientID   string  `json:"patientId"`
		PhoneMasked *string `json:"phoneMasked"`
	}

	// ① ROLE_ADMIN：list 带 phoneMasked 且为脱敏值（不含明文连续 11 位）
	w, resp := e.do(http.MethodGet, "/api/v1/feedbacks", nil, map[string]string{
		"X-User-Id": "A0001", "X-Role": roleAdmin,
	})
	require.Equal(t, http.StatusOK, w.Code)
	var adminList []item
	require.NoError(t, json.Unmarshal(resp.Data, &adminList))
	require.Len(t, adminList, 1)
	require.NotNil(t, adminList[0].PhoneMasked, "admin 侧 phoneMasked 必须装配")
	assert.Equal(t, "138****6688", *adminList[0].PhoneMasked)
	assert.NotContains(t, string(resp.Data), "13866886688", "常规读接口绝不回明文")

	// ①-b 非 admin（cs）：phoneMasked 字段整体缺席（服务端裁剪）
	w, resp = e.do(http.MethodGet, "/api/v1/feedbacks", nil, map[string]string{
		"X-User-Id": "C0001", "X-Role": "ROLE_CS",
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, string(resp.Data), "phoneMasked", "cs 响应体不含 phoneMasked 字段（非前端单防）")
	assert.NotContains(t, string(resp.Data), "13866886688")

	// ② reveal：admin 出明文
	w, resp = e.do(http.MethodPost, "/api/v1/admin/patients/PT-001/phone/reveal", nil, map[string]string{
		"X-User-Id": "A0001", "X-Role": roleAdmin,
	})
	require.Equal(t, http.StatusOK, w.Code, "admin reveal 应 200：%s", string(resp.Data))
	var rv struct {
		PatientID string `json:"patientId"`
		Phone     string `json:"phone"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &rv))
	assert.Equal(t, "13866886688", rv.Phone)

	// ③ 审计：reveal 后落一行 sensitive_read，detail 记脱敏、不含明文
	var sr *repo.AuditInput
	for i := range e.store.auditRows {
		if e.store.auditRows[i].Action == "sensitive_read" {
			sr = &e.store.auditRows[i]
		}
	}
	require.NotNil(t, sr, "reveal 必须落一行 sensitive_read 审计")
	assert.Equal(t, "PT-001", sr.TargetID)
	require.NotNil(t, sr.Detail)
	assert.Equal(t, "138****6688", sr.Detail["phoneMasked"], "审计 detail 只记脱敏值")
	blob, _ := json.Marshal(sr.Detail)
	assert.NotContains(t, string(blob), "13866886688", "明文绝不入审计 detail")

	// ②-b reveal：cs → 403（requireAdminRole 兜底）
	e.store.auditRows = nil
	w, _ = e.do(http.MethodPost, "/api/v1/admin/patients/PT-001/phone/reveal", nil, map[string]string{
		"X-User-Id": "C0001", "X-Role": "ROLE_CS",
	})
	assert.Equal(t, http.StatusForbidden, w.Code, "非 admin reveal 应 403")
	for i := range e.store.auditRows {
		assert.NotEqual(t, "sensitive_read", e.store.auditRows[i].Action, "403 不产生敏感查看审计")
	}
}
