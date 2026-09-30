// T498 服务层用例：受控 mock 注入的门禁、校验序与来源印章出站。
//
// 对应卡面四条验收：
//  1. 开关默认关 ⇒ 零值（不调 SetMockIngest）即 403；
//  2. mock 端点仅在开关开时可用、关时返回 403 ⇒ TestT498_GateOff...（并钉「门禁在方法体最前」：
//     把设备探测打成错误源仍能拿到 403，证明未开开关时连 device_id 存在性都不探）；
//  3. 注入帧能触发对应告警且可溯源 ⇒ 来源印章随内联评估请求出站、随降级负载入队（两条通路都测）；
//  4. 关开关后真实链路不受影响 ⇒ 门禁只作用于 InjectMock，UploadSingle 不看这个开关。
//
// 落库形状（帧 + 审计同事务、幂等冲突回查）在 repo 层，需要真库：
// repo/mock_t498_test.go（词表/语句形状）与 repo/mock_t498_integration_test.go（真库那一格）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// fakeMockStore 注入落库替身：记下每次入参，返回可控结果
type fakeMockStore struct {
	calls []repo.MockFrameInput
	res   repo.MockFrameResult
	err   error
}

func newMockStore() *fakeMockStore {
	return &fakeMockStore{res: repo.MockFrameResult{RecordID: 7001, AuditLogID: 9001}}
}

func (f *fakeMockStore) InsertMockFrame(_ context.Context, in repo.MockFrameInput) (repo.MockFrameResult, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return repo.MockFrameResult{}, f.err
	}
	return f.res, nil
}

func mockReq(reason string) *model.MockFrameRequest {
	return &model.MockFrameRequest{
		DeviceID:  testDevice,
		Timestamp: fixedNow.Unix(),
		Points:    pts(3.0, 2.0, 1.0), // 前 3 点 3.0/2.0/1.0 N（上报载荷单位是 mN，pts 负责换算）
		Battery:   80,
		Reason:    reason,
	}
}

// TestT498_GateOffIsForbiddenBeforeAnyProbe 验收 1 + 验收 2（关时 403），
// 并钉门禁的位置：把设备仓储打成错误源，拿到的仍是 403 而不是 90001 ⇒ 判定先于探测。
func TestT498_GateOffIsForbiddenBeforeAnyProbe(t *testing.T) {
	env := newTestEnv()
	store := newMockStore()
	env.svc.SetMockIngest(false, store)
	env.devices.err = errors.New("device store must not be reached")

	resp, appErr := env.svc.InjectMock(context.Background(), mockReq("boss acceptance"), "10.0.0.9")
	require.Nil(t, resp)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeForbidden, appErr.Code)
	assert.Equal(t, 403, appErr.HTTPStatus, "验收 2 要求 HTTP 面可判「关」")
	assert.Empty(t, store.calls, "未开开关不得触库")
	assert.Equal(t, 0, env.alerts.calls, "未开开关不得触发告警评估")
	assert.Empty(t, env.cache.lastseen, "未开开关不得写实时缓存")
	assert.False(t, env.svc.MockIngestEnabled())
}

// TestT498_StoreNotConfiguredFailsClosed 开关开了但没装配真库 ⇒ 500，不静默假成功。
func TestT498_StoreNotConfiguredFailsClosed(t *testing.T) {
	env := newTestEnv()
	env.svc.SetMockIngest(true, nil)

	resp, appErr := env.svc.InjectMock(context.Background(), mockReq("wiring check"), "10.0.0.9")
	require.Nil(t, resp)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)
	assert.Equal(t, 500, appErr.HTTPStatus)
}

