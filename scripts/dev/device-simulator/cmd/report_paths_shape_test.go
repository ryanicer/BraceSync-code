// Package main — 上报路径与请求体形状回归测试（T562 M1）。
//
// 立这两条测试的成因：模拟器长期把上报路径写成 /api/v1/device/report 与 /report/batch，
// 而网关注册的是 deviceReportRoutes 那两条（services/gateway/cmd/server/proxy_services.go），
// 现网实测前者 404 page not found；因为 scripts/dev 既不在 go.work 也不在任何 CI，
// 这个 404 空跑没有任何一道门会红（T551 S-1）。本文件把「路径」与「请求体形状」
// 钉成断言，形状漂移至少要在这里响，而不是等到链路断三天后靠人肉发现。
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReportPathsMatchGatewayRegistration 钉住两条路径与网关注册一致。
// 期望值取自 services/gateway/cmd/server/proxy_services.go 的 deviceReportRoutes
// （dev := r.Group("/api/v1") 前缀 + "/device/records" 与 "/device/records/batch"）。
func TestReportPathsMatchGatewayRegistration(t *testing.T) {
	assert.Equal(t, "/api/v1/device/records", PathSingle,
		"单帧上报路径漂移：网关注册的是 /api/v1 + /device/records")
	assert.Equal(t, "/api/v1/device/records/batch", PathBatch,
		"批量补传路径漂移：网关注册的是 /api/v1 + /device/records/batch")
}

// TestFrameBodyShape 单帧请求体必须对齐 data-service 的 SingleFrameRequest：
// timestamp 是 Unix 秒（JSON number，不是 RFC3339 串）、points 是 20 个 mN 数，
// 且不再有云端根本不收的 pressures / wearing 字段。
func TestFrameBodyShape(t *testing.T) {
	e := NewSimEngine([]SimConfig{{DeviceID: "DEV_SHAPE", Secret: "s", BaseURL: "http://x"}}, time.Minute)
	frame := e.generateFrame("DEV_SHAPE")

	raw, err := json.Marshal(frame)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))

	assert.ElementsMatch(t,
		[]string{"device_id", "timestamp", "points", "battery", "firmware"},
		keysOf(generic),
		"请求体字段集与 SingleFrameRequest 不一致（旧形是 pressures/wearing）")

	// timestamp：JSON number（Unix 秒），不是字符串
	sec, ok := generic["timestamp"].(float64)
	require.True(t, ok, "timestamp 必须是 Unix 秒数字，实测类型 %T", generic["timestamp"])
	assert.Greater(t, sec, float64(1767225600), "timestamp 应晚于 2026-01-01T00:00:00Z（云端区间下界）")

	pts, ok := generic["points"].([]any)
	require.True(t, ok, "points 必须是数组")
	require.Len(t, pts, 20, "points 必须是 20 点（P01–P20 顺序）")

	// 量纲：mN 口径下正常帧的单点应达千位级（1.2N = 1200mN），
	// 若哪天有人把单位改回 N 或 kPa，这一条会先响（单位口径见 MnPerN 注释）。
	first, ok := pts[0].(float64)
	require.True(t, ok)
	assert.GreaterOrEqual(t, first, float64(800), "points[0] 量级不符 mN 口径（应约 1000+ 而非 1–25）")
	assert.Less(t, first, float64(100000), "points[0] 超出 mN 合理上界")

	assert.Equal(t, defaultFirmware, generic["firmware"], "firmware 是协议必填字段")
}

// TestFaultFrameKeepsShape 故障模式改的是数值与 fault_code，不许改形状。
func TestFaultFrameKeepsShape(t *testing.T) {
	e := NewSimEngine([]SimConfig{{DeviceID: "DEV_FAULT", Secret: "s", BaseURL: "http://x"}}, time.Minute)
	e.EnableFaultMode()

	for i := 0; i < 50; i++ {
		frame := e.generateFrame("DEV_FAULT")
		require.Len(t, frame.Points, 20, "故障帧也必须 20 点")
		assert.Greater(t, frame.Timestamp, int64(0), "故障帧 timestamp 仍为 Unix 秒")
	}
}

