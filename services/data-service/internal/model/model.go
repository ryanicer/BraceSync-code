// Package model data-service 领域模型与 API DTO 定义
//
// 对齐：docs/ §4（设备上报接口）
//
//	docs/ / getPatientRealtime）
//	docs/ §3.5 / §4.3 / §4.7
package model

import (
	"fmt"
	"math"
	"time"
)

// PointCount 压力采集点位数（P01–P20）
const PointCount = 20

// 时间合法性约束（device-protocol.md §4.1）：
// 云端校验 timestamp 须落在 [2026-01-01T00:00:00Z, 服务器时间+1h]，超出返回 20402
var (
	// MinValidTime 合法 timestamp 下界（防时钟故障污染按月分区表）
	MinValidTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// MaxFutureDrift 合法 timestamp 上界相对服务器时间的偏移
	MaxFutureDrift = time.Hour
	// BackfillMaxAge 补传帧最大年龄（设备离线缓存上限 7 天，协议 §6）
	BackfillMaxAge = 7 * 24 * time.Hour
)

// WearingThresholdN 佩戴判定阈值默认值（PRD §8.1：帧 max_pressure > 阈值视为佩戴帧）。
// 🔴 T203 ÷10 后兜底值 0.05；生产生效值走 sys_configs `wearing_pressure_threshold`。
const WearingThresholdN = 0.05

// PressureThresholds 压力量纲阈值（PRD §7D.12：可配置参数，sys_configs 驱动，不硬编码）。
// T203 ÷10 后量纲：所有数值已同步下调。
type PressureThresholds struct {
	// WearingN 佩戴判定阈值（sys_configs: wearing_pressure_threshold，T203 后默认 0.05）
	WearingN float64
	// HeatmapMaxN 热力图色阶上界（sys_configs: heatmap_max_n，T203 后默认 6）
	HeatmapMaxN float64
	// PressureHighN 压力偏高告警阈值（sys_configs: threshold_pressure_high，T203 后默认 5）
	PressureHighN float64
}

// DefaultPressureThresholds T203 ÷10 后默认口径（配置缺失/非法时兜底）
func DefaultPressureThresholds() PressureThresholds {
	return PressureThresholds{
		WearingN:      WearingThresholdN,
		HeatmapMaxN:   6.0, // T203: 60 → 6
		PressureHighN: 5.0, // T203: 45 → 5
	}
}

// MnPerN 设备上报 mN（毫牛）→ N 换算分母（PRD §7A.2 数据单位口径，T173 D1 权威裁定：
// 入口 ÷1000 归一为 N；原「N×100 定点 / ÷100」口径作废）。入口归一点：service.toPendingFrame。
const MnPerN = 1000.0

// cstZone 业务切日时区（架构 §3.5：业务切日/定时任务按 Asia/Shanghai）。
// 用固定偏移避免容器缺 tzdata 导致 LoadLocation 失败。
var cstZone = time.FixedZone("Asia/Shanghai", 8*3600)

// CSTZone 返回业务切日时区（Asia/Shanghai，UTC+8）
func CSTZone() *time.Location { return cstZone }

// ─────────────────────────────────────────────────────────────
// 错误码（device-protocol.md §4.4 设备域 2xxxx；架构 §3.5 分段）
// ─────────────────────────────────────────────────────────────

const (
	CodeOK           = 0
	CodeInvalidParam = 20400 // 参数非法（含 points 长度 != 20、批量 >100 帧）
	CodeBadTimestamp = 20402 // timestamp 超出合法区间（含补传 >7 天时间窗）
	// CodeDeviceIDMismatch 原为 20403，与 device-service 的 CodeForbidden 撞值（同数字不同契约：
	// 那边 HTTP 403 越权、这边 HTTP 400 上报身份不一致）。T437 按 T402 甲-1 形状收为本域
	// 「域号 3 + 0 + HTTP 状态三位」，20403 此后仅设备域持有（见 code_lock_t437_test.go 的跨服务扫描）。
	CodeDeviceIDMismatch = 30400 // X-Device-Id 头与请求体 device_id 不一致
	CodeDeviceNotFound   = 20404 // device_id 未注册
	CodeDeviceUnbound    = 20409 // 设备未绑定患者
	CodeRateLimited      = 20429 // 限流（设备按 Retry-After 退避）
	CodePatientNotFound  = 10404 // 患者档案不存在（档案 owner 是 user-service：同其 CodeNotFound / device-service CodeUserResNotFound 的 10404；T340）
	CodeQueryParam       = 30001 // 数据域：查询参数非法
	// CodeForbidden 按 T402 甲-1 收为「域号 3 + 0 + HTTP 状态三位 403」。旧值是裸 HTTP 状态数字
	// 403，与 user 10403 / device 20403 / msg 50403 的形状不一致（那三域早按本规则取值）。
	// 本域与设备域撞值的另一格 CodeDeviceIDMismatch 已由 T437 一并收为 30400，两域不再共用 20403。
	CodeForbidden = 30403 // 越权访问（水平越权 / 无数据权限），HTTP 403
	CodeInternal  = 90001 // 系统内部错误
)

