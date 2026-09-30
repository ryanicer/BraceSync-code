// T498 注入端点的 HTTP 面（路由存在性 + 门控响应 + 响应体字段）。
//
// 为什么服务层测过还要这一层：门禁在 service 层，但「403 到底是 HTTP 状态还是 body 里的 code」
// 是验收 2 的可观测口径 —— Boss 那边只会看请求返回什么。这一层不经 service 的替身，
// 走的是 gin 路由表 + 统一响应体（fail()），漏接路由 / 漏接 status 都会在这里现形。
//
// 另一件事：/internal/* 不经 gateway，所以没有 JWT 注入的 X-User-Id/X-Role 身份头。
// 下面两格「不带任何身份头」的断言就是在登记这个信任面：能打到这个端点 = 能进内网，
// 身份只剩审计行里的自报 operator + 客观 ip。这条已作为待裁项写进卡（不是本卡偷偷放开的口子）。
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// stubMockStore service.MockFrameStore 的内存实现（注入落库 + 审计同事务那一格在 repo 层测）
type stubMockStore struct {
	calls []repo.MockFrameInput
	res   repo.MockFrameResult
	err   error
}

func (s *stubMockStore) InsertMockFrame(_ context.Context, in repo.MockFrameInput) (repo.MockFrameResult, error) {
	s.calls = append(s.calls, in)
	if s.err != nil {
		return repo.MockFrameResult{}, s.err
	}
	return s.res, nil
}

// tsNow 一分钟前：与同包既有用例同取径，落在 validateTimestamp 允许的窗口内
func tsNow() time.Time { return time.Now().Add(-time.Minute) }

func mockBody(t *testing.T, deviceID string, ts time.Time, pointCount int, reason string) string {
	t.Helper()
	points := make([]float64, pointCount)
	for i := range points {
		points[i] = 1500
	}
	pj, err := json.Marshal(points)
	require.NoError(t, err)
	return fmt.Sprintf(`{"device_id":%q,"timestamp":%d,"points":%s,"battery":80,`+
		`"reason":%q,"operator":"ops-script"}`, deviceID, ts.Unix(), pj, reason)
}

func TestT498_MockFrameRouteRegistered(t *testing.T) {
	srv := newTestServer(nil)
	svc := &stubMockStore{res: repo.MockFrameResult{RecordID: 5001, AuditLogID: 8001}}
	srv.svc.SetMockIngest(true, svc)

	w := doReq(t, srv, http.MethodPost, "/internal/mock-frame",
		mockBody(t, hDevice, tsNow(), model.PointCount, "boss acceptance"), nil)
	require.Equal(t, http.StatusOK, w.Code, "路由没接上时这里是 404，会伪装成「开关无效」")

	code, data := decodeBody(t, w)
	assert.Equal(t, model.CodeOK, code)
	assert.Equal(t, "5001", data["record_id"])
	assert.Equal(t, model.IngestMock, data["ingest_source"], "响应体自证来源，调用方不必回库查")
	assert.False(t, data["duplicated"].(bool))
	assert.Equal(t, float64(8001), data["audit_log_id"],
		"审计行 id 必须回给调用方：注入现场与留痕要能双向对查")

	require.Len(t, svc.calls, 1)
	assert.Equal(t, hDevice, svc.calls[0].DeviceID)
	assert.Equal(t, hPatient, svc.calls[0].PatientID, "患者归属取自设备绑定，不是注入方自报")
	assert.Equal(t, "boss acceptance", svc.calls[0].Reason)
}

func TestT498_GateOffReturns403BeforeTouchingStore(t *testing.T) {
	srv := newTestServer(nil)
	svc := &stubMockStore{}
	srv.svc.SetMockIngest(false, svc)

	w := doReq(t, srv, http.MethodPost, "/internal/mock-frame",
		mockBody(t, hDevice, tsNow(), model.PointCount, "should be refused"), nil)

	require.Equal(t, http.StatusForbidden, w.Code, "验收 2 的 HTTP 面：关开关后这一跳必须是 403")
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Trace   *struct {
			ErrorCode int `json:"errorCode"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, model.CodeForbidden, resp.Code)
	require.NotNil(t, resp.Trace)
	assert.Equal(t, model.CodeForbidden, resp.Trace.ErrorCode, "T464：trace.errorCode 与 body.code 同值")
	assert.Empty(t, svc.calls, "拒绝必须发生在触库之前")

	// T464 出口口径：message 是用户面文案，技术串（含开关名）只进日志不得回显。
	// 🔴 现文案是「查看患者数据」口径、对运维调用方不准确，已作为待裁项写进卡；
	// 这里只钉「不泄露技术文本」这条通用不变量，不钉那句文案本身（改了不必回本卡）。
	assert.NotContains(t, resp.Message, "ALLOW_MOCK_INGEST")
	assert.NotContains(t, resp.Message, "mock ingest is disabled")
	assert.NotEmpty(t, resp.Message)
}

func TestT498_GateOnStillEnforcesBusinessValidation(t *testing.T) {
	srv := newTestServer(nil)
	svc := &stubMockStore{res: repo.MockFrameResult{RecordID: 1, AuditLogID: 1}}
	srv.svc.SetMockIngest(true, svc)

	t.Run("点数不足", func(t *testing.T) {
		w := doReq(t, srv, http.MethodPost, "/internal/mock-frame",
			mockBody(t, hDevice, tsNow(), 19, "typo in script"), nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		code, _ := decodeBody(t, w)
		assert.Equal(t, model.CodeInvalidParam, code)
	})

	t.Run("设备未绑定", func(t *testing.T) {
		w := doReq(t, srv, http.MethodPost, "/internal/mock-frame",
			mockBody(t, "PRS-NOT-BOUND", tsNow(), model.PointCount, "wrong device"), nil)
		assert.Equal(t, http.StatusNotFound, w.Code, "与真实上报同口径：未注册设备 20404")
		code, _ := decodeBody(t, w)
		assert.Equal(t, model.CodeDeviceNotFound, code)
	})

	t.Run("缺 reason", func(t *testing.T) {
		w := doReq(t, srv, http.MethodPost, "/internal/mock-frame",
			mockBody(t, hDevice, tsNow(), model.PointCount, "   "), nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		code, _ := decodeBody(t, w)
		assert.Equal(t, model.CodeInvalidParam, code)
	})

	assert.Empty(t, svc.calls, "三类校验都得拦在触库之前")
}

// TestT498_RealUploadUnaffectedByGateOff 验收 4：开关关掉不影响真实上报链路。
// 真实端点看的是另一套（限流 + 设备绑定），与 ALLOW_MOCK_INGEST 无任何耦合。
func TestT498_RealUploadUnaffectedByGateOff(t *testing.T) {
	srv := newTestServer(nil)
	srv.svc.SetMockIngest(false, &stubMockStore{})

	w := doReq(t, srv, http.MethodPost, "/api/v1/device/records",
		validFrameBody(tsNow()), map[string]string{"X-Device-Id": hDevice})
	require.Equal(t, http.StatusOK, w.Code, "真实上报被 mock 开关影响 = 开关接到了公共链路上")
	code, data := decodeBody(t, w)
	assert.Equal(t, model.CodeOK, code)
	assert.NotEmpty(t, data["record_id"])
}
