// Package handler T193 配网密钥归属校验（HTTP 层，真实路由 + 内存 store）
//
// 卡片要求：放开患者领 provision-key，但限「只能领自己已绑定设备」。
// 本文件锁死四件事：
//  1. 患者领本人绑定设备 → 200，且响应契约零变更（provision_key_hex 32hex / expires_in_sec 300）；
//  2. 患者领他人设备、领未绑定设备、缺身份头 → 403（20403），拒绝响应不夹带密钥；
//  3. 归属取实时绑定：换绑/解绑后原患者立刻失去领卡资格（防"注册时快照"式假实现）；
//  4. 越权探测不得占用该设备的重发间隔窗口（校验落在派生之前）。
//
// gateway 侧只测角色白名单（其 device-service 替身一律回 code=0，测不出归属），
// 见 services/gateway/cmd/server/rbac_impl_test.go TestRBAC_T193_ProvisionKeyRoles。
package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// provPath 领卡路径（:deviceId 已代入）
func provPath(deviceID string) string { return "/api/v1/devices/" + deviceID + "/provision-key" }

// asPatient 患者 JWT 经网关后注入的头（X-User-Id = JWT sub = patient_id，见 user-service patientLogin）
func asPatient(patientID string) map[string]string {
	return map[string]string{"X-User-Id": patientID, "X-Role": "patient"}
}

// registerAndBind 注册设备并绑定给指定患者——走真实注册/绑定端点，不手改 store，
// 以便同时证明 devices.patient_id 确实被绑定动作写入（只在测试里塞字段的实现不可信）。
func registerAndBind(t *testing.T, env *testEnv, deviceID, patientID string) {
	t.Helper()
	env.store.AddPatient(patientID)
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": deviceID}, nil)
	require.Equal(t, model.CodeOK, resp.Code, "注册设备 %s 失败", deviceID)
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/"+deviceID+"/bind",
		map[string]string{"patientId": patientID}, map[string]string{"X-User-Id": "TECH-INSTALL"})
	require.Equal(t, model.CodeOK, resp.Code, "绑定 %s→%s 失败", deviceID, patientID)
}

// assertKeyIssued 校验领卡成功 + 响应字段与 T067 契约一致（T193 红线：不改字段、不改派生算法）
func assertKeyIssued(t *testing.T, code int, data json.RawMessage) {
	t.Helper()
	require.Equal(t, model.CodeOK, code)
	var got struct {
		ProvisionKeyHex string `json:"provision_key_hex"`
		ExpiresInSec    int    `json:"expires_in_sec"`
	}
	require.NoError(t, json.Unmarshal(data, &got), "响应 data 须为 {provision_key_hex, expires_in_sec}")
	assert.Len(t, got.ProvisionKeyHex, 32, "provision key 须 32 hex 字符（16B）")
	_, err := hex.DecodeString(got.ProvisionKeyHex)
	assert.NoError(t, err, "provision key 须合法 hex")
	assert.Equal(t, 300, got.ExpiresInSec, "expires_in_sec 须保持 T067 固定值 300")
}

// TestProvisionKey_T193_PatientOwnDevice 患者领本人绑定设备 → 200（验收项 1）
func TestProvisionKey_T193_PatientOwnDevice(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-OWN", "P-T193-A")

	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-OWN"), nil, asPatient("P-T193-A"))
	assert.Equal(t, http.StatusOK, status)
	assertKeyIssued(t, resp.Code, resp.Data)
	t.Logf("[T193-wire] 患者领本人设备 POST %s → %d code=%d data=%s",
		provPath("DEV-T193-OWN"), status, resp.Code, string(resp.Data))
}

// TestProvisionKey_T193_PatientOtherDevice 患者领他人绑定设备 → 403（验收项 2）
func TestProvisionKey_T193_PatientOtherDevice(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-VICTIM", "P-T193-B")

	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-VICTIM"), nil, asPatient("P-T193-ATTACKER"))
	assert.Equal(t, http.StatusForbidden, status, "患者领他人设备须 403")
	assert.Equal(t, model.CodeForbidden, resp.Code)
	assert.NotContains(t, string(resp.Data), "provision_key_hex", "拒绝响应不得夹带密钥")

	// 反证"患者一律 403"的假修法，同时证明越权探测没占用重发间隔
	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-VICTIM"), nil, asPatient("P-T193-B"))
	assert.Equal(t, http.StatusOK, status, "越权探测后真本人仍须能领卡")
	assertKeyIssued(t, resp.Code, resp.Data)
}

// TestProvisionKey_T193_PatientUnboundDevice 患者领未绑定设备 → 403；技师口径不变
func TestProvisionKey_T193_PatientUnboundDevice(t *testing.T) {
	env := newTestEnv(t)
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": "DEV-T193-FREE"}, nil)
	require.Equal(t, model.CodeOK, resp.Code)

	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-FREE"), nil, asPatient("P-T193-C"))
	assert.Equal(t, http.StatusForbidden, status, "未绑定设备不得对患者发钥匙")
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// 技师对同一未绑定设备照旧可领（T089 安装流程 bind 前也要配网），本卡不动这条口径
	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-FREE"), nil,
		map[string]string{"X-User-Id": "TECH-1", "X-Role": "technician"})
	assert.Equal(t, http.StatusOK, status, "技师领卡口径不得变")
	assertKeyIssued(t, resp.Code, resp.Data)
}

