//go:build integration
// +build integration

// Package handler T193 集成测试：真实 PG15 上验证「患者只能领自己已绑定设备的配网密钥」。
//
// 为什么不只信 provision_ownership_t193_test.go（内存 FakeStore）：
// 本卡归属判定的事实源是 devices.patient_id 这一列，而 FakeStore 的 Bind 是测试自己写的桩——
// 若 PGStore 的绑定事务没把这列写进去（同仓 T122/T137「链路通了但字段没落库」缺陷族），
// fake 测试照样全绿而真机患者永远 403。故这里走真实 PGStore + 真实 HTTP 路由，
// 并直接回读 devices.patient_id 打印实测值。
//
// 运行：make test-integration（CI go-integration job 已含 ./services/device-service/...）
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/device-service/internal/model"
)

// itEnsurePatient 幂等插入患者行（patients owner=user-service，此处为集成测试最小夹具）
func itEnsurePatient(t *testing.T, patientID, name, hashPrefix string) {
	t.Helper()
	_, err := itPool.Exec(context.Background(),
		`INSERT INTO patients (patient_id, name, phone_enc, phone_hash, status)
		 VALUES ($1, $2, '\x00'::bytea, $3 || repeat('0', 60), 'active')
		 ON CONFLICT (patient_id) DO NOTHING`,
		patientID, name, hashPrefix)
	require.NoError(t, err, "插入患者夹具失败 %s", patientID)
}

// itDevicePatient 直读 devices.patient_id —— 归属校验事实源的实测值（nil = 未绑定）
func itDevicePatient(t *testing.T, deviceID string) *string {
	t.Helper()
	var pid *string
	err := itPool.QueryRow(context.Background(),
		`SELECT patient_id FROM devices WHERE device_id = $1`, deviceID).Scan(&pid)
	require.NoError(t, err, "读取 devices.patient_id 失败 %s", deviceID)
	return pid
}

// TestIT_T193_ProvisionKeyOwnership 患者领卡三态 + 解绑回收 + 重发间隔未被本卡削弱
func TestIT_T193_ProvisionKeyOwnership(t *testing.T) {
	const (
		deviceID = "DEV-IT-T193-OWN"
		ownerID  = "P-IT-T193-A"
		otherID  = "P-IT-T193-B"
		techID   = "T-IT-T193"
	)
	itEnsurePatient(t, ownerID, "T193本人甲", "da1a")
	itEnsurePatient(t, otherID, "T193他人乙", "da1b")

	env := itEnv(t)

	// 注册 + 技师绑定（均走真实端点 → 真实 PGStore 事务）
	_, resp := env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": deviceID}, nil)
	require.Equal(t, 0, resp.Code, "注册失败：%s", string(resp.Data))
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/"+deviceID+"/bind",
		map[string]string{"patientId": ownerID}, map[string]string{"X-User-Id": techID, "X-Role": "technician"})
	require.Equal(t, 0, resp.Code, "绑定失败：%s", string(resp.Data))

	got := itDevicePatient(t, deviceID)
	require.NotNil(t, got, "绑定后 devices.patient_id 须已落库（否则本卡患者永远 403）")
	assert.Equal(t, ownerID, *got)
	t.Logf("[T193-it] 绑定后 devices[%s].patient_id = %s", deviceID, *got)

	// ① 他人患者 → 403
	status, resp := env.do(t, http.MethodPost, provPath(deviceID), nil, asPatient(otherID))
	assert.Equal(t, http.StatusForbidden, status, "他人患者领卡须 403")
	assert.Equal(t, model.CodeForbidden, resp.Code)
	t.Logf("[T193-it] 他人患者  POST %s (X-Role=patient, X-User-Id=%s) → %d %s",
		provPath(deviceID), otherID, status, string(resp.Data))

	// ② 本人患者 → 200 + 密钥（响应契约与 T067 一致）
	status, resp = env.do(t, http.MethodPost, provPath(deviceID), nil, asPatient(ownerID))
	require.Equal(t, http.StatusOK, status, "本人患者应可领卡")
	assertKeyIssued(t, resp.Code, resp.Data)
	t.Logf("[T193-it] 本人患者  POST %s (X-Role=patient, X-User-Id=%s) → %d %s",
		provPath(deviceID), ownerID, status, string(resp.Data))

	// ③ 重发间隔未被削弱（红线：本卡只放权限，不动既有防护）
	status, resp = env.do(t, http.MethodPost, provPath(deviceID), nil, asPatient(ownerID))
	assert.Equal(t, http.StatusTooManyRequests, status, "同设备重发间隔内仍须 429")
	assert.Equal(t, model.CodeTooMany, resp.Code)

	// ④ 解绑后原本人也回收：归属判定早于重发间隔，须 403 而非 429
	_, resp = env.do(t, http.MethodPost, "/api/v1/devices/"+deviceID+"/unbind", nil,
		map[string]string{"X-User-Id": techID, "X-Role": "technician"})
	require.Equal(t, 0, resp.Code)
	require.Nil(t, itDevicePatient(t, deviceID), "解绑后 devices.patient_id 须置 NULL")

	status, resp = env.do(t, http.MethodPost, provPath(deviceID), nil, asPatient(ownerID))
	assert.Equal(t, http.StatusForbidden, status, "解绑后原患者须 403（且不得被 429 掩盖）")
	assert.Equal(t, model.CodeForbidden, resp.Code)

	var denied struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &denied))
	assert.NotContains(t, string(denied.Data), "provision_key_hex", "回收后的拒绝响应不得夹带密钥")
}
