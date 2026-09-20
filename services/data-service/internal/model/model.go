// Package model data-service 领域模型与 API DTO 定义
//
// 对齐：docs/ §4（设备上报接口）
//
//	docs/ / getPatientRealtime）
//	docs/ §3.5 / §4.3 / §4.7
package model

import (
	"fmt"
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
	CodeOK               = 0
	CodeInvalidParam     = 20400 // 参数非法（含 points 长度 != 20、批量 >100 帧）
	CodeBadTimestamp     = 20402 // timestamp 超出合法区间（含补传 >7 天时间窗）
	CodeDeviceIDMismatch = 20403 // X-Device-Id 头与请求体 device_id 不一致
	CodeDeviceNotFound   = 20404 // device_id 未注册
	CodeDeviceUnbound    = 20409 // 设备未绑定患者
	CodeRateLimited      = 20429 // 限流（设备按 Retry-After 退避）
	CodeQueryParam       = 30001 // 数据域：查询参数非法
	CodeForbidden        = 403   // 越权访问（水平越权 / 无数据权限）
	CodeInternal         = 90001 // 系统内部错误
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
}

// BuildSensorPoints 将 20 点已校准值转为前端 SensorPoint 数组（阈值可配置，T173）
func BuildSensorPoints(points [PointCount]float32, th PressureThresholds) []SensorPoint {
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
		}
	}
	return out
}

// HeatmapMaxN 热力图色阶上界默认值（T203 ÷10 后 6N）。
const HeatmapMaxN = 6.0

// HeatmapPoint 热力图 20 点单格（RealtimeSnapshot.pressureHeatmap 元素）
type HeatmapPoint struct {
	PointID       string  `json:"pointId"`       // P01–P20
	Row           int     `json:"row"`           // 1–4
	Col           int     `json:"col"`           // 1–5
	Label         string  `json:"label"`         // RrCc，如 R3C2
	PressureValue float64 `json:"pressureValue"` // 压力 N
	IsMax         bool    `json:"isMax"`         // 是否为当前最大点（前端 ★ + 脉冲用）
}

// BuildHeatmap 将 20 点压力值构造为 HeatmapPoint 数组（唯一 IsMax 标记）
func BuildHeatmap(points [PointCount]float32) []HeatmapPoint {
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

// SeedHeatmap 根据 patientID 生成带梯度的 20 点兜底热力图（无真实帧时用）
// 策略：按字符 hash 定最大点位置与强度，每行递增加 6N 基础 + 列 sin 波动（参考设计实时监控.html）
// heatmapMaxN 色阶上界（可配置，T173）
func SeedHeatmap(patientID string, heatmapMaxN float64) []HeatmapPoint {
	var seed uint32
	for _, r := range patientID {
		seed = seed*31 + uint32(r)
	}
	if seed == 0 {
		seed = 0x9e3779b1
	}
	var pts [PointCount]float32
	maxIdx := int(seed % PointCount)
	for i := 0; i < PointCount; i++ {
		r := i / 5
		c := i % 5
		// 基础 + 行梯度 + 列波动 + hash 扰动（10–50N）
		base := float32(12 + r*6)
		wave := float32(float64(seed>>uint((c+1)*3)&7) / 7.0 * 8) // 0–8
		loc := float32(0)
		if i == maxIdx {
			loc = 18 // 最大点额外+18N
		}
		v := base + wave + loc
		if v > float32(heatmapMaxN) {
			v = float32(heatmapMaxN) - 2
		}
		pts[i] = v
	}
	return BuildHeatmap(pts)
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
}

// ToDTO 领域实体 → 前端 DTO（阈值可配置，T173；Calibrated 由读取侧按校准结果回填）
func (r *PressureRecord) ToDTO(th PressureThresholds) PressureRecordDTO {
	return PressureRecordDTO{
		RecordID:   fmt.Sprintf("%d", r.RecordID),
		DeviceID:   r.DeviceID,
		PatientID:  r.PatientID,
		Timestamp:  r.Ts.UTC().Format(time.RFC3339),
		Points:     BuildSensorPoints(r.Points, th),
		UploadTime: r.UploadTime.UTC().Format(time.RFC3339),
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
	PressureHeatmap []HeatmapPoint      `json:"pressureHeatmap"` // 热力图 20 点（独立数据源，有 seed 兜底）
}

// Dashboard 常量 (shared between service + integration tests)
const (
	// RankingWindowDays 排行/趋势/分布固定近 7 日窗口 (dashboard.go:36, repo/integration tests)
	RankingWindowDays = 7
)
