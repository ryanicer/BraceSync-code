// T407：设备当前生效基线只读 GET /api/v1/devices/:deviceId/baseline。
//
// 卡面缺口：daily_wear_stats.avg_pressure 吃的是 pressure_records 里**校准前**的 20 点原值，
// 而 /records、/realtime 每帧回的是 calibration.Apply 减过偏移后的值。device-service 侧只有
// POST /baselines（写），读接口一条都没有 ⇒ 验收方拿不到那 20 个偏移，无法把帧值反推回
// 聚合式吃的值（Ella T366 验收「缺 1」）。
//
// 本文件守四件事：
//  1. 已校准设备回 calibration 实际减的那一条，且与 install 详情的 offsetValues 同源
//     （两侧一分叉，用本端点反推出来的 avg_pressure 就是另一个数）；
//  2. 一机多装：换基线后必须跟着换成最新那条（规矩 A：baseline_id 最大），旧安装仍回自己的；
//  3. 设备存在却从未校准 = 200 + calibrated:false + offsetValues 空数组（不是 null，与
//     install 详情「未校准为 []」同形）；404/20404 只留给设备不存在；
//  4. 医护读侧挂在 T378 同一道门上：跨团队 403，同团队照常读。
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

// t407DTO 读接口响应体（createdAt 只判存在，不断具体时刻）
type t407DTO struct {
	DeviceID     string    `json:"deviceId"`
	Calibrated   bool      `json:"calibrated"`
	OffsetValues []float32 `json:"offsetValues"`
	BaselineID   string    `json:"baselineId"`
	InstallID    string    `json:"installId"`
	CalibratorID string    `json:"calibratorId"`
	CreatedAt    string    `json:"createdAt"`
}

// t407Get 读一次设备当前生效基线。返回 HTTP 状态、业务码、data 原文与结构体。
func t407Get(t *testing.T, env *testEnv, deviceID string) (int, int, json.RawMessage, t407DTO) {
	t.Helper()
	status, resp := env.do(t, http.MethodGet, "/api/v1/devices/"+deviceID+"/baseline", nil, nil)
	var dto t407DTO
	if len(resp.Data) > 0 {
		require.NoError(t, json.Unmarshal(resp.Data, &dto), "data=%s", string(resp.Data))
	}
	return status, resp.Code, resp.Data, dto
}

func TestT407_BaselineRead_CalibratedDeviceMatchesInstallDetail(t *testing.T) {
	env := newTestEnv(t)
	const dev, pat, tech = "DEV-T407-A", "P-T407-A", "T-T407-A"
	want := offsetsWith(map[int]float32{0: 0.5, 19: 1.25})

	installID := createInstallForCalib(t, env, dev, pat, tech)
	saveBaselineFor(t, env, installID, want, tech)

	status, code, _, dto := t407Get(t, env, dev)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, model.CodeOK, code)
	assert.Equal(t, dev, dto.DeviceID)
	assert.True(t, dto.Calibrated, "已校准设备必须 calibrated=true")
	assert.Equal(t, want, dto.OffsetValues, "偏移值必须逐点等于校准写入的那一条")
	assert.NotEmpty(t, dto.BaselineID)
	assert.Equal(t, installID, dto.InstallID)
	assert.Equal(t, tech, dto.CalibratorID)
	assert.NotEmpty(t, dto.CreatedAt)

	detail := getInstallDetail(t, env, installID)
	assert.Equal(t, detail.OffsetValues, dto.OffsetValues,
		"设备维度基线与安装详情偏移值必须同源（读的是 baselines 同一条）")
}