// AppError 业务错误：携带统一响应 code 与建议 HTTP 状态
type AppError struct {
	Code          int    `json:"code"`
	Message       string `json:"message"`
	HTTPStatus    int    `json:"-"`
	RetryAfterSec int    `json:"-"` // 仅 20429 使用
}

func (e *AppError) Error() string { return fmt.Sprintf("code=%d: %s", e.Code, e.Message) }

func newAppError(code, httpStatus int, format string, args ...any) *AppError {
	return &AppError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

func ErrInvalidParam(format string, args ...any) *AppError {
	return newAppError(CodeInvalidParam, 400, format, args...)
}

func ErrBadTimestamp(format string, args ...any) *AppError {
	return newAppError(CodeBadTimestamp, 400, format, args...)
}

func ErrDeviceIDMismatch() *AppError {
	return newAppError(CodeDeviceIDMismatch, 400, "X-Device-Id header does not match body device_id")
}

func ErrDeviceNotFound(deviceID string) *AppError {
	return newAppError(CodeDeviceNotFound, 404, "device %q not registered", deviceID)
}

// ErrPatientNotFound T340：查无此人必须与「有此人但暂无数据」在 HTTP 面上可区分，故状态位显式 404。
func ErrPatientNotFound(patientID string) *AppError {
	return newAppError(CodePatientNotFound, 404, "patient %q not found", patientID)
}

func ErrDeviceUnbound(deviceID string) *AppError {
	return newAppError(CodeDeviceUnbound, 400, "device %q not bound to any patient", deviceID)
}

func ErrRateLimited(retryAfterSec int) *AppError {
	e := newAppError(CodeRateLimited, 429, "rate limited")
	e.RetryAfterSec = retryAfterSec
	return e
}

func ErrQueryParam(format string, args ...any) *AppError {
	return newAppError(CodeQueryParam, 400, format, args...)
}

func ErrForbidden(format string, args ...any) *AppError {
	return newAppError(CodeForbidden, 403, format, args...)
}

func ErrInternal(format string, args ...any) *AppError {
	return newAppError(CodeInternal, 500, format, args...)
}

// ─────────────────────────────────────────────────────────────
// 设备上报 DTO（device-protocol.md §4.1 / §4.2）
// ─────────────────────────────────────────────────────────────

// SingleFrameRequest 单帧实时上报请求体
type SingleFrameRequest struct {
	// DeviceID 仅为 gateway 身份头未就绪时的联调回退；
	// 生产以 gateway 注入的 X-Device-Id 为准（验签归 gateway，本服务不越权）。
	DeviceID  string `json:"device_id,omitempty"`
	Timestamp int64  `json:"timestamp"` // 采集时刻，Unix 秒
	// Points 20 点压力值（P01–P20 顺序）。
	// 🔴 单位 = mN（毫牛，PRD §7A.2 数据单位口径，T173 D1 权威裁定：设备上报 mN）；
	// 入口统一 ÷1000 归一为 N 后落库/判定（toPendingFrame）。⚠️ docs/api/device-protocol.md
	// 仍为旧「N×100」量纲，冲突已上报待订正，以本注释口径为准。
	Points    []float64 `json:"points"`
	Battery   int       `json:"battery"`
	Firmware  string    `json:"firmware"`
	WifiRSSI  *int      `json:"wifi_rssi,omitempty"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchFrame 补传批次中的单帧
type BatchFrame struct {
	Timestamp int64     `json:"timestamp"`
	Points    []float64 `json:"points"`
	Battery   int       `json:"battery"`
	FaultCode int       `json:"fault_code,omitempty"`
}

// BatchRequest 批量补传请求体（单请求 ≤100 帧）
type BatchRequest struct {
	DeviceID string       `json:"device_id,omitempty"` // 同 SingleFrameRequest.DeviceID
	Frames   []BatchFrame `json:"frames"`
	Firmware string       `json:"firmware"`
}

// MaxBatchFrames 单请求补传帧数上限（协议 §4.2）
const MaxBatchFrames = 100

// DeviceConfig 云端配置捎带下发（协议 §4.1：唯一的配置下发通道）
type DeviceConfig struct {
	IntervalMinutes int `json:"interval_minutes"`
	ConfigVersion   int `json:"config_version"`
}

// SingleFrameResponse 单帧上报成功响应 data
type SingleFrameResponse struct {
	RecordID   string       `json:"record_id"`
	Duplicated bool         `json:"duplicated"` // 幂等命中：重复帧未重复落库
	Config     DeviceConfig `json:"config"`
}

// RejectedFrame 补传批次中业务校验失败的帧（协议 §4.2 部分成功语义）
type RejectedFrame struct {
	Index  int    `json:"index"`
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

// BatchResponse 批量补传成功响应 data
type BatchResponse struct {
	Accepted   int             `json:"accepted"`
	Duplicated int             `json:"duplicated"`
	Rejected   []RejectedFrame `json:"rejected"`
	Config     DeviceConfig    `json:"config"`
}

// ─────────────────────────────────────────────────────────────
// T498 受控 mock 注入（POST /internal/mock-frame）
// ─────────────────────────────────────────────────────────────

// 来源印章取值（pressure_records.ingest_source / alerts.ingest_source，迁移 000033）。
// 库侧 CHECK 只放行这两个值 + NULL；NULL = 本卡之前的存量行与 seed 手工示例帧，
// 刻意不给列 DEFAULT，也不回填（000033 up.sql 里写了理由）。
const (
	IngestReal = "real" // 设备真实上报链路（两条上报端点 + alert:pending 补偿）
	IngestMock = "mock" // 受控注入端点写入
)

// MockFrameRequest 注入请求体。字段形状对齐 SingleFrameRequest（points 同为 mN 量纲，
// 同样经 toPendingFrame ÷1000 归一），差别有两处：
//   - DeviceID 必填：注入端点不经 gateway，没有 X-Device-Id 身份头，目标设备只能显式给；
//   - Reason 必填：它同时是 audit_logs.detail 里「为什么要造这帧」的唯一答案。
type MockFrameRequest struct {
	DeviceID  string    `json:"device_id"`
	Timestamp int64     `json:"timestamp"` // 采集时刻，Unix 秒，与真实上报同一合法区间校验
	Points    []float64 `json:"points"`
	Battery   int       `json:"battery"`
	FaultCode int       `json:"fault_code,omitempty"`
	Reason    string    `json:"reason"`
	// Operator 注入方自报的操作者标识。/internal/* 不经 gateway，服务端无从验证身份，
	// 只能原样落进审计 detail.operator；客观那一半是同事务的 ip 列。
	Operator string `json:"operator,omitempty"`
}

// MockFrameResponse 注入成功响应 data
type MockFrameResponse struct {
	// RecordID 本次注入帧的 id；Duplicated=true 时是**冲突那条既有帧**的 id
	// （可能是真帧，也可能是既往注入帧 —— 它的来源落在审计 detail 的
	// conflicting_ingest_source，响应体不带，见 repo.MockFrameResult 的注释）。
	RecordID     string `json:"record_id"`
	Duplicated   bool   `json:"duplicated"`    // 幂等命中：本次未新增帧，只补了一行审计
	IngestSource string `json:"ingest_source"` // 恒为 IngestMock，回显给调用方以便对拍
	AuditLogID   int64  `json:"audit_log_id"`  // 与注入帧同事务写入的 audit_logs.log_id
}

// ─────────────────────────────────────────────────────────────
// 领域实体与查询 DTO（对齐 shared-types PressureRecord / SensorPoint）
// ─────────────────────────────────────────────────────────────

// PressureRecord pressure_records 表行
type PressureRecord struct {
	RecordID    int64
	DeviceID    string
	PatientID   string
	Ts          time.Time
	Points      [PointCount]float32
	MaxPressure float32 // DB 生成列
	UploadTime  time.Time
}

// MaxPoint 返回最大压力点位编号（如 "P06"）；并列取首个
func (r *PressureRecord) MaxPoint() string {
	idx := 0
	for i := 1; i < PointCount; i++ {
		if r.Points[i] > r.Points[idx] {
			idx = i
		}
	}
	return PointID(idx)
}

// MaxPointValue 20 点里的最大值，与库侧生成列 max_pressure = greatest(p01..p20) 同口径。
// T366：Redis 回退路径没有 DB 行，DTO 的 maxPressure 由本函数按同一口径现算，
// 保证「实时帧」与「落库帧」的佩戴帧判据列可比（传 raw 点，不传校准后的点）。
func MaxPointValue(points [PointCount]float32) float32 {
	m := points[0]
	for i := 1; i < PointCount; i++ {
		if points[i] > m {
			m = points[i]
		}
	}
	return m
}

// PointID 点位下标（0 起）转点位编号（P01–P20）
func PointID(i int) string { return fmt.Sprintf("P%02d", i+1) }

// PointLabel 点位下标转行优先标签（PRD §8.3：Pn = RrCc，4 行 5 列）
func PointLabel(i int) (row, col int, label string) {
	row, col = i/5+1, i%5+1
	return row, col, fmt.Sprintf("R%dC%d", row, col)
}

// 压力展示分级阈值（仅用于前端 status 渲染，非告警阈值）：
// warning/critical 分界由 PressureThresholds.PressureHighN 派生（0.75×/1.0×，与告警引擎 pressure_high 同源）。
func pressureWarningN(th PressureThresholds) float64  { return 0.75 * th.PressureHighN }
func pressureCriticalN(th PressureThresholds) float64 { return th.PressureHighN }

// PointStatus 依据压力值返回展示状态（阈值可配置，T173）
func PointStatus(v float32, th PressureThresholds) string {
	switch {
	case float64(v) >= pressureCriticalN(th):
		return "critical"
	case float64(v) >= pressureWarningN(th):
		return "warning"
	default:
		return "normal"
	}
}

// SensorPoint 对齐 shared-types SensorPoint
type SensorPoint struct {
	PointID       string  `json:"pointId"`
	Row           int     `json:"row"`
	Col           int     `json:"col"`
	Label         string  `json:"label"`
	PressureValue float64 `json:"pressureValue"`
	Status        string  `json:"status"`
	// PressureKpa T643 A 路展示档：与热力图同源走 KpaFromN 在本响应内派生，不落库、不改 Status 分档
	// （分档恒按 PressureValue，N）。nil = 面积未配置/非法，前端显示「--」，不用默认值补位。
	PressureKpa *int `json:"pressureKpa"`
}

// BuildSensorPoints 将 20 点已校准值转为前端 SensorPoint 数组（阈值可配置，T173）；
// areaCm2 为设备有效受压面积（devices.contact_area_cm2，与 BuildHeatmap 同一枚来源），用于派生逐点 kPa 展示档。
func BuildSensorPoints(points [PointCount]float32, th PressureThresholds, areaCm2 *float64) []SensorPoint {
	out := make([]SensorPoint, PointCount)
	for i, v := range points {
		row, col, label := PointLabel(i)
		out[i] = SensorPoint{
			PointID:       PointID(i),
			Row:           row,
			Col:           col,
			Label:         label,
			PressureValue: float64(v),
			Status:        PointStatus(v, th),
			PressureKpa:   KpaFromN(float64(v), areaCm2),
		}
	}
	return out
}

// HeatmapMaxN 热力图色阶上界默认值（T203 ÷10 后 6N）。
const HeatmapMaxN = 6.0

// KpaFromN 展示层 N 到 kPa 的换算（T508，PRD §7A.2.1 三）。三步序，逐字对齐稿面图：
// ①归零（负值按 0，零点漂移不折算成负压力）→ ②kPa = N / area(cm²) × 10 →
// ③取整 round half up（k 非负，math.Round 的四舍五入即 half up）。
// 边界（裁定 b）：0 N 仍 0，真非零最小 1 kPa。
//
// 返回 nil = 不可换算，前端显示「--」，这是 fail-closed 而不是 0：
// 面积缺失 / ≤0 / NaN / Inf、以及被除后仍非有限或超出可显示整数范围。
// 0.64 cm² 只是后台配置项的默认值，**不在这里补位** —— 库里没配就是没配。
//
// 只在展示层用：落库、告警判档、聚合恒为 N（PRD 裁定四）。
func KpaFromN(n float64, areaCm2 *float64) *int {
	if areaCm2 == nil {
		return nil
	}
	a := *areaCm2
	if !(a > 0) || math.IsInf(a, 0) { // NaN > 0 恒假，一并挡掉
		return nil
	}
	// 不用 math.Max：它对 (0, NaN) 返回 0，会把 NaN 洗成合法零值
	zeroed := n
	if zeroed < 0 {
		zeroed = 0
	}
	k := zeroed / a * 10
	if math.IsNaN(k) || math.IsInf(k, 0) {
		return nil
	}
	if k == 0 {
		zero := 0
		return &zero
	}
	// 面积下界不设（写侧只挡非正数），极端配置会把结果推出 int32；溢出成负数是假读数，不如「--」
	if k > math.MaxInt32 {
		return nil
	}
	v := int(math.Max(1, math.Round(k)))
	return &v
}

// HeatmapPoint 热力图 20 点单格（RealtimeSnapshot.pressureHeatmap 元素）
type HeatmapPoint struct {
	PointID       string  `json:"pointId"`       // P01–P20
	Row           int     `json:"row"`           // 1–4
	Col           int     `json:"col"`           // 1–5
	Label         string  `json:"label"`         // RrCc，如 R3C2
	PressureValue float64 `json:"pressureValue"` // 压力 N
	IsMax         bool    `json:"isMax"`         // 是否为当前最大点（前端 ★ + 脉冲用）
	// PressureKpa T508 展示档：同一响应内派生，不落库；nil = 面积未配置/非法，前端显示「--」。
	// IsMax 与色阶分档仍按 PressureValue（N）判，kPa 只改数值文本。
	PressureKpa *int `json:"pressureKpa"`
}

// BuildHeatmap 将 20 点压力值构造为 HeatmapPoint 数组（唯一 IsMax 标记）；
// areaCm2 为设备有效受压面积（devices.contact_area_cm2，T508），用于派生每点 kPa 展示值。
func BuildHeatmap(points [PointCount]float32, areaCm2 *float64) []HeatmapPoint {
	out := make([]HeatmapPoint, PointCount)
	maxIdx, maxV := 0, float32(-1)
	for i, v := range points {
		row, col, label := PointLabel(i)
		out[i] = HeatmapPoint{
			PointID:       PointID(i),
			Row:           row,
			Col:           col,
			Label:         label,
			PressureValue: float64(v),
			PressureKpa:   KpaFromN(float64(v), areaCm2),
			IsMax:         false,
		}
		if v > maxV {
			maxIdx, maxV = i, v
		}
	}
	if maxV > 0 {
		out[maxIdx].IsMax = true
	}
	return out
}

// PressureRecordDTO 对齐 shared-types PressureRecord（camelCase）
type PressureRecordDTO struct {
	RecordID   string        `json:"recordId"`
	DeviceID   string        `json:"deviceId"`
	PatientID  string        `json:"patientId"`
	Timestamp  string        `json:"timestamp"` // ISO 8601 UTC
	Points     []SensorPoint `json:"points"`
	UploadTime string        `json:"uploadTime"`
	// Calibrated 是否已应用基线校准（T173 读侧派生：false = 缺基线，值为 ÷1000 后 raw）。
	// 读取侧在减偏移后设置；DTO 构造默认 false，不得静默当已校准。
	Calibrated bool `json:"calibrated"`
	// MaxPressure 库侧生成列 max_pressure 的**原始**值（= raw p01..p20 取最大，迁移 000001:154-156）。
	// T366：与 Points **不同层**——Points 是基线校准（减偏移）后的值，本列不做校准。
	// 日聚合的佩戴帧判据用的正是这一列（rollup_repo.go aggregateDateSQL），
	// 缺它则验收侧只能拿 max(points) 当代理，而代理与判据不是同一件东西（T352 交件 §三实测）。
	// T620 桶行例外：带 interval 的降采样查询里本列与 Points 同为**桶内均值**（不是单帧最大值，
	// 仍不做校准），此时 Timestamp 是桶起点、recordId 是桶起点秒。
	MaxPressure float32 `json:"maxPressure"`
}

// ToDTO 领域实体 → 前端 DTO（阈值可配置，T173；Calibrated 由读取侧按校准结果回填；
// areaCm2 供逐点 kPa 展示档派生，T643 A 路）
func (r *PressureRecord) ToDTO(th PressureThresholds, areaCm2 *float64) PressureRecordDTO {
	return PressureRecordDTO{
		RecordID:    fmt.Sprintf("%d", r.RecordID),
		DeviceID:    r.DeviceID,
		PatientID:   r.PatientID,
		Timestamp:   r.Ts.UTC().Format(time.RFC3339),
		Points:      BuildSensorPoints(r.Points, th, areaCm2),
		UploadTime:  r.UploadTime.UTC().Format(time.RFC3339),
		MaxPressure: r.MaxPressure,
	}
}

// HistoryPage 对齐 shared-types PaginatedResponse<PressureRecord>
type HistoryPage struct {
	List     []PressureRecordDTO `json:"list"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

// ─────────────────────────────────────────────────────────────
// T021：聚合与归档领域模型
// ─────────────────────────────────────────────────────────────

// WearTargetMinutes 默认每日佩戴目标时长（PRD §7A.11：22h = 1320min）
const WearTargetMinutes = 22 * 60

// MaxWearMinutesPerDay 单患者每日佩戴分钟物理上限（24h = 1440min）
// 用于 rollup clamp + Dashboard SQL 防御（T083：防止设备高频上报导致 wear_minutes 异常放大）
const MaxWearMinutesPerDay = 24 * 60

// DailyWearStats daily_wear_stats 表行（架构 §4.4 日聚合）
type DailyWearStats struct {
	PatientID     string
	StatDate      time.Time // 业务时区（Asia/Shanghai）切日，存储为 DATE
	WearMinutes   int
	AvgPressure   float32
	MaxPressure   float32
	MaxPoint      string // P01..P20
	FrameCount    int
	AbnormalCount int
	UpdatedAt     time.Time
	// T366 聚合印章（迁移 000027）：只由本服务聚合任务的 UPSERT 写，读侧据此判来源。
	// 两者同生同灭；任一为 nil = 该行无印章 = 不是聚合任务写的（seed 示例行 / 印章上线前的历史行）。
	AggregatedAt      *time.Time // 本次聚合发生的时刻（UTC）
	WearingThresholdN *float64   // 本次聚合实际生效的佩戴帧判定阈值（N）
}

// HasRollupStamp 行上是否带聚合印章（两列都在才算；半枚按无印章处理）
func (s *DailyWearStats) HasRollupStamp() bool {
	return s.AggregatedAt != nil && s.WearingThresholdN != nil
}

// 日聚合行来源（T366，读侧派生，非表列）
const (
	// ProvenanceRollup 有聚合印章 ⇒ 本服务聚合任务写入，口径可信
	ProvenanceRollup = "rollup"
	// ProvenanceCorroborated 无印章，但行上声明的 frame_count 与该患者该 CST 日的
	// pressure_records 明细帧数一致 ⇒ 数字被明细佐证，来源不明但不假
	ProvenanceCorroborated = "corroborated"
	// ProvenanceUnsupported 无印章且未被明细佐证（帧数不符，或明细根本不可查）
	// ⇒ 示例行 / 手工行 / 明细已不在库，不得当作聚合口径证据
	ProvenanceUnsupported = "unsupported"
)

// 日聚合行的口径代次（T411，读侧派生，非表列）——回答「这一行的 wearMinutes/avgPressure
// 是哪一代算法算出来的、能不能由现存明细独立复算」。
//
// 🔴 与 provenance 是两个问题：provenance 问「这行是谁写的、帧数对不对得上」，
// 代次问「这行的数值口径属于哪一代」。corroborated 不等于代次已知，
// 七值语义见下，消费方不得把 ambiguous / unmatched / no_detail / unknown 当成「已归属现口径」。
const (
	// GenSealed 带聚合印章 ⇒ 由 T366 之后的聚合任务写入，而那时代码已是现口径（T352 早于 T366）。
	// 这一档靠印章推定，不依赖复算结果。
	GenSealed = "current_by_seal"
	// GenRecomputedCurrent 无印章，复算只对上现口径（跨度分钟 + 佩戴帧 20 点全点均值）
	GenRecomputedCurrent = "current_recomputed"
	// GenRecomputedLegacy 无印章，复算只对上老口径（佩戴帧数 × 采集间隔 + AVG(max_pressure) 全帧）
	GenRecomputedLegacy = "legacy_recomputed"
	// GenAmbiguous 无印章，两代算出的分钟与均值都相同 ⇒ 单凭这两个字段不可区分
	// （例：佩戴帧的跨度折算恰等于「帧数 × 采集间隔」）
	GenAmbiguous = "ambiguous"
	// GenUnmatched 无印章且**有明细**，但两代都对不上 ⇒ 代次不可归属，
	// 这是复算判出的红：行数值并非由现存明细按这两代任一口径算出
	// （也可能该日实际阈值/间隔不是本次假设值，假设值随行下发供复核）
	GenUnmatched = "unmatched"
	// GenNoDetail 无印章，查明细确实 0 帧（明细已不在库 / 该日无上报）⇒ 复算没有输入，不可归属。
	// 与 GenUnmatched 分开：后者是「有输入且算不符」，这一档是「无输入」，不得混判成数据缺陷。
	GenNoDetail = "no_detail"
	// GenUnknown 明细不可查（佐证源未注入 / 查询失败）⇒ 复算根本没做，不得当成以上任何一种
	GenUnknown = "unknown"
)

// AvgPressureToleranceRel 复算日均的相对容差：表列是 real（float32，约 7 位有效数字），
// SQL 的 AVG 以 float8 算完再窄化，逐位相等做不到，故按相对量级判定；
// 两代日均的实测差在 1% 量级以上（老口径取帧峰值均值，系统性偏高），容差不会把两代并成一档。
const AvgPressureToleranceRel = 1e-4

// AvgPressureToleranceAbs 零值附近的绝对兜底（避免 0 与 1e-9 被判不等）
const AvgPressureToleranceAbs = 1e-6

// WearGenerationCheck T411：一行的复算现场（代次判定用到的全部输入与两代候选值）。
// 消费方据此可自行核对判定过程，而不是只接受一个结论字符串。
// 整块为 nil = 未做复算（GenUnknown），与「做了但两代都不符」（GenUnmatched）是两回事。
type WearGenerationCheck struct {
	// AssumedWearingThresholdN 本次复算用的佩戴阈值（N）：有印章时取行上印章值，
	// 无印章时取当前 sys_configs 值（历史行真实阈值不可知，故这只是假设，不是事实）。
	AssumedWearingThresholdN float64 `json:"assumedWearingThresholdN"`
	// AssumedIntervalMinutes 老口径候选用的采集间隔（分钟，sys_configs collect_interval_minutes）。
	AssumedIntervalMinutes int `json:"assumedIntervalMinutes"`
	// DetailFrames 现存明细帧数（该患者该 CST 日）
	DetailFrames int `json:"detailFrames"`
	// FrameCountMatchesDetail 行上声明的 frameCount 与明细帧数是否相等（T366 判据，原样复述）
	FrameCountMatchesDetail bool `json:"frameCountMatchesDetail"`
	// WearingFramesAtAssumedN 按假设阈值判出的佩戴帧数（两代分钟候选共同依赖它）
	WearingFramesAtAssumedN int `json:"wearingFramesAtAssumedN"`
	// ExpectedWearMinutesCurrent 现口径候选：跨度 + 一个实测间隔，封顶 1440
	ExpectedWearMinutesCurrent int `json:"expectedWearMinutesCurrent"`
	// ExpectedWearMinutesLegacy 老口径候选：佩戴帧数 × 采集间隔，封顶 1440
	ExpectedWearMinutesLegacy int `json:"expectedWearMinutesLegacy"`
	// ExpectedAvgPressureCurrent 现口径候选：佩戴帧 20 点全点均值
	ExpectedAvgPressureCurrent float32 `json:"expectedAvgPressureCurrent"`
	// ExpectedAvgPressureLegacy 老口径候选：全部帧的 max_pressure 均值（无佩戴过滤）
	ExpectedAvgPressureLegacy float32 `json:"expectedAvgPressureLegacy"`
	// MatchCurrent 行的 wearMinutes 与 avgPressure 同时对上现口径候选
	MatchCurrent bool `json:"matchCurrent"`
	// MatchLegacy 行的 wearMinutes 与 avgPressure 同时对上老口径候选
	MatchLegacy bool `json:"matchLegacy"`
}

// DailyWearDayDTO 患者日佩戴聚合响应（对齐 daily_wear_stats 表列 + camelCase 契约）
type DailyWearDayDTO struct {
	Date          string  `json:"date"`          // YYYY-MM-DD（Asia/Shanghai 切日）
	WearMinutes   int     `json:"wearMinutes"`   // 佩戴分钟数
	AvgPressure   float32 `json:"avgPressure"`   // 日均压力
	MaxPressure   float32 `json:"maxPressure"`   // 日最大压力
	MaxPoint      string  `json:"maxPoint"`      // 最大点位（P01..P20，空串兜底）
	FrameCount    int     `json:"frameCount"`    // 日帧总数
	AbnormalCount int     `json:"abnormalCount"` // 日异常/告警数
	// AvgPressureKpa / MaxPressureKpa T643 A 路展示档：与上面两枚 N 值同源于本行，读接口层用
	// KpaFromN 派生（分母 devices.contact_area_cm2），不落库、不改聚合与告警判档（PRD 裁定四）。
	// nil（JSON null）= 面积未配置或非法，前端显示「--」，不退 0 也不用默认 0.64 补位。
	AvgPressureKpa *int `json:"avgPressureKpa"`
	MaxPressureKpa *int `json:"maxPressureKpa"`
	// ── T366 可解释性四字段（示例行与聚合行在读接口层可辨 + 可独立复算）──
	Provenance string `json:"provenance"` // rollup | corroborated | unsupported
	// DetailFrameCount 该患者该 CST 日 pressure_records 的**实际**明细帧数。
	// 聚合行（rollup）与它恒等；不等即说明这一行不是从现存明细算出来的。
	// nil（JSON null）= 明细不可查，区别于 0 = 查明细确实没有帧。
	DetailFrameCount *int `json:"detailFrameCount"`
	// AggregatedAt 聚合时刻（RFC3339 UTC）。nil（JSON null）= 无聚合印章，
	// 不得退化成空串或零值时间（T361 同族教训：未知要用 null 表达）。
	AggregatedAt *string `json:"aggregatedAt"`
	// WearingThresholdN 本行聚合实际生效的佩戴帧阈值（N），复算日均/佩戴分钟用同一个值。
	// nil（JSON null）= 无印章 ⇒ 复算者需自行取 sys_configs.wearing_pressure_threshold。
	WearingThresholdN *float64 `json:"wearingThresholdN"`
	// ── T411 口径代次（回答 T366 三值答不了的那一半：这一行的数值是哪一代算的）──
	// WearGeneration 七值枚举，见 Gen* 常量组。corroborated 行的代次可以仍是 unknown/no_detail/unmatched，
	// 这不是矛盾：帧数对得上不代表数值口径可归属。
	WearGeneration string `json:"wearGeneration"`
	// WearRecompute 复算现场（假设值 + 两代候选值 + 命中情况）；nil（JSON null）= 未做复算。
	WearRecompute *WearGenerationCheck `json:"wearRecompute"`
}

// WearMinutesLegacy 老口径（T352 之前）佩戴分钟 = 佩戴帧数 × 采集间隔，封顶物理日。
// 只用于读侧「这一行是不是老口径算出来的」反证，写侧已不再使用该折算。
func WearMinutesLegacy(wearingFrames, intervalMinutes int) int {
	return min(wearingFrames*intervalMinutes, MaxWearMinutesPerDay)
}

// AvgPressureMatches 日均压力复算是否对上（real 列窄化带来的位差按相对容差吸收）
func AvgPressureMatches(stored, expected float32) bool {
	diff := float64(stored) - float64(expected)
	if diff < 0 {
		diff = -diff
	}
	return diff <= math.Max(float64(expected)*AvgPressureToleranceRel, AvgPressureToleranceAbs)
}

// HealthReport health_reports 表行（PRD §7A.11 健康报告）
type HealthReport struct {
	ReportID           int64
	PatientID          string
	ReportType         string // "weekly" | "monthly"
	PeriodStart        time.Time
	PeriodEnd          time.Time
	WearComplianceRate float64 // 达标率 %
	AvgPressure        float32
	TrendJudgment      string // "up" | "flat" | "down"
	Suggestion         string
	GenerateTime       time.Time
}

// ArchiveStatus archive_status 表行（T021 冷归档三步走状态追踪）
type ArchiveStatus struct {
	PartitionName string
	PeriodYear    int
	PeriodMonth   int
	Status        string // pending | exported | verified | cleaned | failed
	RowCount      int64
	Checksum      string
	ExportPath    string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RealtimeSnapshot 对齐 api-contracts.ts getPatientRealtime 返回结构
type RealtimeSnapshot struct {
	DeviceID        string              `json:"deviceId"` // 当前绑定设备号（devices.patient_id 反查）；未绑定为空串
	Status          string              `json:"status"`   // online / offline / abnormal
	TodayHours      float64             `json:"todayHours"`
	MaxPressure     float64             `json:"maxPressure"`
	MaxPoint        string              `json:"maxPoint"`
	Battery         int                 `json:"battery"` // 最新帧电量 0-100，来自 Redis rt:frame
	Events          int                 `json:"events"`  // 今日异常值
	PressureRecords []PressureRecordDTO `json:"pressureRecords"`
	Alerts          []any               `json:"alerts"`          // 今日告警摘要，明细由 alert-service 提供
	PressureHeatmap []HeatmapPoint      `json:"pressureHeatmap"` // 热力图 20 点；T325：只在有真实帧时下发，无帧为空数组（不再 seed 兜底）
	// HeatmapMaxN / PressureHighN 展示口径（T296）：与告警引擎同源的可配置阈值，
	// 供前端色阶上界与分级渲染用——写死常量会与 sys_configs 漂移（T203 前端即因写死 60/45 滞后一个量级）。
	HeatmapMaxN   float64 `json:"heatmapMaxN"`
	PressureHighN float64 `json:"pressureHighN"`
	// T508 双单位（N/kPa）展示档：三字段全部同源于 device-service 写的 devices.contact_area_cm2，
	// 只在本响应内派生，不落库、不进告警与聚合（PRD 裁定四）。
	//   - ContactAreaCm2：当前绑定设备的面积；null = 未配置（前端「未配置面积，暂无法换算」)
	//   - HeatmapMaxKpa：色阶上界的 kPa 档文本值，与 HeatmapMaxN 同一次换算；不可换算为 null
	//   - 每点 kPa 见 HeatmapPoint.PressureKpa
	//
	// 换算失败一律是 null 而不是 0：0 kPa 是「读到零压力」，与「配置缺失」在稿面图上是两回事。
	ContactAreaCm2 *float64 `json:"contactAreaCm2"`
	HeatmapMaxKpa  *int     `json:"heatmapMaxKpa"`
}

// Dashboard 常量 (shared between service + integration tests)
const (
	// RankingWindowDays 趋势查询缺省天数（dashboard.go validateDays；T489 起排行/分布改随
	// period 参数取窗口，不再用这个常量，见 service/dashboard.go rankingFromDate）
	RankingWindowDays = 7
)