// TestSendFrameHitsRecordsPath 走一次真实 HTTP 客户端（httptest 桩），
// 断言请求落在 PathSingle 上且四个签名头齐备 —— 路径写进函数才算被测过。
func TestSendFrameHitsRecordsPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"record_id":"R1","duplicated":false}}`))
	}))
	defer srv.Close()

	e := NewSimEngine([]SimConfig{{DeviceID: "DEV_HTTP", Secret: "test_secret", BaseURL: srv.URL}}, time.Minute)
	require.NoError(t, sendFrame(srv.URL, "DEV_HTTP", "test_secret", e.generateFrame("DEV_HTTP")))

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, PathSingle, gotPath, "实际发出的路径 ≠ PathSingle（模拟器空跑那一形的直接断言）")
	assert.Equal(t, "DEV_HTTP", gotHeaders.Get("X-Device-Id"))
	assert.Len(t, gotHeaders.Get("X-Nonce"), 32, "X-Nonce 必须是 32 hex（T067）")
	assert.NotEmpty(t, gotHeaders.Get("X-Timestamp"))
	assert.Len(t, gotHeaders.Get("X-Signature"), 64, "X-Signature 必须是 HMAC-SHA256 的 64 hex")
}

// TestSendFrameErrorCarriesRawResponse 用桩服务分别造 404 与 401，钉住「路径不存在」与
// 「路径在但要签名」这两种读数的分型依据：状态码 + 原始响应体都要能在错误信息里看到
// （M1 判据①要求附原始响应，靠字符串分型不靠猜）。
// 注：-mode=probe 本身只走 log.Fatalf（进程内不可断言），由 bash 门 check-routes.sh 侧证。
func TestSendFrameErrorCarriesRawResponse(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()

	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":20401,"message":"设备身份校验未通过"}`))
	}))
	defer unauthorized.Close()

	// 404 面：旧路径的形状 —— sendFrame 的错误里必须带原始响应，供取证分型
	errOld := sendFrame(notFound.URL, "DEV_404", "s", Frame{
		Timestamp: 1759550000, Points: normalPressures(), Battery: 85, Firmware: defaultFirmware,
	})
	require.Error(t, errOld)
	assert.Contains(t, errOld.Error(), "404", "404 读数要能在错误信息里看到")

	// 401 面：新路径 + 错签名的形状 —— 路径在，只是要签名
	errNew := sendFrame(unauthorized.URL, "DEV_401", "s", Frame{
		Timestamp: 1759550000, Points: normalPressures(), Battery: 85, Firmware: defaultFirmware,
	})
	require.Error(t, errNew)
	assert.Contains(t, errNew.Error(), "20401", "401 的原始响应体要落进错误信息（这是「路径存在」的证据）")
}

// TestBatchShapeAndChunking 补传请求体对齐 BatchRequest，且单请求不超云端上限。
func TestBatchShapeAndChunking(t *testing.T) {
	frames := SimulateBackfill(336)
	require.Len(t, frames, 336)

	chunks := chunkFrames(frames, MaxBatchFrames)
	require.Len(t, chunks, 4, "336 帧按 100 上限应分 4 批（协议 §4.2）")
	assert.Equal(t, []int{100, 100, 100, 36}, chunkSizes(chunks))
	for _, c := range chunks {
		assert.LessOrEqual(t, len(c), MaxBatchFrames, "超限批会被云端 400 整批拒收")
	}

	batch := BatchReport{DeviceID: "DEV_BATCH", Frames: chunks[0], Firmware: defaultFirmware}
	raw, err := json.Marshal(batch)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	assert.ElementsMatch(t, []string{"device_id", "frames", "firmware"}, keysOf(generic),
		"批次请求体字段集与 BatchRequest 不一致")

	first, ok := generic["frames"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, first)
	item, ok := first[0].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"timestamp", "points", "battery"}, keysOf(item),
		"批次内单帧字段集与 BatchFrame 不一致（device_id 只属于批次层）")
	_, isNumber := item["timestamp"].(float64)
	assert.True(t, isNumber, "batch 帧 timestamp 也必须是 Unix 秒数字")
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func chunkSizes(chunks [][]BatchFrame) []int {
	out := make([]int, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, len(c))
	}
	return out
}