func TestT498_GateOnWritesStampedFrameAndStopsCountingStat(t *testing.T) {
	env := newTestEnv()
	store := newMockStore()
	env.svc.SetMockIngest(true, store)

	req := mockReq("  boss acceptance: pressure_high ")
	req.Operator = "  ops-script  "
	resp, appErr := env.svc.InjectMock(context.Background(), req, "10.0.0.9")
	require.Nil(t, appErr)
	require.NotNil(t, resp)
	assert.Equal(t, "7001", resp.RecordID)
	assert.Equal(t, model.IngestMock, resp.IngestSource, "响应体要自证这一帧是注入产物")
	assert.Equal(t, int64(9001), resp.AuditLogID)
	assert.False(t, resp.Duplicated)

	require.Len(t, store.calls, 1)
	in := store.calls[0]
	assert.Equal(t, testDevice, in.DeviceID)
	assert.Equal(t, testPatient, in.PatientID, "患者归属取自设备绑定，不是注入方自报")
	assert.Equal(t, "ops-script", in.Operator, "自报标识去空白后入审计")
	assert.Equal(t, "boss acceptance: pressure_high", in.Reason)
	assert.Equal(t, "10.0.0.9", in.IP)
	assert.Equal(t, float32(3.0), in.Frame.Points[0], "注入与真实上报共用 ÷1000 归一，库里形状一致")
	assert.Equal(t, float32(1.0), in.Frame.Points[2], "下标不得错位：P01..P20 与传感器点位一一对应")
	assert.Equal(t, float32(0), in.Frame.Points[3], "未给的点位补 0，不落脏值")
	assert.True(t, in.Frame.Ts.Equal(fixedNow))

	require.Len(t, env.alerts.reqs, 1)
	assert.Equal(t, model.IngestMock, env.alerts.reqs[0].IngestSource,
		"验收 3：来源印章要随内联评估出站，alert-service 才能给告警盖章")

	// 刻意差异：lastseen 与 rt:frame 保留（佩戴中断判定 + 实时页），stat:today 不写
	assert.True(t, env.cache.lastseen[testDevice].Equal(fixedNow), "注入帧要能推进 lastseen，否则触发不了佩戴中断")
	assert.NotEmpty(t, env.cache.rtFrame[testDevice])
	assert.Empty(t, env.cache.stat, "注入帧不得累计 stat:today 的佩戴分钟数/峰值")
	assert.True(t, env.svc.MockIngestEnabled())
}

// TestT498_AlertHitStillIncrementsAbnormalCount 文件头那条例外的看守：
// countStat=false 关掉的是「佩戴分钟数/峰值」，异常计数仍要跟 alerts 表对平。
func TestT498_AlertHitStillIncrementsAbnormalCount(t *testing.T) {
	env := newTestEnv()
	env.svc.SetMockIngest(true, newMockStore())
	env.alerts.push(nil, &AlertEvalResult{ShouldAlert: true, AlertType: "pressure_high", SensorPoint: "P03"})

	_, appErr := env.svc.InjectMock(context.Background(), mockReq("trigger pressure_high"), "10.0.0.9")
	require.Nil(t, appErr)

	require.Len(t, env.cache.stat, 1)
	e := env.cache.stat[testPatient]
	assert.Equal(t, 1, e.abn, "注入帧触发的告警必须计入异常数，否则告警列表与概览自相矛盾")
	assert.Equal(t, 0, e.wear, "佩戴分钟数不累计（countStat=false）")
	assert.Equal(t, float64(0), e.max, "峰值不累计（countStat=false）")
}

// TestT498_CompensationPayloadKeepsSource 内联熔断 → alert:pending 降级这一路同样带来源，
// 否则「补偿通路触发的告警」在库里无从溯源。
func TestT498_CompensationPayloadKeepsSource(t *testing.T) {
	env := newTestEnv()
	env.svc.SetMockIngest(true, newMockStore())
	env.alerts.push(errors.New("alert-service down"), nil)

	_, appErr := env.svc.InjectMock(context.Background(), mockReq("degrade leg"), "10.0.0.9")
	require.Nil(t, appErr, "降级不阻塞注入成功返回（与真实上报同口径）")

	require.Len(t, env.cache.pending, 1)
	assert.Contains(t, env.cache.pending[0], `"ingest_source":"mock"`,
		"降级负载丢了来源印章：alert-service 补偿评估落库的告警会读成未表态")

	// 正对照：负载本身仍是可解析的帧引用（键名与 alert-service consumer.FrameRef 一致）
	var decoded struct {
		Frame struct {
			DeviceID     string    `json:"device_id"`
			PatientID    string    `json:"patient_id"`
			Points       []float64 `json:"points"`
			IngestSource string    `json:"ingest_source"`
		} `json:"frame"`
	}
	require.NoError(t, json.Unmarshal([]byte(env.cache.pending[0]), &decoded))
	assert.Equal(t, testDevice, decoded.Frame.DeviceID)
	assert.Len(t, decoded.Frame.Points, model.PointCount)
	assert.Equal(t, model.IngestMock, decoded.Frame.IngestSource)
}