// TestProvisionKey_T193_UnknownDevice 患者领未注册设备 → 404/20404（沿用既有错误码契约）
func TestProvisionKey_T193_UnknownDevice(t *testing.T) {
	env := newTestEnv(t)
	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-GHOST"), nil, asPatient("P-T193-D"))
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, resp.Code)
}

// TestProvisionKey_T193_RebindRevokesPreviousOwner 换绑后旧患者立即失去领卡资格
// —— 校验读的是实时绑定态，不是注册时的快照。
func TestProvisionKey_T193_RebindRevokesPreviousOwner(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-REBIND", "P-T193-OLD")

	// 新患者先探一次：此刻设备仍属 OLD → 403（且不占用重发间隔）
	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-REBIND"), nil, asPatient("P-T193-NEW"))
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, model.CodeForbidden, resp.Code)

	env.store.AddPatient("P-T193-NEW")
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/DEV-T193-REBIND/rebind",
		map[string]string{"patientId": "P-T193-NEW"}, map[string]string{"X-User-Id": "TECH-INSTALL"})
	require.Equal(t, model.CodeOK, resp.Code, "换绑失败")

	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-REBIND"), nil, asPatient("P-T193-OLD"))
	assert.Equal(t, http.StatusForbidden, status, "换绑后旧患者须 403")

	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-REBIND"), nil, asPatient("P-T193-NEW"))
	assert.Equal(t, http.StatusOK, status, "换绑后新患者须能领卡")
	assertKeyIssued(t, resp.Code, resp.Data)
}

// TestProvisionKey_T193_UnbindRevokesOwner 解绑后原绑定患者 → 403
func TestProvisionKey_T193_UnbindRevokesOwner(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-UNBIND", "P-T193-E")

	_, resp := env.do(t, http.MethodPost, "/api/v1/devices/DEV-T193-UNBIND/unbind", nil,
		map[string]string{"X-User-Id": "TECH-INSTALL"})
	require.Equal(t, model.CodeOK, resp.Code)

	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-UNBIND"), nil, asPatient("P-T193-E"))
	assert.Equal(t, http.StatusForbidden, status, "解绑后原患者须 403")
	assert.Equal(t, model.CodeForbidden, resp.Code)
}

// TestProvisionKey_T193_FailClosedOnMissingIdentity 缺 X-Role / 缺 X-User-Id / 未知角色
// （鉴权链路异常、或绕开网关直连本服务）→ 一律 fail-closed 403：
// 服务层角色白名单与 gateway provisionKeyRoles 同形（ROLE_ADMIN / technician / patient）。
func TestProvisionKey_T193_FailClosedOnMissingIdentity(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-NOROLE", "P-T193-F")

	// 只有 X-User-Id、无 X-Role（且该 uid 恰为设备本人——角色缺失时归属成立也不放行）
	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-NOROLE"), nil,
		map[string]string{"X-User-Id": "P-T193-F"})
	assert.Equal(t, http.StatusForbidden, status, "缺角色头须 fail-closed")
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// 两个头都没有
	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-NOROLE"), nil, nil)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, model.CodeForbidden, resp.Code)

	// 医生/客服/未知角色直连：本层按角色拒绝（与 gateway 同口径），即使携带的 X-User-Id 就是本人
	for _, role := range []string{"ROLE_DOCTOR", "ROLE_CS", "ROLE_UNKNOWN"} {
		status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-NOROLE"), nil,
			map[string]string{"X-User-Id": "P-T193-F", "X-Role": role})
		assert.Equal(t, http.StatusForbidden, status, "role=%s 直连不得领到钥匙", role)
		assert.Equal(t, model.CodeForbidden, resp.Code)
	}

	// 反证：真本人带 patient 角色仍可领
	status, resp = env.do(t, http.MethodPost, provPath("DEV-T193-NOROLE"), nil, asPatient("P-T193-F"))
	assert.Equal(t, http.StatusOK, status)
	assertKeyIssued(t, resp.Code, resp.Data)
}

// TestProvisionKey_T193_AdminOpsUnchanged ROLE_ADMIN 领任意设备（运维兜底）口径不变
func TestProvisionKey_T193_AdminOpsUnchanged(t *testing.T) {
	env := newTestEnv(t)
	registerAndBind(t, env, "DEV-T193-ADMIN", "P-T193-G")

	status, resp := env.do(t, http.MethodPost, provPath("DEV-T193-ADMIN"), nil,
		map[string]string{"X-User-Id": "ADM001", "X-Role": "ROLE_ADMIN"})
	assert.Equal(t, http.StatusOK, status)
	assertKeyIssued(t, resp.Code, resp.Data)
}