func TestT407_BaselineRead_LatestBaselineIDWinsAcrossInstalls(t *testing.T) {
	env := newTestEnv(t)
	const dev, pat, tech = "DEV-T407-B", "P-T407-B", "T-T407-B"
	first := offsetsWith(map[int]float32{0: 0.2})
	second := offsetsWith(map[int]float32{0: 9.9})

	i1 := createInstallForCalib(t, env, dev, pat, tech)
	saveBaselineFor(t, env, i1, first, tech)
	_, _, _, mid := t407Get(t, env, dev)
	require.Equal(t, first, mid.OffsetValues, "单次校准后应回那一条基线")

	i2 := createInstallForCalib(t, env, dev, pat, tech) // 同机换装再校准
	saveBaselineFor(t, env, i2, second, tech)

	_, _, _, cur := t407Get(t, env, dev)
	assert.Equal(t, second, cur.OffsetValues, "规矩 A：同机多基线取 baseline_id 最新一条")
	assert.Equal(t, i2, cur.InstallID, "不得回上一次安装的基线")
	assert.NotEqual(t, mid.BaselineID, cur.BaselineID, "换新基线后 baselineId 必须跟着换")

	// 反证：库里两条基线各自归属自己的安装 —— 否则上面的「最新胜出」是在守空 detector
	d1 := getInstallDetail(t, env, i1)
	assert.Equal(t, first, d1.OffsetValues, "旧安装详情仍指自己的基线（per-install 语义未被本端点改写）")
	d2 := getInstallDetail(t, env, i2)
	assert.Equal(t, second, d2.OffsetValues, "新安装详情回新基线")
}

func TestT407_BaselineRead_UncalibratedDeviceReturnsEmptyArray(t *testing.T) {
	env := newTestEnv(t)
	const dev = "DEV-T407-C"
	_, _ = env.do(t, http.MethodPost, "/api/v1/devices", map[string]string{"deviceId": dev}, nil)

	status, code, raw, dto := t407Get(t, env, dev)
	require.Equal(t, http.StatusOK, status, "设备存在但未校准不得报 404")
	assert.Equal(t, model.CodeOK, code)
	assert.False(t, dto.Calibrated)
	require.NotNil(t, dto.OffsetValues, "未校准必须回空数组而非 null（与 install 详情同形）")
	assert.Empty(t, dto.OffsetValues)
	assert.Contains(t, string(raw), `"offsetValues":[]`)
	assert.Empty(t, dto.BaselineID, "未校准时不得凭空造出 baselineId")
}

func TestT407_BaselineRead_UnknownDeviceFailsWith20404(t *testing.T) {
	env := newTestEnv(t)
	status, code, _, _ := t407Get(t, env, "DEV-T407-NOT-EXIST")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, model.CodeNotFound, code)
}

// 医护读侧：复用 T378 的 fake ListStore 编排团队判定。
// 漏挂 assertDeviceInScope 的后果是任意医护令牌能读全院设备的校准偏移。
func TestT407_BaselineRead_DoctorTeamScope(t *testing.T) {
	r, ls, fs := t378Env(t, t378OwnTeam)
	ctx := context.Background()
	for _, id := range []string{"DEV-T378-OWN", "DEV-T378-OTHER"} {
		_, err := fs.RegisterDevice(ctx, &model.Device{DeviceID: id})
		require.NoError(t, err, "注册设备夹具失败 %s", id)
	}
	ls.deviceTeamResult["DEV-T378-OTHER"] = false

	w, resp := t378Get(t, r, "/api/v1/devices/DEV-T378-OWN/baseline", t378Hdr(t378DoctorHDR, t378DoctorUID))
	require.Equal(t, http.StatusOK, w.Code, resp.Message)
	var dto t407DTO
	require.NoError(t, json.Unmarshal(resp.Data, &dto))
	assert.False(t, dto.Calibrated, "同团队设备无基线时仍回 200 + 未校准")

	w, resp = t378Get(t, r, "/api/v1/devices/DEV-T378-OTHER/baseline", t378Hdr(t378DoctorHDR, t378DoctorUID))
	assert.Equal(t, http.StatusForbidden, w.Code, "跨团队设备不得读偏移值")
	assert.Equal(t, model.CodeForbidden, resp.Code)
}