// TestT498_BusinessValidationRunsBeforeStore 校验序：业务校验全在触库之前，
// 且与真实链路复用同一批函数（同一份 20 点/时间戳/绑定判据）。
func TestT498_BusinessValidationRunsBeforeStore(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*model.MockFrameRequest)
		wantCode int
	}{
		{"缺 device_id", func(r *model.MockFrameRequest) { r.DeviceID = "   " }, model.CodeInvalidParam},
		{"缺 reason", func(r *model.MockFrameRequest) { r.Reason = "" }, model.CodeInvalidParam},
		{"点数不是 20", func(r *model.MockFrameRequest) { r.Points = r.Points[:19] }, model.CodeInvalidParam},
		{"时间戳缺失", func(r *model.MockFrameRequest) { r.Timestamp = 0 }, model.CodeBadTimestamp},
		{"设备未注册", func(r *model.MockFrameRequest) { r.DeviceID = "PRS-NOT-REGISTERED" }, model.CodeDeviceNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv()
			store := newMockStore()
			env.svc.SetMockIngest(true, store)
			req := mockReq("validation")
			tc.mutate(req)

			resp, appErr := env.svc.InjectMock(context.Background(), req, "10.0.0.9")
			require.Nil(t, resp, tc.name)
			require.NotNil(t, appErr, tc.name)
			assert.Equal(t, tc.wantCode, appErr.Code)
			assert.Empty(t, store.calls, "校验未过不得触库（帧与审计同事务，半条都不许写）")
			assert.Equal(t, 0, env.alerts.calls)
		})
	}
}

// TestT498_StoreFailureAbortsTheWholeInjection 落库失败 ⇒ 不写缓存、不评估告警。
// 事务那一半在 repo（审计失败回滚帧），这里钉 service 侧不留下「有告警无帧」的中间态。
func TestT498_StoreFailureAbortsTheWholeInjection(t *testing.T) {
	env := newTestEnv()
	store := newMockStore()
	store.err = errors.New("tx aborted")
	env.svc.SetMockIngest(true, store)

	resp, appErr := env.svc.InjectMock(context.Background(), mockReq("store failure"), "10.0.0.9")
	require.Nil(t, resp)
	require.NotNil(t, appErr)
	assert.Equal(t, model.CodeInternal, appErr.Code)
	assert.Empty(t, env.cache.lastseen)
	assert.Equal(t, 0, env.alerts.calls)
}

// TestT498_DuplicatedIsSurfacedNotHidden 幂等命中（同 (device_id, ts) 已有帧）要显式回给调用方：
// 注入脚本据此知道「这条没新增，只是又留了一次痕」。
func TestT498_DuplicatedIsSurfacedNotHidden(t *testing.T) {
	env := newTestEnv()
	store := newMockStore()
	store.res = repo.MockFrameResult{RecordID: 4200, Duplicated: true, AuditLogID: 9002, ExistingSource: model.IngestReal}
	env.svc.SetMockIngest(true, store)

	resp, appErr := env.svc.InjectMock(context.Background(), mockReq("repeat injection"), "10.0.0.9")
	require.Nil(t, appErr)
	assert.True(t, resp.Duplicated)
	assert.Equal(t, "4200", resp.RecordID, "冲突时回的是挡住本次的那条帧 id")
	assert.Equal(t, int64(9002), resp.AuditLogID)
	assert.Equal(t, model.IngestMock, resp.IngestSource)
}

// TestT498_RealUploadPathIgnoresTheGate 验收 4：开关只作用于注入通路，
// 真实上报端点在开关关/开两种状态下行为一致。
func TestT498_RealUploadPathIgnoresTheGate(t *testing.T) {
	for _, gateOn := range []bool{false, true} {
		env := newTestEnv()
		env.svc.SetMockIngest(gateOn, newMockStore())

		resp, appErr := env.svc.UploadSingle(context.Background(), testDevice, singleReq(fixedNow, pts(2.5)))
		require.Nil(t, appErr, gateOn)
		require.NotNil(t, resp, gateOn)
		require.Len(t, env.alerts.reqs, 1, gateOn)
		assert.Equal(t, model.IngestReal, env.alerts.reqs[0].IngestSource,
			"真实链路盖 real 章，与开关状态无关")
	}
}

// TestT498_EvalRequestKeyIsBackwardCompatible 契约兼容：不带 ingest_source 的旧调用方
// 反序列化成空值（落 NULL），新调用方带值；omitempty 保证老 alert-service 不受影响。
func TestT498_EvalRequestKeyIsBackwardCompatible(t *testing.T) {
	var legacy AlertEvalRequest
	require.NoError(t, json.Unmarshal([]byte(`{"device_id":"D","patient_id":"P","points":[0]}`), &legacy))
	assert.Empty(t, legacy.IngestSource)

	b, err := json.Marshal(&AlertEvalRequest{DeviceID: "D", IngestSource: model.IngestMock})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"ingest_source":"mock"`)

	b, err = json.Marshal(&AlertEvalRequest{DeviceID: "D"})
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(b), "ingest_source"), "空值不得出站（旧 alert-service 的严格解析面）")
}
