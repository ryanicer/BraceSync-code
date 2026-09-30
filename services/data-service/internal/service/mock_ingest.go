// T498 受控 mock 帧注入（开关 + 注入通路）。
//
// 卡面背景：无硬件期 Boss 验收要求触发数据用 mock，但「做好开关，随时切到真实数据」。
// 现状（开工实测，origin/main 5685f3e）：全仓无 mock 数据源开关，造数据只能直接打
// 真实上报端点 POST /api/v1/device/records 灌假帧 ⇒ 与真实采集链路无从区分，也关不掉。
//
// 本文件的三档收口（缺一档都不算「受控」）：
//  1. 进程级：ALLOW_MOCK_INGEST 决定端点可用与否，默认关；APP_ENV=production 时开了直接
//     log.Fatal（cmd/server/main.go），不给「生产起来忘了关」留窗口；
//  2. 请求级：每个请求再查一次 mockEnabled（验收 2「关时返回 403」），
//     所以即便运维只改了配置没重启，语义也不会漂；
//  3. 数据级：注入帧落库带 ingest_source='mock' 印章 + 同事务一行 audit_logs
//     （repo/mock_t498.go），来源可查、动作可追责。
//
// 与真实上报链路的**刻意差异**（每条都是选择，不是遗漏）：
//   - 不吃限流器：限流的职责是防设备侧洪水，注入方是运维脚本；
//     更要紧的是不能借用真实设备的配额 —— 借用会让同 device 的真帧被 20429 拒掉，
//     那是「造数据把真数据打死」，与开关的目的相反。
//   - 不回写 devices.last_report_at：那一列的语义是「设备最后一次真实上报」，
//     写进去会让管理端把离线设备显示成在线（假在线比缺一条时间更难排查）。
//   - 不累计 stat:today 的佩戴分钟数/峰值：概览页的佩戴时长与峰值压力不该被造出来的帧推着涨。
//     走 applyRealtimeCache(..., countStat=false)，只保留 lastseen 与 rt:frame 两写
//     （前者是「佩戴中断」这一类触发的前提，后者给实时页看，下一帧真数据即覆盖）。
//     🔴 例外：abnormal_count 仍会随告警命中 +1（evaluateInline 的公共支路），
//     因为它是 alerts 表行数的对账口径 —— 若这里跳过，注入帧触发的告警会在
//     「告警列表有 1 条 / 概览异常数 0」之间自相矛盾。这条差异登记在卡内待裁。
package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/bracesync/bracesync/services/data-service/internal/model"
	"github.com/bracesync/bracesync/services/data-service/internal/repo"
)

// MockFrameStore 注入落库契约（*repo.RecordRepo 实现）。
// 单列成接口是为了让「帧 + 审计同事务」这件事留在 repo 层，service 不拼 SQL。
type MockFrameStore interface {
	InsertMockFrame(ctx context.Context, in repo.MockFrameInput) (repo.MockFrameResult, error)
}

// SetMockIngest 装配注入通路：enabled = ALLOW_MOCK_INGEST（生产已在 main 里 log.Fatal 挡掉），
// store = 注入落库实现（nil = 未装配真库，端点存在但一律 500，不静默假成功）。
func (s *RecordService) SetMockIngest(enabled bool, store MockFrameStore) {
	s.mockEnabled = enabled
	s.mocks = store
	if enabled {
		log.Warn().Msg("T498 mock ingest enabled: POST /internal/mock-frame will accept fabricated frames")
	}
}

// MockIngestEnabled 供测试与 /healthz 类只读探针回读当前门控状态。
func (s *RecordService) MockIngestEnabled() bool { return s.mockEnabled }

// InjectMock 注入一帧假数据。校验序与 UploadSingle 同族（门禁在最前，口径同 T404 对
// assertDeviceWriteRole 的位置要求），业务校验全部复用真实链路的同一批函数，
// 免得「注入的帧」与「真实的帧」在库里长得不一样。
func (s *RecordService) InjectMock(ctx context.Context, req *model.MockFrameRequest, ip string) (*model.MockFrameResponse, *model.AppError) {
	if !s.mockEnabled {
		// 门禁判定在方法体最前：未开开关时连设备存在性都不探测，避免端点变成一条
		// 可以试探 device_id 是否注册的旁路面。
		return nil, model.ErrForbidden("mock ingest is disabled (set ALLOW_MOCK_INGEST=true outside production)")
	}
	if s.mocks == nil {
		return nil, model.ErrInternal("mock ingest store not configured")
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		return nil, model.ErrInvalidParam("device_id is required for mock injection")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, model.ErrInvalidParam("reason is required (it is the only audit answer to why this frame was fabricated)")
	}
	if len(req.Points) != model.PointCount {
		return nil, model.ErrInvalidParam("points length %d != %d", len(req.Points), model.PointCount)
	}
	ts, appErr := validateTimestamp(req.Timestamp, s.now())
	if appErr != nil {
		return nil, appErr
	}
	patientID, appErr := s.requireBinding(ctx, deviceID)
	if appErr != nil {
		return nil, appErr
	}

	frame := toPendingFrame(ts, req.Points, req.Battery, req.FaultCode)
	res, err := s.mocks.InsertMockFrame(ctx, repo.MockFrameInput{
		DeviceID:  deviceID,
		PatientID: patientID,
		Frame:     frame,
		Operator:  strings.TrimSpace(req.Operator),
		Reason:    strings.TrimSpace(req.Reason),
		IP:        ip,
	})
	if err != nil {
		return nil, s.mapStoreError(err)
	}

	now := s.now()
	// countStat=false：见文件头「不累计 stat:today」。
	if appErr := s.applyRealtimeCache(ctx, deviceID, patientID, frame, now, false); appErr != nil {
		return nil, appErr
	}
	// 告警评估与真实链路同源（内联 100ms 熔断 → alert:pending 降级），
	// 差别只有来源字段：AlertEvalRequest 带上 mock，一路传到 alerts.ingest_source。
	s.evaluateInline(ctx, deviceID, patientID, frame, now, model.IngestMock)

	return &model.MockFrameResponse{
		RecordID:     strconv.FormatInt(res.RecordID, 10),
		Duplicated:   res.Duplicated,
		IngestSource: model.IngestMock,
		AuditLogID:   res.AuditLogID,
	}, nil
}
